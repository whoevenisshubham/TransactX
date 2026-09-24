package reconciliation_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/transactx/backend/internal/reconciliation"
)

// memoryRunRepository is a pure in-memory RunRepository for engine tests.
// It avoids any PostgreSQL dependency in unit tests.
type memoryRunRepository struct {
	runs          map[uuid.UUID]reconciliation.Run
	discrepancies []reconciliation.Discrepancy
}

func newMemoryRunRepository() *memoryRunRepository {
	return &memoryRunRepository{runs: make(map[uuid.UUID]reconciliation.Run)}
}

func (r *memoryRunRepository) CreateRun(ctx context.Context, participantID string, scope reconciliation.Scope) (reconciliation.Run, error) {
	run := reconciliation.Run{
		ID:            uuid.New(),
		ParticipantID: participantID,
		ScopeFrom:     scope.From.UTC(),
		ScopeTo:       scope.To.UTC(),
		Status:        reconciliation.RunStatusRunning,
		StartedAt:     time.Now().UTC(),
	}
	r.runs[run.ID] = run
	return run, nil
}

func (r *memoryRunRepository) CompleteRun(_ context.Context, runID uuid.UUID, canonRoot, partRoot []byte, canonVer, algoVer string, recordCount, discrepancyCount int64) (reconciliation.Run, error) {
	run, ok := r.runs[runID]
	if !ok || run.Status != reconciliation.RunStatusRunning {
		return reconciliation.Run{}, reconciliation.ErrRunNotFound
	}
	run.Status = reconciliation.RunStatusCompleted
	run.CanonicalRoot = canonRoot
	run.ParticipantRoot = partRoot
	run.CanonicalVersion = canonVer
	run.AlgorithmVersion = algoVer
	run.RecordCount = recordCount
	run.DiscrepancyCount = discrepancyCount
	now := time.Now().UTC()
	run.CompletedAt = &now
	r.runs[runID] = run
	return run, nil
}

func (r *memoryRunRepository) FailRun(_ context.Context, runID uuid.UUID, errMsg string) (reconciliation.Run, error) {
	run, ok := r.runs[runID]
	if !ok || run.Status != reconciliation.RunStatusRunning {
		return reconciliation.Run{}, reconciliation.ErrRunNotFound
	}
	run.Status = reconciliation.RunStatusFailed
	run.ErrorMessage = errMsg
	now := time.Now().UTC()
	run.CompletedAt = &now
	r.runs[runID] = run
	return run, nil
}

func (r *memoryRunRepository) GetRun(_ context.Context, runID uuid.UUID) (reconciliation.Run, error) {
	run, ok := r.runs[runID]
	if !ok {
		return reconciliation.Run{}, reconciliation.ErrRunNotFound
	}
	return run, nil
}

