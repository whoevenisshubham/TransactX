package bankservice

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/transactx/backend/internal/bank"
)

type Service struct {
	db        *pgxpool.Pool
	bankID    string
	schema    string
	mu        sync.RWMutex
	available bool
	latency   time.Duration
}

func NewService(db *pgxpool.Pool) *Service { return NewParticipantService(db, "BANK-A", "bank_a") }

// NewParticipantService creates the same durable participant implementation
// for an independently owned bank schema. The schema is validated because it
// is interpolated only into identifiers, never into user data.
func NewParticipantService(db *pgxpool.Pool, bankID, schema string) *Service {
	if !validBankID(bankID) || !validSchema(schema) {
		panic("invalid participant bank identity")
	}
	return &Service{db: db, bankID: bankID, schema: schema, available: true}
}

func (service *Service) table(name string) string { return service.schema + "." + name }

func validBankID(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func validSchema(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}

// SetAvailable and SetLatency are process-local test/chaos controls. They do
// not alter durable bank state and are intentionally not exposed as public
// customer operations.
func (service *Service) SetAvailable(available bool) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.available = available
}

func (service *Service) SetLatency(latency time.Duration) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if latency < 0 {
		latency = 0
	}
	service.latency = latency
}

func (service *Service) GetHealth(context.Context) (bank.HealthResult, error) {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return bank.HealthResult{Available: service.available}, nil
}

func (service *Service) ResolveAccount(ctx context.Context, request bank.ResolveAccountRequest) (bank.AccountResult, error) {
	if err := service.before(ctx); err != nil {
		return bank.AccountResult{}, err
	}
	var status string
	err := service.db.QueryRow(ctx, fmt.Sprintf(`SELECT status FROM %s WHERE id = $1`, service.table("accounts")), request.AccountID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return bank.AccountResult{AccountID: request.AccountID, Status: bank.AccountInvalid}, nil
	}
	if err != nil {
		return bank.AccountResult{}, err
	}
	return bank.AccountResult{AccountID: request.AccountID, Status: bank.AccountStatus(status)}, nil
}

