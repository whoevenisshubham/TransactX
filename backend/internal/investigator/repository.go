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

// Repository manages investigation jobs, runbook chunks, and usage reservations.
type Repository interface {
	CreateJob(ctx context.Context, paymentID uuid.UUID, actorID string, idempotencyKey string, question string, mode string) (*JobRecord, bool, error)
	GetJob(ctx context.Context, jobID uuid.UUID) (*JobRecord, error)
	ClaimNextJob(ctx context.Context, leaseOwner string, leaseDuration time.Duration) (*JobRecord, error)
	CompleteJob(ctx context.Context, jobID uuid.UUID, leaseEpoch int64, report *InvestigationReport, timeline []ToolStep, usage *UsageInfo) error
	FailJob(ctx context.Context, jobID uuid.UUID, leaseEpoch int64, errorCode string, errorMessage string, timeline []ToolStep) error
	SearchCorpus(ctx context.Context, query string, tags []string, limit int) ([]CorpusChunk, error)
	InsertCorpusChunk(ctx context.Context, chunk CorpusChunk) error
	CountCorpusChunks(ctx context.Context) (int, error)
	CreateReservation(ctx context.Context, res UsageReservation) error
	CommitReservation(ctx context.Context, resID uuid.UUID, actualTokens int) error
	ReleaseReservation(ctx context.Context, resID uuid.UUID) error
}

type postgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository returns a PostgreSQL backed Repository.
func NewPostgresRepository(pool *pgxpool.Pool) Repository {
	return &postgresRepository{pool: pool}
}

// CreateJob enforces active limits and idempotency, returning (job, isNew, error).
func (r *postgresRepository) CreateJob(ctx context.Context, paymentID uuid.UUID, actorID string, idempotencyKey string, question string, mode string) (*JobRecord, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Check for existing job with same (actor_id, idempotency_key)
	existingQuery := `
		SELECT id, payment_id, actor_id, status, mode, question, report, tool_timeline, usage,
		       error_code, error_message, lease_owner, lease_epoch, lease_expires_at,
		       idempotency_key, created_at, completed_at
		FROM investigation_jobs
		WHERE actor_id = $1 AND idempotency_key = $2
		FOR UPDATE;
	`
	var existing JobRecord
	var reportJSON, timelineJSON, usageJSON []byte
	var errCode, errMsg, leaseOwner sql.NullString
	var leaseExpiresAt, completedAt sql.NullTime

	err = tx.QueryRow(ctx, existingQuery, actorID, idempotencyKey).Scan(
		&existing.ID,
		&existing.PaymentID,
		&existing.ActorID,
		&existing.Status,
		&existing.Mode,
		&existing.Question,
		&reportJSON,
		&timelineJSON,
		&usageJSON,
		&errCode,
		&errMsg,
		&leaseOwner,
		&existing.LeaseEpoch,
		&leaseExpiresAt,
		&existing.IdempotencyKey,
		&existing.CreatedAt,
		&completedAt,
	)
	if err == nil {
		// Existing job found: verify idempotency payload match
		if existing.PaymentID != paymentID || existing.Question != question {
			return nil, false, ErrIdempotencyConflict
		}

		if err := unmarshalJobJSON(&existing, reportJSON, timelineJSON, usageJSON, errCode, errMsg, leaseOwner, leaseExpiresAt, completedAt); err != nil {
			return nil, false, err
		}

		if err := tx.Commit(ctx); err != nil {
			return nil, false, fmt.Errorf("commit existing job tx: %w", err)
		}
		return &existing, false, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf("check existing job: %w", err)
	}

	// Active job bounds: max 1 per actor, max 2 globally
	var actorActiveCount, globalActiveCount int
	actorCountQuery := `
		SELECT COUNT(*) FROM investigation_jobs
		WHERE actor_id = $1 AND status IN ('queued', 'running');
	`
	if err := tx.QueryRow(ctx, actorCountQuery, actorID).Scan(&actorActiveCount); err != nil {
		return nil, false, fmt.Errorf("count actor active jobs: %w", err)
	}
	if actorActiveCount >= 1 {
		return nil, false, ErrActorLimitExceeded
	}

	globalCountQuery := `
		SELECT COUNT(*) FROM investigation_jobs
		WHERE status IN ('queued', 'running');
	`
	if err := tx.QueryRow(ctx, globalCountQuery).Scan(&globalActiveCount); err != nil {
		return nil, false, fmt.Errorf("count global active jobs: %w", err)
	}
	if globalActiveCount >= 2 {
		return nil, false, ErrGlobalLimitExceeded
	}

	if mode == "" {
		mode = ModeEvidenceOnly
	}

	insertQuery := `
		INSERT INTO investigation_jobs (
			payment_id, actor_id, status, mode, question, idempotency_key
		) VALUES ($1, $2, 'queued', $3, $4, $5)
		RETURNING id, payment_id, actor_id, status, mode, question, report, tool_timeline, usage,
		          error_code, error_message, lease_owner, lease_epoch, lease_expires_at,
		          idempotency_key, created_at, completed_at;
	`
	var newJob JobRecord
	err = tx.QueryRow(ctx, insertQuery, paymentID, actorID, mode, question, idempotencyKey).Scan(
		&newJob.ID,
		&newJob.PaymentID,
		&newJob.ActorID,
		&newJob.Status,
		&newJob.Mode,
		&newJob.Question,
		&reportJSON,
		&timelineJSON,
		&usageJSON,
		&errCode,
		&errMsg,
		&leaseOwner,
		&newJob.LeaseEpoch,
		&leaseExpiresAt,
		&newJob.IdempotencyKey,
		&newJob.CreatedAt,
		&completedAt,
	)
	if err != nil {
		return nil, false, fmt.Errorf("insert investigation job: %w", err)
	}

	if err := unmarshalJobJSON(&newJob, reportJSON, timelineJSON, usageJSON, errCode, errMsg, leaseOwner, leaseExpiresAt, completedAt); err != nil {
		return nil, false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, false, fmt.Errorf("commit insert job tx: %w", err)
	}

	return &newJob, true, nil
}