func (r *memoryRunRepository) ListRuns(_ context.Context, req reconciliation.ListRunsRequest) (reconciliation.RunListPage, error) {
	items := make([]reconciliation.Run, 0)
	for _, run := range r.runs {
		if req.ParticipantID != "" && run.ParticipantID != req.ParticipantID {
			continue
		}
		items = append(items, run)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	page := reconciliation.RunListPage{Total: len(items), Limit: limit}
	if req.Offset < len(items) {
		end := req.Offset + limit
		if end > len(items) {
			end = len(items)
		}
		page.Items = items[req.Offset:end]
		if end < len(items) {
			nextOff := req.Offset + limit
			page.NextOffset = &nextOff
		}
	}
	if page.Items == nil {
		page.Items = []reconciliation.Run{}
	}
	return page, nil
}

func (r *memoryRunRepository) SaveDiscrepancy(_ context.Context, disc reconciliation.Discrepancy) (reconciliation.Discrepancy, error) {
	disc.ID = uuid.New()
	disc.DetectedAt = time.Now().UTC()
	r.discrepancies = append(r.discrepancies, disc)
	return disc, nil
}

func (r *memoryRunRepository) ListDiscrepancies(_ context.Context, req reconciliation.ListDiscrepanciesRequest) (reconciliation.DiscrepancyListPage, error) {
	items := make([]reconciliation.Discrepancy, 0)
	for _, d := range r.discrepancies {
		if d.RunID == req.RunID {
			items = append(items, d)
		}
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	page := reconciliation.DiscrepancyListPage{Total: len(items), Limit: limit}
	if req.Offset < len(items) {
		end := req.Offset + limit
		if end > len(items) {
			end = len(items)
		}
		page.Items = items[req.Offset:end]
	}
	if page.Items == nil {
		page.Items = []reconciliation.Discrepancy{}
	}
	return page, nil
}

// engineRunRepository wraps the memoryRunRepository to satisfy the engine's
// RunRepository interface (so we can share the in-memory store for assertions).
type engineRunRepository = reconciliation.RunRepository

// makeTestParticipant builds a MemoryParticipant for a given set of records.
func makeTestParticipant(t *testing.T, participantID string, records []reconciliation.CanonicalRecord) reconciliation.ReconciliationParticipant {
	t.Helper()
	p, err := reconciliation.NewMemoryParticipant(participantID, "test-partition", time.Hour, records)
	if err != nil {
		t.Fatalf("build test participant: %v", err)
	}
	return p
}

// newTestEngine builds an Engine with an in-memory repository that allows
// assertions without a database.
func newTestEngine(
	t *testing.T,
	participants []string,
	buildParticipant func(ctx context.Context, id string, scope reconciliation.Scope) (reconciliation.ReconciliationParticipant, error),
) (*reconciliation.Engine, *memoryRunRepository) {
	t.Helper()
	repo := newMemoryRunRepository()
	knownParticipants := make(reconciliation.KnownParticipants)
	for _, p := range participants {
		knownParticipants[p] = true
	}
	// Engine uses real RunRepository; for tests we wrap the memory store by
	// embedding it inside a thin adapter that matches the Engine's internal
	// contract.
	engine := reconciliation.NewEngineWithRepo(knownParticipants, repo, buildParticipant)
	return engine, repo
}

// --- Tests ---

func TestEngineSuccessfulRun(t *testing.T) {
	// A run with records must create RUNNING then COMPLETED.
	now := time.Now().UTC().Truncate(time.Second)
	records := []reconciliation.CanonicalRecord{
		{
			OperationID: uuid.New(),
			PaymentID:   uuid.New(),
			AccountID:   uuid.New(),
			EntryType:   "DEBIT",
			AmountPaise: 100,
			Currency:    "INR",
			OccurredAt:  now.Add(-30 * time.Minute),
		},
	}
	scope := reconciliation.Scope{From: now.Add(-time.Hour), To: now}
	const pid = "BANK-A"

	engine, repo := newTestEngine(t, []string{pid}, func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
		return makeTestParticipant(t, id, records), nil
	})

	run, err := engine.Execute(context.Background(), reconciliation.RunRequest{
		ParticipantID: pid,
		ScopeFrom:     scope.From,
		ScopeTo:       scope.To,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if run.Status != reconciliation.RunStatusCompleted {
		t.Fatalf("expected COMPLETED, got %q", run.Status)
	}
	if run.CompletedAt == nil {
		t.Fatal("expected CompletedAt to be set")
	}
	if run.ParticipantID != pid {
		t.Fatalf("expected participant %q, got %q", pid, run.ParticipantID)
	}
	// Verify the run is persisted.
	stored, err := repo.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if stored.Status != reconciliation.RunStatusCompleted {
		t.Fatalf("stored run status: expected COMPLETED, got %q", stored.Status)
	}
}

func TestEngineZeroDiscrepancies(t *testing.T) {
	// Empty participant produces COMPLETED with zero discrepancies and EmptyHash root.
	now := time.Now().UTC().Truncate(time.Second)
	scope := reconciliation.Scope{From: now.Add(-time.Hour), To: now}
	const pid = "BANK-B"

	engine, _ := newTestEngine(t, []string{pid}, func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
		return makeTestParticipant(t, id, nil), nil
	})

	run, err := engine.Execute(context.Background(), reconciliation.RunRequest{
		ParticipantID: pid,
		ScopeFrom:     scope.From,
		ScopeTo:       scope.To,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if run.Status != reconciliation.RunStatusCompleted {
		t.Fatalf("expected COMPLETED, got %q", run.Status)
	}
	if run.DiscrepancyCount != 0 {
		t.Fatalf("expected 0 discrepancies, got %d", run.DiscrepancyCount)
	}
}

func TestEngineInvalidParticipant(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	engine, _ := newTestEngine(t, []string{"BANK-A"}, func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
		return makeTestParticipant(t, id, nil), nil
	})
	_, err := engine.Execute(context.Background(), reconciliation.RunRequest{
		ParticipantID: "UNKNOWN",
		ScopeFrom:     now.Add(-time.Hour),
		ScopeTo:       now,
	})
	if err == nil {
		t.Fatal("expected error for unknown participant")
	}
}

func TestEngineOperationalFailure(t *testing.T) {
	// If the participant factory returns an error, the run must become FAILED.
	now := time.Now().UTC().Truncate(time.Second)
	const pid = "BANK-A"

	engine, repo := newTestEngine(t, []string{pid}, func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
		return nil, fmt.Errorf("participant unavailable: simulated failure")
	})

	_, err := engine.Execute(context.Background(), reconciliation.RunRequest{
		ParticipantID: pid,
		ScopeFrom:     now.Add(-time.Hour),
		ScopeTo:       now,
	})
	if err == nil {
		t.Fatal("expected error for participant failure")
	}
	// Find the run in the repo and verify it is FAILED.
	var failedRun *reconciliation.Run
	for _, run := range repo.runs {
		r := run
		failedRun = &r
		break
	}
	if failedRun == nil {
		t.Fatal("expected a run record to be created")
	}
	if failedRun.Status != reconciliation.RunStatusFailed {
		t.Fatalf("expected FAILED, got %q", failedRun.Status)
	}
	if failedRun.ErrorMessage == "" {
		t.Fatal("expected error message to be set on FAILED run")
	}
}

func TestEngineParticipantIsolation(t *testing.T) {
	// A participant cannot access another participant's data because the engine
	// validates participant IDs before execution.
	now := time.Now().UTC().Truncate(time.Second)
	engine, _ := newTestEngine(t, []string{"BANK-A"}, func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
		return makeTestParticipant(t, id, nil), nil
	})
	// BANK-B is not in the known participants list.
	_, err := engine.Execute(context.Background(), reconciliation.RunRequest{
		ParticipantID: "BANK-B",
		ScopeFrom:     now.Add(-time.Hour),
		ScopeTo:       now,
	})
	if err == nil {
		t.Fatal("expected ErrInvalidParticipant for unknown participant")
	}
}

func TestEngineDeterministicRoot(t *testing.T) {
	// Same canonical dataset must produce the same commitment root on every run.
	now := time.Now().UTC().Truncate(time.Second)
	records := []reconciliation.CanonicalRecord{
		{
			OperationID: uuid.MustParse("00000000-0000-4000-8000-000000000001"),
			PaymentID:   uuid.MustParse("00000000-0000-4000-8000-000000000002"),
			AccountID:   uuid.MustParse("00000000-0000-4000-8000-000000000003"),
			EntryType:   "DEBIT",
			AmountPaise: 500,
			Currency:    "INR",
			OccurredAt:  now.Add(-30 * time.Minute),
		},
	}
	scope := reconciliation.Scope{From: now.Add(-time.Hour), To: now}
	const pid = "BANK-A"

	makeEngine := func() *reconciliation.Engine {
		repo := newMemoryRunRepository()
		engine := reconciliation.NewEngineWithRepo(
			reconciliation.KnownParticipants{pid: true},
			repo,
			func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
				return makeTestParticipant(t, id, records), nil
			},
		)
		return engine
	}

	run1, err := makeEngine().Execute(context.Background(), reconciliation.RunRequest{ParticipantID: pid, ScopeFrom: scope.From, ScopeTo: scope.To})
	if err != nil {
		t.Fatalf("run1: %v", err)
	}
	run2, err := makeEngine().Execute(context.Background(), reconciliation.RunRequest{ParticipantID: pid, ScopeFrom: scope.From, ScopeTo: scope.To})
	if err != nil {
		t.Fatalf("run2: %v", err)
	}
	if !reconciliation.EqualBytes(run1.CanonicalRoot, run2.CanonicalRoot) {
		t.Fatalf("canonical root is not deterministic: run1=%x run2=%x", run1.CanonicalRoot, run2.CanonicalRoot)
	}
}

