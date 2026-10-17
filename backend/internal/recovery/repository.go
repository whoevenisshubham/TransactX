package recovery

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// DiscoverStalePayments identifies old non-terminal payments without active jobs
// and schedules them. Never un-parks a needs_attention job.
func (r *Repository) DiscoverStalePayments(ctx context.Context, staleAge time.Duration, limit int) ([]uuid.UUID, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `
		SELECT p.id
		FROM payments p
		LEFT JOIN recovery_jobs rj ON p.id = rj.payment_id
		WHERE p.state NOT IN ('COMPLETED', 'FAILED', 'REVERSED')
		  AND p.created_at <= NOW() - ($1 * INTERVAL '1 millisecond')
		  AND (rj.payment_id IS NULL OR (rj.status = 'resolved' AND p.state NOT IN ('COMPLETED', 'FAILED', 'REVERSED')))
		ORDER BY p.created_at ASC
		LIMIT $2`

	rows, err := r.pool.Query(ctx, query, staleAge.Milliseconds(), limit)
	if err != nil {
		return nil, fmt.Errorf("discover stale payments query: %w", err)
	}
	defer rows.Close()

	var discovered []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan discovered payment id: %w", err)
		}
		discovered = append(discovered, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Upsert scheduled jobs for discovered payments
	for _, paymentID := range discovered {
		upsertQuery := `
			INSERT INTO recovery_jobs (payment_id, status, due_at, lease_epoch, attempt_count, created_at, updated_at)
			VALUES ($1, 'scheduled', NOW(), 0, 0, NOW(), NOW())
			ON CONFLICT (payment_id) DO UPDATE
			SET status = 'scheduled',
			    due_at = NOW(),
			    lease_owner = NULL,
			    lease_until = NULL,
			    updated_at = NOW()
			WHERE recovery_jobs.status = 'resolved'`

		if _, err := r.pool.Exec(ctx, upsertQuery, paymentID); err != nil {
			return nil, fmt.Errorf("upsert discovered recovery job %s: %w", paymentID, err)
		}
	}

	return discovered, nil
}

// UpsertJob schedules or reschedules a job for a payment.
func (r *Repository) UpsertJob(ctx context.Context, paymentID uuid.UUID, dueAt time.Time) error {
	query := `
		INSERT INTO recovery_jobs (payment_id, status, due_at, lease_epoch, attempt_count, created_at, updated_at)
		VALUES ($1, 'scheduled', $2, 0, 0, NOW(), NOW())
		ON CONFLICT (payment_id) DO UPDATE
		SET status = 'scheduled',
		    due_at = EXCLUDED.due_at,
		    lease_owner = NULL,
		    lease_until = NULL,
		    updated_at = NOW()
		WHERE recovery_jobs.status = 'resolved'`

	_, err := r.pool.Exec(ctx, query, paymentID, dueAt)
	return err
}