// GetJob retrieves an investigation job by UUID.
func (r *postgresRepository) GetJob(ctx context.Context, jobID uuid.UUID) (*JobRecord, error) {
	query := `
		SELECT id, payment_id, actor_id, status, mode, question, report, tool_timeline, usage,
		       error_code, error_message, lease_owner, lease_epoch, lease_expires_at,
		       idempotency_key, created_at, completed_at
		FROM investigation_jobs
		WHERE id = $1;
	`
	var job JobRecord
	var reportJSON, timelineJSON, usageJSON []byte
	var errCode, errMsg, leaseOwner sql.NullString
	var leaseExpiresAt, completedAt sql.NullTime

	err := r.pool.QueryRow(ctx, query, jobID).Scan(
		&job.ID,
		&job.PaymentID,
		&job.ActorID,
		&job.Status,
		&job.Mode,
		&job.Question,
		&reportJSON,
		&timelineJSON,
		&usageJSON,
		&errCode,
		&errMsg,
		&leaseOwner,
		&job.LeaseEpoch,
		&leaseExpiresAt,
		&job.IdempotencyKey,
		&job.CreatedAt,
		&completedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrJobNotFound
		}
		return nil, fmt.Errorf("query investigation job: %w", err)
	}

	if err := unmarshalJobJSON(&job, reportJSON, timelineJSON, usageJSON, errCode, errMsg, leaseOwner, leaseExpiresAt, completedAt); err != nil {
		return nil, err
	}

	return &job, nil
}

