package chaos

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	CreateScenario(ctx context.Context, s ChaosScenario) error
	UpdateScenario(ctx context.Context, s ChaosScenario) error
	GetScenario(ctx context.Context, scenarioID string) (*ChaosScenario, error)
	ListScenarios(ctx context.Context, activeOnly bool, limit int) ([]ChaosScenario, error)
	RecordEvent(ctx context.Context, event ChaosEvent) error
	ListEvents(ctx context.Context, scenarioID string, limit int) ([]ChaosEvent, error)
}

type PostgresRepository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) CreateScenario(ctx context.Context, s ChaosScenario) error {
	if r == nil || r.db == nil {
		return nil
	}
	paramsJSON, err := json.Marshal(s.Parameters)
	if err != nil {
		paramsJSON = []byte("{}")
	}

	_, err = r.db.Exec(ctx, `
		INSERT INTO chaos_scenarios (
			scenario_id, scenario_type, target_id, parameters,
			started_at, expires_at, stopped_at, active, mode,
			created_by, stopped_by, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		s.ScenarioID, string(s.Type), s.TargetID, paramsJSON,
		s.StartedAt, s.ExpiresAt, s.StoppedAt, s.Active, s.Mode,
		s.CreatedBy, s.StoppedBy, s.CreatedAt, s.UpdatedAt,
	)
	return err
}

func (r *PostgresRepository) UpdateScenario(ctx context.Context, s ChaosScenario) error {
	if r == nil || r.db == nil {
		return nil
	}
	paramsJSON, err := json.Marshal(s.Parameters)
	if err != nil {
		paramsJSON = []byte("{}")
	}

	_, err = r.db.Exec(ctx, `
		UPDATE chaos_scenarios SET
			parameters = $1,
			expires_at = $2,
			stopped_at = $3,
			active = $4,
			stopped_by = $5,
			updated_at = $6
		WHERE scenario_id = $7`,
		paramsJSON, s.ExpiresAt, s.StoppedAt, s.Active, s.StoppedBy, s.UpdatedAt, s.ScenarioID,
	)
	return err
}

func (r *PostgresRepository) GetScenario(ctx context.Context, scenarioID string) (*ChaosScenario, error) {
	if r == nil || r.db == nil {
		return nil, ErrScenarioNotFound
	}
	row := r.db.QueryRow(ctx, `
		SELECT id, scenario_id, scenario_type, target_id, parameters,
		       started_at, expires_at, stopped_at, active, mode,
		       created_by, stopped_by, created_at, updated_at
		FROM chaos_scenarios
		WHERE scenario_id = $1`, scenarioID)

	var s ChaosScenario
	var st string
	var paramsBytes []byte
	err := row.Scan(
		&s.ID, &s.ScenarioID, &st, &s.TargetID, &paramsBytes,
		&s.StartedAt, &s.ExpiresAt, &s.StoppedAt, &s.Active, &s.Mode,
		&s.CreatedBy, &s.StoppedBy, &s.CreatedAt, &s.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrScenarioNotFound
	}
	if err != nil {
		return nil, err
	}
	s.Type = ScenarioType(st)
	if len(paramsBytes) > 0 {
		_ = json.Unmarshal(paramsBytes, &s.Parameters)
	}
	return &s, nil
}

func (r *PostgresRepository) ListScenarios(ctx context.Context, activeOnly bool, limit int) ([]ChaosScenario, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	query := `
		SELECT id, scenario_id, scenario_type, target_id, parameters,
		       started_at, expires_at, stopped_at, active, mode,
		       created_by, stopped_by, created_at, updated_at
		FROM chaos_scenarios `
	var rows pgx.Rows
	var err error

	if activeOnly {
		query += `WHERE active = true ORDER BY started_at DESC LIMIT $1`
		rows, err = r.db.Query(ctx, query, limit)
	} else {
		query += `ORDER BY started_at DESC LIMIT $1`
		rows, err = r.db.Query(ctx, query, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var scenarios []ChaosScenario
	for rows.Next() {
		var s ChaosScenario
		var st string
		var paramsBytes []byte
		if err := rows.Scan(
			&s.ID, &s.ScenarioID, &st, &s.TargetID, &paramsBytes,
			&s.StartedAt, &s.ExpiresAt, &s.StoppedAt, &s.Active, &s.Mode,
			&s.CreatedBy, &s.StoppedBy, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		s.Type = ScenarioType(st)
		if len(paramsBytes) > 0 {
			_ = json.Unmarshal(paramsBytes, &s.Parameters)
		}
		scenarios = append(scenarios, s)
	}
	return scenarios, rows.Err()
}

func (r *PostgresRepository) RecordEvent(ctx context.Context, event ChaosEvent) error {
	if r == nil || r.db == nil {
		return nil
	}
	paramsJSON, err := json.Marshal(event.Parameters)
	if err != nil {
		paramsJSON = []byte("{}")
	}
	detailsJSON, err := json.Marshal(event.Details)
	if err != nil {
		detailsJSON = []byte("{}")
	}

	occurred := event.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now()
	}

	_, err = r.db.Exec(ctx, `
		INSERT INTO chaos_events (
			scenario_id, event_type, target_id, fault_type,
			actor_id, actor_role, parameters, details, occurred_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		event.ScenarioID, string(event.EventType), event.TargetID, event.FaultType,
		event.ActorID, event.ActorRole, paramsJSON, detailsJSON, occurred,
	)
	return err
}

func (r *PostgresRepository) ListEvents(ctx context.Context, scenarioID string, limit int) ([]ChaosEvent, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	rows, err := r.db.Query(ctx, `
		SELECT id, scenario_id, event_type, target_id, fault_type,
		       actor_id, actor_role, parameters, details, occurred_at
		FROM chaos_events
		WHERE scenario_id = $1
		ORDER BY occurred_at DESC, id DESC
		LIMIT $2`, scenarioID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []ChaosEvent
	for rows.Next() {
		var e ChaosEvent
		var et string
		var paramsBytes, detailsBytes []byte
		if err := rows.Scan(
			&e.ID, &e.ScenarioID, &et, &e.TargetID, &e.FaultType,
			&e.ActorID, &e.ActorRole, &paramsBytes, &detailsBytes, &e.OccurredAt,
		); err != nil {
			return nil, err
		}
		e.EventType = EventType(et)
		if len(paramsBytes) > 0 {
			_ = json.Unmarshal(paramsBytes, &e.Parameters)
		}
		if len(detailsBytes) > 0 {
			_ = json.Unmarshal(detailsBytes, &e.Details)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