// ClaimDueJobs claims up to maxJobs due jobs using FOR UPDATE SKIP LOCKED
// in a short transaction, increments the lease epoch, sets lease owner/until,
// records an attempt row, and commits.
func (r *Repository) ClaimDueJobs(ctx context.Context, workerID string, maxJobs int, leaseDuration time.Duration) ([]*Job, error) {
	if maxJobs <= 0 {
		return nil, nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin claim transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	selectQuery := `
		SELECT payment_id, status, due_at, lease_owner, lease_until, lease_epoch, attempt_count, last_reason_code, last_attempt_id, created_at, updated_at
		FROM recovery_jobs
		WHERE ((status IN ('scheduled', 'deferred') AND (lease_until IS NULL OR lease_until < NOW()))
		    OR (status = 'running' AND lease_until < NOW()))
		  AND due_at <= NOW()
		ORDER BY due_at ASC
		LIMIT $1
		FOR UPDATE SKIP LOCKED`

	rows, err := tx.Query(ctx, selectQuery, maxJobs)
	if err != nil {
		return nil, fmt.Errorf("query due jobs: %w", err)
	}

	var jobs []*Job
	for rows.Next() {
		var j Job
		if err := rows.Scan(
			&j.PaymentID,
			&j.Status,
			&j.DueAt,
			&j.LeaseOwner,
			&j.LeaseUntil,
			&j.LeaseEpoch,
			&j.AttemptCount,
			&j.LastReasonCode,
			&j.LastAttemptID,
			&j.CreatedAt,
			&j.UpdatedAt,
		); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan due job: %w", err)
		}
		jobs = append(jobs, &j)
	}
	rows.Close()

	if len(jobs) == 0 {
		return nil, nil
	}

	for _, j := range jobs {
		// Read current payment state for the attempt row
		var currentState string
		err := tx.QueryRow(ctx, "SELECT state FROM payments WHERE id = $1", j.PaymentID).Scan(&currentState)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				currentState = "UNKNOWN"
			} else {
				return nil, fmt.Errorf("read payment state before attempt: %w", err)
			}
		}

		attemptID := uuid.New()
		newEpoch := j.LeaseEpoch + 1
		newAttemptCount := j.AttemptCount + 1

		updateJobQuery := `
			UPDATE recovery_jobs
			SET status = 'running',
			    lease_owner = $1,
			    lease_until = NOW() + ($2 * INTERVAL '1 millisecond'),
			    lease_epoch = $3,
			    attempt_count = $4,
			    last_attempt_id = $5,
			    updated_at = NOW()
			WHERE payment_id = $6
			RETURNING lease_until`

		var dbLeaseUntil time.Time
		if err := tx.QueryRow(ctx, updateJobQuery, workerID, leaseDuration.Milliseconds(), newEpoch, newAttemptCount, attemptID, j.PaymentID).Scan(&dbLeaseUntil); err != nil {
			return nil, fmt.Errorf("update claimed job %s: %w", j.PaymentID, err)
		}

		insertAttemptQuery := `
			INSERT INTO recovery_attempts (id, payment_id, lease_epoch, worker_identity, started_at, state_before)
			VALUES ($1, $2, $3, $4, NOW(), $5)`

		if _, err := tx.Exec(ctx, insertAttemptQuery, attemptID, j.PaymentID, newEpoch, workerID, currentState); err != nil {
			return nil, fmt.Errorf("insert recovery attempt %s: %w", attemptID, err)
		}

		// Update in-memory job fields to reflect the active claim
		j.Status = JobStatusRunning
		j.LeaseOwner = &workerID
		j.LeaseUntil = &dbLeaseUntil
		j.LeaseEpoch = newEpoch
		j.AttemptCount = newAttemptCount
		j.LastAttemptID = &attemptID
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit claimed jobs: %w", err)
	}

	return jobs, nil
}

