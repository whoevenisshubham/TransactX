package chaos

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrRepoUnavailable = errors.New("chaos repository is unavailable")
)

type Repository interface {
	CreateScenarioWithEvent(ctx context.Context, s ChaosScenario, event ChaosEvent) error
	UpdateScenarioWithEvent(ctx context.Context, s ChaosScenario, event ChaosEvent) error
	GetScenario(ctx context.Context, scenarioID string) (*ChaosScenario, error)
	GetActiveScenarioByTarget(ctx context.Context, targetID string, now time.Time) (*ChaosScenario, error)
	ListActiveScenarios(ctx context.Context, now time.Time) ([]ChaosScenario, error)
	ListScenarios(ctx context.Context, activeOnly bool, now time.Time, limit int) ([]ChaosScenario, error)
	ExpireScenario(ctx context.Context, scenarioID string, now time.Time, event ChaosEvent) (bool, error)
	ResetScenarios(ctx context.Context, targetID string, stoppedAt time.Time, stoppedBy string, actorID, actorRole string) ([]ChaosScenario, error)
	RecordEvent(ctx context.Context, event ChaosEvent) error
	ListEvents(ctx context.Context, scenarioID string, limit int) ([]ChaosEvent, error)
}

type PostgresRepository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) CreateScenarioWithEvent(ctx context.Context, s ChaosScenario, event ChaosEvent) error {
	if r == nil || r.db == nil {
		return ErrRepoUnavailable
	}
	paramsJSON, err := json.Marshal(s.Parameters)
	if err != nil {
		paramsJSON = []byte("{}")
	}

	eventParamsJSON, err := json.Marshal(event.Parameters)
	if err != nil {
		eventParamsJSON = []byte("{}")
	}
	eventDetailsJSON, err := json.Marshal(event.Details)
	if err != nil {
		eventDetailsJSON = []byte("{}")
	}

	occurred := event.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now()
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		INSERT INTO chaos_scenarios (
			scenario_id, scenario_type, target_id, parameters,
			started_at, expires_at, stopped_at, active, mode,
			created_by, stopped_by, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		s.ScenarioID, string(s.Type), s.TargetID, paramsJSON,
		s.StartedAt, s.ExpiresAt, s.StoppedAt, s.Active, s.Mode,
		s.CreatedBy, s.StoppedBy, s.CreatedAt, s.UpdatedAt,
	)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO chaos_events (
			scenario_id, event_type, target_id, fault_type,
			actor_id, actor_role, parameters, details, occurred_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		event.ScenarioID, string(event.EventType), event.TargetID, event.FaultType,
		event.ActorID, event.ActorRole, eventParamsJSON, eventDetailsJSON, occurred,
	)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *PostgresRepository) UpdateScenarioWithEvent(ctx context.Context, s ChaosScenario, event ChaosEvent) error {
	if r == nil || r.db == nil {
		return ErrRepoUnavailable
	}
	paramsJSON, err := json.Marshal(s.Parameters)
	if err != nil {
		paramsJSON = []byte("{}")
	}

	eventParamsJSON, err := json.Marshal(event.Parameters)
	if err != nil {
		eventParamsJSON = []byte("{}")
	}
	eventDetailsJSON, err := json.Marshal(event.Details)
	if err != nil {
		eventDetailsJSON = []byte("{}")
	}

	occurred := event.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now()
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
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
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrScenarioNotFound
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO chaos_events (
			scenario_id, event_type, target_id, fault_type,
			actor_id, actor_role, parameters, details, occurred_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		event.ScenarioID, string(event.EventType), event.TargetID, event.FaultType,
		event.ActorID, event.ActorRole, eventParamsJSON, eventDetailsJSON, occurred,
	)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *PostgresRepository) ExpireScenario(ctx context.Context, scenarioID string, now time.Time, event ChaosEvent) (bool, error) {
	if r == nil || r.db == nil {
		return false, ErrRepoUnavailable
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var targetID string
	var faultType string
	err = tx.QueryRow(ctx, `
		UPDATE chaos_scenarios SET
			active = false,
			stopped_at = $1,
			stopped_by = 'SYSTEM_AUTO_EXPIRY',
			updated_at = $2
		WHERE scenario_id = $3 AND active = true AND expires_at <= $2
		RETURNING target_id, scenario_type`,
		event.OccurredAt, now, scenarioID,
	).Scan(&targetID, &faultType)

	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	eventDetailsJSON, _ := json.Marshal(event.Details)
	if len(eventDetailsJSON) == 0 {
		eventDetailsJSON = []byte("{}")
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO chaos_events (
			scenario_id, event_type, target_id, fault_type,
			actor_id, actor_role, parameters, details, occurred_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		scenarioID, string(EventTypeChaosExpired), targetID, faultType,
		"SYSTEM", "SYSTEM", []byte("{}"), eventDetailsJSON, event.OccurredAt,
	)
	if err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (r *PostgresRepository) ResetScenarios(ctx context.Context, targetID string, stoppedAt time.Time, stoppedBy string, actorID, actorRole string) ([]ChaosScenario, error) {
	if r == nil || r.db == nil {
		return nil, ErrRepoUnavailable
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var rows pgx.Rows
	if targetID != "" {
		rows, err = tx.Query(ctx, `
			UPDATE chaos_scenarios SET
				active = false,
				stopped_at = $1,
				stopped_by = $2,
				updated_at = $1
			WHERE target_id = $3 AND active = true
			RETURNING id, scenario_id, scenario_type, target_id, parameters,
			          started_at, expires_at, stopped_at, active, mode,
			          created_by, stopped_by, created_at, updated_at`,
			stoppedAt, stoppedBy, targetID,
		)
	} else {
		rows, err = tx.Query(ctx, `
			UPDATE chaos_scenarios SET
				active = false,
				stopped_at = $1,
				stopped_by = $2,
				updated_at = $1
			WHERE active = true
			RETURNING id, scenario_id, scenario_type, target_id, parameters,
			          started_at, expires_at, stopped_at, active, mode,
			          created_by, stopped_by, created_at, updated_at`,
			stoppedAt, stoppedBy,
		)
	}
	if err != nil {
		return nil, err
	}

	var resetList []ChaosScenario
	for rows.Next() {
		var s ChaosScenario
		var st string
		var paramsBytes []byte
		if err := rows.Scan(
			&s.ID, &s.ScenarioID, &st, &s.TargetID, &paramsBytes,
			&s.StartedAt, &s.ExpiresAt, &s.StoppedAt, &s.Active, &s.Mode,
			&s.CreatedBy, &s.StoppedBy, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			rows.Close()
			return nil, err
		}
		s.Type = ScenarioType(st)
		if len(paramsBytes) > 0 {
			_ = json.Unmarshal(paramsBytes, &s.Parameters)
		}
		resetList = append(resetList, s)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, rows.Err()
	}

	for _, s := range resetList {
		detailsJSON, _ := json.Marshal(map[string]any{
			"resetAt": stoppedAt.Format(time.RFC3339),
		})
		_, err = tx.Exec(ctx, `
			INSERT INTO chaos_events (
				scenario_id, event_type, target_id, fault_type,
				actor_id, actor_role, parameters, details, occurred_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			s.ScenarioID, string(EventTypeChaosReset), s.TargetID, string(s.Type),
			actorID, actorRole, []byte("{}"), detailsJSON, stoppedAt,
		)
		if err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return resetList, nil
}

func (r *PostgresRepository) GetScenario(ctx context.Context, scenarioID string) (*ChaosScenario, error) {
	if r == nil || r.db == nil {
		return nil, ErrRepoUnavailable
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

func (r *PostgresRepository) GetActiveScenarioByTarget(ctx context.Context, targetID string, now time.Time) (*ChaosScenario, error) {
	if r == nil || r.db == nil {
		return nil, ErrRepoUnavailable
	}
	row := r.db.QueryRow(ctx, `
		SELECT id, scenario_id, scenario_type, target_id, parameters,
		       started_at, expires_at, stopped_at, active, mode,
		       created_by, stopped_by, created_at, updated_at
		FROM chaos_scenarios
		WHERE target_id = $1 AND active = true AND expires_at > $2
		ORDER BY started_at DESC
		LIMIT 1`, targetID, now)

	var s ChaosScenario
	var st string
	var paramsBytes []byte
	err := row.Scan(
		&s.ID, &s.ScenarioID, &st, &s.TargetID, &paramsBytes,
		&s.StartedAt, &s.ExpiresAt, &s.StoppedAt, &s.Active, &s.Mode,
		&s.CreatedBy, &s.StoppedBy, &s.CreatedAt, &s.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
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

func (r *PostgresRepository) ListActiveScenarios(ctx context.Context, now time.Time) ([]ChaosScenario, error) {
	if r == nil || r.db == nil {
		return nil, ErrRepoUnavailable
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, scenario_id, scenario_type, target_id, parameters,
		       started_at, expires_at, stopped_at, active, mode,
		       created_by, stopped_by, created_at, updated_at
		FROM chaos_scenarios
		WHERE active = true AND expires_at > $1
		ORDER BY started_at DESC`, now)
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

func (r *PostgresRepository) ListScenarios(ctx context.Context, activeOnly bool, now time.Time, limit int) ([]ChaosScenario, error) {
	if r == nil || r.db == nil {
		return nil, ErrRepoUnavailable
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
		query += `WHERE active = true AND expires_at > $1 ORDER BY started_at DESC LIMIT $2`
		rows, err = r.db.Query(ctx, query, now, limit)
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
		return ErrRepoUnavailable
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
		return nil, ErrRepoUnavailable
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
