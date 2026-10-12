package outbox

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store interface {
	GetEvent(ctx context.Context, id uuid.UUID) (*Event, error)
	ListEventsByAggregate(ctx context.Context, aggregateType string, aggregateID uuid.UUID) ([]*Event, error)
	GetDelivery(ctx context.Context, consumerName string, eventID uuid.UUID) (*Delivery, error)
	ListDeliveries(ctx context.Context, consumerName, status string, limit int) ([]*Delivery, error)
	CountPendingDeliveries(ctx context.Context, consumerName string) (int64, error)
}

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) GetEvent(ctx context.Context, id uuid.UUID) (*Event, error) {
	query := `
		SELECT id, event_type, schema_version, aggregate_type, aggregate_id, aggregate_version,
		       occurred_at, payload, traceparent, created_at
		FROM outbox_events
		WHERE id = $1`

	var e Event
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&e.ID,
		&e.EventType,
		&e.SchemaVersion,
		&e.AggregateType,
		&e.AggregateID,
		&e.AggregateVersion,
		&e.OccurredAt,
		&e.Payload,
		&e.Traceparent,
		&e.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get outbox event: %w", err)
	}
	return &e, nil
}

func (s *PostgresStore) ListEventsByAggregate(ctx context.Context, aggregateType string, aggregateID uuid.UUID) ([]*Event, error) {
	query := `
		SELECT id, event_type, schema_version, aggregate_type, aggregate_id, aggregate_version,
		       occurred_at, payload, traceparent, created_at
		FROM outbox_events
		WHERE aggregate_type = $1 AND aggregate_id = $2
		ORDER BY aggregate_version ASC, created_at ASC`

	rows, err := s.pool.Query(ctx, query, aggregateType, aggregateID)
	if err != nil {
		return nil, fmt.Errorf("list outbox events: %w", err)
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(
			&e.ID,
			&e.EventType,
			&e.SchemaVersion,
			&e.AggregateType,
			&e.AggregateID,
			&e.AggregateVersion,
			&e.OccurredAt,
			&e.Payload,
			&e.Traceparent,
			&e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan outbox event: %w", err)
		}
		events = append(events, &e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func (s *PostgresStore) GetDelivery(ctx context.Context, consumerName string, eventID uuid.UUID) (*Delivery, error) {
	query := `
		SELECT consumer_name, event_id, status, attempts, next_attempt_at,
		       lease_owner, lease_until, lease_epoch, delivered_at, last_error_code, created_at
		FROM outbox_deliveries
		WHERE consumer_name = $1 AND event_id = $2`

	var d Delivery
	err := s.pool.QueryRow(ctx, query, consumerName, eventID).Scan(
		&d.ConsumerName,
		&d.EventID,
		&d.Status,
		&d.Attempts,
		&d.NextAttemptAt,
		&d.LeaseOwner,
		&d.LeaseUntil,
		&d.LeaseEpoch,
		&d.DeliveredAt,
		&d.LastErrorCode,
		&d.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get outbox delivery: %w", err)
	}
	return &d, nil
}

func (s *PostgresStore) ListDeliveries(ctx context.Context, consumerName, status string, limit int) ([]*Delivery, error) {
	if limit <= 0 {
		limit = 50
	}
	var query string
	var args []any

	if status != "" {
		query = `
			SELECT consumer_name, event_id, status, attempts, next_attempt_at,
			       lease_owner, lease_until, lease_epoch, delivered_at, last_error_code, created_at
			FROM outbox_deliveries
			WHERE consumer_name = $1 AND status = $2
			ORDER BY next_attempt_at ASC, created_at ASC
			LIMIT $3`
		args = []any{consumerName, status, limit}
	} else {
		query = `
			SELECT consumer_name, event_id, status, attempts, next_attempt_at,
			       lease_owner, lease_until, lease_epoch, delivered_at, last_error_code, created_at
			FROM outbox_deliveries
			WHERE consumer_name = $1
			ORDER BY next_attempt_at ASC, created_at ASC
			LIMIT $2`
		args = []any{consumerName, limit}
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list outbox deliveries: %w", err)
	}
	defer rows.Close()

	var deliveries []*Delivery
	for rows.Next() {
		var d Delivery
		if err := rows.Scan(
			&d.ConsumerName,
			&d.EventID,
			&d.Status,
			&d.Attempts,
			&d.NextAttemptAt,
			&d.LeaseOwner,
			&d.LeaseUntil,
			&d.LeaseEpoch,
			&d.DeliveredAt,
			&d.LastErrorCode,
			&d.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan outbox delivery: %w", err)
		}
		deliveries = append(deliveries, &d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return deliveries, nil
}

func (s *PostgresStore) CountPendingDeliveries(ctx context.Context, consumerName string) (int64, error) {
	query := `
		SELECT COUNT(*)
		FROM outbox_deliveries
		WHERE consumer_name = $1 AND status IN ('pending', 'processing')`

	var count int64
	if err := s.pool.QueryRow(ctx, query, consumerName).Scan(&count); err != nil {
		return 0, fmt.Errorf("count pending deliveries: %w", err)
	}
	return count, nil
}