func (service *Service) HoldFunds(ctx context.Context, request bank.HoldFundsRequest) (bank.HoldResult, error) {
	if err := service.before(ctx); err != nil {
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

	identity := operationIdentity{request: request.OperationRequest, operationType: "HOLD"}
	if err := service.lockActiveAccount(ctx, tx, request.AccountID); err != nil {
		return bank.HoldResult{}, err
	}
	if existing, found, err := service.existingOperation(ctx, tx, identity); err != nil || found {
		if err != nil {
			return bank.HoldResult{}, err
		}
		return bank.HoldResult{OperationResult: existing, HoldID: request.OperationID}, nil
	}
	result, err := tx.Exec(ctx, fmt.Sprintf(`
		UPDATE %s SET balance_paise = balance_paise - $2, version = version + 1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND balance_paise >= $2`, service.table("accounts")), request.AccountID, request.AmountPaise)
	if err != nil {
		return bank.HoldResult{}, err
	}
	if result.RowsAffected() != 1 {
		return bank.HoldResult{}, &bank.AdapterError{Code: bank.ErrCodeInsufficientFunds, Message: "bank account has insufficient funds"}
	}
	if err := service.insertOperation(ctx, tx, identity, "ACTIVE", uuid.Nil, uuid.Nil); err != nil {
		return bank.HoldResult{}, err
	}
	if err := service.insertLedgerEntry(ctx, tx, request.OperationRequest, "HOLD"); err != nil {
		return bank.HoldResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return bank.HoldResult{}, err
	}
	return bank.HoldResult{OperationResult: operationResult(service, request.OperationRequest), HoldID: request.OperationID}, nil
}

func (service *Service) ProvisionalCredit(ctx context.Context, request bank.ProvisionalCreditRequest) (bank.OperationResult, error) {
	if err := service.before(ctx); err != nil {
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
	identity := operationIdentity{request: request.OperationRequest, operationType: "PROVISIONAL_CREDIT"}
	if err := service.lockActiveAccount(ctx, tx, request.AccountID); err != nil {
		return bank.OperationResult{}, err
	}
	if existing, found, err := service.existingOperation(ctx, tx, identity); err != nil || found {
		return existing, err
	}
	if err := service.insertOperation(ctx, tx, identity, "PROVISIONAL", uuid.Nil, uuid.Nil); err != nil {
		return bank.OperationResult{}, err
	}
	if err := service.insertLedgerEntry(ctx, tx, request.OperationRequest, "PROVISIONAL_CREDIT"); err != nil {
		return bank.OperationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return bank.OperationResult{}, err
	}
	return operationResult(service, bank.OperationRequest{PaymentID: request.PaymentID, OperationID: request.OperationID, IdempotencyKey: request.IdempotencyKey}), nil
}

func (service *Service) ConfirmHold(ctx context.Context, request bank.ConfirmHoldRequest) (bank.OperationResult, error) {
	if err := service.before(ctx); err != nil {
		return bank.OperationResult{}, err
	}
	if request.PaymentID == uuid.Nil || request.OperationID == uuid.Nil || request.IdempotencyKey == "" || request.HoldID == uuid.Nil {
		return bank.OperationResult{}, invalidOperationError()
	}
	tx, err := service.db.Begin(ctx)
	if err != nil {
		return bank.OperationResult{}, err
	}
	defer tx.Rollback(ctx)
	identity := operationIdentity{request: bank.OperationRequest{PaymentID: request.PaymentID, OperationID: request.OperationID, IdempotencyKey: request.IdempotencyKey}, operationType: "CONFIRM_HOLD", holdID: request.HoldID}
	if existing, found, err := service.existingOperation(ctx, tx, identity); err != nil || found {
		return existing, err
	}

	var paymentID, accountID uuid.UUID
	var amount int64
	var currency, status, operationType string
	err = tx.QueryRow(ctx, fmt.Sprintf(`SELECT payment_id, account_id, amount_paise, currency, status, operation_type FROM %s WHERE operation_id = $1 FOR UPDATE`, service.table("operations")), request.HoldID).Scan(&paymentID, &accountID, &amount, &currency, &status, &operationType)
	if errors.Is(err, pgx.ErrNoRows) {
		return bank.OperationResult{}, &bank.AdapterError{Code: bank.ErrCodeInvalidAccount, Message: "hold is invalid"}
	}
	if err != nil {
		return bank.OperationResult{}, err
	}
	if paymentID != request.PaymentID {
		return bank.OperationResult{}, &bank.AdapterError{Code: bank.ErrCodePermanentFailure, Message: "hold payment does not match"}
	}
	if operationType != "HOLD" && operationType != "PROVISIONAL_CREDIT" {
		return bank.OperationResult{}, &bank.AdapterError{Code: bank.ErrCodePermanentFailure, Message: "operation cannot be confirmed"}
	}
	if existing, found, err := service.existingOperation(ctx, tx, identity); err != nil || found {
		return existing, err
	}
	if status != "ACTIVE" && status != "PROVISIONAL" && status != "CONFIRMED" && status != "FINAL" {
		return bank.OperationResult{}, &bank.AdapterError{Code: bank.ErrCodePermanentFailure, Message: "operation is no longer confirmable"}
	}
	if err := service.insertOperation(ctx, tx, identityWithAmount(identity, accountID, amount, currency), "CONFIRMED", request.HoldID, uuid.Nil); err != nil {
		return bank.OperationResult{}, err
	}
	if status == "ACTIVE" {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET status = 'CONFIRMED', updated_at = CURRENT_TIMESTAMP WHERE operation_id = $1`, service.table("operations")), request.HoldID); err != nil {
			return bank.OperationResult{}, err
		}
	} else if status == "PROVISIONAL" {
		if amount > math.MaxInt64 {
			return bank.OperationResult{}, invalidOperationError()
		}
		var balance int64
		if err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT balance_paise FROM %s WHERE id = $1 FOR UPDATE`, service.table("accounts")), accountID).Scan(&balance); err != nil {
			return bank.OperationResult{}, err
		}
		if amount > math.MaxInt64-balance {
			return bank.OperationResult{}, &bank.AdapterError{Code: bank.ErrCodePermanentFailure, Message: "bank account balance overflow"}
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET balance_paise = balance_paise + $2, version = version + 1, updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND status = 'ACTIVE'`, service.table("accounts")), accountID, amount); err != nil {
			return bank.OperationResult{}, err
		}
		if err := service.insertLedgerEntry(ctx, tx, bank.OperationRequest{PaymentID: request.PaymentID, OperationID: request.OperationID, AccountID: accountID, AmountPaise: amount, Currency: currency}, "FINAL_CREDIT"); err != nil {
			return bank.OperationResult{}, err
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET status = 'FINAL', updated_at = CURRENT_TIMESTAMP WHERE operation_id = $1`, service.table("operations")), request.HoldID); err != nil {
			return bank.OperationResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return bank.OperationResult{}, err
	}
	return operationResult(service, bank.OperationRequest{PaymentID: request.PaymentID, OperationID: request.OperationID, IdempotencyKey: request.IdempotencyKey}), nil
}

