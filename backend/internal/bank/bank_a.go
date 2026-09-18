package bank

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

type SimulatedAccount struct {
	ID           uuid.UUID
	BalancePaise int64
	Status       AccountStatus
}

type BankA struct {
	mu            sync.RWMutex
	available     bool
	accounts      map[uuid.UUID]SimulatedAccount
	operations    map[uuid.UUID]bankOperation
	byIdempotency map[string]uuid.UUID
	ledger        []LedgerEntry
}

type bankOperation struct {
	PaymentID           uuid.UUID
	OperationID         uuid.UUID
	IdempotencyKey      string
	OperationType       string
	AccountID           uuid.UUID
	HoldID              uuid.UUID
	OriginalOperationID uuid.UUID
	AmountPaise         int64
	Currency            string
	Status              string
}

var _ BankAdapter = (*BankA)(nil)

func NewBankA(accounts []SimulatedAccount) (*BankA, error) {
	state := make(map[uuid.UUID]SimulatedAccount, len(accounts))
	for _, account := range accounts {
		if account.ID == uuid.Nil {
			return nil, &AdapterError{Code: ErrCodePermanentFailure, Message: "bank account ID is required"}
		}
		if account.BalancePaise < 0 {
			return nil, &AdapterError{Code: ErrCodePermanentFailure, Message: "bank account balance cannot be negative"}
		}
		if account.Status == "" {
			account.Status = AccountActive
		}
		if account.Status != AccountActive && account.Status != AccountInactive {
			return nil, &AdapterError{Code: ErrCodePermanentFailure, Message: "bank account status is invalid"}
		}
		if _, exists := state[account.ID]; exists {
			return nil, &AdapterError{Code: ErrCodePermanentFailure, Message: "bank account ID is duplicated"}
		}
		state[account.ID] = account
	}
	return &BankA{available: true, accounts: state, operations: make(map[uuid.UUID]bankOperation), byIdempotency: make(map[string]uuid.UUID)}, nil
}

func (bank *BankA) SetAvailable(available bool) {
	bank.mu.Lock()
	defer bank.mu.Unlock()
	bank.available = available
}

func (bank *BankA) ValidateAccount(ctx context.Context, request AccountValidationRequest) (AccountValidationResult, error) {
	if err := bank.checkContext(ctx); err != nil {
		return AccountValidationResult{}, err
	}
	bank.mu.RLock()
	defer bank.mu.RUnlock()
	if !bank.available {
		return AccountValidationResult{}, bank.unavailableError()
	}
	account, exists := bank.accounts[request.AccountID]
	if !exists {
		return AccountValidationResult{AccountID: request.AccountID, Status: AccountInvalid}, nil
	}
	return AccountValidationResult{AccountID: request.AccountID, Status: account.Status}, nil
}

func (bank *BankA) ResolveAccount(ctx context.Context, request ResolveAccountRequest) (AccountResult, error) {
	return bank.ValidateAccount(ctx, request)
}

func (bank *BankA) GetHealth(ctx context.Context) (HealthResult, error) {
	return bank.Health(ctx)
}

func (bank *BankA) HoldFunds(ctx context.Context, request HoldFundsRequest) (HoldResult, error) {
	bank.mu.Lock()
	defer bank.mu.Unlock()
	if err := bank.checkLocked(ctx); err != nil {
		return HoldResult{}, err
	}
	if err := validateOperation(request.OperationRequest); err != nil {
		return HoldResult{}, err
	}
	if existing, found, err := bank.existing(request.OperationRequest, "HOLD", uuid.Nil, uuid.Nil); found || err != nil {
		return HoldResult{OperationResult: existingResult(existing), HoldID: request.OperationID}, err
	}
	account, err := bank.activeAccount(request.AccountID)
	if err != nil {
		return HoldResult{}, err
	}
	if account.BalancePaise < request.AmountPaise {
		return HoldResult{}, &AdapterError{Code: ErrCodeInsufficientFunds, Message: "bank account has insufficient funds"}
	}
	account.BalancePaise -= request.AmountPaise
	bank.accounts[request.AccountID] = account
	operation := bankOperation{PaymentID: request.PaymentID, OperationID: request.OperationID, IdempotencyKey: request.IdempotencyKey, OperationType: "HOLD", AccountID: request.AccountID, AmountPaise: request.AmountPaise, Currency: request.Currency, Status: "ACTIVE"}
	bank.store(operation)
	bank.appendLedger(operation, "HOLD")
	return HoldResult{OperationResult: operationResultWithID(request.PaymentID, request.OperationID), HoldID: request.OperationID}, nil
}

