package operations

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrPaymentNotFound     = errors.New("payment not found")
	ErrInvalidCursor       = errors.New("invalid or expired cursor")
	ErrInvalidFilterState  = errors.New("unknown or invalid payment state filter")
	ErrIdempotencyConflict = errors.New("idempotency key reused with different payload")
	ErrCentralUnavailable  = errors.New("central payment authority is currently unavailable")
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// ComputeFilterHash computes a stable SHA-256 hash representing normalized worklist filters.
func ComputeFilterHash(filter WorklistFilter) string {
	h := sha256.New()
	parts := []string{
		strings.TrimSpace(filter.State),
		strings.TrimSpace(filter.RecoveryStatus),
		strings.TrimSpace(filter.BankCode),
	}
	if filter.PaymentID != nil {
		parts = append(parts, filter.PaymentID.String())
	} else {
		parts = append(parts, "")
	}
	if filter.CreatedFrom != nil {
		parts = append(parts, filter.CreatedFrom.UTC().Format(time.RFC3339))
	} else {
		parts = append(parts, "")
	}
	if filter.CreatedTo != nil {
		parts = append(parts, filter.CreatedTo.UTC().Format(time.RFC3339))
	} else {
		parts = append(parts, "")
	}
	h.Write([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(h.Sum(nil))
}

// EncodeWorklistCursor serializes a pagination cursor into base64url JSON.
func EncodeWorklistCursor(payload WorklistCursorPayload) (string, error) {
	bytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// DecodeWorklistCursor deserializes and validates a base64url JSON cursor.
func DecodeWorklistCursor(cursor string, expectedHash string) (*WorklistCursorPayload, error) {
	if len(cursor) > 1024 {
		return nil, ErrInvalidCursor
	}
	bytes, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	var payload WorklistCursorPayload
	if err := json.Unmarshal(bytes, &payload); err != nil {
		return nil, ErrInvalidCursor
	}
	if payload.FilterHash != expectedHash {
		return nil, ErrInvalidCursor
	}
	return &payload, nil
}

// EncodeAttemptsCursor serializes an attempts cursor.
func EncodeAttemptsCursor(payload AttemptsCursorPayload) (string, error) {
	bytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// DecodeAttemptsCursor deserializes an attempts cursor.
func DecodeAttemptsCursor(cursor string) (*AttemptsCursorPayload, error) {
	if len(cursor) > 1024 {
		return nil, ErrInvalidCursor
	}
	bytes, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	var payload AttemptsCursorPayload
	if err := json.Unmarshal(bytes, &payload); err != nil {
		return nil, ErrInvalidCursor
	}
	return &payload, nil
}

func (r *Repository) GetWorklist(ctx context.Context, filter WorklistFilter) (*Worklist, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}

	filterHash := ComputeFilterHash(filter)
	var cursorPayload *WorklistCursorPayload
	if strings.TrimSpace(filter.Cursor) != "" {
		cp, err := DecodeWorklistCursor(filter.Cursor, filterHash)
		if err != nil {
			return nil, err
		}
		cursorPayload = cp
	}

	// Validate state filter
	var filterStates []string
	if strings.TrimSpace(filter.State) != "" {
		rawStates := strings.Split(filter.State, ",")
		for _, s := range rawStates {
			trimmed := strings.TrimSpace(s)
			if trimmed == "" {
				continue
			}
			if !AllowedCentralStates[trimmed] {
				return nil, fmt.Errorf("%w: %s", ErrInvalidFilterState, trimmed)
			}
			filterStates = append(filterStates, trimmed)
		}
	}

	// Construct dynamic query
	var queryBuilder strings.Builder
	queryBuilder.WriteString(`
		SELECT 
			w.payment_id,
			w.current_state,
			w.event_version,
			w.amount_paise,
			w.currency,
			b_src.code AS source_bank_code,
			b_dst.code AS destination_bank_code,
			w.created_at,
			w.updated_at,
			COALESCE(w.recovery_status, 'none'),
			COALESCE(w.recovery_attempts, 0),
			rj.due_at,
			w.last_recovery_reason
		FROM ops_payment_worklist w
		LEFT JOIN banks b_src ON w.source_bank_id = b_src.id
		LEFT JOIN banks b_dst ON w.destination_bank_id = b_dst.id
		LEFT JOIN recovery_jobs rj ON w.payment_id = rj.payment_id
		WHERE 1=1
	`)

	args := []any{}
	argIdx := 1

	if cursorPayload != nil {
		queryBuilder.WriteString(fmt.Sprintf(" AND (w.updated_at < $%d OR (w.updated_at = $%d AND w.payment_id < $%d))", argIdx, argIdx, argIdx+1))
		args = append(args, cursorPayload.UpdatedAt, cursorPayload.PaymentID)
		argIdx += 2
	}

	if len(filterStates) > 0 {
		queryBuilder.WriteString(fmt.Sprintf(" AND w.current_state = ANY($%d)", argIdx))
		args = append(args, filterStates)
		argIdx++
	} else {
		// Default scope: nonterminal / attention payments
		queryBuilder.WriteString(" AND w.current_state NOT IN ('COMPLETED', 'FAILED', 'REVERSED')")
	}

	if strings.TrimSpace(filter.RecoveryStatus) != "" {
		queryBuilder.WriteString(fmt.Sprintf(" AND w.recovery_status = $%d", argIdx))
		args = append(args, strings.TrimSpace(filter.RecoveryStatus))
		argIdx++
	}

	if strings.TrimSpace(filter.BankCode) != "" {
		code := strings.TrimSpace(filter.BankCode)
		queryBuilder.WriteString(fmt.Sprintf(" AND (b_src.code = $%d OR b_dst.code = $%d)", argIdx, argIdx))
		args = append(args, code)
		argIdx++
	}

	if filter.PaymentID != nil {
		queryBuilder.WriteString(fmt.Sprintf(" AND w.payment_id = $%d", argIdx))
		args = append(args, *filter.PaymentID)
		argIdx++
	}

	if filter.CreatedFrom != nil {
		queryBuilder.WriteString(fmt.Sprintf(" AND w.created_at >= $%d", argIdx))
		args = append(args, *filter.CreatedFrom)
		argIdx++
	}

	if filter.CreatedTo != nil {
		queryBuilder.WriteString(fmt.Sprintf(" AND w.created_at <= $%d", argIdx))
		args = append(args, *filter.CreatedTo)
		argIdx++
	}

	queryBuilder.WriteString(fmt.Sprintf(" ORDER BY w.updated_at DESC, w.payment_id DESC LIMIT $%d", argIdx))
	args = append(args, limit+1)

	rows, err := r.db.Query(ctx, queryBuilder.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("query ops_payment_worklist: %w", err)
	}
	defer rows.Close()

	items := []WorklistItem{}
	now := time.Now().UTC()

	for rows.Next() {
		var item WorklistItem
		var eventVersion int64
		var amountPaise int64
		var srcCode, dstCode, lastReason *string
		var nextAttemptAt *time.Time

		err := rows.Scan(
			&item.PaymentID,
			&item.State,
			&eventVersion,
			&amountPaise,
			&item.Currency,
			&srcCode,
			&dstCode,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.RecoveryStatus,
			&item.AttemptCount,
			&nextAttemptAt,
			&lastReason,
		)
		if err != nil {
			return nil, fmt.Errorf("scan worklist item: %w", err)
		}

		item.SourceBankCode = srcCode
		item.DestinationBankCode = dstCode
		item.NextAttemptAt = nextAttemptAt
		item.ReasonCode = lastReason
		item.AmountPaise = strconv.FormatInt(amountPaise, 10)
		item.ProjectionVersion = strconv.FormatInt(eventVersion, 10)
		item.AgeMs = now.Sub(item.CreatedAt).Milliseconds()
		if item.AgeMs < 0 {
			item.AgeMs = 0
		}

		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	var nextCursor *string
	if len(items) > limit {
		lastItem := items[limit-1]
		cp := WorklistCursorPayload{
			UpdatedAt:  lastItem.UpdatedAt,
			PaymentID:  lastItem.PaymentID,
			FilterHash: filterHash,
		}
		cStr, err := EncodeWorklistCursor(cp)
		if err == nil {
			nextCursor = &cStr
		}
		items = items[:limit]
	}

	// Projection status computation
	projStatus := r.computeProjectionStatus(ctx)

	return &Worklist{
		Items:      items,
		NextCursor: nextCursor,
		AsOf:       now,
		Projection: projStatus,
	}, nil
}

func (r *Repository) computeProjectionStatus(ctx context.Context) WorklistProjectionStatus {
	status := WorklistProjectionStatus{
		Status: "ready",
	}

	var lastProjected *time.Time
	_ = r.db.QueryRow(ctx, "SELECT MAX(projected_at) FROM ops_payment_worklist").Scan(&lastProjected)
	status.UpdatedAt = lastProjected

	var oldestPendingAgeMs *float64
	err := r.db.QueryRow(ctx, `
		SELECT EXTRACT(EPOCH FROM (NOW() - MIN(created_at))) * 1000
		FROM outbox_deliveries
		WHERE consumer_name = 'ops-worklist-v1' AND status = 'pending'
	`).Scan(&oldestPendingAgeMs)

	if err == nil && oldestPendingAgeMs != nil {
		age := int64(*oldestPendingAgeMs)
		status.OldestPendingEventAgeMs = &age
		// Lag threshold: 10 seconds
		if age > 10000 {
			status.Status = "lagging"
		}
	} else {
		zero := int64(0)
		status.OldestPendingEventAgeMs = &zero
	}

	return status
}

// RawEvidenceData bundles the raw database queries needed for evidence generation.
type RawEvidenceData struct {
	PaymentID           uuid.UUID
	State               string
	AmountPaise         int64
	Currency            string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	CompletedAt         *time.Time
	EventVersion        int64
	SourceBankCode      *string
	DestinationBankCode *string
	SourceBankID        *uuid.UUID
	DestinationBankID   *uuid.UUID
	RouteBankID         *uuid.UUID
	ExecutionTargetID   *string
	RouteReasonCode     *string
	RouteSelectedAt     *time.Time
	Operations          []RawOperation
	Transitions         []TransitionSnapshot
	RecoveryJob         *RawRecoveryJob
	ReconRunIDs         []uuid.UUID
	IntegrityRunID      *uuid.UUID
	IntegrityViolations *int
	IntegrityOccurredAt *time.Time
	TraceIDs            []string
}

type RawOperation struct {
	OperationID   uuid.UUID
	OperationType string
	BankCode      string
	BankID        uuid.UUID
	CentralStatus string
	CreatedAt     time.Time
}

type RawRecoveryJob struct {
	Status        string
	AttemptCount  int
	DueAt         *time.Time
	LastAttemptID *uuid.UUID
	ReasonCode    *string
}

// GetPaymentRawEvidence loads central payment evidence records within a read-only transaction.
func (r *Repository) GetPaymentRawEvidence(ctx context.Context, paymentID uuid.UUID) (*RawEvidenceData, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCentralUnavailable, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	data := &RawEvidenceData{
		PaymentID:   paymentID,
		Operations:  []RawOperation{},
		Transitions: []TransitionSnapshot{},
		ReconRunIDs: []uuid.UUID{},
		TraceIDs:    []string{},
	}

	// 1. Authoritative central payment row
	paymentQuery := `
		SELECT 
			p.id, p.state, p.amount_paise, p.currency, p.created_at, p.updated_at, p.completed_at, p.event_version,
			b_src.code, b_dst.code, p.source_bank_id, p.destination_bank_id, p.route_bank_id
		FROM payments p
		LEFT JOIN banks b_src ON p.source_bank_id = b_src.id
		LEFT JOIN banks b_dst ON p.destination_bank_id = b_dst.id
		WHERE p.id = $1`

	err = tx.QueryRow(ctx, paymentQuery, paymentID).Scan(
		&data.PaymentID,
		&data.State,
		&data.AmountPaise,
		&data.Currency,
		&data.CreatedAt,
		&data.UpdatedAt,
		&data.CompletedAt,
		&data.EventVersion,
		&data.SourceBankCode,
		&data.DestinationBankCode,
		&data.SourceBankID,
		&data.DestinationBankID,
		&data.RouteBankID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPaymentNotFound
		}
		return nil, fmt.Errorf("%w: %v", ErrCentralUnavailable, err)
	}

	// 2. Persisted route decision
	routeQuery := `
		SELECT execution_target_id, reason_code, selected_at
		FROM payment_route_decisions
		WHERE payment_id = $1
		ORDER BY selected_at DESC, id DESC
		LIMIT 1`
	_ = tx.QueryRow(ctx, routeQuery, paymentID).Scan(
		&data.ExecutionTargetID,
		&data.RouteReasonCode,
		&data.RouteSelectedAt,
	)

	// 3. Central operations
	opsQuery := `
		SELECT pbo.operation_id, pbo.operation_type, b.code, pbo.bank_id, pbo.status, pbo.created_at
		FROM payment_bank_operations pbo
		JOIN banks b ON pbo.bank_id = b.id
		WHERE pbo.payment_id = $1
		ORDER BY pbo.created_at ASC
		LIMIT 25`
	opsRows, err := tx.Query(ctx, opsQuery, paymentID)
	if err == nil {
		for opsRows.Next() {
			var op RawOperation
			if err := opsRows.Scan(&op.OperationID, &op.OperationType, &op.BankCode, &op.BankID, &op.CentralStatus, &op.CreatedAt); err == nil {
				data.Operations = append(data.Operations, op)
			}
		}
		opsRows.Close()
	}

	// 4. Transitions
	transQuery := `
		SELECT id, sequence_number, from_state, to_state, transitioned_at
		FROM payment_state_transitions
		WHERE payment_id = $1
		ORDER BY sequence_number ASC
		LIMIT 205`
	transRows, err := tx.Query(ctx, transQuery, paymentID)
	if err == nil {
		for transRows.Next() {
			var t TransitionSnapshot
			var seqNum int64
			if err := transRows.Scan(&t.ID, &seqNum, &t.FromState, &t.ToState, &t.OccurredAt); err == nil {
				t.Sequence = strconv.FormatInt(seqNum, 10)
				t.EvidenceID = fmt.Sprintf("transition:%s", t.ID)
				data.Transitions = append(data.Transitions, t)
			}
		}
		transRows.Close()
	}

	// 5. Recovery job
	var rj RawRecoveryJob
	rjQuery := `
		SELECT status, attempt_count, due_at, last_attempt_id, last_reason_code
		FROM recovery_jobs
		WHERE payment_id = $1`
	err = tx.QueryRow(ctx, rjQuery, paymentID).Scan(
		&rj.Status,
		&rj.AttemptCount,
		&rj.DueAt,
		&rj.LastAttemptID,
		&rj.ReasonCode,
	)
	if err == nil {
		data.RecoveryJob = &rj
	}

	// 6. Reconciliation runs (completed runs overlapping creation)
	reconQuery := `
		SELECT id FROM recon_runs
		WHERE status = 'COMPLETED' AND started_at <= $1 AND (completed_at IS NULL OR completed_at >= $1)
		ORDER BY started_at DESC
		LIMIT 5`
	reconRows, err := tx.Query(ctx, reconQuery, data.CreatedAt)
	if err == nil {
		for reconRows.Next() {
			var rid uuid.UUID
			if err := reconRows.Scan(&rid); err == nil {
				data.ReconRunIDs = append(data.ReconRunIDs, rid)
			}
		}
		reconRows.Close()
	}

	// 7. Latest completed integrity run
	integQuery := `
		SELECT id, (failed_checks + error_checks) AS violations, completed_at
		FROM integrity_runs
		WHERE status = 'COMPLETED'
		ORDER BY started_at DESC
		LIMIT 1`
	_ = tx.QueryRow(ctx, integQuery).Scan(
		&data.IntegrityRunID,
		&data.IntegrityViolations,
		&data.IntegrityOccurredAt,
	)

	// 8. Distributed trace IDs from outbox events
	traceQuery := `
		SELECT DISTINCT traceparent
		FROM outbox_events
		WHERE aggregate_id = $1 AND traceparent IS NOT NULL AND traceparent <> ''
		LIMIT 5`
	traceRows, err := tx.Query(ctx, traceQuery, paymentID)
	if err == nil {
		for traceRows.Next() {
			var tp string
			if err := traceRows.Scan(&tp); err == nil {
				// Parse W3C traceparent (format: 00-{traceId}-{spanId}-{flags})
				parts := strings.Split(tp, "-")
				if len(parts) >= 3 && len(parts[1]) == 32 {
					data.TraceIDs = append(data.TraceIDs, parts[1])
				}
			}
		}
		traceRows.Close()
	}

	return data, nil
}

// GetRecoveryAttempts retrieves paginated recovery attempts for a payment.
func (r *Repository) GetRecoveryAttempts(ctx context.Context, paymentID uuid.UUID, limit int, cursor string) (*RecoveryAttemptsPage, error) {
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}

	// Verify payment exists
	var exists bool
	err := r.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM payments WHERE id = $1)", paymentID).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("check payment: %w", err)
	}
	if !exists {
		return nil, ErrPaymentNotFound
	}

	var cursorPayload *AttemptsCursorPayload
	if strings.TrimSpace(cursor) != "" {
		cp, err := DecodeAttemptsCursor(cursor)
		if err != nil {
			return nil, err
		}
		cursorPayload = cp
	}

	queryBuilder := strings.Builder{}
	queryBuilder.WriteString(`
		SELECT id, payment_id, worker_identity, started_at, finished_at, state_before, state_after, result, reason_code, operation_ids, trace_id, lease_epoch
		FROM recovery_attempts
		WHERE payment_id = $1
	`)
	args := []any{paymentID}
	argIdx := 2

	if cursorPayload != nil {
		queryBuilder.WriteString(fmt.Sprintf(" AND (started_at < $%d OR (started_at = $%d AND id < $%d))", argIdx, argIdx, argIdx+1))
		args = append(args, cursorPayload.StartedAt, cursorPayload.AttemptID)
		argIdx += 2
	}

	queryBuilder.WriteString(fmt.Sprintf(" ORDER BY started_at DESC, id DESC LIMIT $%d", argIdx))
	args = append(args, limit+1)

	rows, err := r.db.Query(ctx, queryBuilder.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("query recovery_attempts: %w", err)
	}
	defer rows.Close()

	items := []RecoveryAttemptItem{}
	now := time.Now().UTC()

	for rows.Next() {
		var item RecoveryAttemptItem
		var epoch int64
		var rawWorkerID string
		var opIds []uuid.UUID

		err := rows.Scan(
			&item.AttemptID,
			&item.PaymentID,
			&rawWorkerID,
			&item.StartedAt,
			&item.FinishedAt,
			&item.StateBefore,
			&item.StateAfter,
			&item.Result,
			&item.ReasonCode,
			&opIds,
			&item.TraceID,
			&epoch,
		)
		if err != nil {
			return nil, fmt.Errorf("scan attempt: %w", err)
		}

		if opIds == nil {
			opIds = []uuid.UUID{}
		}
		item.OperationIds = opIds
		item.LeaseEpoch = strconv.FormatInt(epoch, 10)
		item.WorkerID = sanitizeWorkerID(rawWorkerID)

		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows err: %w", err)
	}

	var nextCursor *string
	if len(items) > limit {
		lastItem := items[limit-1]
		cp := AttemptsCursorPayload{
			StartedAt: lastItem.StartedAt,
			AttemptID: lastItem.AttemptID,
		}
		cStr, err := EncodeAttemptsCursor(cp)
		if err == nil {
			nextCursor = &cStr
		}
		items = items[:limit]
	}

	return &RecoveryAttemptsPage{
		Items:      items,
		NextCursor: nextCursor,
		AsOf:       now,
	}, nil
}