func (service *Service) ReleaseHold(ctx context.Context, request bank.ReleaseHoldRequest) (bank.OperationResult, error) {
	if err := service.before(ctx); err != nil {
		return bank.OperationResult{}, err
	}
	if request.PaymentID == uuid.Nil || request.OperationID == uuid.Nil || request.IdempotencyKey == "" || request.HoldID == uuid.Nil {
		return bank.OperationResult{}, invalidOperationError()
	}
	tx, err := service.db.Begin(ctx)
	if err != nil {
		return bank.OperationResult{}, err
	}
	defer tx.Rollback(ctx)
	identity := operationIdentity{request: bank.OperationRequest{PaymentID: request.PaymentID, OperationID: request.OperationID, IdempotencyKey: request.IdempotencyKey}, operationType: "RELEASE_HOLD", holdID: request.HoldID}
	if existing, found, err := service.existingOperation(ctx, tx, identity); err != nil || found {
		return existing, err
	}
	var paymentID, accountID uuid.UUID
	var amount int64
	var currency, status, operationType string
	err = tx.QueryRow(ctx, fmt.Sprintf(`SELECT payment_id, account_id, amount_paise, currency, status, operation_type FROM %s WHERE operation_id = $1 FOR UPDATE`, service.table("operations")), request.HoldID).Scan(&paymentID, &accountID, &amount, &currency, &status, &operationType)
	if errors.Is(err, pgx.ErrNoRows) {
		return bank.OperationResult{}, &bank.AdapterError{Code: bank.ErrCodeInvalidAccount, Message: "hold is invalid"}
	}
	if err != nil {
		return bank.OperationResult{}, err
	}
	if paymentID != request.PaymentID || operationType != "HOLD" || status == "CONFIRMED" {
		return bank.OperationResult{}, &bank.AdapterError{Code: bank.ErrCodePermanentFailure, Message: "hold cannot be released"}
	}
	if existing, found, err := service.existingOperation(ctx, tx, identity); err != nil || found {
		return existing, err
	}
	if err := service.insertOperation(ctx, tx, identityWithAmount(identity, accountID, amount, currency), "RELEASED", request.HoldID, uuid.Nil); err != nil {
		return bank.OperationResult{}, err
	}
	if status == "ACTIVE" {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET balance_paise = balance_paise + $2, version = version + 1, updated_at = CURRENT_TIMESTAMP WHERE id = $1`, service.table("accounts")), accountID, amount); err != nil {
			return bank.OperationResult{}, err
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET status = 'RELEASED', updated_at = CURRENT_TIMESTAMP WHERE operation_id = $1`, service.table("operations")), request.HoldID); err != nil {
			return bank.OperationResult{}, err
		}
		if err := service.insertLedgerEntry(ctx, tx, bank.OperationRequest{PaymentID: request.PaymentID, OperationID: request.OperationID, AccountID: accountID, AmountPaise: amount, Currency: currency}, "RELEASE"); err != nil {
			return bank.OperationResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return bank.OperationResult{}, err
	}
	return operationResult(service, bank.OperationRequest{PaymentID: request.PaymentID, OperationID: request.OperationID, IdempotencyKey: request.IdempotencyKey}), nil
}