func (bank *BankA) ProvisionalCredit(ctx context.Context, request ProvisionalCreditRequest) (OperationResult, error) {
	bank.mu.Lock()
	defer bank.mu.Unlock()
	if err := bank.checkLocked(ctx); err != nil {
		return OperationResult{}, err
	}
	if err := validateOperation(request.OperationRequest); err != nil {
		return OperationResult{}, err
	}
	if existing, found, err := bank.existing(request.OperationRequest, "PROVISIONAL_CREDIT", uuid.Nil, uuid.Nil); found || err != nil {
		return existingResult(existing), err
	}
	if _, err := bank.activeAccount(request.AccountID); err != nil {
		return OperationResult{}, err
	}
	operation := bankOperation{PaymentID: request.PaymentID, OperationID: request.OperationID, IdempotencyKey: request.IdempotencyKey, OperationType: "PROVISIONAL_CREDIT", AccountID: request.AccountID, AmountPaise: request.AmountPaise, Currency: request.Currency, Status: "PROVISIONAL"}
	bank.store(operation)
	bank.appendLedger(operation, "PROVISIONAL_CREDIT")
	return operationResultWithID(request.PaymentID, request.OperationID), nil
}

func (bank *BankA) ConfirmHold(ctx context.Context, request ConfirmHoldRequest) (OperationResult, error) {
	bank.mu.Lock()
	defer bank.mu.Unlock()
	if err := bank.checkLocked(ctx); err != nil {
		return OperationResult{}, err
	}
	if request.PaymentID == uuid.Nil || request.OperationID == uuid.Nil || request.IdempotencyKey == "" || request.HoldID == uuid.Nil {
		return OperationResult{}, invalidOperation()
	}
	if existing, found, err := bank.existing(OperationRequest{PaymentID: request.PaymentID, OperationID: request.OperationID, IdempotencyKey: request.IdempotencyKey}, "CONFIRM_HOLD", request.HoldID, uuid.Nil); found || err != nil {
		return existingResult(existing), err
	}
	target, ok := bank.operations[request.HoldID]
	if !ok || target.PaymentID != request.PaymentID || (target.OperationType != "HOLD" && target.OperationType != "PROVISIONAL_CREDIT") {
		return OperationResult{}, &AdapterError{Code: ErrCodeInvalidAccount, Message: "hold is invalid"}
	}
	if target.Status != "ACTIVE" && target.Status != "PROVISIONAL" && target.Status != "CONFIRMED" && target.Status != "FINAL" {
		return OperationResult{}, &AdapterError{Code: ErrCodePermanentFailure, Message: "operation is no longer confirmable"}
	}
	confirm := bankOperation{PaymentID: request.PaymentID, OperationID: request.OperationID, IdempotencyKey: request.IdempotencyKey, OperationType: "CONFIRM_HOLD", AccountID: target.AccountID, HoldID: request.HoldID, AmountPaise: target.AmountPaise, Currency: target.Currency, Status: "CONFIRMED"}
	if target.Status == "ACTIVE" {
		target.Status = "CONFIRMED"
		bank.operations[target.OperationID] = target
	} else if target.Status == "PROVISIONAL" {
		account := bank.accounts[target.AccountID]
		if target.AmountPaise > math.MaxInt64-account.BalancePaise {
			return OperationResult{}, &AdapterError{Code: ErrCodePermanentFailure, Message: "bank account balance overflow"}
		}
		account.BalancePaise += target.AmountPaise
		bank.accounts[target.AccountID] = account
		target.Status = "FINAL"
		bank.operations[target.OperationID] = target
		bank.appendLedger(confirm, "FINAL_CREDIT")
	}
	bank.store(confirm)
	return operationResultWithID(request.PaymentID, request.OperationID), nil
}

