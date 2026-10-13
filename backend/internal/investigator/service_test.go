package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fixtureCase struct {
	TrustedPaymentId   string
	ExpectedValidation string
	Evidence           EvidenceBundle
	Report             InvestigationReport
}

func loadFixture(t *testing.T, name string) fixtureCase {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testfixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	var fixture fixtureCase
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *testClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *testClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(duration)
}

type countingEvidenceReader struct {
	bundle  EvidenceBundle
	err     error
	calls   atomic.Int32
	started chan struct{}
	block   bool
}

func (reader *countingEvidenceReader) ReadPaymentEvidence(ctx context.Context, scope TrustedScope, paymentID string) (EvidenceBundle, error) {
	reader.calls.Add(1)
	if reader.started != nil {
		select {
		case reader.started <- struct{}{}:
		default:
		}
	}
	if reader.block {
		<-ctx.Done()
		return EvidenceBundle{}, ctx.Err()
	}
	if reader.err != nil {
		return EvidenceBundle{}, reader.err
	}
	if scope.SelectedPaymentID != paymentID || reader.bundle.PaymentId != paymentID {
		return EvidenceBundle{}, ErrScopeViolation
	}
	return reader.bundle, nil
}

type countingRetriever struct {
	delegate       RunbookRetriever
	calls          atomic.Int32
	interruptFirst bool
	interruptEvery bool
}

func (retriever *countingRetriever) Retrieve(ctx context.Context, scope TrustedScope, query string, tags []string, topK int) ([]RunbookChunk, error) {
	call := retriever.calls.Add(1)
	if retriever.interruptEvery || (retriever.interruptFirst && call == 1) {
		return nil, context.Canceled
	}
	return retriever.delegate.Retrieve(ctx, scope, query, tags, topK)
}

func activeTestCorpus(t *testing.T) *MemoryCorpus {
	t.Helper()
	build := approvedCorpus(t)
	corpus := NewMemoryCorpus()
	if err := corpus.Stage(build); err != nil {
		t.Fatal(err)
	}
	if err := corpus.Activate(build.Version, build.ManifestSHA256); err != nil {
		t.Fatal(err)
	}
	return corpus
}

func newTestService(t *testing.T, clock *testClock, reader EvidenceReader, retriever RunbookRetriever, mutate func(*ServiceConfig)) (*Service, *MemoryInvestigationStore) {
	t.Helper()
	config := DefaultServiceConfig()
	config.Now = clock.Now
	config.JobTimeout = 2 * time.Minute
	if mutate != nil {
		mutate(&config)
	}
	store := NewMemoryInvestigationStore(clock.Now)
	service, err := NewService(config, store, ToolDependencies{Evidence: reader, Runbooks: retriever})
	if err != nil {
		t.Fatal(err)
	}
	return service, store
}

func TestEvidenceOnlyServiceCompletesIdempotently(t *testing.T) {
	fixture := loadFixture(t, "healthy.json")
	clock := &testClock{now: time.Now().UTC()}
	reader := &countingEvidenceReader{bundle: fixture.Evidence}
	retriever := &countingRetriever{delegate: activeTestCorpus(t)}
	service, _ := newTestService(t, clock, reader, retriever, nil)
	scope := TrustedScope{ActorID: "operator-1", Role: "OPS_VIEWER", SelectedPaymentID: fixture.TrustedPaymentId}

	job, created, err := service.Start(context.Background(), scope, fixture.TrustedPaymentId, "Why is this payment in its current state?", "key-1")
	if err != nil || !created || job.Status != StatusQueued || job.Mode != ModeEvidenceOnly {
		t.Fatalf("start = %+v, created=%t, err=%v", job, created, err)
	}
	duplicate, duplicateCreated, err := service.Start(context.Background(), scope, fixture.TrustedPaymentId, "Why is this payment in its current state?", "key-1")
	if err != nil || duplicateCreated || duplicate.ID != job.ID {
		t.Fatalf("duplicate = %+v, created=%t, err=%v", duplicate, duplicateCreated, err)
	}
	if _, _, err := service.Start(context.Background(), scope, fixture.TrustedPaymentId, "Different question", "key-1"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("idempotency conflict = %v", err)
	}

	completed, err := service.Run(context.Background(), job.ID, "worker-1")
	if err != nil || completed.Status != StatusCompleted || completed.Report == nil {
		t.Fatalf("run = %+v, err=%v", completed, err)
	}
	if completed.Report.Mode != ModeEvidenceOnly || completed.Report.SnapshotId != fixture.Evidence.SnapshotId || len(completed.Report.Facts) < 2 {
		t.Fatalf("unexpected evidence-only report: %+v", completed.Report)
	}
	if reader.calls.Load() != 1 || retriever.calls.Load() != 1 {
		t.Fatalf("calls: evidence=%d retrieval=%d", reader.calls.Load(), retriever.calls.Load())
	}
	if len(completed.SourceHashes) != 2 || len(completed.ToolTimeline) != 2 {
		t.Fatalf("checkpoint evidence missing: hashes=%v timeline=%v", completed.SourceHashes, completed.ToolTimeline)
	}
	for _, entry := range completed.ToolTimeline {
		if entry.Status != "completed" {
			t.Fatalf("successful timeline status = %q, want completed", entry.Status)
		}
	}
	public := PublicInvestigation(completed)
	if public.Report == nil || public.Error != nil || public.CompletedAt == nil || public.ToolTimeline == nil {
		t.Fatalf("public response = %+v", public)
	}
	metrics := service.Metrics()
	if metrics.Created != 1 || metrics.Duplicates != 1 || metrics.Completed != 1 || metrics.ToolCalls != 2 {
		t.Fatalf("metrics = %+v", metrics)
	}

	foreign := scope
	foreign.ActorID = "operator-2"
	if _, err := service.Get(context.Background(), foreign, completed.ID); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("foreign job read = %v", err)
	}
	foreignPayment := scope
	foreignPayment.SelectedPaymentID = "99999999-9999-4999-8999-999999999999"
	if _, _, err := service.Start(context.Background(), foreignPayment, fixture.TrustedPaymentId, "question", "foreign"); !errors.Is(err, ErrScopeViolation) {
		t.Fatalf("foreign payment start = %v", err)
	}
}

