package reconciliation_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/reconciliation"
)

// countingParticipant wraps a ReconciliationParticipant and records all method calls
// to provide deterministic verification of traversal and pruning.
type countingParticipant struct {
	reconciliation.ReconciliationParticipant
	mu               sync.Mutex
	getChildrenCalls int
	getBucketIDCalls int
	getRecordsCalls  int
	visitedNodes     []string
}

func newCountingParticipant(p reconciliation.ReconciliationParticipant) *countingParticipant {
	return &countingParticipant{ReconciliationParticipant: p}
}

func (c *countingParticipant) GetChildren(ctx context.Context, ref reconciliation.NodeRef) ([]reconciliation.NodeResult, error) {
	c.mu.Lock()
	c.getChildrenCalls++
	c.visitedNodes = append(c.visitedNodes, ref.Path)
	c.mu.Unlock()
	return c.ReconciliationParticipant.GetChildren(ctx, ref)
}

func (c *countingParticipant) GetBucketID(ctx context.Context, ref reconciliation.NodeRef) (reconciliation.BucketID, error) {
	c.mu.Lock()
	c.getBucketIDCalls++
	c.mu.Unlock()
	return c.ReconciliationParticipant.GetBucketID(ctx, ref)
}

func (c *countingParticipant) GetRecords(ctx context.Context, ref reconciliation.BucketRef) ([]reconciliation.CanonicalRecord, error) {
	c.mu.Lock()
	c.getRecordsCalls++
	c.mu.Unlock()
	return c.ReconciliationParticipant.GetRecords(ctx, ref)
}

func (c *countingParticipant) HasVisited(path string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range c.visitedNodes {
		if p == path {
			return true
		}
	}
	return false
}

type mutableSnapshotSource struct {
	bankID  string
	entries []bank.LedgerEntry
}

func (m *mutableSnapshotSource) GetLedgerSnapshot(_ context.Context, _ bank.LedgerScope) (bank.LedgerSnapshot, error) {
	return bank.LedgerSnapshot{
		BankID:     m.bankID,
		SnapshotID: uuid.New(),
		CapturedAt: time.Now().UTC(),
		Entries:    append([]bank.LedgerEntry(nil), m.entries...),
	}, nil
}

// --- helper: in-memory RunStore for engine unit tests ---

type memoryRunRepository struct {
	runs          map[uuid.UUID]reconciliation.Run
	discrepancies []reconciliation.Discrepancy
}

func newMemoryRunRepository() *memoryRunRepository {
	return &memoryRunRepository{runs: make(map[uuid.UUID]reconciliation.Run)}
}

func (m *memoryRunRepository) CreateRun(_ context.Context, participantID string, scope reconciliation.Scope) (reconciliation.Run, error) {
	run := reconciliation.Run{
		ID:            uuid.New(),
		ParticipantID: participantID,
		ScopeFrom:     scope.From.UTC(),
		ScopeTo:       scope.To.UTC(),
		Status:        reconciliation.RunStatusRunning,
		StartedAt:     time.Now().UTC(),
	}
	m.runs[run.ID] = run
	return run, nil
}

