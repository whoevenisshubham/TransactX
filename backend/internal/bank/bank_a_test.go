package bank_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/bank"
)

var _ bank.BankAdapter = (*bank.BankA)(nil)

func TestBankAValidateAccount(t *testing.T) {
	activeID, inactiveID := uuid.New(), uuid.New()
	adapter := newTestBank(t, bank.SimulatedAccount{ID: activeID, BalancePaise: 1000, Status: bank.AccountActive}, bank.SimulatedAccount{ID: inactiveID, BalancePaise: 1000, Status: bank.AccountInactive})

	tests := []struct {
		name   string
		id     uuid.UUID
		status bank.AccountStatus
	}{
		{name: "active", id: activeID, status: bank.AccountActive},
		{name: "inactive", id: inactiveID, status: bank.AccountInactive},
		{name: "unknown", id: uuid.New(), status: bank.AccountInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := adapter.ValidateAccount(context.Background(), bank.AccountValidationRequest{AccountID: test.id})
			if err != nil || result.AccountID != test.id || result.Status != test.status {
				t.Fatalf("validation = %+v, err = %v", result, err)
			}
		})
	}
}

func TestBankADebitAndCreditPreserveCorrelation(t *testing.T) {
	accountID := uuid.New()
	paymentID := uuid.New()
	adapter := newTestBank(t, bank.SimulatedAccount{ID: accountID, BalancePaise: 1000, Status: bank.AccountActive})

	debit, err := adapter.Debit(context.Background(), bank.DebitRequest{PaymentID: paymentID, AccountID: accountID, AmountPaise: 250, Currency: "INR"})
	if err != nil || debit.PaymentID != paymentID || debit.OperationID == uuid.Nil || debit.BankReference == "" || debit.Status != bank.OperationSucceeded {
		t.Fatalf("debit = %+v, err = %v", debit, err)
	}
	credit, err := adapter.Credit(context.Background(), bank.CreditRequest{PaymentID: paymentID, AccountID: accountID, AmountPaise: 100, Currency: "INR"})
	if err != nil || credit.PaymentID != paymentID || credit.OperationID == uuid.Nil || credit.BankReference == "" || credit.Status != bank.OperationSucceeded {
		t.Fatalf("credit = %+v, err = %v", credit, err)
	}
}

func TestBankAFailureClassifications(t *testing.T) {
	activeID, inactiveID := uuid.New(), uuid.New()
	adapter := newTestBank(t, bank.SimulatedAccount{ID: activeID, BalancePaise: 100, Status: bank.AccountActive}, bank.SimulatedAccount{ID: inactiveID, BalancePaise: 100, Status: bank.AccountInactive})

	tests := []struct {
		name     string
		account  uuid.UUID
		amount   int64
		wantCode bank.ErrorCode
	}{
		{name: "insufficient funds", account: activeID, amount: 101, wantCode: bank.ErrCodeInsufficientFunds},
		{name: "invalid account", account: uuid.New(), amount: 1, wantCode: bank.ErrCodeInvalidAccount},
		{name: "inactive account", account: inactiveID, amount: 1, wantCode: bank.ErrCodeInactiveAccount},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := adapter.Debit(context.Background(), bank.DebitRequest{PaymentID: uuid.New(), AccountID: test.account, AmountPaise: test.amount, Currency: "INR"})
			var adapterErr *bank.AdapterError
			if !errors.As(err, &adapterErr) || adapterErr.Code != test.wantCode {
				t.Fatalf("err = %v, want adapter code %s", err, test.wantCode)
			}
		})
	}
}

func TestBankAHealthIsAvailableByDefault(t *testing.T) {
	adapter := newTestBank(t)
	result, err := adapter.Health(context.Background())
	if err != nil || !result.Available {
		t.Fatalf("health = %+v, err = %v", result, err)
	}
}

func TestBankAUnavailableStateUsesTypedError(t *testing.T) {
	adapter := newTestBank(t)
	adapter.SetAvailable(false)
	_, err := adapter.Debit(context.Background(), bank.DebitRequest{PaymentID: uuid.New(), AccountID: uuid.New(), AmountPaise: 1, Currency: "INR"})
	var adapterErr *bank.AdapterError
	if !errors.As(err, &adapterErr) || adapterErr.Code != bank.ErrCodeBankUnavailable {
		t.Fatalf("err = %v, want bank unavailable", err)
	}
}

func TestBankACreditOverflowReturnsPermanentFailureAndPreservesBalance(t *testing.T) {
	accountID := uuid.New()
	adapter := newTestBank(t, bank.SimulatedAccount{ID: accountID, BalancePaise: math.MaxInt64 - 1, Status: bank.AccountActive})

	_, err := adapter.Credit(context.Background(), bank.CreditRequest{PaymentID: uuid.New(), AccountID: accountID, AmountPaise: 2, Currency: "INR"})
	var adapterErr *bank.AdapterError
	if !errors.As(err, &adapterErr) || adapterErr.Code != bank.ErrCodePermanentFailure {
		t.Fatalf("err = %v, want permanent failure", err)
	}

	if _, err := adapter.Debit(context.Background(), bank.DebitRequest{PaymentID: uuid.New(), AccountID: accountID, AmountPaise: math.MaxInt64 - 1, Currency: "INR"}); err != nil {
		t.Fatalf("balance was modified after rejected overflow credit: %v", err)
	}
}

func newTestBank(t *testing.T, accounts ...bank.SimulatedAccount) *bank.BankA {
	t.Helper()
	adapter, err := bank.NewBankA(accounts)
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}
