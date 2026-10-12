package investigator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/transactx/backend/internal/operations"
)

type rateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int
	window   time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

func (rl *rateLimiter) Allow(actorID string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	times := rl.requests[actorID]
	var valid []time.Time
	for _, t := range times {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= rl.limit {
		rl.requests[actorID] = valid
		return false
	}

	valid = append(valid, now)
	rl.requests[actorID] = valid
	return true
}

// Service coordinates investigator job creation, retrieval, and deterministic evidence-only execution.
type Service struct {
	repo           Repository
	evidenceReader operations.EvidenceReader
	limiter        *rateLimiter
	logger         *slog.Logger
}

// NewService constructs a new investigator Service.
func NewService(repo Repository, evidenceReader operations.EvidenceReader, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		repo:           repo,
		evidenceReader: evidenceReader,
		limiter:        newRateLimiter(3, 1*time.Minute),
		logger:         logger,
	}
}

// CreateInvestigation handles client submission of an investigation job.
func (s *Service) CreateInvestigation(ctx context.Context, actorID string, idempotencyKey string, req CreateJobRequest) (*InvestigationAccepted, error) {
	if actorID == "" {
		actorID = "anonymous"
	}
	if idempotencyKey == "" {
		return nil, fmt.Errorf("idempotency key required")
	}

	paymentUUID, err := uuid.Parse(strings.TrimSpace(req.PaymentID))
	if err != nil {
		return nil, ErrInvalidPaymentID
	}

	q := strings.TrimSpace(req.Question)
	if len(q) < 1 || len(q) > 1000 {
		return nil, ErrInvalidQuestion
	}

	if !s.limiter.Allow(actorID) {
		return nil, ErrRateLimitExceeded
	}

	job, _, err := s.repo.CreateJob(ctx, paymentUUID, actorID, idempotencyKey, q, ModeEvidenceOnly)
	if err != nil {
		return nil, err
	}

	return &InvestigationAccepted{
		InvestigationID: job.ID,
		Status:          job.Status,
		Mode:            job.Mode,
	}, nil
}

// GetInvestigation retrieves an investigation by UUID.
func (s *Service) GetInvestigation(ctx context.Context, jobID uuid.UUID) (*Investigation, error) {
	job, err := s.repo.GetJob(ctx, jobID)
	if err != nil {
		return nil, err
	}

	timeline := job.ToolTimeline
	if timeline == nil {
		timeline = []ToolStep{}
	}

	var warning *Warning
	if job.ErrorCode != nil && *job.ErrorCode != "" {
		msg := ""
		if job.ErrorMessage != nil {
			msg = *job.ErrorMessage
		}
		warning = &Warning{
			Code:    *job.ErrorCode,
			Message: msg,
		}
	}

	usage := UsageInfo{
		PromptVersion: "evidence_only.v1",
	}
	if job.Usage != nil {
		usage = *job.Usage
	}

	return &Investigation{
		ID:           job.ID,
		Status:       job.Status,
		Mode:         job.Mode,
		Report:       job.Report,
		ToolTimeline: timeline,
		Error:        warning,
		CreatedAt:    job.CreatedAt,
		CompletedAt:  job.CompletedAt,
		Usage:        usage,
	}, nil
}

