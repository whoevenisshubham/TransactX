package investigator

import (
	"context"
	"time"
)

// TrustedScope is supplied by authenticated server code, never decoded from tool arguments.
type TrustedScope struct{ ActorID, Role, SelectedPaymentID string }
type EvidenceReader interface {
	ReadPaymentEvidence(context.Context, TrustedScope, string) (EvidenceBundle, error)
}
type RecoveryReader interface {
	ReadRecoveryAttempts(context.Context, TrustedScope, string, int) ([]RecoveryAttempt, error)
}
type RunbookChunk struct {
	ID, Path, Heading, SHA256, Version, Content string
	StartLine, EndLine                          int
	AllowedRoles                                []string
	SourceSHA256, ImplementationStatus          string
	Tags                                        []string
}
type RunbookRetriever interface {
	Retrieve(context.Context, TrustedScope, string, []string, int) ([]RunbookChunk, error)
}
type AnalyticsReader interface {
	ReadWindows(context.Context, TrustedScope, string, string, string, string, int) ([]AnalyticsWindow, error)
}

type ReconciliationReader interface {
	ReadRelatedReconciliation(context.Context, TrustedScope, string, string) (ReconciliationContext, error)
}
type GenerationRequest struct {
	Scope                                                  TrustedScope
	Evidence                                               EvidenceBundle
	Chunks                                                 []RunbookChunk
	PromptVersion                                          string
	InputTokenLimit, OutputTokenLimit, ToolRoundsRemaining int
}
type Usage struct {
	InputTokens  *int64 `json:"inputTokens"`
	OutputTokens *int64 `json:"outputTokens"`
}
type GenerationResult struct {
	Report            *InvestigationReport
	Usage             Usage
	ProviderRequestID string
}
type Provider interface {
	Generate(context.Context, GenerationRequest) (GenerationResult, error)
}

type Job struct {
	ID, ActorID, Role, PaymentID, Question, IdempotencyKey, RequestHash string
	Status, Mode, LeaseOwner, ErrorCode                                 string
	LeaseEpoch                                                          int64
	LeaseExpiresAt, DeadlineAt, CreatedAt, CompletedAt                  time.Time
	Snapshot                                                            *EvidenceBundle
	SnapshotHash                                                        string
	Chunks                                                              []RunbookChunk
	RetrievalComplete                                                   bool
	ToolTimeline                                                        []ToolTimelineEntry
	ToolCallKeys                                                        []string
	SourceHashes                                                        []SourceHash
	EvidenceAttempts, RetrievalAttempts                                 int
	Report                                                              *InvestigationReport
	Usage                                                               Usage
}

type NewJob struct {
	Scope                               TrustedScope
	PaymentID, Question, IdempotencyKey string
	RequestHash, Mode                   string
	DeadlineAt                          time.Time
	CreatedAt                           time.Time
}

// M2 must implement actor-scoped idempotency, lease epochs and transactional
// reservations. SaveCheckpoint rejects stale lease owners/epochs.
type InvestigationStore interface {
	Create(context.Context, NewJob) (Job, bool, error)
	Claim(context.Context, string, string, time.Duration) (Job, error)
	SaveCheckpoint(context.Context, Job) error
	Get(context.Context, TrustedScope, string) (Job, error)
	ReserveUsage(context.Context, string, int64, int64) (string, error)
	CompleteUsage(context.Context, string, Usage) error
}

// TraceReader is restricted to IDs already present in selected evidence.
type TraceReader interface {
	ReadTrace(context.Context, TrustedScope, string) (Trace, error)
}