func (m *memoryRunRepository) CompleteRun(_ context.Context, runID uuid.UUID, canonRoot, partRoot []byte, canonVer, algoVer string, recordCount, discrepancyCount int64) (reconciliation.Run, error) {
	run, ok := m.runs[runID]
	if !ok {
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
	m.runs[runID] = run
	return run, nil
}

func (m *memoryRunRepository) FailRun(_ context.Context, runID uuid.UUID, errMsg string) (reconciliation.Run, error) {
	run, ok := m.runs[runID]
	if !ok {
		return reconciliation.Run{}, reconciliation.ErrRunNotFound
	}
	run.Status = reconciliation.RunStatusFailed
	run.ErrorMessage = errMsg
	now := time.Now().UTC()
	run.CompletedAt = &now
	m.runs[runID] = run
	return run, nil
}

func (m *memoryRunRepository) GetRun(_ context.Context, runID uuid.UUID) (reconciliation.Run, error) {
	run, ok := m.runs[runID]
	if !ok {
		return reconciliation.Run{}, reconciliation.ErrRunNotFound
	}
	return run, nil
}

func (m *memoryRunRepository) ListRuns(_ context.Context, req reconciliation.ListRunsRequest) (reconciliation.RunListPage, error) {
	var matched []reconciliation.Run
	for _, r := range m.runs {
		if req.ParticipantID != "" && r.ParticipantID != req.ParticipantID {
			continue
		}
		matched = append(matched, r)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	page := reconciliation.RunListPage{Total: len(matched), Limit: limit}
	if req.Offset < len(matched) {
		end := req.Offset + limit
		if end > len(matched) {
			end = len(matched)
		}
		page.Items = matched[req.Offset:end]
	}
	if page.Items == nil {
		page.Items = []reconciliation.Run{}
	}
	return page, nil
}

func (m *memoryRunRepository) SaveDiscrepancy(_ context.Context, disc reconciliation.Discrepancy) (reconciliation.Discrepancy, error) {
	disc.ID = uuid.New()
	disc.DetectedAt = time.Now().UTC()
	m.discrepancies = append(m.discrepancies, disc)
	return disc, nil
}

func (m *memoryRunRepository) ListDiscrepancies(_ context.Context, req reconciliation.ListDiscrepanciesRequest) (reconciliation.DiscrepancyListPage, error) {
	var items []reconciliation.Discrepancy
	for _, d := range m.discrepancies {
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

func makeTestParticipant(t *testing.T, participantID string, records []reconciliation.CanonicalRecord) reconciliation.ReconciliationParticipant {
	t.Helper()
	p, err := reconciliation.NewMemoryParticipant(participantID, participantID, time.Hour, records)
	if err != nil {
		t.Fatalf("build test participant: %v", err)
	}
	return p
}

func newTestEngine(
	t *testing.T,
	participants []string,
	canonicalFactory func(ctx context.Context, id string, scope reconciliation.Scope) (reconciliation.ReconciliationParticipant, error),
	participantFactory func(ctx context.Context, id string, scope reconciliation.Scope) (reconciliation.ReconciliationParticipant, error),
) (*reconciliation.Engine, *memoryRunRepository) {
	t.Helper()
	repo := newMemoryRunRepository()
	knownParticipants := make(reconciliation.KnownParticipants)
	for _, p := range participants {
		knownParticipants[p] = true
	}
	engine := reconciliation.NewEngineWithRepo(knownParticipants, repo, canonicalFactory, participantFactory)
	return engine, repo
}

func makeSampleRecords(base time.Time) []reconciliation.CanonicalRecord {
	return []reconciliation.CanonicalRecord{
		{
			OperationID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			PaymentID:   uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
			AccountID:   uuid.MustParse("00000000-0000-0000-0000-000000000001"),
			EntryType:   "DEBIT",
			AmountPaise: 1000,
			Currency:    "INR",
			OccurredAt:  base.Add(30 * time.Minute), // Bucket 0: [base, base+1h)
		},
		{
			OperationID: uuid.MustParse("22222222-2222-2222-2222-222222222222"),
			PaymentID:   uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
			AccountID:   uuid.MustParse("00000000-0000-0000-0000-000000000002"),
			EntryType:   "CREDIT",
			AmountPaise: 2000,
			Currency:    "INR",
			OccurredAt:  base.Add(1*time.Hour + 30*time.Minute), // Bucket 1: [base+1h, base+2h)
		},
		{
			OperationID: uuid.MustParse("33333333-3333-3333-3333-333333333333"),
			PaymentID:   uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc"),
			AccountID:   uuid.MustParse("00000000-0000-0000-0000-000000000003"),
			EntryType:   "DEBIT",
			AmountPaise: 3000,
			Currency:    "INR",
			OccurredAt:  base.Add(2*time.Hour + 30*time.Minute), // Bucket 2: [base+2h, base+3h)
		},
		{
			OperationID: uuid.MustParse("44444444-4444-4444-4444-444444444444"),
			PaymentID:   uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd"),
			AccountID:   uuid.MustParse("00000000-0000-0000-0000-000000000004"),
			EntryType:   "CREDIT",
			AmountPaise: 4000,
			Currency:    "INR",
			OccurredAt:  base.Add(3*time.Hour + 30*time.Minute), // Bucket 3: [base+3h, base+4h)
		},
	}
}

func makeNRecords(base time.Time, n int) []reconciliation.CanonicalRecord {
	records := make([]reconciliation.CanonicalRecord, n)
	for i := 0; i < n; i++ {
		records[i] = reconciliation.CanonicalRecord{
			OperationID: uuid.New(),
			PaymentID:   uuid.New(),
			AccountID:   uuid.New(),
			EntryType:   "DEBIT",
			AmountPaise: int64((i + 1) * 1000),
			Currency:    "INR",
			OccurredAt:  base.Add(time.Duration(i)*time.Hour + 30*time.Minute),
		}
	}
	return records
}

// --- Required Tests ---

// CASE A: Identical data -> 0 discrepancies, COMPLETED status
func TestTwoSidedIdenticalRoots(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(4 * time.Hour)}
	const pid = "BANK-A"

	canonRecords := makeSampleRecords(base)
	partRecords := makeSampleRecords(base)

	engine, repo := newTestEngine(
		t,
		[]string{pid},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, canonRecords), nil
		},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, partRecords), nil
		},
	)

	run, err := engine.Execute(context.Background(), reconciliation.RunRequest{
		ParticipantID: pid,
		ScopeFrom:     scope.From,
		ScopeTo:       scope.To,
	})
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	if run.Status != reconciliation.RunStatusCompleted {
		t.Fatalf("run status = %q, want COMPLETED", run.Status)
	}
	if run.DiscrepancyCount != 0 {
		t.Fatalf("discrepancyCount = %d, want 0", run.DiscrepancyCount)
	}
	if len(repo.discrepancies) != 0 {
		t.Fatalf("saved discrepancies = %d, want 0", len(repo.discrepancies))
	}
	if !bytes.Equal(run.CanonicalRoot, run.ParticipantRoot) {
		t.Fatal("expected identical canonical and participant roots for identical data")
	}
}

// CASE B: One mutation in one bucket -> affected region found, COMPLETED status
func TestTwoSidedSingleBucketMismatch(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(4 * time.Hour)}
	const pid = "BANK-A"

	canonRecords := makeSampleRecords(base)
	partRecords := makeSampleRecords(base)
	// Mutate Bucket 2 in participant side (AmountPaise 3000 -> 9999)
	partRecords[2].AmountPaise = 9999

	engine, repo := newTestEngine(
		t,
		[]string{pid},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, canonRecords), nil
		},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, partRecords), nil
		},
	)

	run, err := engine.Execute(context.Background(), reconciliation.RunRequest{
		ParticipantID: pid,
		ScopeFrom:     scope.From,
		ScopeTo:       scope.To,
	})
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	if run.Status != reconciliation.RunStatusCompleted {
		t.Fatalf("run status = %q, want COMPLETED", run.Status)
	}
	if bytes.Equal(run.CanonicalRoot, run.ParticipantRoot) {
		t.Fatal("expected divergent canonical and participant roots")
	}
	if run.DiscrepancyCount != 1 {
		t.Fatalf("discrepancyCount = %d, want 1", run.DiscrepancyCount)
	}
	if len(repo.discrepancies) != 1 {
		t.Fatalf("saved discrepancies = %d, want 1", len(repo.discrepancies))
	}
	d := repo.discrepancies[0]
	if d.MismatchCategory != reconciliation.MismatchRecordDifference {
		t.Fatalf("discrepancy category = %q, want %q", d.MismatchCategory, reconciliation.MismatchRecordDifference)
	}
	if d.Evidence["field"] != "amount_paise" {
		t.Fatalf("discrepancy field = %q, want amount_paise", d.Evidence["field"])
	}
}

