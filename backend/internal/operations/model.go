package operations

import (
	"time"

	"github.com/google/uuid"
)

// Allowed central payment states for worklist filtering.
var AllowedCentralStates = map[string]bool{
	"CREATED":                      true,
	"VALIDATING":                   true,
	"LOCAL_SETTLEMENT":             true,
	"ROUTING":                      true,
	"PROCESSING":                   true,
	"BANK_SETTLED_CENTRAL_PENDING": true,
	"COMMITTED":                    true,
	"COMPLETED":                    true,
	"FAILED":                       true,
	"PENDING_RECONCILIATION":       true,
	"REVERSED":                     true,
}

// Terminal central states that are excluded from default worklist scope.
var TerminalCentralStates = map[string]bool{
	"COMPLETED": true,
	"FAILED":    true,
	"REVERSED":  true,
}

// WorklistFilter represents input query filters for GET /api/ops/worklist.
type WorklistFilter struct {
	State          string
	RecoveryStatus string
	BankCode       string
	PaymentID      *uuid.UUID
	CreatedFrom    *time.Time
	CreatedTo      *time.Time
	Limit          int
	Cursor         string
}

// Worklist represents the response data for the worklist projection.
type Worklist struct {
	Items      []WorklistItem           `json:"items"`
	NextCursor *string                  `json:"nextCursor"`
	AsOf       time.Time                `json:"asOf"`
	Projection WorklistProjectionStatus `json:"projection"`
}

// WorklistProjectionStatus tracks the lag and status of the worklist projection.
type WorklistProjectionStatus struct {
	Status                  string     `json:"status"`
	UpdatedAt               *time.Time `json:"updatedAt"`
	OldestPendingEventAgeMs *int64     `json:"oldestPendingEventAgeMs"`
}

// WorklistItem represents an item in the operator worklist.
type WorklistItem struct {
	PaymentID           uuid.UUID  `json:"paymentId"`
	State               string     `json:"state"`
	AmountPaise         string     `json:"amountPaise"`
	Currency            string     `json:"currency"`
	SourceBankCode      *string    `json:"sourceBankCode"`
	DestinationBankCode *string    `json:"destinationBankCode"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
	AgeMs               int64      `json:"ageMs"`
	RecoveryStatus      string     `json:"recoveryStatus"`
	AttemptCount        int        `json:"attemptCount"`
	NextAttemptAt       *time.Time `json:"nextAttemptAt"`
	ReasonCode          *string    `json:"reasonCode"`
	ProjectionVersion   string     `json:"projectionVersion"`
}

// WorklistCursorPayload represents the validated content decoded from an opaque cursor.
type WorklistCursorPayload struct {
	UpdatedAt  time.Time `json:"u"`
	PaymentID  uuid.UUID `json:"p"`
	FilterHash string    `json:"h"`
}

// AttemptsCursorPayload represents cursor pagination state for recovery attempts.
type AttemptsCursorPayload struct {
	StartedAt time.Time `json:"s"`
	AttemptID uuid.UUID `json:"a"`
}

// EvidenceBundle represents a complete snapshot of evidence for a payment.
type EvidenceBundle struct {
	SchemaVersion        string                   `json:"schemaVersion"`
	PaymentID            uuid.UUID                `json:"paymentId"`
	SnapshotID           string                   `json:"snapshotId"`
	CollectedAt          time.Time                `json:"collectedAt"`
	Payment              PaymentSnapshot          `json:"payment"`
	Route                RouteSnapshot            `json:"route"`
	Operations           []OperationSnapshot      `json:"operations"`
	Transitions          []TransitionSnapshot     `json:"transitions"`
	TransitionsTruncated bool                     `json:"transitionsTruncated"`
	Recovery             RecoverySnapshot         `json:"recovery"`
	Reconciliation       ReconciliationSnapshot   `json:"reconciliation"`
	Integrity            IntegritySnapshot        `json:"integrity"`
	Traces               TracesSnapshot           `json:"traces"`
	Sources              []SourceSnapshot         `json:"sources"`
	Warnings             []WarningSnapshot        `json:"warnings"`
}

// PaymentSnapshot holds authoritative central payment state.
type PaymentSnapshot struct {
	State        string     `json:"state"`
	AmountPaise  string     `json:"amountPaise"`
	Currency     string     `json:"currency"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	CompletedAt  *time.Time `json:"completedAt"`
	EventVersion string     `json:"eventVersion"`
	EvidenceID   string     `json:"evidenceId"`
}

// RouteSnapshot holds persisted routing decision facts.
type RouteSnapshot struct {
	SourceBankCode      *string    `json:"sourceBankCode"`
	DestinationBankCode *string    `json:"destinationBankCode"`
	ExecutionTargetID   *string    `json:"executionTargetId"`
	SelectedAt          *time.Time `json:"selectedAt"`
	ReasonCode          *string    `json:"reasonCode"`
	Status              string     `json:"status"`
}

