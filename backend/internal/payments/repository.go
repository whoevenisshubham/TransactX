package payments

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/transactx/backend/internal/accounts"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/ledger"
)

var ErrNotFound = errors.New("payment not found")
var ErrIdempotencyConflict = errors.New("idempotency key reused with a different request")
var ErrInsufficientFunds = errors.New("insufficient funds")

type Repository struct {
	db                             *pgxpool.Pool
	adapterResolver                func(ctx context.Context, payment Payment) (bank.BankAdapter, bank.BankAdapter, bool)
	getIdempotentFn                func(ctx context.Context, userID uuid.UUID, key, requestHash string) (Payment, bool, error)
	recoverRoutedFn                func(ctx context.Context, payment Payment, sourceAdapter, destinationAdapter bank.BankAdapter) error
	getFn                          func(ctx context.Context, id uuid.UUID) (Payment, error)
	getSelectedExecutionTargetIDFn func(ctx context.Context, paymentID uuid.UUID) (string, bool, error)
}

func NewRepository(db *pgxpool.Pool) *Repository { return &Repository{db: db} }

func (repository *Repository) SetAdapterResolver(fn func(ctx context.Context, payment Payment) (bank.BankAdapter, bank.BankAdapter, bool)) {
	repository.adapterResolver = fn
}

func (repository *Repository) SetSelectedExecutionTargetIDFn(fn func(ctx context.Context, paymentID uuid.UUID) (string, bool, error)) {
	repository.getSelectedExecutionTargetIDFn = fn
}

func (repository *Repository) SetMockHooks(
	getIdempotent func(ctx context.Context, userID uuid.UUID, key, requestHash string) (Payment, bool, error),
	recoverRouted func(ctx context.Context, payment Payment, sourceAdapter, destinationAdapter bank.BankAdapter) error,
	get func(ctx context.Context, id uuid.UUID) (Payment, error),
) {
	repository.getIdempotentFn = getIdempotent
	repository.recoverRoutedFn = recoverRouted
	repository.getFn = get
}

const routedPaymentColumns = `id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency,
	state, route_bank_id, source_bank_id, destination_bank_id, source_bank_account_id, destination_bank_account_id,
	failure_reason, created_at, updated_at, completed_at, note, origin`

const routedPaymentSelect = `SELECT ` + routedPaymentColumns

func (repository *Repository) Create(ctx context.Context, payment Payment) (Payment, error) {
	tx, err := repository.db.Begin(ctx)
	if err != nil {
		return Payment{}, err
	}
	defer tx.Rollback(ctx)

	created, err := scanPayment(tx.QueryRow(ctx, `
		INSERT INTO payments
			(id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state, note, origin)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, COALESCE(NULLIF($9, ''), 'ONLINE'))
		RETURNING id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency,
			state, route_bank_id, source_bank_id, destination_bank_id, source_bank_account_id, destination_bank_account_id,
			failure_reason, created_at, updated_at, completed_at, note, origin`,
		payment.ID, payment.InitiatedByUserID, payment.SenderAccountID, payment.ReceiverAccountID,
		payment.AmountPaise, payment.Currency, payment.State, payment.Note, payment.Origin))
	if err != nil {
		return Payment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Payment{}, err
	}
	return created, nil
}