func (service *Service) ReverseProvisionalCredit(ctx context.Context, request bank.ReverseCreditRequest) (bank.OperationResult, error) {
	if err := service.before(ctx); err != nil {
		return bank.OperationResult{}, err
	}
	if request.PaymentID == uuid.Nil || request.OperationID == uuid.Nil || request.IdempotencyKey == "" || request.OriginalOperationID == uuid.Nil {
		return bank.OperationResult{}, invalidOperationError()
	}
	tx, err := service.db.Begin(ctx)
	if err != nil {
		return bank.OperationResult{}, err
	}
	defer tx.Rollback(ctx)
	identity := operationIdentity{request: bank.OperationRequest{PaymentID: request.PaymentID, OperationID: request.OperationID, IdempotencyKey: request.IdempotencyKey}, operationType: "REVERSE_PROVISIONAL_CREDIT", originalOperationID: request.OriginalOperationID}
	if existing, found, err := service.existingOperation(ctx, tx, identity); err != nil || found {
		return existing, err
	}
	var paymentID, accountID uuid.UUID
	var amount int64
	var currency, status, operationType string
	err = tx.QueryRow(ctx, fmt.Sprintf(`SELECT payment_id, account_id, amount_paise, currency, status, operation_type FROM %s WHERE operation_id = $1 FOR UPDATE`, service.table("operations")), request.OriginalOperationID).Scan(&paymentID, &accountID, &amount, &currency, &status, &operationType)
	if errors.Is(err, pgx.ErrNoRows) {
		return bank.OperationResult{}, &bank.AdapterError{Code: bank.ErrCodeInvalidAccount, Message: "credit operation is invalid"}
	}
	if err != nil {
		return bank.OperationResult{}, err
	}
	if paymentID != request.PaymentID || operationType != "PROVISIONAL_CREDIT" {
		return bank.OperationResult{}, &bank.AdapterError{Code: bank.ErrCodePermanentFailure, Message: "credit operation does not match"}
	}
	if existing, found, err := service.existingOperation(ctx, tx, identity); err != nil || found {
		return existing, err
	}
	if status != "PROVISIONAL" && status != "REVERSED" {
		return bank.OperationResult{}, &bank.AdapterError{Code: bank.ErrCodePermanentFailure, Message: "credit is no longer provisional"}
	}
	if err := service.insertOperation(ctx, tx, identityWithAmount(identity, accountID, amount, currency), "REVERSED", uuid.Nil, request.OriginalOperationID); err != nil {
		return bank.OperationResult{}, err
	}
	if status == "PROVISIONAL" {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET status = 'REVERSED', updated_at = CURRENT_TIMESTAMP WHERE operation_id = $1`, service.table("operations")), request.OriginalOperationID); err != nil {
			return bank.OperationResult{}, err
		}
		if err := service.insertLedgerEntry(ctx, tx, bank.OperationRequest{PaymentID: request.PaymentID, OperationID: request.OperationID, AccountID: accountID, AmountPaise: amount, Currency: currency}, "REVERSE_CREDIT"); err != nil {
			return bank.OperationResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return bank.OperationResult{}, err
	}
	return operationResult(service, bank.OperationRequest{PaymentID: request.PaymentID, OperationID: request.OperationID, IdempotencyKey: request.IdempotencyKey}), nil
}

func (service *Service) GetOperationStatus(ctx context.Context, request bank.OperationStatusRequest) (bank.OperationResult, error) {
	if request.OperationID == uuid.Nil {
		return bank.OperationResult{}, invalidOperationError()
	}
	var paymentID, operationID uuid.UUID
	var reference, status string
	err := service.db.QueryRow(ctx, fmt.Sprintf(`SELECT payment_id, operation_id, bank_reference, status FROM %s WHERE operation_id = $1`, service.table("operations")), request.OperationID).Scan(&paymentID, &operationID, &reference, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return bank.OperationResult{PaymentID: request.PaymentID, OperationID: request.OperationID, Status: bank.OperationPending}, nil
	}
	if err != nil {
		return bank.OperationResult{}, err
	}
	if request.PaymentID != uuid.Nil && request.PaymentID != paymentID {
		return bank.OperationResult{}, &bank.AdapterError{Code: bank.ErrCodePermanentFailure, Message: "operation payment does not match"}
	}
	return bank.OperationResult{PaymentID: paymentID, OperationID: operationID, BankReference: reference, Status: operationStatus(status)}, nil
}

func (service *Service) GetLedgerSnapshot(ctx context.Context, scope bank.LedgerScope) (bank.LedgerSnapshot, error) {
	rows, err := service.db.Query(ctx, fmt.Sprintf(`SELECT operation_id, payment_id, account_id, entry_type, amount_paise, currency, occurred_at FROM %s WHERE occurred_at >= $1 AND occurred_at < $2 ORDER BY occurred_at, id`, service.table("ledger_entries")), scope.From, scope.To)
	if err != nil {
		return bank.LedgerSnapshot{}, err
	}
	defer rows.Close()
	snapshot := bank.LedgerSnapshot{BankID: service.bankID, SnapshotID: uuid.New(), CapturedAt: time.Now().UTC()}
	for rows.Next() {
		var entry bank.LedgerEntry
		if err := rows.Scan(&entry.OperationID, &entry.PaymentID, &entry.AccountID, &entry.EntryType, &entry.AmountPaise, &entry.Currency, &entry.OccurredAt); err != nil {
			return bank.LedgerSnapshot{}, err
		}
		snapshot.Entries = append(snapshot.Entries, entry)
	}
	return snapshot, rows.Err()
}

func (service *Service) before(ctx context.Context) error {
	service.mu.RLock()
	available, latency := service.available, service.latency
	service.mu.RUnlock()
	if !available {
		return &bank.AdapterError{Code: bank.ErrCodeBankUnavailable, Message: "bank A is unavailable"}
	}
	if latency <= 0 {
		if err := ctx.Err(); err != nil {
			return &bank.AdapterError{Code: bank.ErrCodeTransientFailure, Message: "bank operation context ended", Err: err}
		}
		return nil
	}
	timer := time.NewTimer(latency)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return &bank.AdapterError{Code: bank.ErrCodeTransientFailure, Message: "bank operation context ended", Err: ctx.Err()}
	}
}

type operationIdentity struct {
	request             bank.OperationRequest
	operationType       string
	holdID              uuid.UUID
	originalOperationID uuid.UUID
}

func identityWithAmount(identity operationIdentity, accountID uuid.UUID, amount int64, currency string) operationIdentity {
	identity.request.AccountID = accountID
	identity.request.AmountPaise = amount
	identity.request.Currency = currency
	return identity
}

func (service *Service) existingOperation(ctx context.Context, tx pgx.Tx, expected operationIdentity) (bank.OperationResult, bool, error) {
	var paymentID, operationID uuid.UUID
	var accountID, holdID, originalOperationID *uuid.UUID
	var amount *int64
	var currency, operationType, idempotencyKey, reference, status string
	err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT payment_id, operation_id, idempotency_key, operation_type, account_id, hold_id, original_operation_id, amount_paise, currency, bank_reference, status FROM %s WHERE operation_id = $1 OR idempotency_key = $2`, service.table("operations")), expected.request.OperationID, expected.request.IdempotencyKey).Scan(&paymentID, &operationID, &idempotencyKey, &operationType, &accountID, &holdID, &originalOperationID, &amount, &currency, &reference, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return bank.OperationResult{}, false, nil
	}
	if err != nil {
		return bank.OperationResult{}, false, err
	}
	if paymentID != expected.request.PaymentID || operationID != expected.request.OperationID || idempotencyKey != expected.request.IdempotencyKey || operationType != expected.operationType || !optionalUUIDMatches(accountID, expected.request.AccountID) || !optionalUUIDMatches(holdID, expected.holdID) || !optionalUUIDMatches(originalOperationID, expected.originalOperationID) || (expected.request.AmountPaise != 0 && (amount == nil || *amount != expected.request.AmountPaise)) || (expected.request.Currency != "" && currency != expected.request.Currency) {
		return bank.OperationResult{}, false, &bank.AdapterError{Code: bank.ErrCodePermanentFailure, Message: "bank operation identity or payload conflicts with an existing operation"}
	}
	return bank.OperationResult{PaymentID: paymentID, OperationID: operationID, BankReference: reference, Status: operationStatus(status)}, true, nil
}