// OperationSnapshot holds central and participant operation observation.
type OperationSnapshot struct {
	OperationID       uuid.UUID  `json:"operationId"`
	OperationType     string     `json:"operationType"`
	BankCode          string     `json:"bankCode"`
	CentralStatus     string     `json:"centralStatus"`
	ParticipantStatus *string    `json:"participantStatus"`
	Presence          string     `json:"presence"`
	LastObservedAt    *time.Time `json:"lastObservedAt"`
	SourceStatus      string     `json:"sourceStatus"`
	EvidenceID        string     `json:"evidenceId"`
}

// TransitionSnapshot holds an immutable payment state change.
type TransitionSnapshot struct {
	ID         uuid.UUID `json:"id"`
	Sequence   string    `json:"sequence"`
	FromState  string    `json:"fromState"`
	ToState    string    `json:"toState"`
	OccurredAt time.Time `json:"occurredAt"`
	EvidenceID string    `json:"evidenceId"`
}

// RecoverySnapshot holds recovery job summary state.
type RecoverySnapshot struct {
	Status        string     `json:"status"`
	AttemptCount  int        `json:"attemptCount"`
	NextAttemptAt *time.Time `json:"nextAttemptAt"`
	LastAttemptID *uuid.UUID `json:"lastAttemptId"`
	ReasonCode    *string    `json:"reasonCode"`
	SourceStatus  string     `json:"sourceStatus"`
}

// ReconciliationSnapshot holds related reconciliation run observations.
type ReconciliationSnapshot struct {
	SourceStatus     string      `json:"sourceStatus"`
	RunIDs           []uuid.UUID `json:"runIds"`
	DiscrepancyCount *int        `json:"discrepancyCount"`
	ScopeMismatch    bool        `json:"scopeMismatch"`
}

// IntegritySnapshot holds latest runtime integrity check summary.
type IntegritySnapshot struct {
	SourceStatus   string     `json:"sourceStatus"`
	RunID          *uuid.UUID `json:"runId"`
	OccurredAt     *time.Time `json:"occurredAt"`
	ViolationCount *int       `json:"violationCount"`
}

// TracesSnapshot holds distributed tracing context.
type TracesSnapshot struct {
	SourceStatus string   `json:"sourceStatus"`
	TraceIDs     []string `json:"traceIds"`
	Truncated    bool     `json:"truncated"`
}

// SourceSnapshot records observation timestamps and errors per source.
type SourceSnapshot struct {
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	ObservedAt *time.Time `json:"observedAt"`
	ErrorCode  *string    `json:"errorCode"`
}

// WarningSnapshot provides human-readable warning codes and safe operator copy.
type WarningSnapshot struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// RecoveryAttemptItem represents a single recovery attempt in history.
type RecoveryAttemptItem struct {
	AttemptID    uuid.UUID   `json:"attemptId"`
	PaymentID    uuid.UUID   `json:"paymentId"`
	WorkerID     string      `json:"workerId"`
	StartedAt    time.Time   `json:"startedAt"`
	FinishedAt   *time.Time  `json:"finishedAt"`
	StateBefore  string      `json:"stateBefore"`
	StateAfter   *string     `json:"stateAfter"`
	Result       string      `json:"result"`
	ReasonCode   *string     `json:"reasonCode"`
	OperationIds []uuid.UUID `json:"operationIds"`
	TraceID      *string     `json:"traceId"`
	LeaseEpoch   string      `json:"leaseEpoch"`
}

// RecoveryAttemptsPage represents a paginated list of recovery attempts.
type RecoveryAttemptsPage struct {
	Items      []RecoveryAttemptItem `json:"items"`
	NextCursor *string               `json:"nextCursor"`
	AsOf       time.Time             `json:"asOf"`
}

// RecoveryRecheckRequest represents request body for POST /api/ops/payments/{paymentID}/recovery/recheck.
type RecoveryRecheckRequest struct {
	Reason string `json:"reason"`
}

// RecoveryRecheckResult represents response data for recovery recheck.
type RecoveryRecheckResult struct {
	PaymentID   uuid.UUID  `json:"paymentId"`
	ScheduledAt *time.Time `json:"scheduledAt"`
	Status      string     `json:"status"`
}

// ExportManifest contains the content hash and export metadata.
type ExportManifest struct {
	PaymentID    uuid.UUID `json:"paymentId"`
	SnapshotID   string    `json:"snapshotId"`
	BundleSHA256 string    `json:"bundleSha256"`
	ExportedAt   time.Time `json:"exportedAt"`
	Algorithm    string    `json:"algorithm"`
	Note         string    `json:"note"`
}

// EvidenceExportBundle combines the evidence bundle and its verification manifest.
type EvidenceExportBundle struct {
	Bundle   EvidenceBundle `json:"bundle"`
	Manifest ExportManifest `json:"manifest"`
}