// CASE C: Two mutations in separate branches -> BOTH regions preserved
func TestTwoSidedMultipleBucketMismatches(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(4 * time.Hour)}
	const pid = "BANK-A"

	canonRecords := makeSampleRecords(base)
	partRecords := makeSampleRecords(base)
	// Mutate Bucket 0 (left branch) and Bucket 2 (right branch)
	partRecords[0].AmountPaise = 1111 // Bucket 0 mutated
	partRecords[2].AmountPaise = 3333 // Bucket 2 mutated

	engine, repo := newTestEngine(
		t,
		[]string{pid},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, canonRecords), nil
		},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, partRecords), nil
		},
	)

	run, err := engine.Execute(context.Background(), reconciliation.RunRequest{
		ParticipantID: pid,
		ScopeFrom:     scope.From,
		ScopeTo:       scope.To,
	})
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	if run.Status != reconciliation.RunStatusCompleted {
		t.Fatalf("run status = %q, want COMPLETED", run.Status)
	}
	if bytes.Equal(run.CanonicalRoot, run.ParticipantRoot) {
		t.Fatal("expected divergent canonical and participant roots")
	}
	// Must have at least 2 discrepancies across the 2 distinct bucket regions
	if run.DiscrepancyCount < 2 {
		t.Fatalf("discrepancyCount = %d, want at least 2", run.DiscrepancyCount)
	}

	// Verify both bucket start times are represented
	bucketStarts := make(map[time.Time]bool)
	for _, d := range repo.discrepancies {
		bucketStarts[d.BucketStart] = true
	}
	wantStart0 := base
	wantStart2 := base.Add(2 * time.Hour)
	if !bucketStarts[wantStart0] {
		t.Errorf("missing discrepancy for Bucket 0 start %v", wantStart0)
	}
	if !bucketStarts[wantStart2] {
		t.Errorf("missing discrepancy for Bucket 2 start %v", wantStart2)
	}
}

// CASE D: Three dispersed mutations -> ALL regions preserved (never early exits)
func TestTwoSidedDispersedMismatches(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(4 * time.Hour)}
	const pid = "BANK-A"

	canonRecords := makeSampleRecords(base)
	// Participant mutations across 3 separate buckets:
	// Bucket 0: Amount changed
	// Bucket 2: Currency changed
	// Bucket 3: Record completely missing on participant side
	partRecords := []reconciliation.CanonicalRecord{
		canonRecords[0],
		canonRecords[1],
		canonRecords[2],
	}
	partRecords[0].AmountPaise = 777777
	partRecords[2].Currency = "USD"
	// canonRecords[3] is omitted from participant side

	engine, repo := newTestEngine(
		t,
		[]string{pid},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, canonRecords), nil
		},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, partRecords), nil
		},
	)

	run, err := engine.Execute(context.Background(), reconciliation.RunRequest{
		ParticipantID: pid,
		ScopeFrom:     scope.From,
		ScopeTo:       scope.To,
	})
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	if run.Status != reconciliation.RunStatusCompleted {
		t.Fatalf("run status = %q, want COMPLETED", run.Status)
	}
	if run.DiscrepancyCount < 3 {
		t.Fatalf("discrepancyCount = %d, want at least 3", run.DiscrepancyCount)
	}

	categories := make(map[string]int)
	for _, d := range repo.discrepancies {
		categories[d.MismatchCategory]++
	}
	if categories[reconciliation.MismatchRecordDifference] < 2 {
		t.Errorf("expected at least 2 RECORD_MISMATCH discrepancies, got %d", categories[reconciliation.MismatchRecordDifference])
	}
	if categories[reconciliation.MismatchMissingRecord] < 1 {
		t.Errorf("expected at least 1 MISSING_PARTICIPANT_RECORD discrepancy, got %d", categories[reconciliation.MismatchMissingRecord])
	}
}

// Record differences: missing record, extra record, field mismatch
func TestTwoSidedRecordDifference(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(2 * time.Hour)}
	const pid = "BANK-A"

	canonRecords := []reconciliation.CanonicalRecord{
		{
			OperationID: uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000001"),
			PaymentID:   uuid.New(),
			AccountID:   uuid.New(),
			EntryType:   "DEBIT",
			AmountPaise: 500,
			Currency:    "INR",
			OccurredAt:  base.Add(15 * time.Minute),
		},
	}

	// Participant has an extra record not present in canonical
	partRecords := []reconciliation.CanonicalRecord{
		canonRecords[0],
		{
			OperationID: uuid.MustParse("bbbbbbbb-0000-0000-0000-000000000002"),
			PaymentID:   uuid.New(),
			AccountID:   uuid.New(),
			EntryType:   "CREDIT",
			AmountPaise: 999,
			Currency:    "INR",
			OccurredAt:  base.Add(30 * time.Minute),
		},
	}

	engine, repo := newTestEngine(
		t,
		[]string{pid},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, canonRecords), nil
		},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, partRecords), nil
		},
	)

	run, err := engine.Execute(context.Background(), reconciliation.RunRequest{
		ParticipantID: pid,
		ScopeFrom:     scope.From,
		ScopeTo:       scope.To,
	})
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	if run.Status != reconciliation.RunStatusCompleted {
		t.Fatalf("run status = %q, want COMPLETED", run.Status)
	}
	if run.DiscrepancyCount != 1 {
		t.Fatalf("discrepancyCount = %d, want 1", run.DiscrepancyCount)
	}
	if repo.discrepancies[0].MismatchCategory != reconciliation.MismatchExtraRecord {
		t.Fatalf("category = %q, want %q", repo.discrepancies[0].MismatchCategory, reconciliation.MismatchExtraRecord)
	}
}

