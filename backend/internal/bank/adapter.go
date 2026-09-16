package bank

import (
	"context"

	"github.com/google/uuid"
)

// BankAdapter is the domain contract that a simulated bank exposes to the payment service.
type BankAdapter interface {
	ValidateAccount(context.Context, AccountValidationRequest) (AccountValidationResult, error)
	Debit(context.Context, DebitRequest) (OperationResult, error)
	Credit(context.Context, CreditRequest) (OperationResult, error)
	Health(context.Context) (HealthResult, error)
}

type AccountValidationRequest struct {
	AccountID uuid.UUID
}

type AccountValidationResult struct {
	AccountID uuid.UUID
	Status    AccountStatus
}

type AccountStatus string

const (
	AccountActive   AccountStatus = "ACTIVE"
	AccountInactive AccountStatus = "INACTIVE"
	AccountInvalid  AccountStatus = "INVALID"
)

type DebitRequest struct {
	PaymentID   uuid.UUID
	AccountID   uuid.UUID
	AmountPaise int64
	Currency    string
}

type CreditRequest struct {
	PaymentID   uuid.UUID
	AccountID   uuid.UUID
	AmountPaise int64
	Currency    string
}

type OperationResult struct {
	PaymentID     uuid.UUID
	OperationID   uuid.UUID
	BankReference string
	Status        OperationStatus
}

type OperationStatus string

const (
	OperationSucceeded OperationStatus = "SUCCEEDED"
	// OperationPending means the bank operation outcome is unknown or unresolved.
	// The bank may have accepted or committed the operation, so callers must not
	// blindly repeat the monetary operation. The payment layer must use the
	// correlation metadata and later status or reconciliation mechanisms before
	// deciding whether a retry is safe. This is distinct from definite success
	// and definite business failure.
	OperationPending OperationStatus = "PENDING"
)

type HealthResult struct {
	Available bool
}

type ErrorCode string

const (
	ErrCodeInsufficientFunds ErrorCode = "INSUFFICIENT_FUNDS"
	ErrCodeInvalidAccount    ErrorCode = "INVALID_ACCOUNT"
	ErrCodeInactiveAccount   ErrorCode = "INACTIVE_ACCOUNT"
	ErrCodeBankUnavailable   ErrorCode = "BANK_UNAVAILABLE"
	ErrCodeTransientFailure  ErrorCode = "TRANSIENT_FAILURE"
	ErrCodePermanentFailure  ErrorCode = "PERMANENT_FAILURE"
)

type AdapterError struct {
	Code    ErrorCode
	Message string
	Err     error
}

func (err *AdapterError) Error() string {
	if err.Message != "" {
		return err.Message
	}
	return string(err.Code)
}

func (err *AdapterError) Unwrap() error { return err.Err }