func TestPublicInvestigationMatchesSharedContractShape(t *testing.T) {
	input, output := int64(10), int64(5)
	completedAt := time.Date(2026, 10, 12, 1, 2, 3, 0, time.UTC)
	job := Job{
		ID: "00000000-0000-4000-8000-000000000001", Status: StatusFailed, Mode: ModeLLM,
		ErrorCode: "provider_rate_limited", CreatedAt: completedAt.Add(-time.Second), CompletedAt: completedAt,
		ProviderModel: "fixture-model", PromptVersion: InvestigatorPromptVersion,
		Usage: Usage{InputTokens: &input, OutputTokens: &output}, ToolTimeline: []ToolTimelineEntry{},
	}
	response := PublicInvestigation(job)
	if response.Error == nil || response.Error.Message == "" || response.Usage.Model == nil || *response.Usage.Model != "fixture-model" || response.Usage.PromptVersion != InvestigatorPromptVersion {
		t.Fatalf("public response omitted shared-contract metadata: %+v", response)
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var shape map[string]any
	if err := json.Unmarshal(raw, &shape); err != nil {
		t.Fatal(err)
	}
	usage := shape["usage"].(map[string]any)
	problem := shape["error"].(map[string]any)
	if len(usage) != 4 || len(problem) != 2 {
		t.Fatalf("public usage/error keys = %v/%v", usage, problem)
	}
}

func TestReportsStayWithinSharedContractBounds(t *testing.T) {
	fixture := loadFixture(t, "healthy.json")
	operation := fixture.Evidence.Operations[0]
	fixture.Evidence.Operations = nil
	for index := 0; index < 20; index++ {
		copy := operation
		copy.OperationId = fmt.Sprintf("%08d-2222-4222-8222-222222222222", index)
		copy.EvidenceId = "operation:" + copy.OperationId
		fixture.Evidence.Operations = append(fixture.Evidence.Operations, copy)
	}
	report, err := BuildEvidenceOnlyReport(fixture.Evidence, nil, "Summarize participant operations")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Facts) != 9 {
		t.Fatalf("grouped fact count = %d, want 9", len(report.Facts))
	}
	for _, fact := range report.Facts {
		if len(fact.EvidenceIds) > 10 || len(fact.Assertions) > 10 {
			t.Fatalf("fact exceeds shared contract: %+v", fact)
		}
	}

	partial := loadFixture(t, "partial_evidence.json").Evidence
	partial.Operations = nil
	missingOperation := loadFixture(t, "partial_evidence.json").Evidence.Operations[0]
	for index := 0; index < 20; index++ {
		copy := missingOperation
		copy.OperationId = fmt.Sprintf("%08d-2222-4222-8222-222222222222", index)
		copy.EvidenceId = "operation:" + copy.OperationId
		partial.Operations = append(partial.Operations, copy)
	}
	partial.Sources = []Source{{Name: "central", Status: "available"}}
	for index := 1; index < 20; index++ {
		partial.Sources = append(partial.Sources, Source{Name: fmt.Sprintf("participant-%02d", index), Status: "unavailable"})
	}
	report, err = BuildEvidenceOnlyReport(partial, nil, "Explain trace reconciliation and integrity evidence")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.MissingEvidence) != 20 || !strings.HasPrefix(report.MissingEvidence[19], "additional_missing_evidence_omitted:") {
		t.Fatalf("bounded missing evidence = %v", report.MissingEvidence)
	}
}