// Stores canonical and participant roots separately when divergent
func TestCanonicalAndParticipantRootsStoredSeparately(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(2 * time.Hour)}
	const pid = "BANK-A"

	canonRecords := makeSampleRecords(base)[:2]
	partRecords := makeSampleRecords(base)[:2]
	partRecords[0].AmountPaise = 999999

	engine, repo := newTestEngine(
		t,
		[]string{pid},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, canonRecords), nil
		},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, partRecords), nil
		},
	)

	run, err := engine.Execute(context.Background(), reconciliation.RunRequest{
		ParticipantID: pid,
		ScopeFrom:     scope.From,
		ScopeTo:       scope.To,
	})
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	if bytes.Equal(run.CanonicalRoot, run.ParticipantRoot) {
		t.Fatal("expected CanonicalRoot and ParticipantRoot to be distinct")
	}
	if len(run.CanonicalRoot) != 32 {
		t.Fatalf("CanonicalRoot length = %d, want 32", len(run.CanonicalRoot))
	}
	if len(run.ParticipantRoot) != 32 {
		t.Fatalf("ParticipantRoot length = %d, want 32", len(run.ParticipantRoot))
	}

	// Verify in repository
	saved, err := repo.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRun error: %v", err)
	}
	if bytes.Equal(saved.CanonicalRoot, saved.ParticipantRoot) {
		t.Fatal("saved CanonicalRoot and ParticipantRoot in repository are not distinct")
	}
}

// Canonical source is independent of participant mutations
func TestCanonicalSourceIsIndependent(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(2 * time.Hour)}
	const pid = "BANK-A"

	initialRecords := makeSampleRecords(base)[:2]

	// 1. Canonical source: independent ReconciliationParticipant instance
	canonP := makeTestParticipant(t, pid, initialRecords)

	// 2. Participant-side source: independent RepositoryParticipant backed by a mutable snapshot source
	entries := make([]bank.LedgerEntry, len(initialRecords))
	for i, r := range initialRecords {
		entries[i] = bank.LedgerEntry{
			OperationID: r.OperationID,
			PaymentID:   r.PaymentID,
			AccountID:   r.AccountID,
			EntryType:   r.EntryType,
			AmountPaise: r.AmountPaise,
			Currency:    r.Currency,
			OccurredAt:  r.OccurredAt,
		}
	}
	partSource := &mutableSnapshotSource{bankID: pid, entries: entries}
	partP, err := reconciliation.NewRepositoryParticipant(partSource, pid, pid, time.Hour)
	if err != nil {
		t.Fatalf("build RepositoryParticipant: %v", err)
	}
	if err := partP.Initialize(context.Background(), scope); err != nil {
		t.Fatalf("initialize participant: %v", err)
	}

	canonRootInitial, err := canonP.GetRoot(context.Background(), scope)
	if err != nil {
		t.Fatalf("canonical GetRoot: %v", err)
	}
	partRootInitial, err := partP.GetRoot(context.Background(), scope)
	if err != nil {
		t.Fatalf("participant GetRoot: %v", err)
	}

	// Initially, both independent sources have identical roots for identical data
	if !bytes.Equal(canonRootInitial.Root, partRootInitial.Root) {
		t.Fatal("expected initial canonical and participant roots to be identical")
	}

	// 3. Mutate participant-side data in the participant's own source and refresh it
	partSource.entries[0].AmountPaise = 888888
	partSource.entries[1].AmountPaise = 777777
	if err := partP.Refresh(context.Background(), scope); err != nil {
		t.Fatalf("participant Refresh: %v", err)
	}

	partRootMutated, err := partP.GetRoot(context.Background(), scope)
	if err != nil {
		t.Fatalf("mutated participant GetRoot: %v", err)
	}

	// Proves changing participant-side data changes participant root
	if bytes.Equal(partRootInitial.Root, partRootMutated.Root) {
		t.Fatal("expected participant root to change after participant data mutation")
	}

	// Proves canonical root does NOT change when participant-side data is mutated
	canonRootAfter, err := canonP.GetRoot(context.Background(), scope)
	if err != nil {
		t.Fatalf("canonical GetRoot after: %v", err)
	}
	if !bytes.Equal(canonRootInitial.Root, canonRootAfter.Root) {
		t.Fatal("canonical participant root was modified when participant side changed")
	}

	// Proves the two sources have diverged
	if bytes.Equal(canonRootAfter.Root, partRootMutated.Root) {
		t.Fatal("expected canonical and participant roots to diverge after mutation")
	}
}

// One-sided subtree/node coverage:
// Verifies when a divergent node exists only in canonical ledger:
// - the run completes normally (status COMPLETED)
// - discrepancy is preserved
// - no panic
// - the affected region is identified
// - reconciliation does not stop before processing remaining divergent regions
func TestTwoSidedOneSidedSubtreeCoverage(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(4 * time.Hour)}
	const pid = "BANK-A"

	// Canonical has 4 records across 4 buckets (Bucket 0, 1, 2, 3)
	canonRecords := makeSampleRecords(base)
	// Participant has records only in Buckets 0, 1, 2 (Bucket 3 does not exist on participant side)
	// AND Bucket 0 has a mutated amount (divergent region in a separate branch)
	partRecords := []reconciliation.CanonicalRecord{
		canonRecords[0],
		canonRecords[1],
		canonRecords[2],
	}
	partRecords[0].AmountPaise = 999999 // mutated Bucket 0

	engine, repo := newTestEngine(
		t,
		[]string{pid},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, canonRecords), nil
		},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, partRecords), nil
		},
	)

	run, err := engine.Execute(context.Background(), reconciliation.RunRequest{
		ParticipantID: pid,
		ScopeFrom:     scope.From,
		ScopeTo:       scope.To,
	})
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	// 1. Run completes normally without panic
	if run.Status != reconciliation.RunStatusCompleted {
		t.Fatalf("status = %q, want COMPLETED", run.Status)
	}

	// 2. Both divergent regions are identified and preserved (does not stop early)
	if run.DiscrepancyCount != 2 {
		t.Fatalf("discrepancyCount = %d, want 2", run.DiscrepancyCount)
	}
	if len(repo.discrepancies) != 2 {
		t.Fatalf("saved discrepancies = %d, want 2", len(repo.discrepancies))
	}

	// 3. Verify the one-sided node discrepancy (Bucket 3 exists only on canonical side)
	var foundOneSided, foundBucket0 bool
	for _, d := range repo.discrepancies {
		if d.MismatchCategory == reconciliation.MismatchMissingRecord && d.Evidence["reason"] == "node exists only in canonical ledger" {
			foundOneSided = true
			if !d.BucketStart.Equal(base.Add(3 * time.Hour)) {
				t.Errorf("one-sided discrepancy BucketStart = %v, want %v", d.BucketStart, base.Add(3*time.Hour))
			}
			if d.BucketWidthNs != int64(time.Hour) {
				t.Errorf("one-sided discrepancy BucketWidthNs = %d, want %d", d.BucketWidthNs, int64(time.Hour))
			}
			expectedBucketKey := reconciliation.BucketID{
				Partition: pid,
				Start:     base.Add(3 * time.Hour),
				Width:     time.Hour,
			}.String()
			if d.BucketKey != expectedBucketKey {
				t.Errorf("one-sided discrepancy BucketKey = %q, want %q", d.BucketKey, expectedBucketKey)
			}
		}
		if d.MismatchCategory == reconciliation.MismatchRecordDifference && d.Evidence["field"] == "amount_paise" {
			foundBucket0 = true
			if !d.BucketStart.Equal(base) {
				t.Errorf("bucket 0 discrepancy BucketStart = %v, want %v", d.BucketStart, base)
			}
		}
	}

	if !foundOneSided {
		t.Fatal("expected one-sided node discrepancy (node exists only in canonical ledger)")
	}
	if !foundBucket0 {
		t.Fatal("expected mutated bucket 0 discrepancy (field amount_paise mismatch)")
	}
}