func (repository *Repository) GetIdempotent(ctx context.Context, userID uuid.UUID, key, requestHash string) (Payment, bool, error) {
	if repository.getIdempotentFn != nil {
		return repository.getIdempotentFn(ctx, userID, key, requestHash)
	}
	if repository.db == nil {
		return Payment{}, false, nil
	}
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
			(id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state, note, origin)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, COALESCE(NULLIF($9, ''), 'ONLINE'))
		RETURNING id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency,
			state, route_bank_id, source_bank_id, destination_bank_id, source_bank_account_id, destination_bank_account_id,
			failure_reason, created_at, updated_at, completed_at, note, origin`,
		payment.ID, payment.InitiatedByUserID, payment.SenderAccountID, payment.ReceiverAccountID,
		payment.AmountPaise, payment.Currency, payment.State, payment.Note, payment.Origin))
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

func (repository *Repository) CreateAndSettleIdempotent(ctx context.Context, payment Payment, key, requestHash string, accountRepository *accounts.Repository, ledgerRepository *ledger.Repository) (Payment, bool, error) {
	tx, err := repository.db.Begin(ctx)
	if err != nil {
		return Payment{}, false, err
	}
	defer tx.Rollback(ctx)

	var existingHash string
	var existingPaymentID *uuid.UUID
	err = tx.QueryRow(ctx, `SELECT request_hash, payment_id FROM idempotency_records WHERE user_id = $1 AND key = $2`, payment.InitiatedByUserID, key).Scan(&existingHash, &existingPaymentID)
	if err == nil {
		if existingHash != requestHash {
			return Payment{}, false, ErrIdempotencyConflict
		}
		if existingPaymentID == nil {
			return Payment{}, false, ErrNotFound
		}
		existing, getErr := scanPayment(tx.QueryRow(ctx, `
			SELECT id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency,
				state, route_bank_id, source_bank_id, destination_bank_id, source_bank_account_id, destination_bank_account_id,
				failure_reason, created_at, updated_at, completed_at, note, origin
			FROM payments WHERE id = $1`, *existingPaymentID))
		return existing, true, getErr
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, false, err
	}

	created, err := scanPayment(tx.QueryRow(ctx, `
		INSERT INTO payments
			(id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state, note, origin)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, COALESCE(NULLIF($9, ''), 'ONLINE'))
		RETURNING id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency,
			state, route_bank_id, source_bank_id, destination_bank_id, source_bank_account_id, destination_bank_account_id,
			failure_reason, created_at, updated_at, completed_at, note, origin`,
		payment.ID, payment.InitiatedByUserID, payment.SenderAccountID, payment.ReceiverAccountID,
		payment.AmountPaise, payment.Currency, payment.State, payment.Note, payment.Origin))
	if err != nil {
		return Payment{}, false, err
	}
	if err := repository.settlePayment(ctx, tx, &created, accountRepository, ledgerRepository); err != nil {
		return Payment{}, false, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO idempotency_records (id, user_id, key, request_hash, payment_id, response_snapshot)
		VALUES ($1, $2, $3, $4, $5, jsonb_build_object('paymentId', $6::text, 'state', $7::text))`,
		uuid.New(), created.InitiatedByUserID, key, requestHash, created.ID, created.ID.String(), created.State)
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err == nil {
		return created, false, nil
	}
	if !isUniqueViolation(err) {
		return Payment{}, false, err
	}
	if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
		return Payment{}, false, rollbackErr
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

