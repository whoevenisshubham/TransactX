package projections

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/outbox"
)

// Projector updates derived projection tables from immutable outbox events.
// It executes projection updates and inbox recording atomically, and enforces
// nondecreasing event_version guards to tolerate out-of-order event arrivals.
type Projector struct {
	pool         *pgxpool.Pool
	consumerName string
}

func NewProjector(pool *pgxpool.Pool, consumerName string) *Projector {
	if consumerName == "" {
		consumerName = outbox.ConsumerOpsWorklist
	}
	return &Projector{
		pool:         pool,
		consumerName: consumerName,
	}
}

// ProjectEvent processes an outbox event into ops_payment_worklist inside an
// isolated transaction with inbox idempotency checking and version guarding.
func (p *Projector) ProjectEvent(ctx context.Context, event *outbox.Event) (duplicate bool, err error) {
	if event.SchemaVersion != 1 {
		return false, fmt.Errorf("%w: version %d", ErrUnsupportedVersion, event.SchemaVersion)
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin projection tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Shared transaction advisory lock prevents conflict with rebuild CLI
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock_shared($1)", WorklistMaintenanceLockKey); err != nil {
		return false, fmt.Errorf("acquire shared maintenance lock: %w", err)
	}

	// 2. Inbox recording: check if this event was already applied by this consumer
	inboxQuery := `
		INSERT INTO consumer_inbox (consumer_name, event_id, processed_at, handler_version)
		VALUES ($1, $2, NOW(), 1)
		ON CONFLICT (consumer_name, event_id) DO NOTHING`

	tag, err := tx.Exec(ctx, inboxQuery, p.consumerName, event.ID)
	if err != nil {
		return false, fmt.Errorf("record consumer inbox: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Event was already processed and committed by this consumer
		return true, nil
	}

	// 3. Resolve target payment ID
	var paymentID uuid.UUID
	switch event.AggregateType {
	case outbox.AggregateTypePayment:
		paymentID = event.AggregateID
	case outbox.AggregateTypeRecovery:
		rp, parseErr := event.ParseRecoveryPayload()
		if parseErr == nil && rp.PaymentID != uuid.Nil {
			paymentID = rp.PaymentID
		} else {
			paymentID = event.AggregateID
		}
	default:
		// Other aggregate types not handled by worklist projector
		return false, tx.Commit(ctx)
	}

	// 4. Fetch authoritative current payment row
	paymentQuery := `
		SELECT id, state, event_version, amount_paise, currency,
		       source_bank_id, destination_bank_id, route_bank_id,
		       created_at, updated_at
		FROM payments
		WHERE id = $1`

	var item WorklistItem
	err = tx.QueryRow(ctx, paymentQuery, paymentID).Scan(
		&item.PaymentID,
		&item.CurrentState,
		&item.EventVersion,
		&item.AmountPaise,
		&item.Currency,
		&item.SourceBankID,
		&item.DestinationBankID,
		&item.RouteBankID,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, fmt.Errorf("%w: %s", ErrPaymentNotFound, paymentID)
		}
		return false, fmt.Errorf("read authoritative payment: %w", err)
	}

	// 5. Fetch authoritative recovery job status (if one exists)
	recoveryQuery := `
		SELECT status, attempt_count, last_reason_code, updated_at
		FROM recovery_jobs
		WHERE payment_id = $1`

	var recStatus string
	var recAttempts int
	var recReason *string
	var recUpdatedAt time.Time
	err = tx.QueryRow(ctx, recoveryQuery, paymentID).Scan(&recStatus, &recAttempts, &recReason, &recUpdatedAt)
	if err == nil {
		item.RecoveryStatus = &recStatus
		item.RecoveryAttempts = recAttempts
		item.LastRecoveryReason = recReason
		item.LastRecoveryAt = &recUpdatedAt
	}

	// 6. Upsert into ops_payment_worklist with event_version guard
	upsertQuery := `
		INSERT INTO ops_payment_worklist (
		    payment_id, current_state, event_version, amount_paise, currency,
		    source_bank_id, destination_bank_id, route_bank_id, created_at, updated_at,
		    recovery_status, recovery_attempts, last_recovery_reason, last_recovery_at,
		    projected_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, NOW())
		ON CONFLICT (payment_id) DO UPDATE
		SET current_state = EXCLUDED.current_state,
		    event_version = EXCLUDED.event_version,
		    amount_paise = EXCLUDED.amount_paise,
		    currency = EXCLUDED.currency,
		    source_bank_id = EXCLUDED.source_bank_id,
		    destination_bank_id = EXCLUDED.destination_bank_id,
		    route_bank_id = EXCLUDED.route_bank_id,
		    updated_at = EXCLUDED.updated_at,
		    recovery_status = EXCLUDED.recovery_status,
		    recovery_attempts = EXCLUDED.recovery_attempts,
		    last_recovery_reason = EXCLUDED.last_recovery_reason,
		    last_recovery_at = EXCLUDED.last_recovery_at,
		    projected_at = NOW()
		WHERE ops_payment_worklist.event_version <= EXCLUDED.event_version`

	if _, err := tx.Exec(ctx, upsertQuery,
		item.PaymentID,
		item.CurrentState,
		item.EventVersion,
		item.AmountPaise,
		item.Currency,
		item.SourceBankID,
		item.DestinationBankID,
		item.RouteBankID,
		item.CreatedAt,
		item.UpdatedAt,
		item.RecoveryStatus,
		item.RecoveryAttempts,
		item.LastRecoveryReason,
		item.LastRecoveryAt,
	); err != nil {
		return false, fmt.Errorf("upsert worklist projection: %w", err)
	}

	// 7. Commit inbox entry and projection update atomically
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit projection tx: %w", err)
	}

	return false, nil
}