// Verifies when a divergent node exists only on participant side:
// - run completes normally
// - MismatchExtraRecord discrepancy is preserved
// - remaining divergent regions in other branches are processed
func TestTwoSidedOneSidedParticipantNodeCoverage(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(4 * time.Hour)}
	const pid = "BANK-A"

	// Canonical has records in buckets 0, 1, 2
	canonRecords := makeSampleRecords(base)[:3]
	// Participant has records in buckets 0, 1, 2, 3 (Bucket 3 is extra on participant side)
	// AND Bucket 0 has a mutated amount
	partRecords := makeSampleRecords(base)
	partRecords[0].AmountPaise = 888888 // mutated Bucket 0

	engine, repo := newTestEngine(
		t,
		[]string{pid},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, canonRecords), nil
		},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, partRecords), nil
		},
	)

	run, err := engine.Execute(context.Background(), reconciliation.RunRequest{
		ParticipantID: pid,
		ScopeFrom:     scope.From,
		ScopeTo:       scope.To,
	})
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	if run.Status != reconciliation.RunStatusCompleted {
		t.Fatalf("status = %q, want COMPLETED", run.Status)
	}
	if run.DiscrepancyCount != 2 {
		t.Fatalf("discrepancyCount = %d, want 2", run.DiscrepancyCount)
	}

	var foundExtraNode, foundBucket0 bool
	for _, d := range repo.discrepancies {
		if d.MismatchCategory == reconciliation.MismatchExtraRecord && d.Evidence["reason"] == "node exists only in participant ledger" {
			foundExtraNode = true
			if !d.BucketStart.Equal(base.Add(3 * time.Hour)) {
				t.Errorf("extra node discrepancy BucketStart = %v, want %v", d.BucketStart, base.Add(3*time.Hour))
			}
			if d.BucketWidthNs != int64(time.Hour) {
				t.Errorf("extra node discrepancy BucketWidthNs = %d, want %d", d.BucketWidthNs, int64(time.Hour))
			}
			expectedBucketKey := reconciliation.BucketID{
				Partition: pid,
				Start:     base.Add(3 * time.Hour),
				Width:     time.Hour,
			}.String()
			if d.BucketKey != expectedBucketKey {
				t.Errorf("extra node discrepancy BucketKey = %q, want %q", d.BucketKey, expectedBucketKey)
			}
		}
		if d.MismatchCategory == reconciliation.MismatchRecordDifference && d.Evidence["field"] == "amount_paise" {
			foundBucket0 = true
			if !d.BucketStart.Equal(base) {
				t.Errorf("bucket 0 discrepancy BucketStart = %v, want %v", d.BucketStart, base)
			}
		}
	}

	if !foundExtraNode {
		t.Fatal("expected one-sided extra node discrepancy (node exists only in participant ledger)")
	}
	if !foundBucket0 {
		t.Fatal("expected mutated bucket 0 discrepancy")
	}
}

