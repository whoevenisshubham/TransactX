package payments

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("payment not found")

type Repository struct{ db *pgxpool.Pool }

func NewRepository(db *pgxpool.Pool) *Repository { return &Repository{db: db} }

func (repository *Repository) Create(ctx context.Context, payment Payment) (Payment, error) {
	tx, err := repository.db.Begin(ctx)
	if err != nil {
		return Payment{}, err
	}
	defer tx.Rollback(ctx)

	created, err := scanPayment(tx.QueryRow(ctx, `
		INSERT INTO payments
			(id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency,
			state, route_bank_id, failure_reason, created_at, updated_at, completed_at`,
		payment.ID, payment.InitiatedByUserID, payment.SenderAccountID, payment.ReceiverAccountID,
		payment.AmountPaise, payment.Currency, payment.State))
	if err != nil {
		return Payment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Payment{}, err
	}
	return created, nil
}

func scanPayment(row pgx.Row) (Payment, error) {
	var payment Payment
	err := row.Scan(
		&payment.ID, &payment.InitiatedByUserID, &payment.SenderAccountID, &payment.ReceiverAccountID,
		&payment.AmountPaise, &payment.Currency, &payment.State, &payment.RouteBankID,
		&payment.FailureReason, &payment.CreatedAt, &payment.UpdatedAt, &payment.CompletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, ErrNotFound
	}
	return payment, err
}
