package payments

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("payment not found")
var ErrIdempotencyConflict = errors.New("idempotency key reused with a different request")

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

func (repository *Repository) GetIdempotent(ctx context.Context, userID uuid.UUID, key, requestHash string) (Payment, bool, error) {
	var record IdempotencyRecord
	var paymentID *uuid.UUID
	err := repository.db.QueryRow(ctx, `
		SELECT key, request_hash, payment_id
		FROM idempotency_records
		WHERE user_id = $1 AND key = $2`, userID, key).Scan(&record.Key, &record.RequestHash, &paymentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, false, nil
	}
	if err != nil {
		return Payment{}, false, err
	}
	if record.RequestHash != requestHash {
		return Payment{}, false, ErrIdempotencyConflict
	}
	if paymentID == nil {
		return Payment{}, false, ErrNotFound
	}
	record.PaymentID = *paymentID
	payment, err := repository.Get(ctx, record.PaymentID)
	return payment, true, err
}

func (repository *Repository) CreateIdempotent(ctx context.Context, payment Payment, key, requestHash string) (Payment, bool, error) {
	tx, err := repository.db.Begin(ctx)
	if err != nil {
		return Payment{}, false, err
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
		return Payment{}, false, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO idempotency_records (id, user_id, key, request_hash, payment_id, response_snapshot)
		VALUES ($1, $2, $3, $4, $5, jsonb_build_object('paymentId', $6::text, 'state', $7::text))`,
		uuid.New(), payment.InitiatedByUserID, key, requestHash, created.ID, created.ID.String(), created.State)
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err == nil {
		return created, false, nil
	}
	if !isUniqueViolation(err) {
		return Payment{}, false, err
	}

	if lookupErr := tx.Rollback(ctx); lookupErr != nil {
		return Payment{}, false, lookupErr
	}
	existing, duplicate, lookupErr := repository.GetIdempotent(ctx, payment.InitiatedByUserID, key, requestHash)
	if lookupErr != nil {
		return Payment{}, false, lookupErr
	}
	if !duplicate {
		return Payment{}, false, err
	}
	return existing, true, nil
}

func (repository *Repository) Get(ctx context.Context, paymentID uuid.UUID) (Payment, error) {
	return scanPayment(repository.db.QueryRow(ctx, `
		SELECT id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency,
			state, route_bank_id, failure_reason, created_at, updated_at, completed_at
		FROM payments WHERE id = $1`, paymentID))
}

func isUniqueViolation(err error) bool {
	var pgError *pgconn.PgError
	return errors.As(err, &pgError) && pgError.Code == "23505"
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