// Requirement 6: TestEqualSubtreeIsPruned proves that identical subtrees are pruned in O(1)
// by comparing child hashes BEFORE any recursive descendant enumeration or leaf fetching.
func TestEqualSubtreeIsPruned(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(8 * time.Hour)}
	const pid = "BANK-A"

	// 8 buckets (0..7)
	canonRecords := makeNRecords(base, 8)
	partRecords := make([]reconciliation.CanonicalRecord, len(canonRecords))
	copy(partRecords, canonRecords)

	// Make branch 1 (right branch) diverge at Bucket 6:
	// Left branch covering Buckets 0..3 (node L2/0) is 100% IDENTICAL on both sides.
	partRecords[6].AmountPaise = 999999

	var canonCounting, partCounting *countingParticipant
	engine, repo := newTestEngine(
		t,
		[]string{pid},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			canonCounting = newCountingParticipant(makeTestParticipant(t, id, canonRecords))
			return canonCounting, nil
		},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			partCounting = newCountingParticipant(makeTestParticipant(t, id, partRecords))
			return partCounting, nil
		},
	)

	run, err := engine.Execute(context.Background(), reconciliation.RunRequest{
		ParticipantID: pid,
		ScopeFrom:     scope.From,
		ScopeTo:       scope.To,
	})
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	if run.Status != reconciliation.RunStatusCompleted {
		t.Fatalf("status = %q, want COMPLETED", run.Status)
	}
	if run.DiscrepancyCount != 1 {
		t.Fatalf("discrepancyCount = %d, want 1", run.DiscrepancyCount)
	}
	if len(repo.discrepancies) != 1 {
		t.Fatalf("saved discrepancies = %d, want 1", len(repo.discrepancies))
	}

	// Verify the discrepancy is indeed Bucket 6
	if !repo.discrepancies[0].BucketStart.Equal(base.Add(6 * time.Hour)) {
		t.Fatalf("discrepancy BucketStart = %v, want %v", repo.discrepancies[0].BucketStart, base.Add(6*time.Hour))
	}

	// PROVE TRUE MERKLE PRUNING via deterministic call counters:
	// For both canonical and participant:
	// - Root L3/0 was evaluated and children (L2/0 and L2/1) were returned.
	// - L2/0 covers [0, 4h). Because Buckets 0..3 are identical, its hashes match!
	// - L2/0 was PRUNED immediately without calling GetChildren on L2/0 or any of its descendants.
	// - L2/1 covers [4h, 8h). Its hashes differ, so L2/1 was descended into.
	for _, p := range []*countingParticipant{canonCounting, partCounting} {
		if !p.HasVisited("L3/0") {
			t.Errorf("expected root L3/0 to be visited")
		}
		if !p.HasVisited("L2/1") {
			t.Errorf("expected divergent node L2/1 to be visited")
		}
		if !p.HasVisited("L1/3") {
			t.Errorf("expected divergent node L1/3 to be visited")
		}
		if !p.HasVisited("L0/6") {
			t.Errorf("expected divergent leaf L0/6 to be visited")
		}

		// PROOF: Identical subtree L2/0 (and its descendants L1/0, L1/1, L0/0, L0/1, L0/2, L0/3)
		// must NEVER have been visited or passed to GetChildren!
		unvisitedIdenticalNodes := []string{"L2/0", "L1/0", "L1/1", "L0/0", "L0/1", "L0/2", "L0/3"}
		for _, node := range unvisitedIdenticalNodes {
			if p.HasVisited(node) {
				t.Fatalf("MERKLE PRUNING VIOLATION: identical node %s was enumerated via GetChildren!", node)
			}
		}
	}

	// Furthermore, verify that leaf bucket records were only fetched for the divergent bucket (Bucket 6),
	// never for the identical buckets 0..3:
	if partCounting.getRecordsCalls != 1 {
		t.Errorf("partCounting.getRecordsCalls = %d, want 1 (only mutated Bucket 6)", partCounting.getRecordsCalls)
	}
	if canonCounting.getRecordsCalls != 1 {
		t.Errorf("canonCounting.getRecordsCalls = %d, want 1 (only mutated Bucket 6)", canonCounting.getRecordsCalls)
	}
}

// Requirement 6: TestTreeHeightMismatchStillUsesBucketIDs preserves 4-vs-2 and 1-vs-2 cases
// with exact discrepancy counts and proves traversal via deterministic call counters.
func TestTreeHeightMismatchStillUsesBucketIDs(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	const pid = "BANK-A"

	t.Run("Canonical4BucketsVsParticipant2Buckets", func(t *testing.T) {
		scope := reconciliation.Scope{From: base, To: base.Add(4 * time.Hour)}
		allRecords := makeSampleRecords(base) // 4 records across 4 buckets
		canonRecords := allRecords             // buckets 0, 1, 2, 3
		partRecords := allRecords[:2]          // buckets 0, 1 only (identical to canon)

		var canonCounting, partCounting *countingParticipant
		engine, repo := newTestEngine(
			t,
			[]string{pid},
			func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
				canonCounting = newCountingParticipant(makeTestParticipant(t, id, canonRecords))
				return canonCounting, nil
			},
			func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
				partCounting = newCountingParticipant(makeTestParticipant(t, id, partRecords))
				return partCounting, nil
			},
		)

		run, err := engine.Execute(context.Background(), reconciliation.RunRequest{
			ParticipantID: pid,
			ScopeFrom:     scope.From,
			ScopeTo:       scope.To,
		})
		if err != nil {
			t.Fatalf("unexpected execute error: %v", err)
		}

		if run.Status != reconciliation.RunStatusCompleted {
			t.Fatalf("status = %q, want COMPLETED", run.Status)
		}

		// Exactly 2 discrepancies: Bucket 2 and Bucket 3 are missing on participant side.
		// Buckets 0 and 1 are identical and must NOT produce false discrepancies.
		if run.DiscrepancyCount != 2 {
			t.Fatalf("discrepancyCount = %d, want 2 (only buckets 2 and 3 missing)", run.DiscrepancyCount)
		}
		if len(repo.discrepancies) != 2 {
			t.Fatalf("saved discrepancies = %d, want 2", len(repo.discrepancies))
		}

		missingStarts := make(map[time.Time]bool)
		for _, d := range repo.discrepancies {
			if d.MismatchCategory != reconciliation.MismatchMissingRecord {
				t.Errorf("unexpected discrepancy category %q, want MISSING_PARTICIPANT_RECORD", d.MismatchCategory)
			}
			missingStarts[d.BucketStart] = true
			if d.BucketStart.Equal(base) || d.BucketStart.Equal(base.Add(time.Hour)) {
				t.Errorf("false discrepancy created for identical bucket at %v", d.BucketStart)
			}
		}

		if !missingStarts[base.Add(2*time.Hour)] {
			t.Errorf("missing expected discrepancy for Bucket 2 at %v", base.Add(2*time.Hour))
		}
		if !missingStarts[base.Add(3*time.Hour)] {
			t.Errorf("missing expected discrepancy for Bucket 3 at %v", base.Add(3*time.Hour))
		}

		// Deterministic call counters prove traversal occurred
		if canonCounting.getChildrenCalls == 0 || partCounting.getChildrenCalls == 0 {
			t.Errorf("expected tree traversal calls for 4-vs-2 height mismatch")
		}
		if canonCounting.getBucketIDCalls == 0 || partCounting.getBucketIDCalls == 0 {
			t.Errorf("expected GetBucketID calls during 4-vs-2 height mismatch reconciliation")
		}
	})

	t.Run("Canonical1BucketVsParticipant2Buckets", func(t *testing.T) {
		scope := reconciliation.Scope{From: base, To: base.Add(2 * time.Hour)}
		allRecords := makeSampleRecords(base)
		canonRecords := allRecords[:1] // bucket 0 only
		partRecords := allRecords[:2]  // bucket 0 (identical) + bucket 1 (extra)

		var canonCounting, partCounting *countingParticipant
		engine, repo := newTestEngine(
			t,
			[]string{pid},
			func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
				canonCounting = newCountingParticipant(makeTestParticipant(t, id, canonRecords))
				return canonCounting, nil
			},
			func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
				partCounting = newCountingParticipant(makeTestParticipant(t, id, partRecords))
				return partCounting, nil
			},
		)

		run, err := engine.Execute(context.Background(), reconciliation.RunRequest{
			ParticipantID: pid,
			ScopeFrom:     scope.From,
			ScopeTo:       scope.To,
		})
		if err != nil {
			t.Fatalf("unexpected execute error: %v", err)
		}

		if run.Status != reconciliation.RunStatusCompleted {
			t.Fatalf("status = %q, want COMPLETED", run.Status)
		}

		// Exactly 1 discrepancy: Bucket 1 is extra on participant side.
		// Bucket 0 is identical and must NOT produce a false discrepancy.
		if run.DiscrepancyCount != 1 {
			t.Fatalf("discrepancyCount = %d, want 1 (only bucket 1 extra)", run.DiscrepancyCount)
		}
		if len(repo.discrepancies) != 1 {
			t.Fatalf("saved discrepancies = %d, want 1", len(repo.discrepancies))
		}

		d := repo.discrepancies[0]
		if d.MismatchCategory != reconciliation.MismatchExtraRecord {
			t.Errorf("category = %q, want EXTRA_PARTICIPANT_RECORD", d.MismatchCategory)
		}
		if !d.BucketStart.Equal(base.Add(time.Hour)) {
			t.Errorf("discrepancy BucketStart = %v, want %v", d.BucketStart, base.Add(time.Hour))
		}

		// Deterministic call counters prove traversal occurred
		if canonCounting.getBucketIDCalls == 0 || partCounting.getBucketIDCalls == 0 {
			t.Errorf("expected GetBucketID calls during 1-vs-2 height mismatch reconciliation")
		}
	})
}