func TestServiceResumesInterruptedRetrievalWithoutRepeatingEvidence(t *testing.T) {
	fixture := loadFixture(t, "partial_evidence.json")
	clock := &testClock{now: time.Now().UTC()}
	reader := &countingEvidenceReader{bundle: fixture.Evidence}
	retriever := &countingRetriever{delegate: activeTestCorpus(t), interruptFirst: true}
	service, store := newTestService(t, clock, reader, retriever, nil)
	scope := TrustedScope{ActorID: "operator", Role: "OPS_ADMIN", SelectedPaymentID: fixture.TrustedPaymentId}
	job, _, err := service.Start(context.Background(), scope, fixture.TrustedPaymentId, "Why is participant status missing?", "resume-key")
	if err != nil {
		t.Fatal(err)
	}
	interrupted, err := service.Run(context.Background(), job.ID, "worker-a")
	if !errors.Is(err, context.Canceled) || interrupted.Snapshot == nil || interrupted.Status != StatusRunning {
		t.Fatalf("interrupted = %+v, err=%v", interrupted, err)
	}
	persisted, err := store.Get(context.Background(), scope, job.ID)
	if err != nil || persisted.Snapshot == nil || persisted.RetrievalComplete {
		t.Fatalf("persisted checkpoint = %+v, err=%v", persisted, err)
	}
	clock.Advance(16 * time.Second)
	completed, err := service.Run(context.Background(), job.ID, "worker-b")
	if err != nil || completed.Status != StatusCompleted {
		t.Fatalf("resumed = %+v, err=%v", completed, err)
	}
	if reader.calls.Load() != 1 || retriever.calls.Load() != 2 || completed.EvidenceAttempts != 1 || completed.RetrievalAttempts != 2 {
		t.Fatalf("resume call bounds: evidence=%d retrieval=%d attempts=%d/%d", reader.calls.Load(), retriever.calls.Load(), completed.EvidenceAttempts, completed.RetrievalAttempts)
	}
	if completed.Report.EvidenceCoverage != "limited" || len(completed.Report.MissingEvidence) == 0 {
		t.Fatalf("partial report did not preserve missing evidence: %+v", completed.Report)
	}
}

func TestServiceBoundsRepeatedInterruptedCoreCalls(t *testing.T) {
	fixture := loadFixture(t, "healthy.json")
	clock := &testClock{now: time.Now().UTC()}
	reader := &countingEvidenceReader{bundle: fixture.Evidence}
	retriever := &countingRetriever{delegate: activeTestCorpus(t), interruptEvery: true}
	service, _ := newTestService(t, clock, reader, retriever, nil)
	scope := TrustedScope{ActorID: "operator", Role: "OPS_ADMIN", SelectedPaymentID: fixture.TrustedPaymentId}
	job, _, err := service.Start(context.Background(), scope, fixture.TrustedPaymentId, "current state", "bounded-retry")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Run(context.Background(), job.ID, "worker-1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("first interruption = %v", err)
	}
	clock.Advance(16 * time.Second)
	if _, err := service.Run(context.Background(), job.ID, "worker-2"); !errors.Is(err, context.Canceled) {
		t.Fatalf("second interruption = %v", err)
	}
	clock.Advance(16 * time.Second)
	failed, err := service.Run(context.Background(), job.ID, "worker-3")
	if err == nil || failed.Status != StatusFailed || failed.ErrorCode != "tool_attempts_exhausted" {
		t.Fatalf("bounded retry result = %+v, err=%v", failed, err)
	}
	if reader.calls.Load() != 1 || retriever.calls.Load() != 2 || failed.RetrievalAttempts != 2 {
		t.Fatalf("unbounded core calls: evidence=%d retrieval=%d attempts=%d", reader.calls.Load(), retriever.calls.Load(), failed.RetrievalAttempts)
	}
}