func (repository *Repository) settlePayment(ctx context.Context, tx pgx.Tx, payment *Payment, accountRepository *accounts.Repository, ledgerRepository *ledger.Repository) error {
	for _, next := range []string{StateValidating, StateLocalSettlement} {
		from := payment.State
		if err := Transition(payment, next); err != nil {
			return err
		}
		if err := repository.RecordTransition(ctx, tx, payment.ID, from, next); err != nil {
			return err
		}
	}
	if err := accountRepository.Debit(ctx, tx, payment.SenderAccountID, payment.AmountPaise); errors.Is(err, accounts.ErrInsufficientFunds) {
		return ErrInsufficientFunds
	} else if err != nil {
		return err
	}
	if err := accountRepository.Credit(ctx, tx, payment.ReceiverAccountID, payment.AmountPaise); err != nil {
		return err
	}
	ledgerTransactionID, err := ledgerRepository.CreateTransaction(ctx, tx, payment.ID)
	if err != nil {
		return err
	}
	if err := ledgerRepository.CreateEntry(ctx, tx, ledger.Entry{
		ID: uuid.New(), LedgerTransactionID: ledgerTransactionID, AccountID: payment.SenderAccountID,
		EntryType: ledger.EntryDebit, AmountPaise: payment.AmountPaise,
	}); err != nil {
		return err
	}
	if err := ledgerRepository.CreateEntry(ctx, tx, ledger.Entry{
		ID: uuid.New(), LedgerTransactionID: ledgerTransactionID, AccountID: payment.ReceiverAccountID,
		EntryType: ledger.EntryCredit, AmountPaise: payment.AmountPaise,
	}); err != nil {
		return err
	}
	from := payment.State
	if err := Transition(payment, StateCommitted); err != nil {
		return err
	}
	if err := repository.RecordTransition(ctx, tx, payment.ID, from, StateCommitted); err != nil {
		return err
	}
	from = payment.State
	if err := Transition(payment, StateCompleted); err != nil {
		return err
	}
	if err := repository.RecordTransition(ctx, tx, payment.ID, from, StateCompleted); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE payments SET state = $2, completed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = $1`, payment.ID, payment.State)
	return err
}

// RecordTransition records one immutable history row in the same transaction as the payment state change.
func (repository *Repository) RecordTransition(ctx context.Context, tx pgx.Tx, paymentID uuid.UUID, fromState, toState string) error {
	if !CanTransition(fromState, toState) {
		return ErrInvalidTransition
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO payment_state_transitions (id, payment_id, from_state, to_state, transitioned_at)
		VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP)
	`, uuid.New(), paymentID, fromState, toState)
	return err
}

