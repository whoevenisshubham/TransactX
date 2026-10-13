package investigator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresInvestigationStore provides the durable PostgreSQL implementation of
// InvestigationStore, fulfilling the M3 C6 closeout gate and passing storetest.Run.
type PostgresInvestigationStore struct {
	pool        *pgxpool.Pool
	now         func() time.Time
	mu          sync.Mutex
	inputLimit  int64
	outputLimit int64
}

// NewPostgresInvestigationStore creates an InvestigationStore backed by PostgreSQL.
func NewPostgresInvestigationStore(pool *pgxpool.Pool, now func() time.Time) (*PostgresInvestigationStore, error) {
	if pool == nil {
		return nil, errors.New("investigation PostgreSQL pool is required")
	}
	if now == nil {
		now = time.Now
	}
	return &PostgresInvestigationStore{
		pool: pool,
		now:  now,
	}, nil
}

// SetUsageLimits configures the aggregate input and output token budgets.
func (s *PostgresInvestigationStore) SetUsageLimits(inputLimit, outputLimit int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inputLimit = inputLimit
	s.outputLimit = outputLimit
	_, _ = s.pool.Exec(context.Background(), `
		INSERT INTO investigation_usage_limits (id, input_limit, output_limit)
		VALUES (1, $1, $2)
		ON CONFLICT (id) DO UPDATE SET input_limit = EXCLUDED.input_limit, output_limit = EXCLUDED.output_limit`,
		inputLimit, outputLimit)
}