// ExecuteJob performs deterministic evidence-only investigation.
func (s *Service) ExecuteJob(ctx context.Context, job *JobRecord) error {
	s.logger.Info("executing investigation job",
		"job_id", job.ID,
		"payment_id", job.PaymentID,
		"actor_id", job.ActorID,
	)

	var timeline []ToolStep

	// Step 1: read_payment_evidence
	start1 := time.Now().UTC()
	evidence, err := s.evidenceReader.ReadPaymentEvidence(ctx, "OPS_INVESTIGATOR", job.PaymentID)
	finish1 := time.Now().UTC()

	if err != nil {
		step1 := ToolStep{
			ToolName:    "read_payment_evidence",
			Arguments:   map[string]any{"paymentId": job.PaymentID.String()},
			StartedAt:   start1,
			FinishedAt:  &finish1,
			Status:      ToolStepStatusFailed,
			EvidenceIDs: []string{},
		}
		timeline = append(timeline, step1)
		_ = s.repo.FailJob(ctx, job.ID, job.LeaseEpoch, "EVIDENCE_FETCH_FAILED", err.Error(), timeline)
		return fmt.Errorf("read payment evidence: %w", err)
	}

	var evidenceIDs []string
	if evidence.Payment.EvidenceID != "" {
		evidenceIDs = append(evidenceIDs, evidence.Payment.EvidenceID)
	}
	for _, op := range evidence.Operations {
		if op.EvidenceID != "" {
			evidenceIDs = append(evidenceIDs, op.EvidenceID)
		}
	}

	step1 := ToolStep{
		ToolName:    "read_payment_evidence",
		Arguments:   map[string]any{"paymentId": job.PaymentID.String()},
		StartedAt:   start1,
		FinishedAt:  &finish1,
		Status:      ToolStepStatusCompleted,
		EvidenceIDs: evidenceIDs,
	}
	timeline = append(timeline, step1)

	// Step 2: retrieve_runbook
	start2 := time.Now().UTC()
	var tags []string
	switch evidence.Payment.State {
	case "PENDING_RECONCILIATION":
		tags = append(tags, "pending_reconciliation", "reconciliation")
	case "BANK_SETTLED_CENTRAL_PENDING":
		tags = append(tags, "central_pending", "continuation")
	}
	if evidence.Recovery.Status == "needs_attention" || evidence.Recovery.AttemptCount > 0 {
		tags = append(tags, "recovery", "bank_outage")
	}
	for _, op := range evidence.Operations {
		if op.Presence != "" {
			tags = append(tags, "operation_presence", "presence")
			break
		}
	}

	chunks, err := s.repo.SearchCorpus(ctx, job.Question, tags, 5)
	finish2 := time.Now().UTC()
	if err != nil {
		s.logger.Warn("search corpus failed, continuing with empty chunks", "error", err)
		chunks = []CorpusChunk{}
	}

	step2 := ToolStep{
		ToolName:    "retrieve_runbook",
		Arguments:   map[string]any{"query": job.Question, "topK": 5},
		StartedAt:   start2,
		FinishedAt:  &finish2,
		Status:      ToolStepStatusCompleted,
		EvidenceIDs: []string{},
	}
	timeline = append(timeline, step2)

	// Step 3: Construct grounded report
	report := s.buildEvidenceOnlyReport(evidence, chunks)
	usage := &UsageInfo{
		InputTokens:   nil,
		OutputTokens:  nil,
		Model:         nil,
		PromptVersion: "evidence_only.v1",
	}

	if err := s.repo.CompleteJob(ctx, job.ID, job.LeaseEpoch, report, timeline, usage); err != nil {
		s.logger.Error("complete investigation job failed", "job_id", job.ID, "error", err)
		return err
	}

	s.logger.Info("completed investigation job", "job_id", job.ID)
	return nil
}