func optionalUUIDMatches(actual *uuid.UUID, expected uuid.UUID) bool {
	return expected == uuid.Nil || (actual != nil && *actual == expected)
}

func (service *Service) insertOperation(ctx context.Context, tx pgx.Tx, identity operationIdentity, status string, holdID, originalOperationID uuid.UUID) error {
	_, err := tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (id, payment_id, operation_id, idempotency_key, operation_type, bank_id, account_id, hold_id, original_operation_id, amount_paise, currency, status, bank_reference)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, '00000000-0000-0000-0000-000000000000'::uuid), NULLIF($8, '00000000-0000-0000-0000-000000000000'::uuid), NULLIF($9, '00000000-0000-0000-0000-000000000000'::uuid), $10, $11, $12, $13)`, service.table("operations")),
		uuid.New(), identity.request.PaymentID, identity.request.OperationID, identity.request.IdempotencyKey, identity.operationType, service.bankID, identity.request.AccountID, holdID, originalOperationID, identity.request.AmountPaise, identity.request.Currency, status, service.bankReference(identity.request.OperationID))
	return err
}

func (service *Service) insertLedgerEntry(ctx context.Context, tx pgx.Tx, request bank.OperationRequest, entryType string) error {
	_, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s (id, operation_id, payment_id, account_id, entry_type, amount_paise, currency) VALUES ($1, $2, $3, $4, $5, $6, $7)`, service.table("ledger_entries")), uuid.New(), request.OperationID, request.PaymentID, request.AccountID, entryType, request.AmountPaise, request.Currency)
	return err
}

