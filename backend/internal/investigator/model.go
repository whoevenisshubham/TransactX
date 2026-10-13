package investigator

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// SchemaVersionV1 is the frozen schema version for investigation reports.
const SchemaVersionV1 = "tx.investigation.v1"

// Mode constants.
const (
	ModeEvidenceOnly = "evidence_only"
	ModeLLM          = "llm"
)

// Status constants for investigation jobs.
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// EvidenceCoverage constants.
const (
	CoverageCompleteForQuestion = "complete_for_question"
	CoverageLimited             = "limited"
	CoverageInsufficient        = "insufficient"
)

// ToolStep status constants.
const (
	ToolStepStatusRunning   = "running"
	ToolStepStatusCompleted = "completed"
	ToolStepStatusFailed    = "failed"
	ToolStepStatusRejected  = "rejected"
)

// Standard domain errors.
var (
	ErrRateLimitExceeded = errors.New("investigation rate limit exceeded (maximum 3 requests per actor per minute)")
	ErrInvalidQuestion   = errors.New("question must be between 1 and 1000 characters")
	ErrInvalidPaymentID  = errors.New("paymentId must be a valid UUID")
)

type AnalyticsWindow struct {
	Id                   string        `json:"id"`
	EntityType           string        `json:"entityType"`
	EntityId             string        `json:"entityId"`
	From                 string        `json:"from"`
	To                   string        `json:"to"`
	SampleCount          int64         `json:"sampleCount"`
	FeatureSchemaVersion string        `json:"featureSchemaVersion"`
	Features             FeatureVector `json:"features"`
	BaselineAlert        *Warning      `json:"baselineAlert"`
	Model                struct {
		Version   *string  `json:"version"`
		Status    string   `json:"status"`
		Score     *float64 `json:"score"`
		Threshold *float64 `json:"threshold"`
	} `json:"model"`
	Advisory bool `json:"advisory"`
}

type Assertion struct {
	Field      string  `json:"field"`
	Value      string  `json:"value"`
	EntityId   *string `json:"entityId"`
	EvidenceId string  `json:"evidenceId"`
}

type CentralPaymentState string

type EvidenceBundle struct {
	SchemaVersion        string          `json:"schemaVersion"`
	PaymentId            string          `json:"paymentId"`
	SnapshotId           string          `json:"snapshotId"`
	CollectedAt          string          `json:"collectedAt"`
	Payment              PaymentSnapshot `json:"payment"`
	Route                Route           `json:"route"`
	Operations           []Operation     `json:"operations"`
	Transitions          []Transition    `json:"transitions"`
	TransitionsTruncated bool            `json:"transitionsTruncated"`
	Recovery             RecoverySummary `json:"recovery"`
	Reconciliation       struct {
		SourceStatus     SourceStatus `json:"sourceStatus"`
		RunIds           []string     `json:"runIds"`
		DiscrepancyCount *int64       `json:"discrepancyCount"`
		ScopeMismatch    bool         `json:"scopeMismatch"`
	} `json:"reconciliation"`
	Integrity struct {
		SourceStatus   SourceStatus `json:"sourceStatus"`
		RunId          *string      `json:"runId"`
		OccurredAt     *string      `json:"occurredAt"`
		ViolationCount *int64       `json:"violationCount"`
	} `json:"integrity"`
	Traces struct {
		SourceStatus SourceStatus `json:"sourceStatus"`
		TraceIds     []string     `json:"traceIds"`
		Truncated    bool         `json:"truncated"`
	} `json:"traces"`
	Sources  []Source  `json:"sources"`
	Warnings []Warning `json:"warnings"`
}

type Fact struct {
	Text          string      `json:"text"`
	EvidenceIds   []string    `json:"evidenceIds"`
	Assertions    []Assertion `json:"assertions"`
	Contradiction bool        `json:"contradiction"`
}

type FeatureVector struct {
	TimeoutRate                *float64 `json:"timeoutRate"`
	FailureRate                *float64 `json:"failureRate"`
	P95LatencyMs               *float64 `json:"p95LatencyMs"`
	OldestRecoveryAgeMs        *float64 `json:"oldestRecoveryAgeMs"`
	RecoveryAttemptsPerPayment *float64 `json:"recoveryAttemptsPerPayment"`
	HealthAvailabilityRate     *float64 `json:"healthAvailabilityRate"`
}