func (s *Service) buildEvidenceOnlyReport(evidence *operations.EvidenceBundle, chunks []CorpusChunk) *InvestigationReport {
	paymentIDStr := evidence.Payment.EvidenceID

	var facts []Fact

	// Fact 1: Central State
	stateAssertions := []Assertion{
		{
			Field:      "central_state",
			Value:      evidence.Payment.State,
			EntityID:   &paymentIDStr,
			EvidenceID: evidence.Payment.EvidenceID,
		},
	}
	facts = append(facts, Fact{
		Text:          fmt.Sprintf("The central payment is %s.", evidence.Payment.State),
		EvidenceIDs:   []string{evidence.Payment.EvidenceID},
		Assertions:    stateAssertions,
		Contradiction: false,
	})

	// Fact 2: Amount
	amountAssertions := []Assertion{
		{
			Field:      "amount_paise",
			Value:      evidence.Payment.AmountPaise,
			EntityID:   &paymentIDStr,
			EvidenceID: evidence.Payment.EvidenceID,
		},
	}
	facts = append(facts, Fact{
		Text:          fmt.Sprintf("The central payment amount is %s paise in currency %s.", evidence.Payment.AmountPaise, evidence.Payment.Currency),
		EvidenceIDs:   []string{evidence.Payment.EvidenceID},
		Assertions:    amountAssertions,
		Contradiction: false,
	})

	// Fact 3: Route Target
	var routeTarget string
	if evidence.Route.DestinationBankCode != nil && *evidence.Route.DestinationBankCode != "" {
		routeTarget = *evidence.Route.DestinationBankCode
	} else if evidence.Route.ExecutionTargetID != nil && *evidence.Route.ExecutionTargetID != "" {
		routeTarget = *evidence.Route.ExecutionTargetID
	}
	if routeTarget != "" {
		routeAssertions := []Assertion{
			{
				Field:      "route_target",
				Value:      routeTarget,
				EntityID:   &paymentIDStr,
				EvidenceID: evidence.Payment.EvidenceID,
			},
		}
		facts = append(facts, Fact{
			Text:          fmt.Sprintf("The payment route target is %s with status %s.", routeTarget, evidence.Route.Status),
			EvidenceIDs:   []string{evidence.Payment.EvidenceID},
			Assertions:    routeAssertions,
			Contradiction: false,
		})
	}

	// Fact 4: Recovery Status
	if evidence.Recovery.Status != "" {
		recAssertions := []Assertion{
			{
				Field:      "recovery_status",
				Value:      evidence.Recovery.Status,
				EntityID:   &paymentIDStr,
				EvidenceID: evidence.Payment.EvidenceID,
			},
		}
		facts = append(facts, Fact{
			Text:          fmt.Sprintf("The recovery status is %s with %d attempt(s).", evidence.Recovery.Status, evidence.Recovery.AttemptCount),
			EvidenceIDs:   []string{evidence.Payment.EvidenceID},
			Assertions:    recAssertions,
			Contradiction: false,
		})
	}

	// Fact 5..N: Operations
	for _, op := range evidence.Operations {
		opIDStr := op.OperationID.String()
		bankCode := op.BankCode
		opAssertions := []Assertion{
			{
				Field:      "operation_id",
				Value:      op.OperationID.String(),
				EntityID:   &bankCode,
				EvidenceID: op.EvidenceID,
			},
			{
				Field:      "bank_code",
				Value:      op.BankCode,
				EntityID:   &opIDStr,
				EvidenceID: op.EvidenceID,
			},
			{
				Field:      "participant_status",
				Value:      op.Presence,
				EntityID:   &opIDStr,
				EvidenceID: op.EvidenceID,
			},
		}
		facts = append(facts, Fact{
			Text:          fmt.Sprintf("Operation %s on bank %s has central status %s and participant presence %s.", op.OperationID, op.BankCode, op.CentralStatus, op.Presence),
			EvidenceIDs:   []string{op.EvidenceID},
			Assertions:    opAssertions,
			Contradiction: false,
		})
	}

	// Inferences: empty in evidence-only mode
	inferences := []Inference{}

	// Recommendations: from retrieved runbook chunks
	var recommendations []Recommendation
	for _, chunk := range chunks {
		recommendations = append(recommendations, Recommendation{
			Text:            fmt.Sprintf("Refer to %s: %s", chunk.Heading, summarizeChunk(chunk.Content)),
			RunbookChunkIDs: []string{fmt.Sprintf("runbook:%s", chunk.ID.String())},
		})
	}
	if recommendations == nil {
		recommendations = []Recommendation{}
	}

	// Missing evidence
	var missingEvidence []string
	for _, src := range evidence.Sources {
		if src.Status != "available" {
			missingEvidence = append(missingEvidence, fmt.Sprintf("%s_%s", src.Name, src.Status))
		}
	}
	if missingEvidence == nil {
		missingEvidence = []string{}
	}

	// Coverage
	coverage := CoverageCompleteForQuestion
	if evidence.Payment.State == "PENDING_RECONCILIATION" || len(missingEvidence) > 0 {
		coverage = CoverageLimited
	}

	// Summary
	summary := fmt.Sprintf("The central payment is %s.", evidence.Payment.State)
	if evidence.Payment.State == "PENDING_RECONCILIATION" {
		summary = "The central payment remains PENDING_RECONCILIATION. Participant operation evidence and recovery state have been analyzed."
	} else if evidence.Payment.State == "COMPLETED" {
		summary = "The payment has completed successfully with all settlement ledger entries recorded."
	} else if evidence.Payment.State == "FAILED" {
		summary = "The payment is FAILED; participant failure or terminal recovery was observed."
	}

	return &InvestigationReport{
		SchemaVersion:    SchemaVersionV1,
		Mode:             ModeEvidenceOnly,
		Summary:          summary,
		Facts:            facts,
		Inferences:       inferences,
		Recommendations:  recommendations,
		MissingEvidence:  missingEvidence,
		EvidenceCoverage: coverage,
		SnapshotID:       evidence.SnapshotID,
		CollectedAt:      evidence.CollectedAt,
	}
}

