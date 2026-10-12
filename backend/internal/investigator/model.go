package investigator

import (
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
	ErrJobNotFound          = errors.New("investigation job not found")
	ErrIdempotencyConflict  = errors.New("idempotency key conflict: request parameters do not match existing job")
	ErrActorLimitExceeded   = errors.New("actor active investigation limit exceeded (maximum 1 active job per actor)")
	ErrGlobalLimitExceeded  = errors.New("global active investigation limit exceeded (maximum 2 active jobs globally)")
	ErrRateLimitExceeded    = errors.New("investigation rate limit exceeded (maximum 3 requests per actor per minute)")
	ErrLeaseLost            = errors.New("investigation job lease lost or superseded")
	ErrInvalidQuestion      = errors.New("question must be between 1 and 1000 characters")
	ErrInvalidPaymentID     = errors.New("paymentId must be a valid UUID")
)

// Assertion represents a typed fact assertion.
type Assertion struct {
	Field      string  `json:"field"`
	Value      string  `json:"value"`
	EntityID   *string `json:"entityId"`
	EvidenceID string  `json:"evidenceId"`
}

// Fact represents a grounded observation with typed assertions.
type Fact struct {
	Text          string      `json:"text"`
	EvidenceIDs   []string    `json:"evidenceIds"`
	Assertions    []Assertion `json:"assertions"`
	Contradiction bool        `json:"contradiction"`
}

// Inference represents an inferred statement.
type Inference struct {
	Text          string   `json:"text"`
	EvidenceIDs   []string `json:"evidenceIds"`
	Contradiction bool     `json:"contradiction"`
}

// Recommendation represents an operational suggestion citing an approved runbook chunk.
type Recommendation struct {
	Text            string   `json:"text"`
	RunbookChunkIDs []string `json:"runbookChunkIds"`
}

// InvestigationReport conforms to v2.schema.json and tx.investigation.v1.
type InvestigationReport struct {
	SchemaVersion    string           `json:"schemaVersion"`
	Mode             string           `json:"mode"`
	Summary          string           `json:"summary"`
	Facts            []Fact           `json:"facts"`
	Inferences       []Inference      `json:"inferences"`
	Recommendations  []Recommendation `json:"recommendations"`
	MissingEvidence  []string         `json:"missingEvidence"`
	EvidenceCoverage string           `json:"evidenceCoverage"`
	SnapshotID       string           `json:"snapshotId"`
	CollectedAt      time.Time        `json:"collectedAt"`
}

// ToolStep captures an observable tool execution step in the timeline.
type ToolStep struct {
	ToolName    string         `json:"toolName"`
	Arguments   map[string]any `json:"arguments"`
	StartedAt   time.Time      `json:"startedAt"`
	FinishedAt  *time.Time     `json:"finishedAt"`
	Status      string         `json:"status"`
	EvidenceIDs []string       `json:"evidenceIds"`
}

// UsageInfo tracks model token usage and prompt metadata.
type UsageInfo struct {
	InputTokens   *int    `json:"inputTokens"`
	OutputTokens  *int    `json:"outputTokens"`
	Model         *string `json:"model"`
	PromptVersion string  `json:"promptVersion"`
}

// Warning captures an error or advisory message conforming to Warning in v2.schema.json.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Investigation represents the authoritative investigation response DTO conforming to v2.schema.json.
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

// InvestigationAccepted represents the 202 response DTO for starting an investigation.
type InvestigationAccepted struct {
	InvestigationID uuid.UUID `json:"investigationId"`
	Status          string    `json:"status"`
	Mode            string    `json:"mode"`
}

// CreateJobRequest represents the HTTP body for POST /api/ops/investigations.
type CreateJobRequest struct {
	PaymentID string `json:"paymentId"`
	Question  string `json:"question"`
}

// CorpusChunk represents an approved runbook chunk in runbook_corpus_chunks.
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

// JobRecord represents the internal database row in investigation_jobs.
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

// UsageReservation represents a token reservation in investigation_usage_reservations.
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
