package projections

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrUnsupportedVersion = errors.New("unsupported event schema version")
	ErrPaymentNotFound    = errors.New("payment not found for projection")
)

const (
	WorklistMaintenanceLockKey = 78891001
)

// WorklistItem represents an item in the operational payment worklist derived projection.
type WorklistItem struct {
	PaymentID          uuid.UUID  `json:"paymentId"`
	CurrentState       string     `json:"currentState"`
	EventVersion       int64      `json:"eventVersion"`
	AmountPaise        int64      `json:"amountPaise"`
	Currency           string     `json:"currency"`
	SourceBankID       *uuid.UUID `json:"sourceBankId,omitempty"`
	DestinationBankID  *uuid.UUID `json:"destinationBankId,omitempty"`
	RouteBankID        *uuid.UUID `json:"routeBankId,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
	RecoveryStatus     *string    `json:"recoveryStatus,omitempty"`
	RecoveryAttempts   int        `json:"recoveryAttempts"`
	LastRecoveryReason *string    `json:"lastRecoveryReason,omitempty"`
	LastRecoveryAt     *time.Time `json:"lastRecoveryAt,omitempty"`
	ProjectedAt        time.Time  `json:"projectedAt"`
}
