package investigator

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type geminiAdmission struct{ pool *pgxpool.Pool }

var ErrGeminiDisabled = errors.New("Gemini generation disabled by owner")

func (a geminiAdmission) reserve(ctx context.Context, owner uuid.UUID, kind, model string) (uuid.UUID, error) {
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)
	// Lock with the emergency-stop update: a completed disable blocks all later
	// admissions across API/worker restarts, without refunding prior attempts.
	var disabled bool
	if err := tx.QueryRow(ctx, `SELECT disabled FROM gemini_free_admission WHERE singleton FOR UPDATE`).Scan(&disabled); err != nil {
		return uuid.Nil, err
	}
	if disabled {
		return uuid.Nil, ErrGeminiDisabled
	}
	var acquired bool
	err = tx.QueryRow(ctx, `UPDATE gemini_free_admission SET lease_owner=$1,busy_until=CURRENT_TIMESTAMP+INTERVAL '60 seconds' WHERE singleton AND (busy_until<=CURRENT_TIMESTAMP OR lease_owner=$1) RETURNING TRUE`, owner).Scan(&acquired)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrProviderRateLimited
	}
	if err != nil {
		return uuid.Nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO gemini_free_daily(day,attempts) VALUES((CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date,0) ON CONFLICT DO NOTHING`); err != nil {
		return uuid.Nil, err
	}
	var attempts int
	err = tx.QueryRow(ctx, `UPDATE gemini_free_daily SET attempts=attempts+1 WHERE day=(CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date AND attempts<10 RETURNING attempts`).Scan(&attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrProviderRateLimited
	}
	if err != nil {
		return uuid.Nil, err
	}
	id := uuid.New()
	if _, err = tx.Exec(ctx, `INSERT INTO gemini_free_attempts(id,day,kind,model,status) VALUES($1,(CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date,$2,$3,'pending')`, id, kind, model); err != nil {
		return uuid.Nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}
func (a geminiAdmission) finish(id uuid.UUID, status, requestID string, usage Usage) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	tag, err := a.pool.Exec(ctx, `UPDATE gemini_free_attempts SET completed_at=CURRENT_TIMESTAMP,status=$2,request_id=NULLIF($3,''),input_tokens=$4,output_tokens=$5 WHERE id=$1 AND status='pending'`, id, status, requestID, usage.InputTokens, usage.OutputTokens)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrProviderUsageUncertain
	}
	return nil
}
func (a geminiAdmission) release(owner uuid.UUID) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := a.pool.Exec(ctx, `UPDATE gemini_free_admission SET lease_owner=NULL,busy_until='epoch' WHERE singleton AND lease_owner=$1`, owner)
	return err
}
