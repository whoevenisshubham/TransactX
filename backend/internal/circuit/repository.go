package circuit

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository persists and queries immutable circuit transition events.
type Repository interface {
	RecordTransition(ctx context.Context, event TransitionEvent) error
	ListRecent(ctx context.Context, targetID string, limit int) ([]TransitionEvent, error)
}

type PostgresRepository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) RecordTransition(ctx context.Context, event TransitionEvent) error {
	if r == nil || r.db == nil {
		return nil
	}
	detailsJSON, err := json.Marshal(event.Details)
	if err != nil {
		detailsJSON = []byte("{}")
	}
	_, err = r.db.Exec(ctx, `
		INSERT INTO circuit_transition_events (
			execution_target_id, previous_state, new_state, reason,
			failure_count, timeout_count, consecutive_successes, active_probes,
			successful_probes, restoration_step, cooldown_duration_ms, rolling_window_ms,
			details, transitioned_at, event_type
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		event.ExecutionTargetID, string(event.PreviousState), string(event.NewState), event.Reason,
		event.FailureCount, event.TimeoutCount, event.ConsecutiveSuccesses, event.ActiveProbes,
		event.SuccessfulProbes, event.RestorationStep, event.CooldownDurationMs, event.RollingWindowMs,
		detailsJSON, event.TransitionedAt, "CIRCUIT_STATE_TRANSITION",
	)
	return err
}

func (r *PostgresRepository) ListRecent(ctx context.Context, targetID string, limit int) ([]TransitionEvent, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, execution_target_id, previous_state, new_state, reason,
		       failure_count, timeout_count, consecutive_successes, active_probes,
		       successful_probes, restoration_step, cooldown_duration_ms, rolling_window_ms,
		       details, transitioned_at, event_type
		FROM circuit_transition_events
		WHERE execution_target_id = $1
		ORDER BY transitioned_at DESC, id DESC
		LIMIT $2`, targetID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []TransitionEvent
	for rows.Next() {
		var e TransitionEvent
		var prevState, newState, eventType string
		var detailsBytes []byte
		if err := rows.Scan(
			&e.ID, &e.ExecutionTargetID, &prevState, &newState, &e.Reason,
			&e.FailureCount, &e.TimeoutCount, &e.ConsecutiveSuccesses, &e.ActiveProbes,
			&e.SuccessfulProbes, &e.RestorationStep, &e.CooldownDurationMs, &e.RollingWindowMs,
			&detailsBytes, &e.TransitionedAt, &eventType,
		); err != nil {
			return nil, err
		}
		e.PreviousState = State(prevState)
		e.NewState = State(newState)
		e.EventType = eventType
		if len(detailsBytes) > 0 {
			_ = json.Unmarshal(detailsBytes, &e.Details)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
