package audit

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Entry struct {
	ID         uuid.UUID
	ActorID    *uuid.UUID
	ActorRole  string
	Operation  string
	EntityType string
	EntityID   string
	ReasonCode string
	Details    map[string]any
	RequestID  string
	TraceID    string
	Result     string
	CreatedAt  time.Time
}

type Store interface {
	Record(ctx context.Context, entry Entry) error
	ListRecent(ctx context.Context, limit int) ([]Entry, error)
}

type PostgresStore struct {
	db *pgxpool.Pool
}

func NewPostgresStore(db *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) Record(ctx context.Context, entry Entry) error {
	if s == nil || s.db == nil {
		return nil
	}
	if entry.Operation == "" || entry.ActorRole == "" || entry.Result == "" {
		return errors.New("audit entry missing required fields")
	}
	detailsJSON, err := json.Marshal(entry.Details)
	if err != nil || len(detailsJSON) == 0 {
		detailsJSON = []byte("{}")
	}

	query := `
		INSERT INTO operational_audit_logs (
			actor_id, actor_role, operation, entity_type, entity_id,
			reason_code, details, request_id, trace_id, result
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at`

	return s.db.QueryRow(ctx, query,
		entry.ActorID,
		entry.ActorRole,
		entry.Operation,
		entry.EntityType,
		entry.EntityID,
		entry.ReasonCode,
		detailsJSON,
		entry.RequestID,
		entry.TraceID,
		entry.Result,
	).Scan(&entry.ID, &entry.CreatedAt)
}

func (s *PostgresStore) ListRecent(ctx context.Context, limit int) ([]Entry, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	query := `
		SELECT id, actor_id, actor_role, operation, entity_type, entity_id,
		       reason_code, details, COALESCE(request_id, ''), COALESCE(trace_id, ''), result, created_at
		FROM operational_audit_logs
		ORDER BY created_at DESC
		LIMIT $1`

	rows, err := s.db.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		var rawDetails []byte
		if err := rows.Scan(
			&e.ID, &e.ActorID, &e.ActorRole, &e.Operation, &e.EntityType, &e.EntityID,
			&e.ReasonCode, &rawDetails, &e.RequestID, &e.TraceID, &e.Result, &e.CreatedAt,
		); err != nil {
			return nil, err
		}
		if len(rawDetails) > 0 {
			_ = json.Unmarshal(rawDetails, &e.Details)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