// ClaimNextJob claims the next available queued or expired running job using FOR UPDATE SKIP LOCKED.
func (r *postgresRepository) ClaimNextJob(ctx context.Context, leaseOwner string, leaseDuration time.Duration) (*JobRecord, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin claim tx: %w", err)
	}
	defer tx.Rollback(ctx)

	selectQuery := `
		SELECT id, payment_id, actor_id, status, mode, question, report, tool_timeline, usage,
		       error_code, error_message, lease_owner, lease_epoch, lease_expires_at,
		       idempotency_key, created_at, completed_at
		FROM investigation_jobs
		WHERE (status = 'queued')
		   OR (status = 'running' AND lease_expires_at IS NOT NULL AND lease_expires_at < NOW())
		ORDER BY created_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED;
	`
	var job JobRecord
	var reportJSON, timelineJSON, usageJSON []byte
	var errCode, errMsg, currentLeaseOwner sql.NullString
	var currentLeaseExpiresAt, completedAt sql.NullTime

	err = tx.QueryRow(ctx, selectQuery).Scan(
		&job.ID,
		&job.PaymentID,
		&job.ActorID,
		&job.Status,
		&job.Mode,
		&job.Question,
		&reportJSON,
		&timelineJSON,
		&usageJSON,
		&errCode,
		&errMsg,
		&currentLeaseOwner,
		&job.LeaseEpoch,
		&currentLeaseExpiresAt,
		&job.IdempotencyKey,
		&job.CreatedAt,
		&completedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // No jobs available
		}
		return nil, fmt.Errorf("select claimable job: %w", err)
	}

	if err := unmarshalJobJSON(&job, reportJSON, timelineJSON, usageJSON, errCode, errMsg, currentLeaseOwner, currentLeaseExpiresAt, completedAt); err != nil {
		return nil, err
	}

	newEpoch := job.LeaseEpoch + 1
	intervalStr := fmt.Sprintf("%d milliseconds", leaseDuration.Milliseconds())

	updateQuery := `
		UPDATE investigation_jobs
		SET status = 'running',
		    lease_owner = $2,
		    lease_epoch = $3,
		    lease_expires_at = NOW() + $4::interval
		WHERE id = $1 AND lease_epoch = $5
		RETURNING lease_expires_at;
	`
	var newExpiresAt time.Time
	err = tx.QueryRow(ctx, updateQuery, job.ID, leaseOwner, newEpoch, intervalStr, job.LeaseEpoch).Scan(&newExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("update claimed job lease: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit claim tx: %w", err)
	}

	job.Status = StatusRunning
	job.LeaseOwner = &leaseOwner
	job.LeaseEpoch = newEpoch
	job.LeaseExpiresAt = &newExpiresAt

	return &job, nil
}

