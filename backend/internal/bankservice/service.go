package bankservice

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/transactx/backend/internal/bank"
)

type Service struct {
	db        *pgxpool.Pool
	available bool
}

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db, available: true} }

func (service *Service) SetAvailable(available bool) { service.available = available }

func (service *Service) GetHealth(context.Context) (bank.HealthResult, error) {
	return bank.HealthResult{Available: service.available}, nil
}

func (service *Service) ResolveAccount(ctx context.Context, request bank.ResolveAccountRequest) (bank.AccountResult, error) {
	if !service.available {
		return bank.AccountResult{}, service.unavailable()
	}
	var status string
	err := service.db.QueryRow(ctx, `SELECT status FROM bank_a.accounts WHERE id = $1`, request.AccountID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return bank.AccountResult{AccountID: request.AccountID, Status: bank.AccountInvalid}, nil
	}
	if err != nil {
		return bank.AccountResult{}, err
	}
	return bank.AccountResult{AccountID: request.AccountID, Status: bank.AccountStatus(status)}, nil
}

func (service *Service) HoldFunds(ctx context.Context, request bank.HoldFundsRequest) (bank.HoldResult, error) {
	if err := service.checkAvailable(); err != nil {
		return bank.HoldResult{}, err
	}
	if err := validateOperation(request.OperationRequest); err != nil {
		return bank.HoldResult{}, err
	}
	tx, err := service.db.Begin(ctx)
	if err != nil {
		return bank.HoldResult{}, err
	}
	defer tx.Rollback(ctx)

	if existing, found, err := service.existingOperation(ctx, tx, request.OperationID); err != nil || found {
		if err != nil {
			return bank.HoldResult{}, err
		}
		return bank.HoldResult{OperationResult: existing, HoldID: request.OperationID}, nil
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO bank_a.operations
			(id, payment_id, operation_id, idempotency_key, operation_type, account_id, amount_paise, currency, status, bank_reference)
		VALUES ($1, $2, $3, $4, 'HOLD', $5, $6, $7, 'ACTIVE', $8)`,
		uuid.New(), request.PaymentID, request.OperationID, request.IdempotencyKey, request.AccountID,
		request.AmountPaise, request.Currency, bankReference(request.OperationID))
	if err != nil {
		return bank.HoldResult{}, err
	}
	result, err := tx.Exec(ctx, `
		UPDATE bank_a.accounts
		SET balance_paise = balance_paise - $2, version = version + 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND status = 'ACTIVE' AND balance_paise >= $2`, request.AccountID, request.AmountPaise)
	if err != nil {
		return bank.HoldResult{}, err
	}
	if result.RowsAffected() != 1 {
		return bank.HoldResult{}, &bank.AdapterError{Code: bank.ErrCodeInsufficientFunds, Message: "bank account has insufficient funds"}
	}
	if err := insertLedgerEntry(ctx, tx, request.OperationRequest, "HOLD"); err != nil {
		return bank.HoldResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return bank.HoldResult{}, err
	}
	return bank.HoldResult{OperationResult: operationResult(request.OperationRequest), HoldID: request.OperationID}, nil
}

func (service *Service) ProvisionalCredit(ctx context.Context, request bank.ProvisionalCreditRequest) (bank.OperationResult, error) {
	if err := service.checkAvailable(); err != nil {
		return bank.OperationResult{}, err
	}
	if err := validateOperation(request.OperationRequest); err != nil {
		return bank.OperationResult{}, err
	}
	tx, err := service.db.Begin(ctx)
	if err != nil {
		return bank.OperationResult{}, err
	}
	defer tx.Rollback(ctx)
	if existing, found, err := service.existingOperation(ctx, tx, request.OperationID); err != nil || found {
		return existing, err
	}
	if err := ensureActiveAccount(ctx, tx, request.AccountID); err != nil {
		return bank.OperationResult{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO bank_a.operations
			(id, payment_id, operation_id, idempotency_key, operation_type, account_id, amount_paise, currency, status, bank_reference)
		VALUES ($1, $2, $3, $4, 'PROVISIONAL_CREDIT', $5, $6, $7, 'PROVISIONAL', $8)`,
		uuid.New(), request.PaymentID, request.OperationID, request.IdempotencyKey, request.AccountID,
		request.AmountPaise, request.Currency, bankReference(request.OperationID))
	if err != nil {
		return bank.OperationResult{}, err
	}
	if err := insertLedgerEntry(ctx, tx, request.OperationRequest, "PROVISIONAL_CREDIT"); err != nil {
		return bank.OperationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return bank.OperationResult{}, err
	}
	return operationResult(request.OperationRequest), nil
}