func (service *Service) lockActiveAccount(ctx context.Context, tx pgx.Tx, accountID uuid.UUID) error {
	var status string
	if err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT status FROM %s WHERE id = $1 FOR UPDATE`, service.table("accounts")), accountID).Scan(&status); errors.Is(err, pgx.ErrNoRows) {
		return &bank.AdapterError{Code: bank.ErrCodeInvalidAccount, Message: "bank account is invalid"}
	} else if err != nil {
		return err
	} else if status != string(bank.AccountActive) {
		return &bank.AdapterError{Code: bank.ErrCodeInactiveAccount, Message: "bank account is inactive"}
	}
	return nil
}

func validateOperation(request bank.OperationRequest) error {
	if request.PaymentID == uuid.Nil || request.OperationID == uuid.Nil || request.IdempotencyKey == "" || request.AccountID == uuid.Nil || request.AmountPaise <= 0 || request.Currency != "INR" {
		return invalidOperationError()
	}
	return nil
}

func invalidOperationError() error {
	return &bank.AdapterError{Code: bank.ErrCodePermanentFailure, Message: "bank operation is invalid"}
}

func operationResult(service *Service, request bank.OperationRequest) bank.OperationResult {
	return operationResultWithStatus(service, request.PaymentID, request.OperationID, bank.OperationSucceeded)
}

func operationResultWithStatus(service *Service, paymentID, operationID uuid.UUID, status bank.OperationStatus) bank.OperationResult {
	return bank.OperationResult{PaymentID: paymentID, OperationID: operationID, BankReference: service.bankReference(operationID), Status: status}
}

func operationStatus(status string) bank.OperationStatus {
	switch status {
	case "ACTIVE", "PROVISIONAL", "CONFIRMED", "FINAL", "RELEASED", "REVERSED":
		return bank.OperationSucceeded
	case "FAILED":
		return bank.OperationFailed
	default:
		return bank.OperationPending
	}
}

func (service *Service) bankReference(operationID uuid.UUID) string {
	return fmt.Sprintf("%s-%s", service.bankID, operationID)
}

var _ bank.BankAdapter = (*Service)(nil)