// HeartbeatLease extends lease_until if the caller still holds the active lease.
func (r *Repository) HeartbeatLease(ctx context.Context, paymentID uuid.UUID, workerID string, leaseEpoch int64, leaseDuration time.Duration) (bool, error) {
	query := `
		UPDATE recovery_jobs
		SET lease_until = NOW() + ($4 * INTERVAL '1 millisecond'),
		    updated_at = NOW()
		WHERE payment_id = $1
		  AND lease_owner = $2
		  AND lease_epoch = $3
		  AND lease_until >= NOW()`

	tag, err := r.pool.Exec(ctx, query, paymentID, workerID, leaseEpoch, leaseDuration.Milliseconds())
	if err != nil {
		return false, fmt.Errorf("heartbeat lease: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// CompleteAttempt conditionally completes an attempt and updates the job.
// Returns false if the lease was lost (epoch changed, owner changed, or lease expired).
func (r *Repository) CompleteAttempt(ctx context.Context, req CompleteAttemptRequest) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin complete attempt tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Update attempt row first
	updateAttemptQuery := `
		UPDATE recovery_attempts
		SET finished_at = NOW(),
		    state_after = $4,
		    result = $5,
		    reason_code = $6,
		    operation_ids = $7,
		    trace_id = $8
		WHERE id = $1 AND payment_id = $2 AND lease_epoch = $3`

	var traceID *string
	if req.TraceID != "" {
		traceID = &req.TraceID
	}

	if _, err := tx.Exec(ctx, updateAttemptQuery,
		req.AttemptID,
		req.PaymentID,
		req.LeaseEpoch,
		req.StateAfter,
		req.Result,
		req.ReasonCode,
		req.OperationIDs,
		traceID,
	); err != nil {
		return false, fmt.Errorf("update attempt: %w", err)
	}

	// Conditionally update recovery_jobs requiring valid lease predicate
	updateJobQuery := `
		UPDATE recovery_jobs
		SET status = $4,
		    due_at = $5,
		    lease_owner = NULL,
		    lease_until = NULL,
		    last_reason_code = $6,
		    updated_at = NOW()
		WHERE payment_id = $1
		  AND lease_owner = $2
		  AND lease_epoch = $3
		  AND lease_until >= NOW()`

	tag, err := tx.Exec(ctx, updateJobQuery,
		req.PaymentID,
		req.WorkerID,
		req.LeaseEpoch,
		string(req.NewStatus),
		req.DueAt,
		req.ReasonCode,
	)
	if err != nil {
		return false, fmt.Errorf("update job status: %w", err)
	}

	if tag.RowsAffected() == 0 {
		// Claim was lost! Roll back so we don't commit inconsistent state
		return false, nil
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit complete attempt: %w", err)
	}

	return true, nil
}

// SweepInterruptedAttempts marks expired running leases as INTERRUPTED.
func (r *Repository) SweepInterruptedAttempts(ctx context.Context, limit int, backoff time.Duration) (int, error) {
	if limit <= 0 {
		limit = 50
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin sweep tx: %w", err)
	}
	defer tx.Rollback(ctx)

	query := `
		SELECT payment_id, lease_epoch, last_attempt_id, attempt_count
		FROM recovery_jobs
		WHERE status = 'running'
		  AND lease_until < NOW()
		LIMIT $1
		FOR UPDATE SKIP LOCKED`

	rows, err := tx.Query(ctx, query, limit)
	if err != nil {
		return 0, fmt.Errorf("query expired jobs: %w", err)
	}

	type expiredJob struct {
		paymentID     uuid.UUID
		leaseEpoch    int64
		lastAttemptID *uuid.UUID
		attemptCount  int
	}

	var expired []expiredJob
	for rows.Next() {
		var e expiredJob
		if err := rows.Scan(&e.paymentID, &e.leaseEpoch, &e.lastAttemptID, &e.attemptCount); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan expired job: %w", err)
		}
		expired = append(expired, e)
	}
	rows.Close()

	swept := 0
	for _, e := range expired {
		if e.lastAttemptID != nil {
			_, _ = tx.Exec(ctx, `
				UPDATE recovery_attempts
				SET finished_at = NOW(),
				    result = 'INTERRUPTED',
				    reason_code = 'LEASE_EXPIRED'
				WHERE id = $1 AND finished_at IS NULL`, *e.lastAttemptID)
		}

		newStatus := JobStatusDeferred
		reason := ReasonLeaseExpired
		dueAt := time.Now().UTC().Add(backoff)

		if e.attemptCount >= DefaultMaxAttempts {
			newStatus = JobStatusNeedsAttention
			reason = ReasonAttemptsExhausted
			dueAt = time.Now().UTC()
		}

		_, err := tx.Exec(ctx, `
			UPDATE recovery_jobs
			SET status = $1,
			    due_at = $2,
			    lease_owner = NULL,
			    lease_until = NULL,
			    last_reason_code = $3,
			    updated_at = NOW()
			WHERE payment_id = $4`,
			string(newStatus), dueAt, reason, e.paymentID)
		if err != nil {
			return 0, fmt.Errorf("update swept job %s: %w", e.paymentID, err)
		}
		swept++
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit sweep tx: %w", err)
	}

	return swept, nil
}

// RecordWorkerHeartbeat registers/updates worker heartbeat evidence.
func (r *Repository) RecordWorkerHeartbeat(ctx context.Context, hb *WorkerHeartbeat) error {
	query := `
		INSERT INTO worker_heartbeats (worker_id, worker_label, started_at, last_seen_at, job_families, build_sha)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (worker_id) DO UPDATE
		SET last_seen_at = EXCLUDED.last_seen_at,
		    job_families = EXCLUDED.job_families,
		    build_sha = EXCLUDED.build_sha`

	_, err := r.pool.Exec(ctx, query,
		hb.WorkerID,
		hb.WorkerLabel,
		hb.StartedAt,
		hb.LastSeenAt,
		hb.JobFamilies,
		hb.BuildSHA,
	)
	return err
}

// ScheduleManualRecheck schedules a manual recheck for a payment.
func (r *Repository) ScheduleManualRecheck(ctx context.Context, paymentID uuid.UUID) error {
	query := `
		INSERT INTO recovery_jobs (payment_id, status, due_at, lease_epoch, attempt_count, created_at, updated_at)
		VALUES ($1, 'scheduled', NOW(), 0, 0, NOW(), NOW())
		ON CONFLICT (payment_id) DO UPDATE
		SET status = 'scheduled',
		    due_at = NOW(),
		    lease_owner = NULL,
		    lease_until = NULL,
		    updated_at = NOW()`

	_, err := r.pool.Exec(ctx, query, paymentID)
	return err
}

// GetJob reads the job for a payment.
func (r *Repository) GetJob(ctx context.Context, paymentID uuid.UUID) (*Job, error) {
	query := `
		SELECT payment_id, status, due_at, lease_owner, lease_until, lease_epoch, attempt_count, last_reason_code, last_attempt_id, created_at, updated_at
		FROM recovery_jobs
		WHERE payment_id = $1`

	var j Job
	err := r.pool.QueryRow(ctx, query, paymentID).Scan(
		&j.PaymentID,
		&j.Status,
		&j.DueAt,
		&j.LeaseOwner,
		&j.LeaseUntil,
		&j.LeaseEpoch,
		&j.AttemptCount,
		&j.LastReasonCode,
		&j.LastAttemptID,
		&j.CreatedAt,
		&j.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &j, nil
}

// GetAttempts lists attempts for a payment ordered by start time.
func (r *Repository) GetAttempts(ctx context.Context, paymentID uuid.UUID) ([]*Attempt, error) {
	query := `
		SELECT id, payment_id, lease_epoch, worker_identity, started_at, finished_at, state_before, state_after, result, reason_code, operation_ids, trace_id, created_at
		FROM recovery_attempts
		WHERE payment_id = $1
		ORDER BY started_at ASC`

	rows, err := r.pool.Query(ctx, query, paymentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attempts []*Attempt
	for rows.Next() {
		var a Attempt
		if err := rows.Scan(
			&a.ID,
			&a.PaymentID,
			&a.LeaseEpoch,
			&a.WorkerIdentity,
			&a.StartedAt,
			&a.FinishedAt,
			&a.StateBefore,
			&a.StateAfter,
			&a.Result,
			&a.ReasonCode,
			&a.OperationIDs,
			&a.TraceID,
			&a.CreatedAt,
		); err != nil {
			return nil, err
		}
		attempts = append(attempts, &a)
	}
	return attempts, rows.Err()
}

// GetQueueMetrics returns aggregate queue depth and age metrics.
func (r *Repository) GetQueueMetrics(ctx context.Context) (total, due, running, attention, resolved int, oldestDueAge time.Duration, err error) {
	query := `
		SELECT
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE status IN ('scheduled', 'deferred') AND due_at <= NOW()) AS due,
			COUNT(*) FILTER (WHERE status = 'running') AS running,
			COUNT(*) FILTER (WHERE status = 'needs_attention') AS attention,
			COUNT(*) FILTER (WHERE status = 'resolved') AS resolved,
			COALESCE(MAX(EXTRACT(EPOCH FROM (NOW() - due_at))) FILTER (WHERE status IN ('scheduled', 'deferred') AND due_at <= NOW()), 0) AS max_age_seconds
		FROM recovery_jobs`

	var maxAgeSec float64
	err = r.pool.QueryRow(ctx, query).Scan(&total, &due, &running, &attention, &resolved, &maxAgeSec)
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}
	oldestDueAge = time.Duration(maxAgeSec * float64(time.Second))
	return
}