type Inference struct {
	Text          string   `json:"text"`
	EvidenceIds   []string `json:"evidenceIds"`
	Contradiction bool     `json:"contradiction"`
}

type InvestigationReport struct {
	SchemaVersion    string           `json:"schemaVersion"`
	Mode             string           `json:"mode"`
	Summary          string           `json:"summary"`
	Facts            []Fact           `json:"facts"`
	Inferences       []Inference      `json:"inferences"`
	Recommendations  []Recommendation `json:"recommendations"`
	MissingEvidence  []string         `json:"missingEvidence"`
	EvidenceCoverage string           `json:"evidenceCoverage"`
	SnapshotId       string           `json:"snapshotId"`
	CollectedAt      string           `json:"collectedAt"`
}

type Operation struct {
	OperationId       string       `json:"operationId"`
	OperationType     string       `json:"operationType"`
	BankCode          string       `json:"bankCode"`
	CentralStatus     string       `json:"centralStatus"`
	ParticipantStatus *string      `json:"participantStatus"`
	Presence          string       `json:"presence"`
	LastObservedAt    *string      `json:"lastObservedAt"`
	SourceStatus      SourceStatus `json:"sourceStatus"`
	EvidenceId        string       `json:"evidenceId"`
}

type PaymentSnapshot struct {
	State        CentralPaymentState `json:"state"`
	AmountPaise  string              `json:"amountPaise"`
	Currency     string              `json:"currency"`
	CreatedAt    string              `json:"createdAt"`
	UpdatedAt    string              `json:"updatedAt"`
	CompletedAt  *string             `json:"completedAt"`
	EventVersion string              `json:"eventVersion"`
	EvidenceId   string              `json:"evidenceId"`
}

type Recommendation struct {
	Text            string   `json:"text"`
	RunbookChunkIds []string `json:"runbookChunkIds"`
}

type ReconciliationContext struct {
	RunId            string       `json:"runId"`
	PaymentId        string       `json:"paymentId"`
	SourceStatus     SourceStatus `json:"sourceStatus"`
	DiscrepancyCount *int64       `json:"discrepancyCount"`
	ScopeMismatch    bool         `json:"scopeMismatch"`
}

type RecoveryAttempt struct {
	AttemptId    string   `json:"attemptId"`
	PaymentId    string   `json:"paymentId"`
	WorkerId     string   `json:"workerId"`
	StartedAt    string   `json:"startedAt"`
	FinishedAt   *string  `json:"finishedAt"`
	StateBefore  string   `json:"stateBefore"`
	StateAfter   *string  `json:"stateAfter"`
	Result       string   `json:"result"`
	ReasonCode   *string  `json:"reasonCode"`
	OperationIds []string `json:"operationIds"`
	TraceId      *string  `json:"traceId"`
	LeaseEpoch   string   `json:"leaseEpoch"`
}

type RecoverySummary struct {
	Status        string       `json:"status"`
	AttemptCount  int64        `json:"attemptCount"`
	NextAttemptAt *string      `json:"nextAttemptAt"`
	LastAttemptId *string      `json:"lastAttemptId"`
	ReasonCode    *string      `json:"reasonCode"`
	SourceStatus  SourceStatus `json:"sourceStatus"`
}

type Route struct {
	SourceBankCode      *string      `json:"sourceBankCode"`
	DestinationBankCode *string      `json:"destinationBankCode"`
	ExecutionTargetId   *string      `json:"executionTargetId"`
	SelectedAt          *string      `json:"selectedAt"`
	ReasonCode          *string      `json:"reasonCode"`
	Status              SourceStatus `json:"status"`
}

type Source struct {
	Name       string       `json:"name"`
	Status     SourceStatus `json:"status"`
	ObservedAt *string      `json:"observedAt"`
	ErrorCode  *string      `json:"errorCode"`
}

type SourceStatus string

type Transition struct {
	Id         string `json:"id"`
	Sequence   string `json:"sequence"`
	FromState  string `json:"fromState"`
	ToState    string `json:"toState"`
	OccurredAt string `json:"occurredAt"`
	EvidenceId string `json:"evidenceId"`
}

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type SourceHash struct {
	ToolName string `json:"toolName"`
	SHA256   string `json:"sha256"`
}

type ToolTimelineEntry struct {
	ToolName    string          `json:"toolName"`
	Arguments   json.RawMessage `json:"arguments"`
	StartedAt   string          `json:"startedAt"`
	FinishedAt  string          `json:"finishedAt"`
	Status      string          `json:"status"`
	EvidenceIds []string        `json:"evidenceIds"`
}

