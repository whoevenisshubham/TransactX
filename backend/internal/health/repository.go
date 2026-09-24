package health

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ db *pgxpool.Pool }

func NewRepository(db *pgxpool.Pool) *Repository { return &Repository{db: db} }

func (repository *Repository) Record(ctx context.Context, sample HealthSample) error {
	_, err := repository.db.Exec(ctx, `INSERT INTO health_samples (target_id, sampled_at, available, latency_ms, outcome, correlation_id) VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))`, sample.TargetID, sample.SampledAt, sample.Available, sample.Latency.Milliseconds(), sample.Outcome, sample.CorrelationID)
	return err
}

func (repository *Repository) ListRecent(ctx context.Context, targetID string, from, to time.Time, limit int) ([]HealthSample, error) {
	rows, err := repository.db.Query(ctx, `SELECT target_id, sampled_at, available, latency_ms, outcome, COALESCE(correlation_id, '') FROM health_samples WHERE target_id = $1 AND sampled_at >= $2 AND sampled_at <= $3 ORDER BY sampled_at DESC, id DESC LIMIT $4`, targetID, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]HealthSample, 0)
	for rows.Next() {
		var sample HealthSample
		var latencyMS int64
		if err := rows.Scan(&sample.TargetID, &sample.SampledAt, &sample.Available, &latencyMS, &sample.Outcome, &sample.CorrelationID); err != nil {
			return nil, err
		}
		sample.Latency = time.Duration(latencyMS) * time.Millisecond
		result = append(result, sample)
	}
	return result, rows.Err()
}
