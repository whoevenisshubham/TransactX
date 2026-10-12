package bank

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// OperationPresence represents the verified presence state of an operation on a participant.
type OperationPresence string

const (
	PresencePresent OperationPresence = "present"
	PresenceAbsent  OperationPresence = "absent"
)

// OperationPresenceResult models the response from a participant presence check.
type OperationPresenceResult struct {
	PaymentID   uuid.UUID         `json:"paymentId"`
	OperationID uuid.UUID         `json:"operationId"`
	Presence    OperationPresence `json:"presence"`
	ObservedAt  time.Time         `json:"observedAt"`
	Status      *OperationStatus  `json:"status"`
}

// OperationPresenceReader is a dedicated capability interface separate from BankAdapter.
// Adapters or clients implementing this interface allow callers to authoritatively determine
// whether an operation was retained on the participant's durable storage.
type OperationPresenceReader interface {
	GetOperationPresence(ctx context.Context, paymentID, operationID uuid.UUID) (OperationPresenceResult, error)
}