// GetTransitions returns the immutable transition history for a payment in deterministic order.
func (repository *Repository) GetTransitions(ctx context.Context, paymentID uuid.UUID) ([]PaymentStateTransition, error) {
	if repository.db == nil {
		return nil, nil
	}
	rows, err := repository.db.Query(ctx, `
		SELECT id, payment_id, from_state, to_state, transitioned_at
		FROM payment_state_transitions
		WHERE payment_id = $1
		ORDER BY transitioned_at ASC, id ASC
	`, paymentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transitions []PaymentStateTransition
	for rows.Next() {
		var tr PaymentStateTransition
		if err := rows.Scan(&tr.ID, &tr.PaymentID, &tr.FromState, &tr.ToState, &tr.TransitionedAt); err != nil {
			return nil, err
		}
		tr.TransitionedAt = tr.TransitionedAt.UTC()
		transitions = append(transitions, tr)
	}
	return transitions, rows.Err()
}

func (repository *Repository) Get(ctx context.Context, paymentID uuid.UUID) (Payment, error) {
	if repository.getFn != nil {
		return repository.getFn(ctx, paymentID)
	}
	if repository.db == nil {
		return Payment{}, ErrNotFound
	}
	return scanPayment(repository.db.QueryRow(ctx, `
		SELECT id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency,
			state, route_bank_id, source_bank_id, destination_bank_id, source_bank_account_id, destination_bank_account_id,
			failure_reason, created_at, updated_at, completed_at, note, origin
		FROM payments WHERE id = $1`, paymentID))
}

func (repository *Repository) ListForUser(ctx context.Context, userID uuid.UUID, limit int) ([]CustomerPayment, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := repository.db.Query(ctx, `
		SELECT p.id, p.amount_paise, p.currency, p.state, p.created_at, p.completed_at,
		       p.note, p.origin, p.failure_reason,
		       sender.name, sender.upi_id, receiver.name, receiver.upi_id,
		       CASE WHEN sender_account.user_id = $1 THEN 'SENT' ELSE 'RECEIVED' END,
		       source_bank.name, source_bank.code, destination_bank.name, destination_bank.code
		FROM payments p
		JOIN accounts sender_account ON sender_account.id = p.sender_account_id
		JOIN users sender ON sender.id = sender_account.user_id
		JOIN accounts receiver_account ON receiver_account.id = p.receiver_account_id
		JOIN users receiver ON receiver.id = receiver_account.user_id
		LEFT JOIN banks source_bank ON source_bank.id = sender_account.bank_id
		LEFT JOIN banks destination_bank ON destination_bank.id = receiver_account.bank_id
		WHERE sender_account.user_id = $1 OR receiver_account.user_id = $1
		ORDER BY p.created_at DESC, p.id DESC
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]CustomerPayment, 0)
	for rows.Next() {
		payment, err := scanCustomerPayment(rows, userID)
		if err != nil {
			return nil, err
		}
		result = append(result, payment)
	}
	return result, rows.Err()
}

func (repository *Repository) GetForUser(ctx context.Context, userID, paymentID uuid.UUID) (CustomerPayment, error) {
	payment, err := scanCustomerPaymentFromRow(repository.db.QueryRow(ctx, `
		SELECT p.id, p.amount_paise, p.currency, p.state, p.created_at, p.completed_at,
		       p.note, p.origin, p.failure_reason,
		       sender.name, sender.upi_id, receiver.name, receiver.upi_id,
		       CASE WHEN sender_account.user_id = $1 THEN 'SENT' ELSE 'RECEIVED' END,
		       source_bank.name, source_bank.code, destination_bank.name, destination_bank.code
		FROM payments p
		JOIN accounts sender_account ON sender_account.id = p.sender_account_id
		JOIN users sender ON sender.id = sender_account.user_id
		JOIN accounts receiver_account ON receiver_account.id = p.receiver_account_id
		JOIN users receiver ON receiver.id = receiver_account.user_id
		LEFT JOIN banks source_bank ON source_bank.id = sender_account.bank_id
		LEFT JOIN banks destination_bank ON destination_bank.id = receiver_account.bank_id
		WHERE (sender_account.user_id = $1 OR receiver_account.user_id = $1) AND p.id = $2`, userID, paymentID), userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CustomerPayment{}, ErrNotFound
	}
	return payment, err
}

type customerPaymentScanner interface {
	Scan(dest ...any) error
}

func scanCustomerPayment(row customerPaymentScanner, userID uuid.UUID) (CustomerPayment, error) {
	return scanCustomerPaymentFromRow(row, userID)
}

func scanCustomerPaymentFromRow(row customerPaymentScanner, userID uuid.UUID) (CustomerPayment, error) {
	var payment CustomerPayment
	var note, sourceBankName, sourceBankCode, destinationBankName, destinationBankCode *string
	if err := row.Scan(
		&payment.ID, &payment.AmountPaise, &payment.Currency, &payment.State, &payment.CreatedAt, &payment.CompletedAt,
		&note, &payment.Origin, &payment.FailureReason, &payment.SenderName, &payment.SenderPaymentID,
		&payment.ReceiverName, &payment.ReceiverPaymentID, &payment.Direction, &sourceBankName, &sourceBankCode,
		&destinationBankName, &destinationBankCode); err != nil {
		return CustomerPayment{}, err
	}
	payment.Note = note
	payment.SourceBankName = sourceBankName
	payment.SourceBankCode = sourceBankCode
	payment.DestinationBankName = destinationBankName
	payment.DestinationBankCode = destinationBankCode
	if payment.CompletedAt != nil {
		duration := payment.CompletedAt.Sub(payment.CreatedAt).Milliseconds()
		if duration < 0 {
			duration = 0
		}
		payment.DurationMs = &duration
	}
	return payment, nil
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
		&payment.SourceBankID, &payment.DestinationBankID, &payment.SourceBankAccountID, &payment.DestinationBankAccountID,
		&payment.FailureReason, &payment.CreatedAt, &payment.UpdatedAt, &payment.CompletedAt,
		&payment.Note, &payment.Origin,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, ErrNotFound
	}
	return payment, err
}