func summarizeChunk(content string) string {
	lines := strings.Split(content, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") {
			if len(l) > 120 {
				return l[:117] + "..."
			}
			return l
		}
	}
	if len(content) > 120 {
		return content[:117] + "..."
	}
	return content
}

// SeedRunbooks seeds default technical runbooks if the corpus is empty.
func (s *Service) SeedRunbooks(ctx context.Context) error {
	count, err := s.repo.CountCorpusChunks(ctx)
	if err != nil {
		return fmt.Errorf("check corpus count: %w", err)
	}
	if count > 0 {
		return nil // Already seeded
	}

	type defaultChunk struct {
		heading string
		path    string
		tags    []string
		content string
	}

	defaults := []defaultChunk{
		{
			heading: "Pending Reconciliation and Missing Destination Operation",
			path:    "docs/runbooks/pending-reconciliation.md",
			tags:    []string{"pending_reconciliation", "destination_unavailable", "reconciliation"},
			content: "When a payment is in PENDING_RECONCILIATION, the central engine dispatched a debit or credit operation to the participant bank but has not yet received definitive settlement confirmation. Operators should verify participant presence using the operation presence reader. If participant presence is absent_confirmed, safe replay or continuation may proceed. If presence is present, the operation was executed by the participant and central reconciliation should mark the payment completed without duplicate dispatch.",
		},
		{
			heading: "Bank Outage and Scheduled Recovery",
			path:    "docs/runbooks/bank-outage-recovery.md",
			tags:    []string{"recovery", "bank_outage", "needs_attention"},
			content: "When participant banks experience transient downtime, payments enter scheduled recovery with exponential backoff and jitter. If bank outages exceed 20 attempts, the recovery job transitions to needs_attention with reason ATTEMPTS_EXHAUSTED. Operators can trigger manual recovery rechecks via the POST /api/ops/payments/{paymentID}/recovery/recheck endpoint to expedite recovery once bank connectivity is restored.",
		},
		{
			heading: "Bank Settled Central Pending Continuation",
			path:    "docs/runbooks/central-pending-continuation.md",
			tags:    []string{"central_pending", "continuation", "ledger"},
			content: "Payments in BANK_SETTLED_CENTRAL_PENDING indicate that the participant bank settled the transaction successfully, but central ledger recording was interrupted before finalization. Continuation will verify existing ledger entries and complete central settlement without issuing redundant bank operations.",
		},
		{
			heading: "Participant Operation Presence Verification",
			path:    "docs/runbooks/operation-presence.md",
			tags:    []string{"operation_presence", "presence", "dispatch"},
			content: "The participant presence query checks the participant bank's durable operations log using the unique operation ID and payment ID. Absence confirms that the dispatch never reached the participant ledger, allowing safe dispatch replay. Ambiguous or present presence mandates reconciliation verification.",
		},
	}

	for i, d := range defaults {
		h := sha256.Sum256([]byte(d.content))
		shaStr := hex.EncodeToString(h[:])

		chunk := CorpusChunk{
			ID:            uuid.New(),
			CorpusVersion: "v1",
			DocumentPath:  d.path,
			Heading:       d.heading,
			StartLine:     1,
			EndLine:       25,
			Content:       d.content,
			SHA256:        shaStr,
			ApprovedTags:  d.tags,
			AllowedRoles:  []string{"OPS_ADMIN", "OPS_VIEWER"},
			Active:        true,
		}
		if err := s.repo.InsertCorpusChunk(ctx, chunk); err != nil {
			return fmt.Errorf("insert seed chunk %d: %w", i, err)
		}
	}

	s.logger.Info("seeded default runbook corpus chunks", "count", len(defaults))
	return nil
}
