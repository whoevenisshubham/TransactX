package payments

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

type IdempotencyRecord struct {
	Key         string
	RequestHash string
	PaymentID   uuid.UUID
}

// PaymentStateTransition represents an immutable state transition event for a payment.
type PaymentStateTransition struct {
	ID             uuid.UUID `json:"id"`
	PaymentID      uuid.UUID `json:"paymentId"`
	FromState      string    `json:"fromState"`
	ToState        string    `json:"toState"`
	TransitionedAt time.Time `json:"transitionedAt"`
}

const (
	StateCreated                   = "CREATED"
	StateValidating                = "VALIDATING"
	StateLocalSettlement           = "LOCAL_SETTLEMENT"
	StateRouting                   = "ROUTING"
	StateProcessing                = "PROCESSING"
	StateBankSettledCentralPending = "BANK_SETTLED_CENTRAL_PENDING"
	StateCommitted                 = "COMMITTED"
	StateCompleted                 = "COMPLETED"
	StateFailed                    = "FAILED"
	StatePendingReconciliation     = "PENDING_RECONCILIATION"
	StateReversed                  = "REVERSED"
	StateOfflineCaptured           = "OFFLINE_CAPTURED"
	StateQueued                    = "QUEUED"
	StateSyncing                   = "SYNCING"
	StateReplayFailed              = "REPLAY_FAILED"
)

var ErrInvalidTransition = errors.New("invalid payment state transition")

type Payment struct {
	ID                       uuid.UUID  `json:"id"`
	InitiatedByUserID        uuid.UUID  `json:"initiatedByUserId"`
	SenderAccountID          uuid.UUID  `json:"senderAccountId"`
	ReceiverAccountID        uuid.UUID  `json:"receiverAccountId"`
	AmountPaise              int64      `json:"amountPaise"`
	Currency                 string     `json:"currency"`
	Note                     *string    `json:"note,omitempty"`
	Origin                   string     `json:"origin"`
	State                    string     `json:"state"`
	RouteBankID              *uuid.UUID `json:"routeBankId,omitempty"`
	FailureReason            *string    `json:"failureReason,omitempty"`
	CreatedAt                time.Time  `json:"createdAt"`
	UpdatedAt                time.Time  `json:"updatedAt"`
	CompletedAt              *time.Time `json:"completedAt,omitempty"`
	SourceBankID             *uuid.UUID `json:"sourceBankId,omitempty"`
	DestinationBankID        *uuid.UUID `json:"destinationBankId,omitempty"`
	SourceBankAccountID      *uuid.UUID `json:"sourceBankAccountId,omitempty"`
	DestinationBankAccountID *uuid.UUID `json:"destinationBankAccountId,omitempty"`
}

// CustomerPayment is the deliberately small, customer-facing payment view.
// Internal bank-operation and routing fields stay behind the API boundary.
type CustomerPayment struct {
	ID                  uuid.UUID  `json:"id"`
	AmountPaise         int64      `json:"amountPaise"`
	Currency            string     `json:"currency"`
	Note                *string    `json:"note,omitempty"`
	Origin              string     `json:"origin"`
	State               string     `json:"state"`
	CreatedAt           time.Time  `json:"createdAt"`
	CompletedAt         *time.Time `json:"completedAt,omitempty"`
	FailureReason       *string    `json:"failureReason,omitempty"`
	SenderName          string     `json:"senderName"`
	SenderPaymentID     string     `json:"senderPaymentIdentifier"`
	ReceiverName        string     `json:"receiverName"`
	ReceiverPaymentID   string     `json:"receiverPaymentIdentifier"`
	Direction           string     `json:"direction"`
	SourceBankName      *string    `json:"sourceBankName,omitempty"`
	SourceBankCode      *string    `json:"sourceBankCode,omitempty"`
	DestinationBankName *string    `json:"destinationBankName,omitempty"`
	DestinationBankCode *string    `json:"destinationBankCode,omitempty"`
	DurationMs          *int64     `json:"durationMs,omitempty"`
}

var validTransitions = map[string]map[string]bool{
	StateCreated: {
		StateValidating: true,
	},
	StateValidating: {
		StateLocalSettlement: true,
		StateRouting:         true,
		StateFailed:          true,
	},
	StateLocalSettlement: {
		StateCommitted: true,
		StateFailed:    true,
	},
	StateRouting: {
		StateProcessing: true,
		StateFailed:     true,
	},
	StateProcessing: {
		StateCommitted:                 true,
		StateFailed:                    true,
		StatePendingReconciliation:     true,
		StateBankSettledCentralPending: true,
	},
	StateBankSettledCentralPending: {
		StateCommitted:             true,
		StatePendingReconciliation: true,
	},
	StateCommitted: {
		StateCompleted: true,
	},
	StatePendingReconciliation: {
		StateCommitted: true,
		StateFailed:    true,
		StateReversed:  true,
	},
	StateOfflineCaptured: {
		StateQueued: true,
	},
	StateQueued: {
		StateSyncing: true,
	},
	StateSyncing: {
		StateCompleted:    true,
		StateReplayFailed: true,
	},
}

func CanTransition(from, to string) bool {
	return validTransitions[from][to]
}

func Transition(payment *Payment, next string) error {
	if !CanTransition(payment.State, next) {
		return ErrInvalidTransition
	}
	payment.State = next
	return nil
}