// Preserved test alias for backwards compatibility
func TestTreeHeightMismatchDoesNotCreateFalseDiscrepancies(t *testing.T) {
	TestTreeHeightMismatchStillUsesBucketIDs(t)
}

// Requirement 6: TestOneSidedSubtreeStillUsesBucketIDs preserves both canonical-only
// and participant-only cases and verifies deterministic call counters.
func TestOneSidedSubtreeStillUsesBucketIDs(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	const pid = "BANK-A"

	t.Run("CanonicalOnlySubtree", func(t *testing.T) {
		scope := reconciliation.Scope{From: base, To: base.Add(4 * time.Hour)}
		canonRecords := makeSampleRecords(base)
		partRecords := []reconciliation.CanonicalRecord{
			canonRecords[0],
			canonRecords[1],
			canonRecords[2],
		}
		partRecords[0].AmountPaise = 999999 // mutated Bucket 0

		var canonCounting, partCounting *countingParticipant
		engine, repo := newTestEngine(
			t,
			[]string{pid},
			func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
				canonCounting = newCountingParticipant(makeTestParticipant(t, id, canonRecords))
				return canonCounting, nil
			},
			func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
				partCounting = newCountingParticipant(makeTestParticipant(t, id, partRecords))
				return partCounting, nil
			},
		)

		run, err := engine.Execute(context.Background(), reconciliation.RunRequest{
			ParticipantID: pid,
			ScopeFrom:     scope.From,
			ScopeTo:       scope.To,
		})
		if err != nil {
			t.Fatalf("unexpected execute error: %v", err)
		}

		if run.Status != reconciliation.RunStatusCompleted {
			t.Fatalf("status = %q, want COMPLETED", run.Status)
		}
		if run.DiscrepancyCount != 2 {
			t.Fatalf("discrepancyCount = %d, want 2", run.DiscrepancyCount)
		}

		var foundOneSided, foundBucket0 bool
		for _, d := range repo.discrepancies {
			if d.MismatchCategory == reconciliation.MismatchMissingRecord && d.Evidence["reason"] == "node exists only in canonical ledger" {
				foundOneSided = true
			}
			if d.MismatchCategory == reconciliation.MismatchRecordDifference && d.Evidence["field"] == "amount_paise" {
				foundBucket0 = true
			}
		}
		if !foundOneSided {
			t.Fatal("expected one-sided node discrepancy")
		}
		if !foundBucket0 {
			t.Fatal("expected mutated bucket 0 discrepancy")
		}

		// Deterministic call counter proof: traversal occurred
		if canonCounting.getChildrenCalls == 0 || partCounting.getChildrenCalls == 0 {
			t.Errorf("expected tree traversal calls for canonical-only subtree")
		}
		if canonCounting.getBucketIDCalls == 0 {
			t.Errorf("expected GetBucketID calls for canonical-only subtree")
		}
	})

	t.Run("ParticipantOnlySubtree", func(t *testing.T) {
		scope := reconciliation.Scope{From: base, To: base.Add(4 * time.Hour)}
		canonRecords := makeSampleRecords(base)[:3]
		partRecords := makeSampleRecords(base)
		partRecords[0].AmountPaise = 888888 // mutated Bucket 0

		var canonCounting, partCounting *countingParticipant
		engine, repo := newTestEngine(
			t,
			[]string{pid},
			func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
				canonCounting = newCountingParticipant(makeTestParticipant(t, id, canonRecords))
				return canonCounting, nil
			},
			func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
				partCounting = newCountingParticipant(makeTestParticipant(t, id, partRecords))
				return partCounting, nil
			},
		)

		run, err := engine.Execute(context.Background(), reconciliation.RunRequest{
			ParticipantID: pid,
			ScopeFrom:     scope.From,
			ScopeTo:       scope.To,
		})
		if err != nil {
			t.Fatalf("unexpected execute error: %v", err)
		}

		if run.Status != reconciliation.RunStatusCompleted {
			t.Fatalf("status = %q, want COMPLETED", run.Status)
		}
		if run.DiscrepancyCount != 2 {
			t.Fatalf("discrepancyCount = %d, want 2", run.DiscrepancyCount)
		}

		var foundExtraNode, foundBucket0 bool
		for _, d := range repo.discrepancies {
			if d.MismatchCategory == reconciliation.MismatchExtraRecord && d.Evidence["reason"] == "node exists only in participant ledger" {
				foundExtraNode = true
			}
			if d.MismatchCategory == reconciliation.MismatchRecordDifference && d.Evidence["field"] == "amount_paise" {
				foundBucket0 = true
			}
		}
		if !foundExtraNode {
			t.Fatal("expected extra participant node discrepancy")
		}
		if !foundBucket0 {
			t.Fatal("expected mutated bucket 0 discrepancy")
		}

		// Deterministic call counter proof: traversal occurred
		if canonCounting.getChildrenCalls == 0 || partCounting.getChildrenCalls == 0 {
			t.Errorf("expected tree traversal calls for participant-only subtree")
		}
		if partCounting.getBucketIDCalls == 0 {
			t.Errorf("expected GetBucketID calls for participant-only subtree")
		}
	})
}


