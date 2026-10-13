package investigator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresInvestigationStore provides the M3-owned PostgreSQL adapter for the
// durable InvestigationStore contract. M2 still owns its canonical migration
// and runtime composition; storetest.Run is the adapter acceptance gate.
type PostgresInvestigationStore struct {
	pool *pgxpool.Pool
	now  func() time.Time
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

// SetUsageLimits configures the aggregate token and integer micro-USD budgets.
// Runtime composition should set these from its reviewed UTC budget window.
func (s *PostgresInvestigationStore) SetUsageLimits(ctx context.Context, limits UsageAllowance) error {
	if limits.InputTokens < 0 || limits.OutputTokens < 0 || limits.CostMicrousd < 0 {
		return ErrUsageBudget
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO investigation_usage_limits (id, input_limit, output_limit, cost_limit_microusd)
		VALUES (1, $1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET
			input_limit = EXCLUDED.input_limit,
			output_limit = EXCLUDED.output_limit,
			cost_limit_microusd = EXCLUDED.cost_limit_microusd`,
		limits.InputTokens, limits.OutputTokens, limits.CostMicrousd)
	if err != nil {
		return fmt.Errorf("set investigation usage limits: %w", err)
	}
	return nil
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
	if job.ProviderCallPending && !current.ProviderCallPending {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(736292)`); err != nil {
			return fmt.Errorf("acquire provider-call admission lock: %w", err)
		}
		var anotherPending bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM investigation_jobs
				WHERE id <> $1 AND provider_call_pending
			)`, job.ID).Scan(&anotherPending); err != nil {
			return fmt.Errorf("check active provider call: %w", err)
		}
		if anotherPending {
			return ErrProviderCallLimit
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
		SET status = $1, lease_owner = $2, lease_epoch = $3, lease_expires_at = $4,
			completed_at = $5, provider_call_pending = $6, job_payload = $7
		WHERE id = $8`, job.Status, job.LeaseOwner, job.LeaseEpoch, job.LeaseExpiresAt, completedAt, job.ProviderCallPending, payload, job.ID)
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

func (s *PostgresInvestigationStore) ReserveUsage(ctx context.Context, job Job, allowance UsageAllowance) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if allowance.InputTokens <= 0 || allowance.OutputTokens <= 0 || allowance.CostMicrousd < 0 {
		return "", ErrUsageBudget
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin reserve usage tx: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	// Lock the budget limit row to serialize budget reservations
	var inputLimit, outputLimit, costLimitMicrousd int64
	err = tx.QueryRow(ctx, `
		SELECT input_limit, output_limit, cost_limit_microusd
		FROM investigation_usage_limits
		WHERE id = 1
		FOR UPDATE`).Scan(&inputLimit, &outputLimit, &costLimitMicrousd)
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
	var existingIn, existingOut, existingCostMicrousd int64
	err = tx.QueryRow(ctx, `
		SELECT id, reserved_input, reserved_output, reserved_cost_microusd, status
		FROM investigation_usage_reservations
		WHERE job_id = $1`, job.ID).Scan(&existingID, &existingIn, &existingOut, &existingCostMicrousd, &existingStatus)
	if err == nil {
		if existingIn == allowance.InputTokens && existingOut == allowance.OutputTokens && existingCostMicrousd == allowance.CostMicrousd && existingStatus == usageReserved {
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
	var consumedIn, consumedOut, consumedCostMicrousd int64
	err = tx.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN status IN ('reserved', 'uncertain') THEN reserved_input ELSE actual_input END), 0),
			COALESCE(SUM(CASE WHEN status IN ('reserved', 'uncertain') THEN reserved_output ELSE actual_output END), 0),
			COALESCE(SUM(CASE WHEN status IN ('reserved', 'uncertain') THEN reserved_cost_microusd ELSE actual_cost_microusd END), 0)
		FROM investigation_usage_reservations`).Scan(&consumedIn, &consumedOut, &consumedCostMicrousd)
	if err != nil {
		return "", fmt.Errorf("calculate consumed usage: %w", err)
	}

	if consumedIn+allowance.InputTokens > inputLimit || consumedOut+allowance.OutputTokens > outputLimit || (allowance.CostMicrousd > 0 && (costLimitMicrousd == 0 || consumedCostMicrousd+allowance.CostMicrousd > costLimitMicrousd)) {
		return "", ErrUsageBudget
	}

	id := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO investigation_usage_reservations (
			id, job_id, reserved_input, reserved_output, reserved_cost_microusd,
			actual_input, actual_output, actual_cost_microusd, status
		) VALUES ($1, $2, $3, $4, $5, NULL, NULL, NULL, 'reserved')`,
		id, job.ID, allowance.InputTokens, allowance.OutputTokens, allowance.CostMicrousd)
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
	var budgetID int
	err = tx.QueryRow(ctx, `
		SELECT id
		FROM investigation_usage_limits
		WHERE id = 1
		FOR UPDATE`).Scan(&budgetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUsageBudget
	} else if err != nil {
		return fmt.Errorf("lock usage limits for completion: %w", err)
	}

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
	var resInput, resOutput, resCostMicrousd int64
	err = tx.QueryRow(ctx, `
		SELECT id, job_id, reserved_input, reserved_output, reserved_cost_microusd, status
		FROM investigation_usage_reservations
		WHERE id = $1
		FOR UPDATE`, job.UsageReservationID).Scan(&resID, &resJobID, &resInput, &resOutput, &resCostMicrousd, &resStatus)
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

	actualInput, actualOutput, actualCostMicrousd := resInput, resOutput, resCostMicrousd
	if job.Usage.InputTokens != nil {
		actualInput = *job.Usage.InputTokens
	}
	if job.Usage.OutputTokens != nil {
		actualOutput = *job.Usage.OutputTokens
	}
	if job.Usage.CostMicrousd != nil {
		actualCostMicrousd = *job.Usage.CostMicrousd
	}
	if actualInput < 0 || actualOutput < 0 || actualCostMicrousd < 0 || actualInput > resInput || actualOutput > resOutput || actualCostMicrousd > resCostMicrousd {
		return ErrUsageBudget
	}

	_, err = tx.Exec(ctx, `
		UPDATE investigation_usage_reservations
		SET status = 'completed', actual_input = $1, actual_output = $2,
			actual_cost_microusd = $3, updated_at = NOW()
		WHERE id = $4`, actualInput, actualOutput, actualCostMicrousd, resID)
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
		SET status = $1, completed_at = $2, provider_call_pending = FALSE, job_payload = $3
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

var _ InvestigationStore = (*PostgresInvestigationStore)(nil)
