package bank

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

type contractBank struct{}

func (contractBank) GetHealth(context.Context) (HealthResult, error) {
	return HealthResult{Available: true}, nil
}

func (contractBank) ResolveAccount(context.Context, ResolveAccountRequest) (AccountResult, error) {
	return AccountResult{Status: AccountActive}, nil
}

func (contractBank) HoldFunds(context.Context, HoldFundsRequest) (HoldResult, error) {
	return HoldResult{OperationResult: OperationResult{Status: OperationSucceeded}}, nil
}

func (contractBank) ProvisionalCredit(context.Context, ProvisionalCreditRequest) (OperationResult, error) {
	return OperationResult{Status: OperationSucceeded}, nil
}

func (contractBank) ConfirmHold(context.Context, ConfirmHoldRequest) (OperationResult, error) {
	return OperationResult{Status: OperationSucceeded}, nil
}

func (contractBank) ReleaseHold(context.Context, ReleaseHoldRequest) (OperationResult, error) {
	return OperationResult{Status: OperationSucceeded}, nil
}

func (contractBank) ReverseProvisionalCredit(context.Context, ReverseCreditRequest) (OperationResult, error) {
	return OperationResult{Status: OperationSucceeded}, nil
}

func (contractBank) GetOperationStatus(context.Context, OperationStatusRequest) (OperationResult, error) {
	return OperationResult{Status: OperationSucceeded}, nil
}

func (contractBank) GetLedgerSnapshot(context.Context, LedgerScope) (LedgerSnapshot, error) {
	return LedgerSnapshot{}, nil
}

var _ BankAdapter = contractBank{}

func TestAdapterContractCarriesPaymentAndOperationMetadata(t *testing.T) {
	paymentID := uuid.New()
	operationID := uuid.New()
	result := OperationResult{
		PaymentID:     paymentID,
		OperationID:   operationID,
		BankReference: "bank-reference-1",
		Status:        OperationSucceeded,
	}

	if result.PaymentID != paymentID || result.OperationID != operationID || result.BankReference == "" {
		t.Fatalf("operation result did not preserve correlation metadata: %+v", result)
	}
}

func TestOperationPendingHasStableUnresolvedStatus(t *testing.T) {
	result := OperationResult{Status: OperationPending}
	if result.Status != OperationStatus("PENDING") {
		t.Fatalf("pending status = %q, want PENDING", result.Status)
	}
}

func TestAdapterErrorCodesClassifyExpectedFailures(t *testing.T) {
	cases := []ErrorCode{
		ErrCodeInsufficientFunds,
		ErrCodeInvalidAccount,
		ErrCodeInactiveAccount,
		ErrCodeBankUnavailable,
		ErrCodeTransientFailure,
		ErrCodePermanentFailure,
	}
	for _, code := range cases {
		err := &AdapterError{Code: code}
		if err.Code != code || err.Error() != string(code) {
			t.Errorf("error code = %q, error = %q; want %q", err.Code, err.Error(), code)
		}
	}
}

func TestAdapterAccountValidationStatuses(t *testing.T) {
	statuses := []AccountStatus{AccountActive, AccountInactive, AccountInvalid}
	for _, status := range statuses {
		result := AccountValidationResult{AccountID: uuid.New(), Status: status}
		if result.Status != status || result.AccountID == uuid.Nil {
			t.Errorf("invalid account validation result: %+v", result)
		}
	}
}