type Trace struct {
	TraceId      string       `json:"traceId"`
	SourceStatus SourceStatus `json:"sourceStatus"`
	CollectedAt  string       `json:"collectedAt"`
	Truncated    bool         `json:"truncated"`
	Spans        []Span       `json:"spans"`
}

type Span struct {
	SpanId       string             `json:"spanId"`
	ParentSpanId *string            `json:"parentSpanId"`
	Service      string             `json:"service"`
	Name         string             `json:"name"`
	StartedAt    string             `json:"startedAt"`
	FinishedAt   *string            `json:"finishedAt"`
	Status       string             `json:"status"`
	Attributes   map[string]*string `json:"attributes"`
}

// ToolStep captures an observable tool execution step in the timeline (M2 compatibility).
type ToolStep struct {
	ToolName    string         `json:"toolName"`
	Arguments   map[string]any `json:"arguments"`
	StartedAt   time.Time      `json:"startedAt"`
	FinishedAt  *time.Time     `json:"finishedAt"`
	Status      string         `json:"status"`
	EvidenceIDs []string       `json:"evidenceIds"`
}

// UsageInfo tracks model token usage and prompt metadata (M2 compatibility).
type UsageInfo struct {
	InputTokens   *int    `json:"inputTokens"`
	OutputTokens  *int    `json:"outputTokens"`
	Model         *string `json:"model"`
	PromptVersion string  `json:"promptVersion"`
}

// Investigation represents the authoritative investigation response DTO conforming to v2.schema.json (M2 compatibility).
type Investigation struct {
	ID           uuid.UUID            `json:"id"`
	Status       string               `json:"status"`
	Mode         string               `json:"mode"`
	Report       *InvestigationReport `json:"report"`
	ToolTimeline []ToolStep           `json:"toolTimeline"`
	Error        *Warning             `json:"error"`
	CreatedAt    time.Time            `json:"createdAt"`
	CompletedAt  *time.Time           `json:"completedAt"`
	Usage        UsageInfo            `json:"usage"`
}

// InvestigationAccepted represents the 202 response DTO for starting an investigation (M2 compatibility).
type InvestigationAccepted struct {
	InvestigationID uuid.UUID `json:"investigationId"`
	Status          string    `json:"status"`
	Mode            string    `json:"mode"`
}

// CreateJobRequest represents the HTTP body for POST /api/ops/investigations (M2 compatibility).
type CreateJobRequest struct {
	PaymentID string `json:"paymentId"`
	Question  string `json:"question"`
}

// CorpusChunk represents an approved runbook chunk in runbook_corpus_chunks (M2 compatibility).
type CorpusChunk struct {
	ID            uuid.UUID `json:"id"`
	CorpusVersion string    `json:"corpusVersion"`
	DocumentPath  string    `json:"documentPath"`
	Heading       string    `json:"heading"`
	StartLine     int       `json:"startLine"`
	EndLine       int       `json:"endLine"`
	Content       string    `json:"content"`
	SHA256        string    `json:"sha256"`
	ApprovedTags  []string  `json:"approvedTags"`
	AllowedRoles  []string  `json:"allowedRoles"`
	Active        bool      `json:"active"`
	CreatedAt     time.Time `json:"createdAt"`
}

// JobRecord represents the internal database row in investigation_jobs (M2 compatibility).
type JobRecord struct {
	ID             uuid.UUID
	PaymentID      uuid.UUID
	ActorID        string
	Status         string
	Mode           string
	Question       string
	Report         *InvestigationReport
	ToolTimeline   []ToolStep
	Usage          *UsageInfo
	ErrorCode      *string
	ErrorMessage   *string
	LeaseOwner     *string
	LeaseEpoch     int64
	LeaseExpiresAt *time.Time
	IdempotencyKey string
	CreatedAt      time.Time
	CompletedAt    *time.Time
}

// UsageReservation represents a token reservation in investigation_usage_reservations (M2 compatibility).
type UsageReservation struct {
	ID             uuid.UUID `json:"id"`
	JobID          uuid.UUID `json:"jobId"`
	ActorID        string    `json:"actorId"`
	Provider       string    `json:"provider"`
	Model          string    `json:"model"`
	ReservedTokens int       `json:"reservedTokens"`
	ActualTokens   *int      `json:"actualTokens"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}