// RecheckRecovery expedites an operational recovery job under idempotency protection.
func (r *Repository) RecheckRecovery(ctx context.Context, actorID uuid.UUID, actorRole string, paymentID uuid.UUID, reason string, idempotencyKey string) (*RecoveryRecheckResult, error) {
	reqHash := sha256Hex(fmt.Sprintf("%s:%s", paymentID, strings.TrimSpace(reason)))

	// 1. Check control_idempotency_records
	var storedStatus, storedHash string
	var storedCode int
	var storedBody []byte
	err := r.db.QueryRow(ctx, `
		SELECT status, request_hash, response_code, response_body
		FROM control_idempotency_records
		WHERE actor_id = $1 AND action = 'RECOVERY_RECHECK' AND key = $2
	`, actorID, idempotencyKey).Scan(&storedStatus, &storedHash, &storedCode, &storedBody)

	if err == nil {
		if storedHash != reqHash {
			return nil, ErrIdempotencyConflict
		}
		var res RecoveryRecheckResult
		if err := json.Unmarshal(storedBody, &res); err == nil {
			return &res, nil
		}
	}

	// 2. Begin transaction
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin recheck tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var state string
	err = tx.QueryRow(ctx, "SELECT state FROM payments WHERE id = $1 FOR UPDATE", paymentID).Scan(&state)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPaymentNotFound
		}
		return nil, fmt.Errorf("query payment state: %w", err)
	}

	var result RecoveryRecheckResult
	result.PaymentID = paymentID

	if TerminalCentralStates[state] {
		result.Status = "already_terminal"
		result.ScheduledAt = nil
	} else {
		now := time.Now().UTC()
		result.Status = "scheduled"
		result.ScheduledAt = &now

		// Expedite job: schedule immediately
		upsertJobQuery := `
			INSERT INTO recovery_jobs (payment_id, status, due_at, last_reason_code, updated_at)
			VALUES ($1, 'scheduled', NOW(), 'OPERATOR_RECHECK', NOW())
			ON CONFLICT (payment_id) DO UPDATE SET
				status = 'scheduled',
				due_at = NOW(),
				last_reason_code = 'OPERATOR_RECHECK',
				updated_at = NOW()`
		if _, err := tx.Exec(ctx, upsertJobQuery, paymentID); err != nil {
			return nil, fmt.Errorf("expedite recovery job: %w", err)
		}

		// Operational audit log
		auditQuery := `
			INSERT INTO operational_audit_logs (
				actor_id, actor_role, operation, entity_type, entity_id, reason_code, details, result
			) VALUES (
				$1, $2, 'RECOVERY_RECHECK', 'PAYMENT', $3, 'OPERATOR_RECHECK',
				jsonb_build_object('reason', $4::text), 'SUCCESS'
			)`
		if _, err := tx.Exec(ctx, auditQuery, actorID, actorRole, paymentID.String(), reason); err != nil {
			return nil, fmt.Errorf("insert operational audit log: %w", err)
		}
	}

	bodyBytes, _ := json.Marshal(result)
	code := 202
	if result.Status == "already_terminal" {
		code = 200
	}

	// Record in control_idempotency_records
	upsertIdempQuery := `
		INSERT INTO control_idempotency_records (
			key, actor_id, action, request_hash, status, response_code, response_body
		) VALUES (
			$1, $2, 'RECOVERY_RECHECK', $3, 'RESOLVED', $4, $5
		)
		ON CONFLICT (actor_id, action, key) DO UPDATE SET
			response_code = EXCLUDED.response_code,
			response_body = EXCLUDED.response_body,
			updated_at = NOW()`
	if _, err := tx.Exec(ctx, upsertIdempQuery, idempotencyKey, actorID, reqHash, code, bodyBytes); err != nil {
		return nil, fmt.Errorf("record control idempotency: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit recheck: %w", err)
	}

	return &result, nil
}

func sanitizeWorkerID(raw string) string {
	if strings.Contains(raw, "@") {
		parts := strings.Split(raw, "@")
		return parts[0]
	}
	if strings.Contains(raw, ".") {
		parts := strings.Split(raw, ".")
		return parts[0]
	}
	return raw
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