func TestServiceCancellationIsResumableAndDeadlineIsTerminal(t *testing.T) {
	fixture := loadFixture(t, "healthy.json")
	clock := &testClock{now: time.Now().UTC()}
	blocking := &countingEvidenceReader{bundle: fixture.Evidence, block: true, started: make(chan struct{}, 1)}
	service, store := newTestService(t, clock, blocking, activeTestCorpus(t), nil)
	scope := TrustedScope{ActorID: "operator", Role: "OPS_ADMIN", SelectedPaymentID: fixture.TrustedPaymentId}
	job, _, err := service.Start(context.Background(), scope, fixture.TrustedPaymentId, "current state", "cancel-key")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, runErr := service.Run(ctx, job.ID, "worker-cancel")
		result <- runErr
	}()
	<-blocking.started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled run = %v", err)
	}
	persisted, err := store.Get(context.Background(), scope, job.ID)
	if err != nil || persisted.Status != StatusRunning || persisted.ErrorCode != "" {
		t.Fatalf("cancelled checkpoint = %+v, err=%v", persisted, err)
	}

	deadlineReader := &countingEvidenceReader{bundle: fixture.Evidence}
	deadlineService, deadlineStore := newTestService(t, clock, deadlineReader, activeTestCorpus(t), func(config *ServiceConfig) {
		config.JobTimeout = time.Second
	})
	deadlineJob, _, err := deadlineService.Start(context.Background(), scope, fixture.TrustedPaymentId, "current state", "deadline-key")
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(2 * time.Second)
	failed, err := deadlineService.Run(context.Background(), deadlineJob.ID, "worker-deadline")
	if !errors.Is(err, context.DeadlineExceeded) || failed.Status != StatusFailed || failed.ErrorCode != "deadline_exceeded" {
		t.Fatalf("deadline run = %+v, err=%v", failed, err)
	}
	storedFailed, getErr := deadlineStore.Get(context.Background(), scope, deadlineJob.ID)
	if getErr != nil || storedFailed.Status != StatusFailed {
		t.Fatalf("stored deadline = %+v, err=%v", storedFailed, getErr)
	}
	if deadlineService.Metrics().Deadlines != 1 || deadlineReader.calls.Load() != 0 {
		t.Fatalf("deadline metrics/calls = %+v/%d", deadlineService.Metrics(), deadlineReader.calls.Load())
	}
}

func TestServiceMarksCentralSourceFailureWithoutPublishingReport(t *testing.T) {
	fixture := loadFixture(t, "healthy.json")
	clock := &testClock{now: time.Now().UTC()}
	reader := &countingEvidenceReader{bundle: fixture.Evidence, err: errors.New("database unavailable")}
	service, store := newTestService(t, clock, reader, activeTestCorpus(t), nil)
	scope := TrustedScope{ActorID: "operator", Role: "OPS_VIEWER", SelectedPaymentID: fixture.TrustedPaymentId}
	job, _, err := service.Start(context.Background(), scope, fixture.TrustedPaymentId, "current state", "source-key")
	if err != nil {
		t.Fatal(err)
	}
	failed, err := service.Run(context.Background(), job.ID, "worker-source")
	if err == nil || failed.Status != StatusFailed || failed.ErrorCode != "source_unavailable" || failed.Report != nil {
		t.Fatalf("source failure = %+v, err=%v", failed, err)
	}
	persisted, getErr := store.Get(context.Background(), scope, job.ID)
	if getErr != nil || persisted.Report != nil || len(persisted.ToolTimeline) != 1 || persisted.ToolTimeline[0].Status != "failed" {
		t.Fatalf("persisted source failure = %+v, err=%v", persisted, getErr)
	}
}

type fakeRecoveryReader struct{}

func (fakeRecoveryReader) ReadRecoveryAttempts(_ context.Context, _ TrustedScope, paymentID string, _ int) ([]RecoveryAttempt, error) {
	return []RecoveryAttempt{{AttemptId: "attempt", PaymentId: paymentID}}, nil
}

type fakeTraceReader struct{}

func (fakeTraceReader) ReadTrace(_ context.Context, _ TrustedScope, traceID string) (Trace, error) {
	return Trace{TraceId: traceID, SourceStatus: "available"}, nil
}

type fakeAnalyticsReader struct{}

func (fakeAnalyticsReader) ReadWindows(_ context.Context, _ TrustedScope, entityType, entityID, _, _ string, _ int) ([]AnalyticsWindow, error) {
	return []AnalyticsWindow{{EntityType: entityType, EntityId: entityID, Advisory: true}}, nil
}

