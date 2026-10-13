package investigator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrInvalidInvestigationRequest = errors.New("invalid investigation request")

type ServiceConfig struct {
	JobTimeout        time.Duration
	EvidenceTimeout   time.Duration
	LeaseDuration     time.Duration
	RetrievalTopK     int
	MaxCoreAttempts   int
	Mode              string
	Provider          Provider
	PromptVersion     string
	InputTokenLimit   int
	OutputTokenLimit  int
	MaxProviderRounds int
	Now               func() time.Time
}

func DefaultServiceConfig() ServiceConfig {
	return ServiceConfig{
		JobTimeout:        30 * time.Second,
		EvidenceTimeout:   3 * time.Second,
		LeaseDuration:     15 * time.Second,
		RetrievalTopK:     5,
		MaxCoreAttempts:   2,
		Mode:              ModeEvidenceOnly,
		PromptVersion:     InvestigatorPromptVersion,
		InputTokenLimit:   8000,
		OutputTokenLimit:  1200,
		MaxProviderRounds: 2,
		Now:               time.Now,
	}
}

type serviceCounters struct {
	created, duplicates, completed, failed, deadlines atomic.Uint64
	toolCalls, toolErrors                             atomic.Uint64
}

type ServiceMetrics struct {
	Created, Duplicates, Completed, Failed, Deadlines uint64
	ToolCalls, ToolErrors                             uint64
}

type Service struct {
	config   ServiceConfig
	store    InvestigationStore
	registry *ToolRegistry
	counters serviceCounters
}