// CompleteJob completes an investigation job with CAS verification on lease_epoch.
func (r *postgresRepository) CompleteJob(ctx context.Context, jobID uuid.UUID, leaseEpoch int64, report *InvestigationReport, timeline []ToolStep, usage *UsageInfo) error {
	reportBytes, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}

	timelineBytes, err := json.Marshal(timeline)
	if err != nil {
		return fmt.Errorf("marshal timeline: %w", err)
	}

	usageBytes, err := json.Marshal(usage)
	if err != nil {
		return fmt.Errorf("marshal usage: %w", err)
	}

	updateQuery := `
		UPDATE investigation_jobs
		SET status = 'completed',
		    report = $2,
		    tool_timeline = $3,
		    usage = $4,
		    completed_at = NOW(),
		    lease_expires_at = NULL
		WHERE id = $1 AND lease_epoch = $5 AND status = 'running';
	`
	tag, err := r.pool.Exec(ctx, updateQuery, jobID, reportBytes, timelineBytes, usageBytes, leaseEpoch)
	if err != nil {
		return fmt.Errorf("exec complete job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

// FailJob marks an investigation job failed with CAS verification on lease_epoch.
func (r *postgresRepository) FailJob(ctx context.Context, jobID uuid.UUID, leaseEpoch int64, errorCode string, errorMessage string, timeline []ToolStep) error {
	timelineBytes, err := json.Marshal(timeline)
	if err != nil {
		return fmt.Errorf("marshal timeline: %w", err)
	}

	updateQuery := `
		UPDATE investigation_jobs
		SET status = 'failed',
		    error_code = $2,
		    error_message = $3,
		    tool_timeline = $4,
		    completed_at = NOW(),
		    lease_expires_at = NULL
		WHERE id = $1 AND lease_epoch = $5 AND status = 'running';
	`
	tag, err := r.pool.Exec(ctx, updateQuery, jobID, errorCode, errorMessage, timelineBytes, leaseEpoch)
	if err != nil {
		return fmt.Errorf("exec fail job: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}

// SearchCorpus searches approved active runbook chunks using full-text search with tag boosting.
func (r *postgresRepository) SearchCorpus(ctx context.Context, query string, tags []string, limit int) ([]CorpusChunk, error) {
	if limit <= 0 || limit > 10 {
		limit = 5
	}
	if tags == nil {
		tags = []string{}
	}

	searchQuery := `
		SELECT id, corpus_version, document_path, heading, start_line, end_line,
		       content, sha256, approved_tags, allowed_roles, active, created_at
		FROM runbook_corpus_chunks
		WHERE active = true
		  AND (
		    $1 = ''
		    OR to_tsvector('english', heading || ' ' || content) @@ plainto_tsquery('english', $1)
		    OR ($2::text[] && approved_tags)
		  )
		ORDER BY
		  CASE WHEN $2::text[] && approved_tags THEN 1 ELSE 0 END DESC,
		  CASE WHEN $1 != '' THEN ts_rank(to_tsvector('english', heading || ' ' || content), plainto_tsquery('english', $1)) ELSE 0.0 END DESC,
		  created_at DESC
		LIMIT $3;
	`
	rows, err := r.pool.Query(ctx, searchQuery, query, tags, limit)
	if err != nil {
		return nil, fmt.Errorf("query runbook corpus: %w", err)
	}
	defer rows.Close()

	var chunks []CorpusChunk
	for rows.Next() {
		var c CorpusChunk
		if err := rows.Scan(
			&c.ID,
			&c.CorpusVersion,
			&c.DocumentPath,
			&c.Heading,
			&c.StartLine,
			&c.EndLine,
			&c.Content,
			&c.SHA256,
			&c.ApprovedTags,
			&c.AllowedRoles,
			&c.Active,
			&c.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan runbook chunk: %w", err)
		}
		chunks = append(chunks, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runbook chunks: %w", err)
	}

	// If no chunks found with specific query, fallback to general active chunks
	if len(chunks) == 0 {
		fallbackQuery := `
			SELECT id, corpus_version, document_path, heading, start_line, end_line,
			       content, sha256, approved_tags, allowed_roles, active, created_at
			FROM runbook_corpus_chunks
			WHERE active = true
			ORDER BY created_at ASC
			LIMIT $1;
		`
		fbRows, fbErr := r.pool.Query(ctx, fallbackQuery, limit)
		if fbErr == nil {
			defer fbRows.Close()
			for fbRows.Next() {
				var c CorpusChunk
				if err := fbRows.Scan(
					&c.ID,
					&c.CorpusVersion,
					&c.DocumentPath,
					&c.Heading,
					&c.StartLine,
					&c.EndLine,
					&c.Content,
					&c.SHA256,
					&c.ApprovedTags,
					&c.AllowedRoles,
					&c.Active,
					&c.CreatedAt,
				); err == nil {
					chunks = append(chunks, c)
				}
			}
		}
	}

	if chunks == nil {
		chunks = []CorpusChunk{}
	}
	return chunks, nil
}

// InsertCorpusChunk inserts or updates an approved runbook chunk.
func (r *postgresRepository) InsertCorpusChunk(ctx context.Context, chunk CorpusChunk) error {
	chunkID := chunk.ID
	if chunkID == uuid.Nil {
		chunkID = uuid.New()
	}
	if chunk.CorpusVersion == "" {
		chunk.CorpusVersion = "v1"
	}
	if chunk.ApprovedTags == nil {
		chunk.ApprovedTags = []string{}
	}
	if chunk.AllowedRoles == nil {
		chunk.AllowedRoles = []string{"OPS_ADMIN", "OPS_VIEWER"}
	}

	upsertQuery := `
		INSERT INTO runbook_corpus_chunks (
			id, corpus_version, document_path, heading, start_line, end_line,
			content, sha256, approved_tags, allowed_roles, active
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (corpus_version, document_path, heading, start_line)
		DO UPDATE SET
			content = EXCLUDED.content,
			sha256 = EXCLUDED.sha256,
			approved_tags = EXCLUDED.approved_tags,
			allowed_roles = EXCLUDED.allowed_roles,
			active = EXCLUDED.active;
	`
	_, err := r.pool.Exec(ctx, upsertQuery,
		chunkID,
		chunk.CorpusVersion,
		chunk.DocumentPath,
		chunk.Heading,
		chunk.StartLine,
		chunk.EndLine,
		chunk.Content,
		chunk.SHA256,
		chunk.ApprovedTags,
		chunk.AllowedRoles,
		chunk.Active,
	)
	if err != nil {
		return fmt.Errorf("upsert corpus chunk: %w", err)
	}
	return nil
}

// CountCorpusChunks returns the count of runbook chunks in the corpus.
func (r *postgresRepository) CountCorpusChunks(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM runbook_corpus_chunks WHERE active = true;").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count corpus chunks: %w", err)
	}
	return count, nil
}

// CreateReservation inserts a token usage reservation.
func (r *postgresRepository) CreateReservation(ctx context.Context, res UsageReservation) error {
	resID := res.ID
	if resID == uuid.Nil {
		resID = uuid.New()
	}
	status := res.Status
	if status == "" {
		status = "reserved"
	}
	query := `
		INSERT INTO investigation_usage_reservations (
			id, job_id, actor_id, provider, model, reserved_tokens, status
		) VALUES ($1, $2, $3, $4, $5, $6, $7);
	`
	_, err := r.pool.Exec(ctx, query, resID, res.JobID, res.ActorID, res.Provider, res.Model, res.ReservedTokens, status)
	if err != nil {
		return fmt.Errorf("create usage reservation: %w", err)
	}
	return nil
}

// CommitReservation updates a reservation with actual token usage.
func (r *postgresRepository) CommitReservation(ctx context.Context, resID uuid.UUID, actualTokens int) error {
	query := `
		UPDATE investigation_usage_reservations
		SET actual_tokens = $1, status = 'committed', updated_at = NOW()
		WHERE id = $2;
	`
	_, err := r.pool.Exec(ctx, query, actualTokens, resID)
	if err != nil {
		return fmt.Errorf("commit usage reservation: %w", err)
	}
	return nil
}

// ReleaseReservation releases a reserved token reservation.
func (r *postgresRepository) ReleaseReservation(ctx context.Context, resID uuid.UUID) error {
	query := `
		UPDATE investigation_usage_reservations
		SET status = 'released', updated_at = NOW()
		WHERE id = $1;
	`
	_, err := r.pool.Exec(ctx, query, resID)
	if err != nil {
		return fmt.Errorf("release usage reservation: %w", err)
	}
	return nil
}

func unmarshalJobJSON(
	job *JobRecord,
	reportJSON, timelineJSON, usageJSON []byte,
	errCode, errMsg, leaseOwner sql.NullString,
	leaseExpiresAt, completedAt sql.NullTime,
) error {
	if len(reportJSON) > 0 && string(reportJSON) != "null" {
		var report InvestigationReport
		if err := json.Unmarshal(reportJSON, &report); err != nil {
			return fmt.Errorf("unmarshal report json: %w", err)
		}
		job.Report = &report
	}
	if len(timelineJSON) > 0 && string(timelineJSON) != "null" {
		var timeline []ToolStep
		if err := json.Unmarshal(timelineJSON, &timeline); err != nil {
			return fmt.Errorf("unmarshal timeline json: %w", err)
		}
		job.ToolTimeline = timeline
	} else {
		job.ToolTimeline = []ToolStep{}
	}
	if len(usageJSON) > 0 && string(usageJSON) != "null" {
		var usage UsageInfo
		if err := json.Unmarshal(usageJSON, &usage); err != nil {
			return fmt.Errorf("unmarshal usage json: %w", err)
		}
		job.Usage = &usage
	}
	if errCode.Valid {
		job.ErrorCode = &errCode.String
	}
	if errMsg.Valid {
		job.ErrorMessage = &errMsg.String
	}
	if leaseOwner.Valid {
		job.LeaseOwner = &leaseOwner.String
	}
	if leaseExpiresAt.Valid {
		job.LeaseExpiresAt = &leaseExpiresAt.Time
	}
	if completedAt.Valid {
		job.CompletedAt = &completedAt.Time
	}
	return nil
}
