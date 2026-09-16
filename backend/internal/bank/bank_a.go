package bank

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/google/uuid"
)

type SimulatedAccount struct {
	ID           uuid.UUID
	BalancePaise int64
	Status       AccountStatus
}

type BankA struct {
	mu        sync.RWMutex
	available bool
	accounts  map[uuid.UUID]SimulatedAccount
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
	return &BankA{available: true, accounts: state}, nil
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

func (bank *BankA) Debit(ctx context.Context, request DebitRequest) (OperationResult, error) {
	bank.mu.Lock()
	defer bank.mu.Unlock()
	if err := bank.checkLocked(ctx); err != nil {
		return OperationResult{}, err
	}
	if err := validateOperation(request.AmountPaise, request.Currency); err != nil {
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
	return operationResult(request.PaymentID), nil
}

func (bank *BankA) Credit(ctx context.Context, request CreditRequest) (OperationResult, error) {
	bank.mu.Lock()
	defer bank.mu.Unlock()
	if err := bank.checkLocked(ctx); err != nil {
		return OperationResult{}, err
	}
	if err := validateOperation(request.AmountPaise, request.Currency); err != nil {
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
	return operationResult(request.PaymentID), nil
}

func (bank *BankA) Health(ctx context.Context) (HealthResult, error) {
	if err := bank.checkContext(ctx); err != nil {
		return HealthResult{}, err
	}
	bank.mu.RLock()
	defer bank.mu.RUnlock()
	return HealthResult{Available: bank.available}, nil
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

func validateOperation(amountPaise int64, currency string) error {
	if amountPaise <= 0 || currency != "INR" {
		return &AdapterError{Code: ErrCodePermanentFailure, Message: "bank operation is invalid"}
	}
	return nil
}

func operationResult(paymentID uuid.UUID) OperationResult {
	operationID := uuid.New()
	return OperationResult{
		PaymentID:     paymentID,
		OperationID:   operationID,
		BankReference: fmt.Sprintf("BANK-A-%s", operationID),
		Status:        OperationSucceeded,
	}
}
