package investigator

import (
	"context"
	"encoding/json"
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
	Question                                               string
	Evidence                                               EvidenceBundle
	Chunks                                                 []RunbookChunk
	PromptVersion                                          string
	InputTokenLimit, OutputTokenLimit, ToolRoundsRemaining int
	Continuation                                           []json.RawMessage
	ToolOutputs                                            []ProviderToolOutput
}
type Usage struct {
	InputTokens  *int64 `json:"inputTokens"`
	OutputTokens *int64 `json:"outputTokens"`
}
type GenerationResult struct {
	Report            *InvestigationReport
	ToolCalls         []ProviderToolCall
	Continuation      []json.RawMessage
	Usage             Usage
	ProviderRequestID string
	ProviderModel     string
}
type ProviderToolCall struct {
	CallID    string
	Name      string
	Arguments json.RawMessage
}
type ProviderToolOutput struct {
	CallID string
	Name   string
	Output json.RawMessage
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
	UsageReservationID                                                  string
	ProviderRounds                                                      int
	ProviderCallPending                                                 bool
	ProviderRequestIDs                                                  []string
	ProviderModel                                                       string
	PromptVersion                                                       string
	ProviderContinuation                                                []json.RawMessage
	ProviderPendingToolCalls                                            []ProviderToolCall
	ProviderToolOutputs                                                 []ProviderToolOutput
	ProviderPendingReport                                               *InvestigationReport
	UsageKnown                                                          bool
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
// reservations. Every mutating method after Claim rejects stale lease
// owners/epochs. CompleteUsage atomically persists the terminal Job and actual
// usage. MarkUsageUncertain is idempotent and retains the reserved allowance.
type InvestigationStore interface {
	Create(context.Context, NewJob) (Job, bool, error)
	Claim(context.Context, string, string, time.Duration) (Job, error)
	SaveCheckpoint(context.Context, Job) error
	Get(context.Context, TrustedScope, string) (Job, error)
	ReserveUsage(context.Context, Job, int64, int64) (string, error)
	CompleteUsage(context.Context, Job) error
	MarkUsageUncertain(context.Context, Job) error
}

// TraceReader is restricted to IDs already present in selected evidence.
type TraceReader interface {
	ReadTrace(context.Context, TrustedScope, string) (Trace, error)
}
