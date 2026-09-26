package bank

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/transactx/backend/internal/common"
)

// BankAdapter is the domain contract used by the payment switch to communicate
// with an independent bank participant.
type BankAdapter interface {
	GetHealth(context.Context) (HealthResult, error)
	ResolveAccount(context.Context, ResolveAccountRequest) (AccountResult, error)
	HoldFunds(context.Context, HoldFundsRequest) (HoldResult, error)
	ProvisionalCredit(context.Context, ProvisionalCreditRequest) (OperationResult, error)
	ConfirmHold(context.Context, ConfirmHoldRequest) (OperationResult, error)
	ReleaseHold(context.Context, ReleaseHoldRequest) (OperationResult, error)
	ReverseProvisionalCredit(context.Context, ReverseCreditRequest) (OperationResult, error)
	GetOperationStatus(context.Context, OperationStatusRequest) (OperationResult, error)
	GetLedgerSnapshot(context.Context, LedgerScope) (LedgerSnapshot, error)
}

type ResolveAccountRequest struct {
	AccountID uuid.UUID
}

type AccountResult struct {
	AccountID uuid.UUID
	Status    AccountStatus
}

type AccountStatus string

const (
	AccountActive   AccountStatus = "ACTIVE"
	AccountInactive AccountStatus = "INACTIVE"
	AccountInvalid  AccountStatus = "INVALID"
)

type OperationRequest struct {
	PaymentID      uuid.UUID
	OperationID    uuid.UUID
	IdempotencyKey string
	AccountID      uuid.UUID
	AmountPaise    int64
	Currency       string
}

type HoldFundsRequest struct{ OperationRequest }

type ProvisionalCreditRequest struct{ OperationRequest }

type ConfirmHoldRequest struct {
	PaymentID      uuid.UUID
	OperationID    uuid.UUID
	IdempotencyKey string
	HoldID         uuid.UUID
}

type ReleaseHoldRequest struct {
	PaymentID      uuid.UUID
	OperationID    uuid.UUID
	IdempotencyKey string
	HoldID         uuid.UUID
}

type ReverseCreditRequest struct {
	PaymentID           uuid.UUID
	OperationID         uuid.UUID
	IdempotencyKey      string
	OriginalOperationID uuid.UUID
}

type OperationStatusRequest struct {
	PaymentID   uuid.UUID
	OperationID uuid.UUID
}

type LedgerScope struct {
	From time.Time
	To   time.Time
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
	OperationFailed    OperationStatus = "FAILED"
	// OperationPending means the bank operation outcome is unknown or unresolved.
	// The bank may have accepted or committed the operation, so callers must not
	// blindly repeat the monetary operation. The payment layer must use the
	// correlation metadata and later status or reconciliation mechanisms before
	// deciding whether a retry is safe. This is distinct from definite success
	// and definite business failure.
	OperationPending OperationStatus = "PENDING"
)

type HoldResult struct {
	OperationResult
	HoldID uuid.UUID
}

type LedgerSnapshot struct {
	BankID     string
	SnapshotID uuid.UUID
	CapturedAt time.Time
	Entries    []LedgerEntry
}

type LedgerEntry struct {
	OperationID uuid.UUID
	PaymentID   uuid.UUID
	AccountID   uuid.UUID
	EntryType   string
	AmountPaise int64
	Currency    string
	OccurredAt  time.Time
}

// Legacy request aliases remain available to keep the M1-5 concrete simulation
// and its callers source-compatible while the routed contract is introduced.
type AccountValidationRequest = ResolveAccountRequest
type AccountValidationResult = AccountResult
type DebitRequest = OperationRequest
type CreditRequest = OperationRequest

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

func ContextWithRequestIDForTest(ctx context.Context, requestID string) context.Context {
	return common.ContextWithRequestID(ctx, requestID)
}