func TestToolRegistryRejectsUntrustedScopeUnknownFieldsAndBounds(t *testing.T) {
	fixture := loadFixture(t, "healthy.json")
	traceID := "0123456789abcdef0123456789abcdef"
	fixture.Evidence.Traces.TraceIds = []string{traceID}
	reader := &countingEvidenceReader{bundle: fixture.Evidence}
	registry, err := NewToolRegistry(ToolDependencies{
		Evidence: reader, Recovery: fakeRecoveryReader{}, Trace: fakeTraceReader{}, Analytics: fakeAnalyticsReader{}, Runbooks: activeTestCorpus(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	scope := TrustedScope{ActorID: "operator", Role: "OPS_ADMIN", SelectedPaymentID: fixture.TrustedPaymentId}

	tests := []struct {
		name string
		call ToolCall
		want error
	}{
		{"scope field", ToolCall{Name: ToolReadPaymentEvidence, Arguments: json.RawMessage(fmt.Sprintf(`{"paymentId":%q,"scope":{"role":"OPS_ADMIN"}}`, fixture.TrustedPaymentId))}, ErrInvalidToolArguments},
		{"foreign payment", ToolCall{Name: ToolReadPaymentEvidence, Arguments: json.RawMessage(`{"paymentId":"foreign"}`)}, ErrScopeViolation},
		{"unknown tool", ToolCall{Name: "run_sql", Arguments: json.RawMessage(`{}`)}, ErrUnknownTool},
		{"recovery bound", ToolCall{Name: ToolReadRecoveryAttempts, Arguments: json.RawMessage(fmt.Sprintf(`{"paymentId":%q,"limit":21}`, fixture.TrustedPaymentId))}, ErrInvalidToolArguments},
		{"foreign trace", ToolCall{Name: ToolReadTraceContext, Arguments: json.RawMessage(`{"traceId":"ffffffffffffffffffffffffffffffff"}`)}, ErrScopeViolation},
		{"foreign analytics entity", ToolCall{Name: ToolReadOperationalWindow, Arguments: json.RawMessage(`{"entityType":"bank","entityId":"BANK-X","from":"2026-10-11T00:00:00Z","to":"2026-10-11T01:00:00Z","limit":12}`)}, ErrScopeViolation},
		{"retrieval topK", ToolCall{Name: ToolRetrieveRunbook, Arguments: json.RawMessage(`{"query":"payment","tags":[],"topK":6}`)}, ErrInvalidToolArguments},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := registry.Execute(context.Background(), scope, &fixture.Evidence, testCase.call)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}

	session, err := NewToolSession(registry, scope, &fixture.Evidence, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	unknown := ToolCall{Name: "mutate_payment", Arguments: json.RawMessage(`{}`)}
	if _, err := session.Execute(context.Background(), unknown); !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("unknown session call = %v", err)
	}
	if _, err := session.Execute(context.Background(), ToolCall{Name: ToolRetrieveRunbook, Arguments: json.RawMessage(`{"query":"payment","tags":[],"topK":1}`)}); !errors.Is(err, ErrToolBudgetExceeded) {
		t.Fatalf("tool budget = %v", err)
	}

	duplicateSession, err := NewToolSession(registry, scope, &fixture.Evidence, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	call := ToolCall{Name: ToolReadTraceContext, Arguments: json.RawMessage(fmt.Sprintf(`{"traceId":%q}`, traceID))}
	if _, err := duplicateSession.Execute(context.Background(), call); err != nil {
		t.Fatal(err)
	}
	if _, err := duplicateSession.Execute(context.Background(), call); !errors.Is(err, ErrDuplicateToolCall) {
		t.Fatalf("duplicate tool call = %v", err)
	}
}

func TestGroundingChecksAssertionMeaning(t *testing.T) {
	fixture := loadFixture(t, "healthy.json")
	report, err := BuildEvidenceOnlyReport(fixture.Evidence, nil, "current state")
	if err != nil {
		t.Fatal(err)
	}
	report.Facts[1].Assertions[0].Value = "999999"
	if err := ValidateReportGrounding(report, fixture.Evidence, nil); err == nil {
		t.Fatal("wrong amount with valid evidence ID passed grounding")
	}
	report, err = BuildEvidenceOnlyReport(fixture.Evidence, nil, "current state")
	if err != nil {
		t.Fatal(err)
	}
	report.Facts[0].EvidenceIds[0] = "payment:unknown"
	if err := ValidateReportGrounding(report, fixture.Evidence, nil); err == nil {
		t.Fatal("unknown evidence ID passed grounding")
	}
	report, err = BuildEvidenceOnlyReport(fixture.Evidence, nil, "current state")
	if err != nil {
		t.Fatal(err)
	}
	report.SnapshotId = "sha256:changed"
	if err := ValidateReportGrounding(report, fixture.Evidence, nil); err == nil {
		t.Fatal("changed snapshot identity passed grounding")
	}
}
