package investigator

import "context"

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
}
type RunbookRetriever interface {
	Retrieve(context.Context, TrustedScope, string, []string, int) ([]RunbookChunk, error)
}
type AnalyticsReader interface {
	ReadWindows(context.Context, TrustedScope, string, string, string, int) ([]AnalyticsWindow, error)
}
type GenerationRequest struct {
	Scope                                                  TrustedScope
	Evidence                                               EvidenceBundle
	Chunks                                                 []RunbookChunk
	PromptVersion                                          string
	InputTokenLimit, OutputTokenLimit, ToolRoundsRemaining int
}
type Usage struct{ InputTokens, OutputTokens *int64 }
type GenerationResult struct {
	Report            *InvestigationReport
	Usage             Usage
	ProviderRequestID string
}
type Provider interface {
	Generate(context.Context, GenerationRequest) (GenerationResult, error)
}

// M2 must implement actor-scoped idempotency, lease epochs and transactional reservations.
type Job struct {
	ID, ActorID, PaymentID, Status, Mode, LeaseOwner string
	LeaseEpoch                                       int64
	Snapshot                                         *EvidenceBundle
	Report                                           *InvestigationReport
}
type InvestigationStore interface {
	Create(context.Context, TrustedScope, string, string, string) (Job, error)
	Claim(context.Context, string, string) (Job, error)
	SaveCheckpoint(context.Context, Job) error
	ReserveUsage(context.Context, string, int64, int64) (string, error)
	CompleteUsage(context.Context, string, Usage) error
}

// TraceReader is restricted to IDs already present in selected evidence.
type TraceReader interface {
	ReadTrace(context.Context, TrustedScope, string) (Trace, error)
}