func (bank *BankA) ReleaseHold(ctx context.Context, request ReleaseHoldRequest) (OperationResult, error) {
	bank.mu.Lock()
	defer bank.mu.Unlock()
	if err := bank.checkLocked(ctx); err != nil {
		return OperationResult{}, err
	}
	if request.PaymentID == uuid.Nil || request.OperationID == uuid.Nil || request.IdempotencyKey == "" || request.HoldID == uuid.Nil {
		return OperationResult{}, invalidOperation()
	}
	if existing, found, err := bank.existing(OperationRequest{PaymentID: request.PaymentID, OperationID: request.OperationID, IdempotencyKey: request.IdempotencyKey}, "RELEASE_HOLD", request.HoldID, uuid.Nil); found || err != nil {
		return existingResult(existing), err
	}
	target, ok := bank.operations[request.HoldID]
	if !ok || target.PaymentID != request.PaymentID || target.OperationType != "HOLD" || target.Status == "CONFIRMED" {
		return OperationResult{}, &AdapterError{Code: ErrCodePermanentFailure, Message: "hold cannot be released"}
	}
	release := bankOperation{PaymentID: request.PaymentID, OperationID: request.OperationID, IdempotencyKey: request.IdempotencyKey, OperationType: "RELEASE_HOLD", AccountID: target.AccountID, HoldID: request.HoldID, AmountPaise: target.AmountPaise, Currency: target.Currency, Status: "RELEASED"}
	if target.Status == "ACTIVE" {
		account := bank.accounts[target.AccountID]
		account.BalancePaise += target.AmountPaise
		bank.accounts[target.AccountID] = account
		target.Status = "RELEASED"
		bank.operations[target.OperationID] = target
		bank.appendLedger(release, "RELEASE")
	}
	bank.store(release)
	return operationResultWithID(request.PaymentID, request.OperationID), nil
}

func (bank *BankA) ReverseProvisionalCredit(ctx context.Context, request ReverseCreditRequest) (OperationResult, error) {
	bank.mu.Lock()
	defer bank.mu.Unlock()
	if err := bank.checkLocked(ctx); err != nil {
		return OperationResult{}, err
	}
	if request.PaymentID == uuid.Nil || request.OperationID == uuid.Nil || request.IdempotencyKey == "" || request.OriginalOperationID == uuid.Nil {
		return OperationResult{}, invalidOperation()
	}
	if existing, found, err := bank.existing(OperationRequest{PaymentID: request.PaymentID, OperationID: request.OperationID, IdempotencyKey: request.IdempotencyKey}, "REVERSE_PROVISIONAL_CREDIT", uuid.Nil, request.OriginalOperationID); found || err != nil {
		return existingResult(existing), err
	}
	target, ok := bank.operations[request.OriginalOperationID]
	if !ok || target.PaymentID != request.PaymentID || target.OperationType != "PROVISIONAL_CREDIT" || (target.Status != "PROVISIONAL" && target.Status != "REVERSED") {
		return OperationResult{}, &AdapterError{Code: ErrCodePermanentFailure, Message: "credit is no longer provisional"}
	}
	reverse := bankOperation{PaymentID: request.PaymentID, OperationID: request.OperationID, IdempotencyKey: request.IdempotencyKey, OperationType: "REVERSE_PROVISIONAL_CREDIT", AccountID: target.AccountID, OriginalOperationID: request.OriginalOperationID, AmountPaise: target.AmountPaise, Currency: target.Currency, Status: "REVERSED"}
	if target.Status == "PROVISIONAL" {
		target.Status = "REVERSED"
		bank.operations[target.OperationID] = target
		bank.appendLedger(reverse, "REVERSE_CREDIT")
	}
	bank.store(reverse)
	return operationResultWithID(request.PaymentID, request.OperationID), nil
}

func (bank *BankA) GetOperationStatus(ctx context.Context, request OperationStatusRequest) (OperationResult, error) {
	if err := bank.checkContext(ctx); err != nil {
		return OperationResult{}, err
	}
	bank.mu.RLock()
	defer bank.mu.RUnlock()
	operation, ok := bank.operations[request.OperationID]
	if !ok {
		return OperationResult{PaymentID: request.PaymentID, OperationID: request.OperationID, Status: OperationPending}, nil
	}
	if request.PaymentID != uuid.Nil && operation.PaymentID != request.PaymentID {
		return OperationResult{}, &AdapterError{Code: ErrCodePermanentFailure, Message: "operation payment does not match"}
	}
	return OperationResult{PaymentID: operation.PaymentID, OperationID: operation.OperationID, BankReference: fmt.Sprintf("BANK-A-%s", operation.OperationID), Status: operationStatus(operation.Status)}, nil
}