func NewService(config ServiceConfig, store InvestigationStore, dependencies ToolDependencies) (*Service, error) {
	if store == nil {
		return nil, errors.New("investigation store is required")
	}
	if config.JobTimeout <= 0 || config.EvidenceTimeout <= 0 || config.LeaseDuration <= 0 || config.RetrievalTopK < 1 || config.RetrievalTopK > 5 || config.MaxCoreAttempts < 1 || config.MaxCoreAttempts > 3 {
		return nil, errors.New("invalid investigation service configuration")
	}
	if config.Mode != ModeEvidenceOnly && config.Mode != ModeLLM {
		return nil, errors.New("invalid investigator mode")
	}
	if config.Mode == ModeLLM && (config.Provider == nil || config.PromptVersion != InvestigatorPromptVersion || config.InputTokenLimit < 1 || config.InputTokenLimit > 8000 || config.OutputTokenLimit < 1 || config.OutputTokenLimit > 1200 || config.MaxProviderRounds < 1 || config.MaxProviderRounds > 2) {
		return nil, errors.New("invalid provider generation configuration")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	registry, err := NewToolRegistry(dependencies)
	if err != nil {
		return nil, err
	}
	return &Service{config: config, store: store, registry: registry}, nil
}

func (service *Service) Start(ctx context.Context, scope TrustedScope, paymentID, question, idempotencyKey string) (Job, bool, error) {
	if err := validateInvestigationRequest(scope, paymentID, question, idempotencyKey); err != nil {
		return Job{}, false, err
	}
	now := service.config.Now().UTC()
	requestHash := investigationRequestHash(paymentID, question, service.config.Mode)
	job, created, err := service.store.Create(ctx, NewJob{
		Scope: scope, PaymentID: paymentID, Question: question, IdempotencyKey: idempotencyKey,
		RequestHash: requestHash, Mode: service.config.Mode, DeadlineAt: now.Add(service.config.JobTimeout), CreatedAt: now,
	})
	if err != nil {
		return Job{}, false, err
	}
	if created {
		service.counters.created.Add(1)
	} else {
		service.counters.duplicates.Add(1)
	}
	return job, created, nil
}

func (service *Service) Get(ctx context.Context, scope TrustedScope, jobID string) (Job, error) {
	return service.store.Get(ctx, scope, jobID)
}

func (service *Service) Run(ctx context.Context, jobID, workerID string) (Job, error) {
	job, err := service.store.Claim(ctx, jobID, workerID, service.config.LeaseDuration)
	if err != nil {
		return Job{}, err
	}
	if !job.DeadlineAt.After(service.config.Now().UTC()) {
		return service.fail(job, "deadline_exceeded", context.DeadlineExceeded)
	}
	jobContext, cancel := context.WithDeadline(ctx, job.DeadlineAt)
	defer cancel()

	if job.Snapshot == nil {
		if job.EvidenceAttempts >= service.config.MaxCoreAttempts {
			return service.fail(job, "tool_attempts_exhausted", errors.New("evidence attempts exhausted"))
		}
		job.EvidenceAttempts++
		if err := service.store.SaveCheckpoint(jobContext, job); err != nil {
			return job, err
		}
		arguments := mustJSON(struct {
			PaymentId string `json:"paymentId"`
		}{job.PaymentID})
		result, timeline, callErr := service.executeCoreTool(jobContext, job, ToolCall{Name: ToolReadPaymentEvidence, Arguments: arguments}, service.config.EvidenceTimeout)
		job.ToolTimeline = append(job.ToolTimeline, timeline)
		if callErr != nil {
			return service.handleCoreError(job, callErr)
		}
		var evidence EvidenceBundle
		if err := json.Unmarshal(result.Payload, &evidence); err != nil {
			return service.fail(job, "invalid_evidence", err)
		}
		job.Snapshot = &evidence
		job.SnapshotHash = result.SourceHash
		job.SourceHashes = append(job.SourceHashes, SourceHash{ToolName: ToolReadPaymentEvidence, SHA256: result.SourceHash})
		if err := service.store.SaveCheckpoint(jobContext, job); err != nil {
			return job, err
		}
	}

	if !job.RetrievalComplete {
		if job.RetrievalAttempts >= service.config.MaxCoreAttempts {
			return service.fail(job, "tool_attempts_exhausted", errors.New("retrieval attempts exhausted"))
		}
		job.RetrievalAttempts++
		if err := service.store.SaveCheckpoint(jobContext, job); err != nil {
			return job, err
		}
		arguments := mustJSON(struct {
			Query string   `json:"query"`
			Tags  []string `json:"tags"`
			TopK  int      `json:"topK"`
		}{retrievalQuery(job.Question, *job.Snapshot), retrievalTags(*job.Snapshot), service.config.RetrievalTopK})
		result, timeline, callErr := service.executeCoreTool(jobContext, job, ToolCall{Name: ToolRetrieveRunbook, Arguments: arguments}, 0)
		job.ToolTimeline = append(job.ToolTimeline, timeline)
		if callErr != nil {
			return service.handleCoreError(job, callErr)
		}
		if err := json.Unmarshal(result.Payload, &job.Chunks); err != nil {
			return service.fail(job, "invalid_retrieval", err)
		}
		job.RetrievalComplete = true
		job.SourceHashes = append(job.SourceHashes, SourceHash{ToolName: ToolRetrieveRunbook, SHA256: result.SourceHash})
		if err := service.store.SaveCheckpoint(jobContext, job); err != nil {
			return job, err
		}
	}

	if job.Mode == ModeLLM {
		return service.runGenerated(jobContext, job)
	}
	report, err := BuildEvidenceOnlyReport(*job.Snapshot, job.Chunks, job.Question)
	if err != nil {
		return service.fail(job, "report_validation_failed", err)
	}
	job.Report = &report
	job.Status = StatusCompleted
	job.CompletedAt = service.config.Now().UTC()
	job.ErrorCode = ""
	if err := service.store.SaveCheckpoint(jobContext, job); err != nil {
		return job, err
	}
	service.counters.completed.Add(1)
	return job, nil
}

func (service *Service) Metrics() ServiceMetrics {
	return ServiceMetrics{
		Created: service.counters.created.Load(), Duplicates: service.counters.duplicates.Load(),
		Completed: service.counters.completed.Load(), Failed: service.counters.failed.Load(),
		Deadlines: service.counters.deadlines.Load(), ToolCalls: service.counters.toolCalls.Load(),
		ToolErrors: service.counters.toolErrors.Load(),
	}
}

func (service *Service) executeCoreTool(ctx context.Context, job Job, call ToolCall, timeout time.Duration) (ToolResult, ToolTimelineEntry, error) {
	started := service.config.Now().UTC()
	entry := ToolTimelineEntry{ToolName: call.Name, Arguments: append(json.RawMessage(nil), call.Arguments...), StartedAt: started.Format(time.RFC3339Nano)}
	toolContext := ctx
	cancel := func() {}
	if timeout > 0 {
		toolContext, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	service.counters.toolCalls.Add(1)
	result, err := service.registry.Execute(toolContext, TrustedScope{ActorID: job.ActorID, Role: job.Role, SelectedPaymentID: job.PaymentID}, job.Snapshot, call)
	entry.FinishedAt = service.config.Now().UTC().Format(time.RFC3339Nano)
	if err != nil {
		service.counters.toolErrors.Add(1)
		entry.Status = timelineStatus(err)
		return ToolResult{}, entry, err
	}
	entry.Status = "completed"
	entry.EvidenceIds = append([]string(nil), result.EvidenceIds...)
	return result, entry, nil
}

func (service *Service) handleCoreError(job Job, err error) (Job, error) {
	if errors.Is(err, context.Canceled) {
		if saveErr := service.saveDetached(job); saveErr != nil {
			return job, saveErr
		}
		return job, err
	}
	if errors.Is(err, context.DeadlineExceeded) || !job.DeadlineAt.After(service.config.Now().UTC()) {
		return service.fail(job, "deadline_exceeded", err)
	}
	return service.fail(job, "source_unavailable", err)
}

func (service *Service) fail(job Job, code string, cause error) (Job, error) {
	job.Status = StatusFailed
	job.ErrorCode = code
	job.CompletedAt = service.config.Now().UTC()
	if code == "deadline_exceeded" {
		service.counters.deadlines.Add(1)
	}
	service.counters.failed.Add(1)
	if err := service.saveDetached(job); err != nil {
		return job, err
	}
	return job, cause
}

func (service *Service) saveDetached(job Job) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return service.store.SaveCheckpoint(ctx, job)
}

func timelineStatus(err error) string {
	if errors.Is(err, ErrUnknownTool) || errors.Is(err, ErrInvalidToolArguments) || errors.Is(err, ErrToolBudgetExceeded) || errors.Is(err, ErrDuplicateToolCall) || errors.Is(err, ErrScopeViolation) {
		return "rejected"
	}
	return "failed"
}

func validateInvestigationRequest(scope TrustedScope, paymentID, question, idempotencyKey string) error {
	if err := validateTrustedScope(scope); err != nil || paymentID != scope.SelectedPaymentID {
		return ErrScopeViolation
	}
	if strings.TrimSpace(question) == "" || utf8.RuneCountInString(question) > 1000 {
		return ErrInvalidInvestigationRequest
	}
	if idempotencyKey == "" || utf8.RuneCountInString(idempotencyKey) > 200 || strings.IndexFunc(idempotencyKey, unicode.IsControl) >= 0 {
		return ErrInvalidInvestigationRequest
	}
	return nil
}

func investigationRequestHash(paymentID, question, mode string) string {
	digest := sha256.Sum256([]byte("tx.investigation.request.v1\x00" + paymentID + "\x00" + question + "\x00" + mode))
	return hex.EncodeToString(digest[:])
}

func retrievalQuery(question string, evidence EvidenceBundle) string {
	query := strings.TrimSpace(question) + " payment state " + string(evidence.Payment.State)
	if evidence.Recovery.ReasonCode != nil {
		query += " recovery reason " + *evidence.Recovery.ReasonCode
	}
	if utf8.RuneCountInString(query) <= maxRetrievalQuery {
		return query
	}
	runes := []rune(query)
	return string(runes[:maxRetrievalQuery])
}

func retrievalTags(evidence EvidenceBundle) []string {
	tags := []string{"payment"}
	switch evidence.Payment.State {
	case "PROCESSING", "PENDING_RECONCILIATION", "BANK_SETTLED_CENTRAL_PENDING":
		tags = append(tags, "recovery")
	}
	if evidence.Route.Status != "available" {
		tags = append(tags, "missing_target")
	}
	return tags
}

func mustJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("encode internal tool arguments: %v", err))
	}
	return raw
}