func (service *Service) ConfirmHold(ctx context.Context, request bank.ConfirmHoldRequest) (bank.OperationResult, error) {
	if err := service.checkAvailable(); err != nil {
		return bank.OperationResult{}, err
	}
	tx, err := service.db.Begin(ctx)
	if err != nil {
		return bank.OperationResult{}, err
	}
	defer tx.Rollback(ctx)
	var paymentID uuid.UUID
	var accountID uuid.UUID
	var amount int64
	var status, operationType string
	err = tx.QueryRow(ctx, `SELECT payment_id, account_id, amount_paise, status, operation_type FROM bank_a.operations WHERE operation_id = $1 AND hold_id IS NULL`, request.HoldID).Scan(&paymentID, &accountID, &amount, &status, &operationType)
	if errors.Is(err, pgx.ErrNoRows) {
		return bank.OperationResult{}, &bank.AdapterError{Code: bank.ErrCodeInvalidAccount, Message: "hold is invalid"}
	}
	if err != nil {
		return bank.OperationResult{}, err
	}
	if paymentID != request.PaymentID {
		return bank.OperationResult{}, &bank.AdapterError{Code: bank.ErrCodePermanentFailure, Message: "hold payment does not match"}
	}
	if status == "CONFIRMED" || status == "FINAL" {
		return operationResultWithStatus(request.PaymentID, request.OperationID, bank.OperationSucceeded), nil
	}
	if status != "ACTIVE" {
		return operationResultWithStatus(request.PaymentID, request.OperationID, bank.OperationFailed), nil
	}
	nextStatus := "CONFIRMED"
	if operationType == "PROVISIONAL_CREDIT" {
		nextStatus = "FINAL"
		if _, err := tx.Exec(ctx, `UPDATE bank_a.accounts SET balance_paise = balance_paise + $2, version = version + 1, updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND status = 'ACTIVE'`, accountID, amount); err != nil {
			return bank.OperationResult{}, err
		}
		if err := insertLedgerEntry(ctx, tx, bank.OperationRequest{PaymentID: request.PaymentID, OperationID: request.OperationID, AccountID: accountID, AmountPaise: amount, Currency: "INR"}, "FINAL_CREDIT"); err != nil {
			return bank.OperationResult{}, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE bank_a.operations SET status = $2, updated_at = CURRENT_TIMESTAMP WHERE operation_id = $1`, request.HoldID, nextStatus); err != nil {
		return bank.OperationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return bank.OperationResult{}, err
	}
	return operationResultWithStatus(request.PaymentID, request.OperationID, bank.OperationSucceeded), nil
}

func (service *Service) ReleaseHold(ctx context.Context, request bank.ReleaseHoldRequest) (bank.OperationResult, error) {
	if err := service.checkAvailable(); err != nil {
		return bank.OperationResult{}, err
	}
	tx, err := service.db.Begin(ctx)
	if err != nil {
		return bank.OperationResult{}, err
	}
	defer tx.Rollback(ctx)
	var paymentID, accountID uuid.UUID
	var amount int64
	var status string
	err = tx.QueryRow(ctx, `SELECT payment_id, account_id, amount_paise, status FROM bank_a.operations WHERE operation_id = $1`, request.HoldID).Scan(&paymentID, &accountID, &amount, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return bank.OperationResult{}, &bank.AdapterError{Code: bank.ErrCodeInvalidAccount, Message: "hold is invalid"}
	}
	if err != nil {
		return bank.OperationResult{}, err
	}
	if paymentID != request.PaymentID || status == "CONFIRMED" {
		return bank.OperationResult{}, &bank.AdapterError{Code: bank.ErrCodePermanentFailure, Message: "hold cannot be released"}
	}
	if status == "ACTIVE" {
		if _, err := tx.Exec(ctx, `UPDATE bank_a.accounts SET balance_paise = balance_paise + $2, version = version + 1, updated_at = CURRENT_TIMESTAMP WHERE id = $1`, accountID, amount); err != nil {
			return bank.OperationResult{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE bank_a.operations SET status = 'RELEASED', updated_at = CURRENT_TIMESTAMP WHERE operation_id = $1`, request.HoldID); err != nil {
			return bank.OperationResult{}, err
		}
		if err := insertLedgerEntry(ctx, tx, bank.OperationRequest{PaymentID: request.PaymentID, OperationID: request.OperationID, AccountID: accountID, AmountPaise: amount, Currency: "INR"}, "RELEASE"); err != nil {
			return bank.OperationResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return bank.OperationResult{}, err
	}
	return operationResultWithStatus(request.PaymentID, request.OperationID, bank.OperationSucceeded), nil
}

func (service *Service) ReverseProvisionalCredit(ctx context.Context, request bank.ReverseCreditRequest) (bank.OperationResult, error) {
	if err := service.checkAvailable(); err != nil {
		return bank.OperationResult{}, err
	}
	tx, err := service.db.Begin(ctx)
	if err != nil {
		return bank.OperationResult{}, err
	}
	defer tx.Rollback(ctx)
	var paymentID, accountID uuid.UUID
	var amount int64
	var status string
	err = tx.QueryRow(ctx, `SELECT payment_id, account_id, amount_paise, status FROM bank_a.operations WHERE operation_id = $1`, request.OriginalOperationID).Scan(&paymentID, &accountID, &amount, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return bank.OperationResult{}, &bank.AdapterError{Code: bank.ErrCodeInvalidAccount, Message: "credit operation is invalid"}
	}
	if err != nil {
		return bank.OperationResult{}, err
	}
	if paymentID != request.PaymentID || status == "REVERSED" {
		return operationResultWithStatus(request.PaymentID, request.OperationID, bank.OperationSucceeded), nil
	}
	if status != "PROVISIONAL" {
		return bank.OperationResult{}, &bank.AdapterError{Code: bank.ErrCodePermanentFailure, Message: "credit is no longer provisional"}
	}
	if _, err := tx.Exec(ctx, `UPDATE bank_a.operations SET status = 'REVERSED', updated_at = CURRENT_TIMESTAMP WHERE operation_id = $1`, request.OriginalOperationID); err != nil {
		return bank.OperationResult{}, err
	}
	if err := insertLedgerEntry(ctx, tx, bank.OperationRequest{PaymentID: request.PaymentID, OperationID: request.OperationID, AccountID: accountID, AmountPaise: amount, Currency: "INR"}, "REVERSE_CREDIT"); err != nil {
		return bank.OperationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return bank.OperationResult{}, err
	}
	return operationResultWithStatus(request.PaymentID, request.OperationID, bank.OperationSucceeded), nil
}

func (service *Service) GetOperationStatus(ctx context.Context, request bank.OperationStatusRequest) (bank.OperationResult, error) {
	var paymentID uuid.UUID
	var operationID uuid.UUID
	var reference, status string
	err := service.db.QueryRow(ctx, `SELECT payment_id, operation_id, bank_reference, status FROM bank_a.operations WHERE operation_id = $1`, request.OperationID).Scan(&paymentID, &operationID, &reference, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return bank.OperationResult{PaymentID: request.PaymentID, OperationID: request.OperationID, Status: bank.OperationPending}, nil
	}
	if err != nil {
		return bank.OperationResult{}, err
	}
	return bank.OperationResult{PaymentID: paymentID, OperationID: operationID, BankReference: reference, Status: operationStatus(status)}, nil
}

func (service *Service) GetLedgerSnapshot(ctx context.Context, scope bank.LedgerScope) (bank.LedgerSnapshot, error) {
	rows, err := service.db.Query(ctx, `SELECT operation_id, payment_id, account_id, entry_type, amount_paise, currency, occurred_at FROM bank_a.ledger_entries WHERE occurred_at >= $1 AND occurred_at < $2 ORDER BY occurred_at, id`, scope.From, scope.To)
	if err != nil {
		return bank.LedgerSnapshot{}, err
	}
	defer rows.Close()
	snapshot := bank.LedgerSnapshot{BankID: "BANK-A", SnapshotID: uuid.New(), CapturedAt: time.Now().UTC()}
	for rows.Next() {
		var entry bank.LedgerEntry
		if err := rows.Scan(&entry.OperationID, &entry.PaymentID, &entry.AccountID, &entry.EntryType, &entry.AmountPaise, &entry.Currency, &entry.OccurredAt); err != nil {
			return bank.LedgerSnapshot{}, err
		}
		snapshot.Entries = append(snapshot.Entries, entry)
	}
	return snapshot, rows.Err()
}

func (service *Service) checkAvailable() error {
	if !service.available {
		return service.unavailable()
	}
	return nil
}

func (service *Service) unavailable() error {
	return &bank.AdapterError{Code: bank.ErrCodeBankUnavailable, Message: "bank A is unavailable"}
}

func validateOperation(request bank.OperationRequest) error {
	if request.PaymentID == uuid.Nil || request.OperationID == uuid.Nil || request.IdempotencyKey == "" || request.AccountID == uuid.Nil || request.AmountPaise <= 0 || request.Currency != "INR" {
		return &bank.AdapterError{Code: bank.ErrCodePermanentFailure, Message: "bank operation is invalid"}
	}
	return nil
}

func ensureActiveAccount(ctx context.Context, tx pgx.Tx, accountID uuid.UUID) error {
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM bank_a.accounts WHERE id = $1 FOR UPDATE`, accountID).Scan(&status); errors.Is(err, pgx.ErrNoRows) {
		return &bank.AdapterError{Code: bank.ErrCodeInvalidAccount, Message: "bank account is invalid"}
	} else if err != nil {
		return err
	} else if status != string(bank.AccountActive) {
		return &bank.AdapterError{Code: bank.ErrCodeInactiveAccount, Message: "bank account is inactive"}
	}
	return nil
}

func (service *Service) existingOperation(ctx context.Context, tx pgx.Tx, operationID uuid.UUID) (bank.OperationResult, bool, error) {
	var result bank.OperationResult
	var status string
	err := tx.QueryRow(ctx, `SELECT payment_id, operation_id, bank_reference, status FROM bank_a.operations WHERE operation_id = $1`, operationID).Scan(&result.PaymentID, &result.OperationID, &result.BankReference, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return bank.OperationResult{}, false, nil
	}
	if err != nil {
		return bank.OperationResult{}, false, err
	}
	result.Status = operationStatus(status)
	return result, true, nil
}

func insertLedgerEntry(ctx context.Context, tx pgx.Tx, request bank.OperationRequest, entryType string) error {
	_, err := tx.Exec(ctx, `INSERT INTO bank_a.ledger_entries (id, operation_id, payment_id, account_id, entry_type, amount_paise, currency) VALUES ($1, $2, $3, $4, $5, $6, $7)`, uuid.New(), request.OperationID, request.PaymentID, request.AccountID, entryType, request.AmountPaise, request.Currency)
	return err
}

func operationResult(request bank.OperationRequest) bank.OperationResult {
	return operationResultWithStatus(request.PaymentID, request.OperationID, bank.OperationSucceeded)
}

func operationResultWithStatus(paymentID, operationID uuid.UUID, status bank.OperationStatus) bank.OperationResult {
	return bank.OperationResult{PaymentID: paymentID, OperationID: operationID, BankReference: bankReference(operationID), Status: status}
}

func operationStatus(status string) bank.OperationStatus {
	switch status {
	case "ACTIVE", "PROVISIONAL":
		return bank.OperationPending
	case "CONFIRMED", "FINAL", "RELEASED", "REVERSED":
		return bank.OperationSucceeded
	case "FAILED":
		return bank.OperationFailed
	default:
		return bank.OperationPending
	}
}

func bankReference(operationID uuid.UUID) string { return fmt.Sprintf("BANK-A-%s", operationID) }

var _ bank.BankAdapter = (*Service)(nil)