func (s *PostgresInvestigationStore) Create(ctx context.Context, request NewJob) (Job, bool, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, false, err
	}
	if err := validateTrustedScope(request.Scope); err != nil || request.PaymentID != request.Scope.SelectedPaymentID || request.IdempotencyKey == "" || request.RequestHash == "" || request.Mode == "" {
		return Job{}, false, ErrScopeViolation
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Job{}, false, fmt.Errorf("begin create tx: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	// Serialize creation to ensure atomic active count evaluation and idempotency
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(736291)`); err != nil {
		return Job{}, false, fmt.Errorf("acquire create lock: %w", err)
	}

	// Check idempotency for (actor_id, idempotency_key)
	var existingPayload []byte
	var existingReqHash string
	err = tx.QueryRow(ctx, `
		SELECT request_hash, job_payload
		FROM investigation_jobs
		WHERE actor_id = $1 AND idempotency_key = $2`, request.Scope.ActorID, request.IdempotencyKey).Scan(&existingReqHash, &existingPayload)
	if err == nil {
		if existingReqHash != request.RequestHash {
			return Job{}, false, ErrIdempotencyConflict
		}
		var existingJob Job
		if err := json.Unmarshal(existingPayload, &existingJob); err != nil {
			return Job{}, false, fmt.Errorf("unmarshal existing job: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return Job{}, false, err
		}
		return existingJob, false, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Job{}, false, fmt.Errorf("query existing job: %w", err)
	}

	// Active job limits: max 1 per actor, max 2 globally
	var actorActive, globalActive int
	err = tx.QueryRow(ctx, `
		SELECT
			COALESCE(count(*) FILTER (WHERE actor_id = $1), 0),
			COALESCE(count(*), 0)
		FROM investigation_jobs
		WHERE status IN ('queued', 'running')`, request.Scope.ActorID).Scan(&actorActive, &globalActive)
	if err != nil {
		return Job{}, false, fmt.Errorf("check active limits: %w", err)
	}
	if actorActive >= 1 {
		return Job{}, false, ErrActorLimitExceeded
	}
	if globalActive >= 2 {
		return Job{}, false, ErrGlobalLimitExceeded
	}

	job := Job{
		ID:             uuid.NewString(),
		ActorID:        request.Scope.ActorID,
		Role:           request.Scope.Role,
		PaymentID:      request.PaymentID,
		Question:       request.Question,
		IdempotencyKey: request.IdempotencyKey,
		RequestHash:    request.RequestHash,
		Status:         StatusQueued,
		Mode:           request.Mode,
		DeadlineAt:     request.DeadlineAt,
		CreatedAt:      request.CreatedAt,
	}
	payload, err := json.Marshal(job)
	if err != nil {
		return Job{}, false, fmt.Errorf("marshal job: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO investigation_jobs (
			id, actor_id, role, payment_id, idempotency_key, request_hash,
			status, mode, question, lease_owner, lease_epoch, lease_expires_at,
			deadline_at, created_at, completed_at, job_payload
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, NULL, 0, NULL,
			$10, $11, NULL, $12
		)`, job.ID, job.ActorID, job.Role, job.PaymentID, job.IdempotencyKey, job.RequestHash,
		job.Status, job.Mode, job.Question, job.DeadlineAt, job.CreatedAt, payload)
	if err != nil {
		return Job{}, false, fmt.Errorf("insert job: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Job{}, false, fmt.Errorf("commit job: %w", err)
	}
	return job, true, nil
}

func (s *PostgresInvestigationStore) Claim(ctx context.Context, jobID, workerID string, leaseDuration time.Duration) (Job, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	if workerID == "" || leaseDuration <= 0 {
		return Job{}, ErrJobNotClaimable
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Job{}, fmt.Errorf("begin claim tx: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var status string
	var leaseExpiresAt sql.NullTime
	var payload []byte
	err = tx.QueryRow(ctx, `
		SELECT status, lease_expires_at, job_payload
		FROM investigation_jobs
		WHERE id = $1
		FOR UPDATE`, jobID).Scan(&status, &leaseExpiresAt, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrJobNotFound
	} else if err != nil {
		return Job{}, fmt.Errorf("query claim job: %w", err)
	}

	now := s.now().UTC()
	isQueued := status == StatusQueued
	isRunningExpired := status == StatusRunning && (!leaseExpiresAt.Valid || !leaseExpiresAt.Time.After(now))
	if !isQueued && !isRunningExpired {
		return Job{}, ErrJobNotClaimable
	}

	var job Job
	if err := json.Unmarshal(payload, &job); err != nil {
		return Job{}, fmt.Errorf("unmarshal job: %w", err)
	}

	job.Status = StatusRunning
	job.LeaseOwner = workerID
	job.LeaseEpoch++
	job.LeaseExpiresAt = now.Add(leaseDuration)

	updatedPayload, err := json.Marshal(job)
	if err != nil {
		return Job{}, fmt.Errorf("marshal job: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE investigation_jobs
		SET status = $1, lease_owner = $2, lease_epoch = $3, lease_expires_at = $4, job_payload = $5
		WHERE id = $6`, job.Status, job.LeaseOwner, job.LeaseEpoch, job.LeaseExpiresAt, updatedPayload, job.ID)
	if err != nil {
		return Job{}, fmt.Errorf("update claim job: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Job{}, fmt.Errorf("commit claim tx: %w", err)
	}
	return job, nil
}

func (s *PostgresInvestigationStore) SaveCheckpoint(ctx context.Context, job Job) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin checkpoint tx: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var currentPayload []byte
	err = tx.QueryRow(ctx, `
		SELECT job_payload
		FROM investigation_jobs
		WHERE id = $1
		FOR UPDATE`, job.ID).Scan(&currentPayload)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrJobNotFound
	} else if err != nil {
		return fmt.Errorf("query checkpoint job: %w", err)
	}

	var current Job
	if err := json.Unmarshal(currentPayload, &current); err != nil {
		return fmt.Errorf("unmarshal current job: %w", err)
	}

	if current.LeaseOwner != job.LeaseOwner || current.LeaseEpoch != job.LeaseEpoch || job.LeaseOwner == "" {
		return ErrLeaseLost
	}
	if current.ActorID != job.ActorID || current.Role != job.Role || current.PaymentID != job.PaymentID || current.Question != job.Question || current.RequestHash != job.RequestHash {
		return ErrScopeViolation
	}
	if current.UsageReservationID != job.UsageReservationID {
		if current.UsageReservationID != "" || job.UsageReservationID == "" {
			return ErrScopeViolation
		}
		var resJobID string
		err := tx.QueryRow(ctx, `SELECT job_id FROM investigation_usage_reservations WHERE id = $1`, job.UsageReservationID).Scan(&resJobID)
		if err != nil || resJobID != job.ID {
			return ErrScopeViolation
		}
	}
	if job.Status != StatusRunning && job.Status != StatusCompleted && job.Status != StatusFailed && job.Status != StatusCancelled {
		return errors.New("invalid investigation job status")
	}
	if job.Status == StatusCompleted && job.Report == nil {
		return errors.New("completed investigation has no report")
	}

	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal checkpoint job: %w", err)
	}

	var completedAt *time.Time
	if !job.CompletedAt.IsZero() {
		completedAt = &job.CompletedAt
	}

	_, err = tx.Exec(ctx, `
		UPDATE investigation_jobs
		SET status = $1, lease_owner = $2, lease_epoch = $3, lease_expires_at = $4, completed_at = $5, job_payload = $6
		WHERE id = $7`, job.Status, job.LeaseOwner, job.LeaseEpoch, job.LeaseExpiresAt, completedAt, payload, job.ID)
	if err != nil {
		return fmt.Errorf("update checkpoint job: %w", err)
	}

	return tx.Commit(ctx)
}

func (s *PostgresInvestigationStore) Get(ctx context.Context, scope TrustedScope, jobID string) (Job, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	if err := validateTrustedScope(scope); err != nil {
		return Job{}, ErrJobNotFound
	}

	var payload []byte
	err := s.pool.QueryRow(ctx, `
		SELECT job_payload
		FROM investigation_jobs
		WHERE id = $1 AND actor_id = $2 AND payment_id = $3`,
		jobID, scope.ActorID, scope.SelectedPaymentID).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrJobNotFound
	} else if err != nil {
		return Job{}, fmt.Errorf("query get job: %w", err)
	}

	var job Job
	if err := json.Unmarshal(payload, &job); err != nil {
		return Job{}, fmt.Errorf("unmarshal get job: %w", err)
	}
	return job, nil
}

func (s *PostgresInvestigationStore) ReserveUsage(ctx context.Context, job Job, input, output int64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if input < 0 || output < 0 || (input == 0 && output == 0) {
		return "", ErrUsageBudget
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin reserve usage tx: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	// Lock the budget limit row to serialize budget reservations
	var inputLimit, outputLimit int64
	err = tx.QueryRow(ctx, `
		SELECT input_limit, output_limit
		FROM investigation_usage_limits
		WHERE id = 1
		FOR UPDATE`).Scan(&inputLimit, &outputLimit)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrUsageBudget
	} else if err != nil {
		return "", fmt.Errorf("query usage limits: %w", err)
	}
	if inputLimit == 0 || outputLimit == 0 {
		return "", ErrUsageBudget
	}

	// Verify job exists and lease is valid
	var currentPayload []byte
	err = tx.QueryRow(ctx, `
		SELECT job_payload
		FROM investigation_jobs
		WHERE id = $1
		FOR UPDATE`, job.ID).Scan(&currentPayload)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrJobNotFound
	} else if err != nil {
		return "", fmt.Errorf("query job for reservation: %w", err)
	}

	var current Job
	if err := json.Unmarshal(currentPayload, &current); err != nil {
		return "", fmt.Errorf("unmarshal job for reservation: %w", err)
	}
	if err := validateUsageLease(current, job); err != nil {
		return "", err
	}

	// Check if this job already has a reservation
	var existingID, existingStatus string
	var existingIn, existingOut int64
	err = tx.QueryRow(ctx, `
		SELECT id, reserved_input, reserved_output, status
		FROM investigation_usage_reservations
		WHERE job_id = $1`, job.ID).Scan(&existingID, &existingIn, &existingOut, &existingStatus)
	if err == nil {
		if existingIn == input && existingOut == output && existingStatus == usageReserved {
			if err := tx.Commit(ctx); err != nil {
				return "", err
			}
			return existingID, nil
		}
		return "", ErrUsageBudget
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("check existing reservation: %w", err)
	}

	// Calculate currently committed/reserved budget
	var consumedIn, consumedOut int64
	err = tx.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN status IN ('reserved', 'uncertain') THEN reserved_input ELSE actual_input END), 0),
			COALESCE(SUM(CASE WHEN status IN ('reserved', 'uncertain') THEN reserved_output ELSE actual_output END), 0)
		FROM investigation_usage_reservations`).Scan(&consumedIn, &consumedOut)
	if err != nil {
		return "", fmt.Errorf("calculate consumed usage: %w", err)
	}

	if consumedIn+input > inputLimit || consumedOut+output > outputLimit {
		return "", ErrUsageBudget
	}

	id := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO investigation_usage_reservations (
			id, job_id, reserved_input, reserved_output, actual_input, actual_output, status
		) VALUES ($1, $2, $3, $4, NULL, NULL, 'reserved')`,
		id, job.ID, input, output)
	if err != nil {
		return "", fmt.Errorf("insert reservation: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit reservation: %w", err)
	}
	return id, nil
}

func (s *PostgresInvestigationStore) CompleteUsage(ctx context.Context, job Job) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin complete usage tx: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	// Lock the budget limit row to serialize with other usage updates
	var inputLimit, outputLimit int64
	_ = tx.QueryRow(ctx, `
		SELECT input_limit, output_limit
		FROM investigation_usage_limits
		WHERE id = 1
		FOR UPDATE`).Scan(&inputLimit, &outputLimit)

	// Get current job
	var currentPayload []byte
	err = tx.QueryRow(ctx, `
		SELECT job_payload
		FROM investigation_jobs
		WHERE id = $1
		FOR UPDATE`, job.ID).Scan(&currentPayload)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrJobNotFound
	} else if err != nil {
		return fmt.Errorf("query job for complete usage: %w", err)
	}

	var current Job
	if err := json.Unmarshal(currentPayload, &current); err != nil {
		return fmt.Errorf("unmarshal current job for complete usage: %w", err)
	}

	// Check reservation
	var resID, resJobID, resStatus string
	var resInput, resOutput int64
	err = tx.QueryRow(ctx, `
		SELECT id, job_id, reserved_input, reserved_output, status
		FROM investigation_usage_reservations
		WHERE id = $1
		FOR UPDATE`, job.UsageReservationID).Scan(&resID, &resJobID, &resInput, &resOutput, &resStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUsageBudget
	} else if err != nil {
		return fmt.Errorf("query reservation for complete usage: %w", err)
	}

	if resStatus == usageCompleted {
		if resJobID != job.ID || current.Status != StatusCompleted || current.UsageReservationID != job.UsageReservationID || current.ActorID != job.ActorID || current.Role != job.Role || current.PaymentID != job.PaymentID || current.RequestHash != job.RequestHash {
			return ErrUsageBudget
		}
		return tx.Commit(ctx)
	}

	if err := validateUsageLease(current, job); err != nil {
		return err
	}

	if resJobID != job.ID || resStatus != usageReserved || job.Status != StatusCompleted || job.Report == nil {
		return ErrUsageBudget
	}

	actualInput, actualOutput := resInput, resOutput
	if job.Usage.InputTokens != nil {
		actualInput = *job.Usage.InputTokens
	}
	if job.Usage.OutputTokens != nil {
		actualOutput = *job.Usage.OutputTokens
	}
	if actualInput < 0 || actualOutput < 0 || actualInput > resInput || actualOutput > resOutput {
		return ErrUsageBudget
	}

	_, err = tx.Exec(ctx, `
		UPDATE investigation_usage_reservations
		SET status = 'completed', actual_input = $1, actual_output = $2, updated_at = NOW()
		WHERE id = $3`, actualInput, actualOutput, resID)
	if err != nil {
		return fmt.Errorf("update reservation completed: %w", err)
	}

	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal completed job: %w", err)
	}

	var completedAt *time.Time
	if !job.CompletedAt.IsZero() {
		completedAt = &job.CompletedAt
	}

	_, err = tx.Exec(ctx, `
		UPDATE investigation_jobs
		SET status = $1, completed_at = $2, job_payload = $3
		WHERE id = $4`, job.Status, completedAt, payload, job.ID)
	if err != nil {
		return fmt.Errorf("update completed job: %w", err)
	}

	return tx.Commit(ctx)
}

func (s *PostgresInvestigationStore) MarkUsageUncertain(ctx context.Context, job Job) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin uncertain usage tx: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var currentPayload []byte
	err = tx.QueryRow(ctx, `
		SELECT job_payload
		FROM investigation_jobs
		WHERE id = $1
		FOR UPDATE`, job.ID).Scan(&currentPayload)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrJobNotFound
	} else if err != nil {
		return fmt.Errorf("query job for uncertain usage: %w", err)
	}

	var current Job
	if err := json.Unmarshal(currentPayload, &current); err != nil {
		return fmt.Errorf("unmarshal job for uncertain usage: %w", err)
	}

	if err := validateUsageLease(current, job); err != nil {
		return err
	}

	var resID, resJobID, resStatus string
	err = tx.QueryRow(ctx, `
		SELECT id, job_id, status
		FROM investigation_usage_reservations
		WHERE id = $1
		FOR UPDATE`, job.UsageReservationID).Scan(&resID, &resJobID, &resStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUsageBudget
	} else if err != nil {
		return fmt.Errorf("query reservation for uncertain usage: %w", err)
	}

	if resJobID != job.ID || resStatus == usageCompleted {
		return ErrUsageBudget
	}
	if resStatus == usageUncertain {
		return tx.Commit(ctx)
	}

	_, err = tx.Exec(ctx, `
		UPDATE investigation_usage_reservations
		SET status = 'uncertain', updated_at = NOW()
		WHERE id = $1`, resID)
	if err != nil {
		return fmt.Errorf("update reservation uncertain: %w", err)
	}

	return tx.Commit(ctx)
}

func (s *PostgresInvestigationStore) EnsureSchema(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, PostgresInvestigationStoreDDL); err != nil {
		return fmt.Errorf("execute store DDL: %w", err)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO investigation_usage_limits (id, input_limit, output_limit)
		VALUES (1, $1, $2)
		ON CONFLICT (id) DO NOTHING`, s.inputLimit, s.outputLimit)
	if err != nil {
		return fmt.Errorf("seed usage limits: %w", err)
	}
	return nil
}

// PostgresInvestigationStoreDDL defines the durable schema required for
// investigation jobs, leases, checkpoints, usage reservations, and limits.
const PostgresInvestigationStoreDDL = `
CREATE TABLE IF NOT EXISTS investigation_jobs (
    id TEXT PRIMARY KEY,
    actor_id TEXT NOT NULL,
    role TEXT NOT NULL,
    payment_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'completed', 'failed', 'cancelled')),
    mode TEXT NOT NULL,
    question TEXT NOT NULL,
    lease_owner TEXT,
    lease_epoch BIGINT NOT NULL DEFAULT 0,
    lease_expires_at TIMESTAMPTZ,
    deadline_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    job_payload JSONB NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_investigation_jobs_actor_idemp ON investigation_jobs (actor_id, idempotency_key);
CREATE INDEX IF NOT EXISTS idx_investigation_jobs_active ON investigation_jobs (status) WHERE status IN ('queued', 'running');

CREATE TABLE IF NOT EXISTS investigation_usage_reservations (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL REFERENCES investigation_jobs(id) ON DELETE CASCADE,
    reserved_input BIGINT NOT NULL,
    reserved_output BIGINT NOT NULL,
    actual_input BIGINT,
    actual_output BIGINT,
    status TEXT NOT NULL CHECK (status IN ('reserved', 'completed', 'uncertain')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_investigation_usage_job ON investigation_usage_reservations (job_id);

CREATE TABLE IF NOT EXISTS investigation_usage_limits (
    id INT PRIMARY KEY DEFAULT 1,
    input_limit BIGINT NOT NULL,
    output_limit BIGINT NOT NULL
);`

var _ InvestigationStore = (*PostgresInvestigationStore)(nil)