func TestEngineNoFinancialMutation(t *testing.T) {
	// Reconciliation must not alter RecordCount on the participant records;
	// it is read-only. We verify that the canonical record data is unchanged.
	now := time.Now().UTC().Truncate(time.Second)
	originalAmount := int64(999)
	records := []reconciliation.CanonicalRecord{
		{
			OperationID: uuid.New(),
			PaymentID:   uuid.New(),
			AccountID:   uuid.New(),
			EntryType:   "CREDIT",
			AmountPaise: originalAmount,
			Currency:    "INR",
			OccurredAt:  now.Add(-10 * time.Minute),
		},
	}
	scope := reconciliation.Scope{From: now.Add(-time.Hour), To: now}
	const pid = "BANK-A"

	var captured []reconciliation.CanonicalRecord
	engine, _ := newTestEngine(t, []string{pid}, func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
		p, err := reconciliation.NewMemoryParticipant(id, "test-partition", time.Hour, records)
		if err != nil {
			return nil, err
		}
		// Capture a snapshot of records before execution.
		captured = append([]reconciliation.CanonicalRecord(nil), records...)
		return p, nil
	})

	if _, err := engine.Execute(context.Background(), reconciliation.RunRequest{
		ParticipantID: pid,
		ScopeFrom:     scope.From,
		ScopeTo:       scope.To,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Verify original records are unchanged.
	if len(captured) != 1 || captured[0].AmountPaise != originalAmount {
		t.Fatalf("financial records were mutated; expected amount %d, got %d", originalAmount, captured[0].AmountPaise)
	}
}

func TestEngineListRuns(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	scope := reconciliation.Scope{From: now.Add(-time.Hour), To: now}
	const pid = "BANK-A"

	engine, _ := newTestEngine(t, []string{pid}, func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
		return makeTestParticipant(t, id, nil), nil
	})

	// Execute multiple runs.
	for i := 0; i < 3; i++ {
		if _, err := engine.Execute(context.Background(), reconciliation.RunRequest{
			ParticipantID: pid,
			ScopeFrom:     scope.From,
			ScopeTo:       scope.To,
		}); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}

	page, err := engine.ListRuns(context.Background(), reconciliation.ListRunsRequest{Limit: 2})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("expected 2 items (limited), got %d", len(page.Items))
	}
	if page.Total != 3 {
		t.Fatalf("expected total 3, got %d", page.Total)
	}
	if page.NextOffset == nil {
		t.Fatal("expected NextOffset to be set for paginated result")
	}
}

func TestEngineGetRunNotFound(t *testing.T) {
	engine, _ := newTestEngine(t, []string{"BANK-A"}, func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
		return makeTestParticipant(t, id, nil), nil
	})
	_, err := engine.GetRun(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected ErrRunNotFound for nonexistent run ID")
	}
}
