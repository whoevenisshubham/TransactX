package payments

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	StateCreated               = "CREATED"
	StateValidating            = "VALIDATING"
	StateRouting               = "ROUTING"
	StateProcessing            = "PROCESSING"
	StateCommitted             = "COMMITTED"
	StateCompleted             = "COMPLETED"
	StateFailed                = "FAILED"
	StatePendingReconciliation = "PENDING_RECONCILIATION"
	StateReversed              = "REVERSED"
	StateOfflineCaptured       = "OFFLINE_CAPTURED"
	StateQueued                = "QUEUED"
	StateSyncing               = "SYNCING"
	StateReplayFailed          = "REPLAY_FAILED"
)

var ErrInvalidTransition = errors.New("invalid payment state transition")

type Payment struct {
	ID                uuid.UUID  `json:"id"`
	InitiatedByUserID uuid.UUID  `json:"initiatedByUserId"`
	SenderAccountID   uuid.UUID  `json:"senderAccountId"`
	ReceiverAccountID uuid.UUID  `json:"receiverAccountId"`
	AmountPaise       int64      `json:"amountPaise"`
	Currency          string     `json:"currency"`
	State             string     `json:"state"`
	RouteBankID       *uuid.UUID `json:"routeBankId,omitempty"`
	FailureReason     *string    `json:"failureReason,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	CompletedAt       *time.Time `json:"completedAt,omitempty"`
}

var validTransitions = map[string]map[string]bool{
	StateCreated: {
		StateValidating: true,
	},
	StateValidating: {
		StateRouting: true,
		StateFailed:  true,
	},
	StateRouting: {
		StateProcessing: true,
		StateFailed:     true,
	},
	StateProcessing: {
		StateCommitted:             true,
		StateFailed:                true,
		StatePendingReconciliation: true,
	},
	StateCommitted: {
		StateCompleted: true,
	},
	StatePendingReconciliation: {
		StateCompleted: true,
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