// Participant source isolation: cannot cross participant boundaries
func TestParticipantSourceIsolation(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(2 * time.Hour)}

	engine, _ := newTestEngine(
		t,
		[]string{"BANK-A"}, // BANK-B not registered
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, nil), nil
		},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, nil), nil
		},
	)

	_, err := engine.Execute(context.Background(), reconciliation.RunRequest{
		ParticipantID: "BANK-B",
		ScopeFrom:     scope.From,
		ScopeTo:       scope.To,
	})
	if !errors.Is(err, reconciliation.ErrInvalidParticipant) {
		t.Fatalf("expected ErrInvalidParticipant, got: %v", err)
	}
}

// Verifies the engine never exits early after the first mismatch
func TestNoFirstMismatchEarlyExit(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(4 * time.Hour)}
	const pid = "BANK-A"

	canonRecords := makeSampleRecords(base)
	partRecords := makeSampleRecords(base)
	// Mutate all 4 buckets
	partRecords[0].AmountPaise += 1
	partRecords[1].AmountPaise += 2
	partRecords[2].AmountPaise += 3
	partRecords[3].AmountPaise += 4

	engine, repo := newTestEngine(
		t,
		[]string{pid},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, canonRecords), nil
		},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, partRecords), nil
		},
	)

	run, err := engine.Execute(context.Background(), reconciliation.RunRequest{
		ParticipantID: pid,
		ScopeFrom:     scope.From,
		ScopeTo:       scope.To,
	})
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	if run.DiscrepancyCount != 4 {
		t.Fatalf("discrepancyCount = %d, want exactly 4 (one for each mutated bucket)", run.DiscrepancyCount)
	}
	if len(repo.discrepancies) != 4 {
		t.Fatalf("saved discrepancies = %d, want 4", len(repo.discrepancies))
	}
}

// Financial state is never mutated during reconciliation
func TestNoFinancialMutation(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(2 * time.Hour)}
	const pid = "BANK-A"

	records := makeSampleRecords(base)[:2]
	originalAmount0 := records[0].AmountPaise
	originalAmount1 := records[1].AmountPaise

	engine, _ := newTestEngine(
		t,
		[]string{pid},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, records), nil
		},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, records), nil
		},
	)

	_, err := engine.Execute(context.Background(), reconciliation.RunRequest{
		ParticipantID: pid,
		ScopeFrom:     scope.From,
		ScopeTo:       scope.To,
	})
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	// Verify original values were untouched
	if records[0].AmountPaise != originalAmount0 || records[1].AmountPaise != originalAmount1 {
		t.Fatal("reconciliation mutated source record data!")
	}
}

// Operational failure marks run as FAILED
func TestEngineOperationalFailure(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(2 * time.Hour)}
	const pid = "BANK-A"

	engine, repo := newTestEngine(
		t,
		[]string{pid},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return nil, errors.New("database connectivity lost")
		},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, nil), nil
		},
	)

	_, err := engine.Execute(context.Background(), reconciliation.RunRequest{
		ParticipantID: pid,
		ScopeFrom:     scope.From,
		ScopeTo:       scope.To,
	})
	if err == nil {
		t.Fatal("expected execute error, got nil")
	}

	// Run must be marked FAILED
	var failedRun reconciliation.Run
	for _, r := range repo.runs {
		failedRun = r
	}
	if failedRun.Status != reconciliation.RunStatusFailed {
		t.Fatalf("status = %q, want FAILED", failedRun.Status)
	}
	if failedRun.ErrorMessage == "" {
		t.Fatal("expected non-empty ErrorMessage on FAILED run")
	}
}

func TestEngineListRuns(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(2 * time.Hour)}
	const pid = "BANK-A"

	engine, _ := newTestEngine(
		t,
		[]string{pid},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, nil), nil
		},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, nil), nil
		},
	)

	for i := 0; i < 5; i++ {
		_, err := engine.Execute(context.Background(), reconciliation.RunRequest{
			ParticipantID: pid,
			ScopeFrom:     scope.From,
			ScopeTo:       scope.To,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	page, err := engine.ListRuns(context.Background(), reconciliation.ListRunsRequest{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 5 {
		t.Fatalf("total = %d, want 5", page.Total)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(page.Items))
	}
}

func TestEngineGetRunNotFound(t *testing.T) {
	engine, _ := newTestEngine(
		t,
		[]string{"BANK-A"},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, nil), nil
		},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return makeTestParticipant(t, id, nil), nil
		},
	)

	_, err := engine.GetRun(context.Background(), uuid.New())
	if !errors.Is(err, reconciliation.ErrRunNotFound) {
		t.Fatalf("expected ErrRunNotFound, got: %v", err)
	}
}
