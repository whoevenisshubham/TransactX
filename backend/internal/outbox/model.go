package outbox

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
)

const (
	ConsumerOpsWorklist = "ops-worklist-v1"

	EventTypePaymentCreated             = "payment.created.v1"
	EventTypePaymentStateChanged        = "payment.state_changed.v1"
	EventTypeRecoveryAttemptStarted     = "recovery.attempt_started.v1"
	EventTypeRecoveryAttemptFinished    = "recovery.attempt_finished.v1"
	EventTypeRecoveryAttentionRequired  = "recovery.attention_required.v1"

	AggregateTypePayment  = "payment"
	AggregateTypeRecovery = "recovery"

	DeliveryStatusPending     = "pending"
	DeliveryStatusProcessing  = "processing"
	DeliveryStatusDelivered   = "delivered"
	DeliveryStatusQuarantined = "quarantined"
)

// Envelope represents the canonical outbox event wire envelope defined in EVENT_ARCHITECTURE.md.
type Envelope struct {
	EventID          uuid.UUID       `json:"eventId"`
	EventType        string          `json:"eventType"`
	SchemaVersion    int             `json:"schemaVersion"`
	AggregateType    string          `json:"aggregateType"`
	AggregateID      uuid.UUID       `json:"aggregateId"`
	AggregateVersion string          `json:"aggregateVersion"` // Decimal-string per EVENT_ARCHITECTURE.md
	OccurredAt       time.Time       `json:"occurredAt"`
	Traceparent      *string         `json:"traceparent,omitempty"`
	Payload          json.RawMessage `json:"payload"`
}

// Event represents an immutable row stored in outbox_events.
type Event struct {
	ID               uuid.UUID       `json:"id"`
	EventType        string          `json:"eventType"`
	SchemaVersion    int             `json:"schemaVersion"`
	AggregateType    string          `json:"aggregateType"`
	AggregateID      uuid.UUID       `json:"aggregateId"`
	AggregateVersion int64           `json:"aggregateVersion"`
	OccurredAt       time.Time       `json:"occurredAt"`
	Payload          json.RawMessage `json:"payload"`
	Traceparent      *string         `json:"traceparent,omitempty"`
	CreatedAt        time.Time       `json:"createdAt"`
}

// Envelope converts the stored event into the canonical public envelope.
func (e *Event) Envelope() Envelope {
	return Envelope{
		EventID:          e.ID,
		EventType:        e.EventType,
		SchemaVersion:    e.SchemaVersion,
		AggregateType:    e.AggregateType,
		AggregateID:      e.AggregateID,
		AggregateVersion: strconv.FormatInt(e.AggregateVersion, 10),
		OccurredAt:       e.OccurredAt,
		Traceparent:      e.Traceparent,
		Payload:          e.Payload,
	}
}

// PaymentPayload represents the sanitized typed payload for payment events.
// It excludes notes, credentials, and customer personal identifiers.
type PaymentPayload struct {
	PaymentID         uuid.UUID  `json:"paymentId"`
	PreviousState     *string    `json:"previousState"`
	NewState          string     `json:"newState"`
	EventVersion      int64      `json:"eventVersion"`
	SourceBankID      *uuid.UUID `json:"sourceBankId,omitempty"`
	DestinationBankID *uuid.UUID `json:"destinationBankId,omitempty"`
	RouteBankID       *uuid.UUID `json:"routeBankId,omitempty"`
	AmountPaise       int64      `json:"amountPaise"`
	Currency          string     `json:"currency"`
	FailureReason     *string    `json:"failureReason,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

// RecoveryPayload represents the sanitized typed payload for recovery events.
type RecoveryPayload struct {
	AttemptID      *uuid.UUID `json:"attemptId,omitempty"`
	PaymentID      uuid.UUID  `json:"paymentId"`
	LeaseEpoch     int64      `json:"leaseEpoch"`
	WorkerIdentity *string    `json:"workerIdentity,omitempty"`
	StateBefore    *string    `json:"stateBefore,omitempty"`
	StateAfter     *string    `json:"stateAfter,omitempty"`
	Result         *string    `json:"result,omitempty"`
	ReasonCode     *string    `json:"reasonCode,omitempty"`
	StartedAt      *time.Time `json:"startedAt,omitempty"`
	FinishedAt     *time.Time `json:"finishedAt,omitempty"`
	Status         *string    `json:"status,omitempty"`
	AttemptCount   *int       `json:"attemptCount,omitempty"`
	LastReasonCode *string    `json:"lastReasonCode,omitempty"`
	LastAttemptID  *uuid.UUID `json:"lastAttemptId,omitempty"`
	UpdatedAt      *time.Time `json:"updatedAt,omitempty"`
}

// Delivery represents a consumer delivery state row in outbox_deliveries.
type Delivery struct {
	ConsumerName  string     `json:"consumerName"`
	EventID       uuid.UUID  `json:"eventId"`
	Status        string     `json:"status"`
	Attempts      int        `json:"attempts"`
	NextAttemptAt time.Time  `json:"nextAttemptAt"`
	LeaseOwner    *string    `json:"leaseOwner,omitempty"`
	LeaseUntil    *time.Time `json:"leaseUntil,omitempty"`
	LeaseEpoch    int64      `json:"leaseEpoch"`
	DeliveredAt   *time.Time `json:"deliveredAt,omitempty"`
	LastErrorCode *string    `json:"lastErrorCode,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
}

// ParsePaymentPayload extracts PaymentPayload from raw JSON.
func (e *Event) ParsePaymentPayload() (*PaymentPayload, error) {
	if e.AggregateType != AggregateTypePayment {
		return nil, fmt.Errorf("event aggregate is %s, expected payment", e.AggregateType)
	}
	var p PaymentPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return nil, fmt.Errorf("unmarshal payment payload: %w", err)
	}
	return &p, nil
}

// ParseRecoveryPayload extracts RecoveryPayload from raw JSON.
func (e *Event) ParseRecoveryPayload() (*RecoveryPayload, error) {
	if e.AggregateType != AggregateTypeRecovery {
		return nil, fmt.Errorf("event aggregate is %s, expected recovery", e.AggregateType)
	}
	var r RecoveryPayload
	if err := json.Unmarshal(e.Payload, &r); err != nil {
		return nil, fmt.Errorf("unmarshal recovery payload: %w", err)
	}
	return &r, nil
}