func (bank *BankA) GetLedgerSnapshot(ctx context.Context, scope LedgerScope) (LedgerSnapshot, error) {
	if err := bank.checkContext(ctx); err != nil {
		return LedgerSnapshot{}, err
	}
	bank.mu.RLock()
	defer bank.mu.RUnlock()
	entries := make([]LedgerEntry, 0, len(bank.ledger))
	for _, entry := range bank.ledger {
		if !entry.OccurredAt.Before(scope.From) && entry.OccurredAt.Before(scope.To) {
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].OccurredAt.Equal(entries[j].OccurredAt) {
			return entries[i].OperationID.String() < entries[j].OperationID.String()
		}
		return entries[i].OccurredAt.Before(entries[j].OccurredAt)
	})
	return LedgerSnapshot{BankID: "BANK-A", SnapshotID: uuid.New(), CapturedAt: time.Now().UTC(), Entries: entries}, nil
}

// Debit and Credit remain available as legacy primitive simulations; routed
// orchestration uses the hold/provisional-credit/confirm contract above.
func (bank *BankA) Debit(ctx context.Context, request DebitRequest) (OperationResult, error) {
	bank.mu.Lock()
	defer bank.mu.Unlock()
	if err := bank.checkLocked(ctx); err != nil {
		return OperationResult{}, err
	}
	if request.OperationID == uuid.Nil {
		request.OperationID = uuid.New()
	}
	if request.IdempotencyKey == "" {
		request.IdempotencyKey = request.OperationID.String()
	}
	if err := validateOperation(request); err != nil {
		return OperationResult{}, err
	}
	account, err := bank.activeAccount(request.AccountID)
	if err != nil {
		return OperationResult{}, err
	}
	if account.BalancePaise < request.AmountPaise {
		return OperationResult{}, &AdapterError{Code: ErrCodeInsufficientFunds, Message: "bank account has insufficient funds"}
	}
	account.BalancePaise -= request.AmountPaise
	bank.accounts[request.AccountID] = account
	return operationResultWithID(request.PaymentID, request.OperationID), nil
}

func (bank *BankA) Credit(ctx context.Context, request CreditRequest) (OperationResult, error) {
	bank.mu.Lock()
	defer bank.mu.Unlock()
	if err := bank.checkLocked(ctx); err != nil {
		return OperationResult{}, err
	}
	if request.OperationID == uuid.Nil {
		request.OperationID = uuid.New()
	}
	if request.IdempotencyKey == "" {
		request.IdempotencyKey = request.OperationID.String()
	}
	if err := validateOperation(request); err != nil {
		return OperationResult{}, err
	}
	account, err := bank.activeAccount(request.AccountID)
	if err != nil {
		return OperationResult{}, err
	}
	if request.AmountPaise > math.MaxInt64-account.BalancePaise {
		return OperationResult{}, &AdapterError{Code: ErrCodePermanentFailure, Message: "bank account balance overflow"}
	}
	account.BalancePaise += request.AmountPaise
	bank.accounts[request.AccountID] = account
	return operationResultWithID(request.PaymentID, request.OperationID), nil
}

func (bank *BankA) Health(ctx context.Context) (HealthResult, error) {
	if err := bank.checkContext(ctx); err != nil {
		return HealthResult{}, err
	}
	bank.mu.RLock()
	defer bank.mu.RUnlock()
	return HealthResult{Available: bank.available}, nil
}

func (bank *BankA) existing(request OperationRequest, operationType string, holdID, originalOperationID uuid.UUID) (bankOperation, bool, error) {
	operationID, byKey := request.OperationID, false
	if operation, ok := bank.operations[operationID]; ok {
		if err := matchOperation(operation, request, operationType, holdID, originalOperationID); err != nil {
			return bankOperation{}, true, err
		}
		return operation, true, nil
	}
	if request.IdempotencyKey != "" {
		operationID, byKey = bank.byIdempotency[request.IdempotencyKey]
		if byKey {
			operation := bank.operations[operationID]
			if err := matchOperation(operation, request, operationType, holdID, originalOperationID); err != nil {
				return bankOperation{}, true, err
			}
			return operation, true, nil
		}
	}
	return bankOperation{}, false, nil
}

