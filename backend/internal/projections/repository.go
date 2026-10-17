package projections

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) GetWorklistItem(ctx context.Context, paymentID uuid.UUID) (*WorklistItem, error) {
	query := `
		SELECT payment_id, current_state, event_version, amount_paise, currency,
		       source_bank_id, destination_bank_id, route_bank_id,
		       created_at, updated_at, recovery_status, recovery_attempts,
		       last_recovery_reason, last_recovery_at, projected_at
		FROM ops_payment_worklist
		WHERE payment_id = $1`

	var item WorklistItem
	err := r.pool.QueryRow(ctx, query, paymentID).Scan(
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
		&item.RecoveryStatus,
		&item.RecoveryAttempts,
		&item.LastRecoveryReason,
		&item.LastRecoveryAt,
		&item.ProjectedAt,
	)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) ListWorklist(ctx context.Context, stateFilter, recoveryFilter string, limit, offset int) ([]*WorklistItem, int64, error) {
	if limit <= 0 {
		limit = 50
	}

	countQuery := `
		SELECT COUNT(*)
		FROM ops_payment_worklist
		WHERE ($1 = '' OR current_state = $1)
		  AND ($2 = '' OR recovery_status = $2)`

	var total int64
	if err := r.pool.QueryRow(ctx, countQuery, stateFilter, recoveryFilter).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count worklist: %w", err)
	}

	query := `
		SELECT payment_id, current_state, event_version, amount_paise, currency,
		       source_bank_id, destination_bank_id, route_bank_id,
		       created_at, updated_at, recovery_status, recovery_attempts,
		       last_recovery_reason, last_recovery_at, projected_at
		FROM ops_payment_worklist
		WHERE ($1 = '' OR current_state = $1)
		  AND ($2 = '' OR recovery_status = $2)
		ORDER BY updated_at DESC
		LIMIT $3 OFFSET $4`

	rows, err := r.pool.Query(ctx, query, stateFilter, recoveryFilter, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list worklist: %w", err)
	}
	defer rows.Close()

	var items []*WorklistItem
	for rows.Next() {
		var item WorklistItem
		if err := rows.Scan(
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
			&item.RecoveryStatus,
			&item.RecoveryAttempts,
			&item.LastRecoveryReason,
			&item.LastRecoveryAt,
			&item.ProjectedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan worklist item: %w", err)
		}
		items = append(items, &item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

func (r *Repository) CountWorklist(ctx context.Context) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM ops_payment_worklist").Scan(&count)
	return count, err
}
