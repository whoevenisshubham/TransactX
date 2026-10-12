package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	MaxDeliveryAttempts = 10
	DefaultClaimLimit   = 25
)

// Dispatcher handles transactional outbox delivery claims, acknowledgements,
// retries with backoff, and quarantine isolation.
type Dispatcher struct {
	pool *pgxpool.Pool
}

func NewDispatcher(pool *pgxpool.Pool) *Dispatcher {
	return &Dispatcher{pool: pool}
}

// ClaimDueDeliveries claims up to maxDeliveries due deliveries using
// FOR UPDATE SKIP LOCKED in a short transaction, incrementing lease epoch and setting owner.
func (d *Dispatcher) ClaimDueDeliveries(ctx context.Context, consumerName, workerID string, maxDeliveries int, leaseDuration time.Duration) ([]*Delivery, error) {
	if maxDeliveries <= 0 {
		maxDeliveries = DefaultClaimLimit
	}

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin claim tx: %w", err)
	}
	defer tx.Rollback(ctx)

	query := `
		SELECT consumer_name, event_id, status, attempts, next_attempt_at,
		       lease_owner, lease_until, lease_epoch, delivered_at, last_error_code, created_at
		FROM outbox_deliveries
		WHERE consumer_name = $1
		  AND (
		      (status = 'pending' AND next_attempt_at <= NOW())
		      OR (status = 'processing' AND lease_until < NOW())
		  )
		ORDER BY next_attempt_at ASC, created_at ASC
		LIMIT $2
		FOR UPDATE SKIP LOCKED`

	rows, err := tx.Query(ctx, query, consumerName, maxDeliveries)
	if err != nil {
		return nil, fmt.Errorf("query due deliveries: %w", err)
	}

	var deliveries []*Delivery
	for rows.Next() {
		var item Delivery
		if err := rows.Scan(
			&item.ConsumerName,
			&item.EventID,
			&item.Status,
			&item.Attempts,
			&item.NextAttemptAt,
			&item.LeaseOwner,
			&item.LeaseUntil,
			&item.LeaseEpoch,
			&item.DeliveredAt,
			&item.LastErrorCode,
			&item.CreatedAt,
		); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan claimed delivery: %w", err)
		}
		deliveries = append(deliveries, &item)
	}
	rows.Close()

	if len(deliveries) == 0 {
		return nil, nil
	}

	now := time.Now().UTC()
	leaseUntil := now.Add(leaseDuration)

	updateQuery := `
		UPDATE outbox_deliveries
		SET status = 'processing',
		    lease_owner = $1,
		    lease_until = $2,
		    lease_epoch = $3,
		    attempts = $4
		WHERE consumer_name = $5 AND event_id = $6`

	for _, item := range deliveries {
		newEpoch := item.LeaseEpoch + 1
		newAttempts := item.Attempts + 1

		if _, err := tx.Exec(ctx, updateQuery, workerID, leaseUntil, newEpoch, newAttempts, item.ConsumerName, item.EventID); err != nil {
			return nil, fmt.Errorf("update claimed delivery %s: %w", item.EventID, err)
		}

		item.Status = DeliveryStatusProcessing
		item.LeaseOwner = &workerID
		item.LeaseUntil = &leaseUntil
		item.LeaseEpoch = newEpoch
		item.Attempts = newAttempts
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit delivery claims: %w", err)
	}

	return deliveries, nil
}

// CompleteDelivery acknowledges a successful delivery using CAS on workerID and leaseEpoch.
func (d *Dispatcher) CompleteDelivery(ctx context.Context, consumerName string, eventID uuid.UUID, workerID string, leaseEpoch int64) (bool, error) {
	query := `
		UPDATE outbox_deliveries
		SET status = 'delivered',
		    delivered_at = NOW(),
		    lease_owner = NULL,
		    lease_until = NULL,
		    last_error_code = NULL
		WHERE consumer_name = $1
		  AND event_id = $2
		  AND lease_owner = $3
		  AND lease_epoch = $4`

	tag, err := d.pool.Exec(ctx, query, consumerName, eventID, workerID, leaseEpoch)
	if err != nil {
		return false, fmt.Errorf("complete delivery: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// FailDelivery records a failure, backing off next_attempt_at or quarantining if attempts >= MaxDeliveryAttempts.
func (d *Dispatcher) FailDelivery(ctx context.Context, consumerName string, eventID uuid.UUID, workerID string, leaseEpoch int64, currentAttempts int, errCode string, nextAttemptAt time.Time) (bool, error) {
	if currentAttempts >= MaxDeliveryAttempts {
		return d.QuarantineDelivery(ctx, consumerName, eventID, workerID, leaseEpoch, fmt.Sprintf("EXHAUSTED_%s", errCode))
	}

	query := `
		UPDATE outbox_deliveries
		SET status = 'pending',
		    next_attempt_at = $5,
		    lease_owner = NULL,
		    lease_until = NULL,
		    last_error_code = $6
		WHERE consumer_name = $1
		  AND event_id = $2
		  AND lease_owner = $3
		  AND lease_epoch = $4`

	tag, err := d.pool.Exec(ctx, query, consumerName, eventID, workerID, leaseEpoch, nextAttemptAt, errCode)
	if err != nil {
		return false, fmt.Errorf("fail delivery: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// QuarantineDelivery isolates an invalid or poisoned event delivery so it does not block others.
func (d *Dispatcher) QuarantineDelivery(ctx context.Context, consumerName string, eventID uuid.UUID, workerID string, leaseEpoch int64, reason string) (bool, error) {
	query := `
		UPDATE outbox_deliveries
		SET status = 'quarantined',
		    lease_owner = NULL,
		    lease_until = NULL,
		    last_error_code = $5
		WHERE consumer_name = $1
		  AND event_id = $2
		  AND lease_owner = $3
		  AND lease_epoch = $4`

	tag, err := d.pool.Exec(ctx, query, consumerName, eventID, workerID, leaseEpoch, reason)
	if err != nil {
		return false, fmt.Errorf("quarantine delivery: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// SweepInterrupted resets expired processing leases back to pending with backoff.
func (d *Dispatcher) SweepInterrupted(ctx context.Context, consumerName string, limit int, backoff time.Duration) (int, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `
		WITH expired AS (
			SELECT consumer_name, event_id
			FROM outbox_deliveries
			WHERE consumer_name = $1
			  AND status = 'processing'
			  AND lease_until < NOW()
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE outbox_deliveries od
		SET status = 'pending',
		    lease_owner = NULL,
		    lease_until = NULL,
		    next_attempt_at = NOW() + ($3 * INTERVAL '1 millisecond'),
		    last_error_code = 'INTERRUPTED'
		FROM expired
		WHERE od.consumer_name = expired.consumer_name
		  AND od.event_id = expired.event_id`

	tag, err := d.pool.Exec(ctx, query, consumerName, limit, backoff.Milliseconds())
	if err != nil {
		return 0, fmt.Errorf("sweep interrupted deliveries: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