func matchOperation(operation bankOperation, request OperationRequest, operationType string, holdID, originalOperationID uuid.UUID) error {
	if operation.PaymentID != request.PaymentID || operation.OperationID != request.OperationID || operation.IdempotencyKey != request.IdempotencyKey || operation.OperationType != operationType || (request.AccountID != uuid.Nil && operation.AccountID != request.AccountID) || (request.AmountPaise != 0 && operation.AmountPaise != request.AmountPaise) || (request.Currency != "" && operation.Currency != request.Currency) || (holdID != uuid.Nil && operation.HoldID != holdID) || (originalOperationID != uuid.Nil && operation.OriginalOperationID != originalOperationID) {
		return &AdapterError{Code: ErrCodePermanentFailure, Message: "bank operation identity or payload conflicts with an existing operation"}
	}
	return nil
}

func existingResult(operation bankOperation) OperationResult {
	if operation.OperationID == uuid.Nil {
		return OperationResult{}
	}
	return OperationResult{PaymentID: operation.PaymentID, OperationID: operation.OperationID, BankReference: fmt.Sprintf("BANK-A-%s", operation.OperationID), Status: operationStatus(operation.Status)}
}

func (bank *BankA) store(operation bankOperation) {
	bank.operations[operation.OperationID] = operation
	bank.byIdempotency[operation.IdempotencyKey] = operation.OperationID
}

func (bank *BankA) appendLedger(operation bankOperation, entryType string) {
	bank.ledger = append(bank.ledger, LedgerEntry{OperationID: operation.OperationID, PaymentID: operation.PaymentID, AccountID: operation.AccountID, EntryType: entryType, AmountPaise: operation.AmountPaise, Currency: operation.Currency, OccurredAt: time.Now().UTC()})
}

func (bank *BankA) activeAccount(accountID uuid.UUID) (SimulatedAccount, error) {
	account, exists := bank.accounts[accountID]
	if !exists {
		return SimulatedAccount{}, &AdapterError{Code: ErrCodeInvalidAccount, Message: "bank account is invalid"}
	}
	if account.Status == AccountInactive {
		return SimulatedAccount{}, &AdapterError{Code: ErrCodeInactiveAccount, Message: "bank account is inactive"}
	}
	return account, nil
}

func (bank *BankA) storeUnavailable() {}

func (bank *BankA) checkLocked(ctx context.Context) error {
	if err := bank.checkContext(ctx); err != nil {
		return err
	}
	if !bank.available {
		return bank.unavailableError()
	}
	return nil
}

func (bank *BankA) checkContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return &AdapterError{Code: ErrCodeTransientFailure, Message: "bank operation context ended", Err: err}
	}
	return nil
}

func (bank *BankA) unavailableError() error {
	return &AdapterError{Code: ErrCodeBankUnavailable, Message: "bank A is unavailable"}
}

func validateOperation(request OperationRequest) error {
	if request.PaymentID == uuid.Nil || request.OperationID == uuid.Nil || request.IdempotencyKey == "" || request.AccountID == uuid.Nil || request.AmountPaise <= 0 || request.Currency != "INR" {
		return invalidOperation()
	}
	return nil
}

func invalidOperation() error {
	return &AdapterError{Code: ErrCodePermanentFailure, Message: "bank operation is invalid"}
}

func operationStatus(status string) OperationStatus {
	switch status {
	case "ACTIVE", "PROVISIONAL", "CONFIRMED", "FINAL", "RELEASED", "REVERSED":
		return OperationSucceeded
	case "FAILED":
		return OperationFailed
	default:
		return OperationPending
	}
}

func operationResultWithID(paymentID, operationID uuid.UUID) OperationResult {
	return OperationResult{PaymentID: paymentID, OperationID: operationID, BankReference: fmt.Sprintf("BANK-A-%s", operationID), Status: OperationSucceeded}
}
