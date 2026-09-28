package reconciliation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/transactx/backend/internal/reconciliation"
)

type BenchmarkCase string

const (
	CaseIdentical          BenchmarkCase = "IDENTICAL"
	CaseSingleMutation     BenchmarkCase = "SINGLE_MUTATION"
	CaseMultipleDispersed  BenchmarkCase = "MULTIPLE_DISPERSED"
	CaseMissingParticipant BenchmarkCase = "MISSING_PARTICIPANT"
	CaseExtraParticipant   BenchmarkCase = "EXTRA_PARTICIPANT"
	CaseLargeScaling       BenchmarkCase = "LARGE_SCALING"
)

// BenchmarkDataset defines a deterministic benchmark scenario for M3-6.
type BenchmarkDataset struct {
	Name               string
	CaseType           BenchmarkCase
	Scope              reconciliation.Scope
	CanonicalRecords   []reconciliation.CanonicalRecord
	ParticipantRecords []reconciliation.CanonicalRecord
	ExpectedDiscs      int
}

// OptimizedInstrumentation records execution metrics for the optimized engine.
type OptimizedInstrumentation struct {
	RecordsConsidered  int64         `json:"recordsConsidered"`
	RecordsInspected   int64         `json:"recordsInspected"`
	NodesVisited       int64         `json:"nodesVisited"`
	DiscrepanciesCount int64         `json:"discrepanciesCount"`
	Duration           time.Duration `json:"durationNs"`
	RootEqual          bool          `json:"rootEqual"`
}

// BenchmarkComparisonResult records side-by-side execution evidence for M3 research.
type BenchmarkComparisonResult struct {
	DatasetName            string                              `json:"datasetName"`
	CaseType               string                              `json:"caseType"`
	ScopeFrom              time.Time                           `json:"scopeFrom"`
	ScopeTo                time.Time                           `json:"scopeTo"`
	CanonicalRecordCount   int                                 `json:"canonicalRecordCount"`
	ParticipantRecordCount int                                 `json:"participantRecordCount"`
	DiscrepancyCount       int                                 `json:"discrepancyCount"`
	DiscrepanciesMatch     bool                                `json:"discrepanciesMatch"`
	Naive                  reconciliation.NaiveInstrumentation `json:"naive"`
	Optimized              OptimizedInstrumentation            `json:"optimized"`
}

// benchmarkRunStore implements reconciliation.RunStore without unbounded slice growth.
// It resets discrepancy tracking on each CreateRun call to prevent benchmark state
// accumulation across thousands of iterations while supporting safe object reuse.
type benchmarkRunStore struct {
	mu            sync.Mutex
	currentRun    reconciliation.Run
	discrepancies []reconciliation.Discrepancy
}

func newBenchmarkRunStore() *benchmarkRunStore {
	return &benchmarkRunStore{
		discrepancies: make([]reconciliation.Discrepancy, 0, 16),
	}
}

func (s *benchmarkRunStore) CreateRun(_ context.Context, participantID string, scope reconciliation.Scope) (reconciliation.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.discrepancies = s.discrepancies[:0] // Reuse pre-allocated slice without unbounded growth
	s.currentRun = reconciliation.Run{
		ID:            uuid.New(),
		ParticipantID: participantID,
		ScopeFrom:     scope.From.UTC(),
		ScopeTo:       scope.To.UTC(),
		Status:        reconciliation.RunStatusRunning,
		StartedAt:     time.Now().UTC(),
	}
	return s.currentRun, nil
}

func (s *benchmarkRunStore) CompleteRun(_ context.Context, _ uuid.UUID, canonRoot, partRoot []byte, canonVer, algoVer string, recordCount, discrepancyCount int64) (reconciliation.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentRun.Status = reconciliation.RunStatusCompleted
	s.currentRun.CanonicalRoot = canonRoot
	s.currentRun.ParticipantRoot = partRoot
	s.currentRun.CanonicalVersion = canonVer
	s.currentRun.AlgorithmVersion = algoVer
	s.currentRun.RecordCount = recordCount
	s.currentRun.DiscrepancyCount = discrepancyCount
	now := time.Now().UTC()
	s.currentRun.CompletedAt = &now
	return s.currentRun, nil
}

func (s *benchmarkRunStore) FailRun(_ context.Context, _ uuid.UUID, errMsg string) (reconciliation.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentRun.Status = reconciliation.RunStatusFailed
	s.currentRun.ErrorMessage = errMsg
	now := time.Now().UTC()
	s.currentRun.CompletedAt = &now
	return s.currentRun, nil
}

func (s *benchmarkRunStore) GetRun(_ context.Context, _ uuid.UUID) (reconciliation.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentRun, nil
}

func (s *benchmarkRunStore) ListRuns(_ context.Context, _ reconciliation.ListRunsRequest) (reconciliation.RunListPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return reconciliation.RunListPage{Items: []reconciliation.Run{s.currentRun}, Total: 1}, nil
}

func (s *benchmarkRunStore) SaveDiscrepancy(_ context.Context, disc reconciliation.Discrepancy) (reconciliation.Discrepancy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	disc.ID = uuid.New()
	disc.DetectedAt = time.Now().UTC()
	s.discrepancies = append(s.discrepancies, disc)
	return disc, nil
}

func (s *benchmarkRunStore) ListDiscrepancies(_ context.Context, _ reconciliation.ListDiscrepanciesRequest) (reconciliation.DiscrepancyListPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return reconciliation.DiscrepancyListPage{Items: s.discrepancies, Total: len(s.discrepancies)}, nil
}

// generateDeterministicRecords generates repeatable, deterministic records
// using SHA-1 named UUIDs and deterministic timestamps.
func generateDeterministicRecords(base time.Time, count int, totalDuration time.Duration) []reconciliation.CanonicalRecord {
	records := make([]reconciliation.CanonicalRecord, count)
	for i := 0; i < count; i++ {
		opID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("op-seed42-%d", i)))
		payID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("pay-seed42-%d", i)))
		accID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("acc-seed42-%d", i%10)))

		entryType := "DEBIT"
		if i%2 == 1 {
			entryType = "CREDIT"
		}

		offsetNs := (int64(i) * int64(totalDuration)) / int64(count)
		occurredAt := base.Add(time.Duration(offsetNs) + time.Minute).UTC()

		records[i] = reconciliation.CanonicalRecord{
			OperationID: opID,
			PaymentID:   payID,
			AccountID:   accID,
			EntryType:   entryType,
			AmountPaise: int64((i+1)*1000 + (i%97)*10),
			Currency:    "INR",
			OccurredAt:  occurredAt,
		}
	}
	return records
}

func cloneRecords(records []reconciliation.CanonicalRecord) []reconciliation.CanonicalRecord {
	cloned := make([]reconciliation.CanonicalRecord, len(records))
	copy(cloned, records)
	return cloned
}

// GetBenchmarkDatasets constructs deterministic datasets covering cases A through F.
func GetBenchmarkDatasets(base time.Time) []BenchmarkDataset {
	scope24 := reconciliation.Scope{From: base, To: base.Add(24 * time.Hour)}
	scope48 := reconciliation.Scope{From: base, To: base.Add(48 * time.Hour)}

	// Base 240 records across 24 hours (10 per hour)
	records240 := generateDeterministicRecords(base, 240, 24*time.Hour)

	// A. Identical
	canonA := cloneRecords(records240)
	partA := cloneRecords(records240)

	// B. Single-record mutation (Hour 12, index 125)
	canonB := cloneRecords(records240)
	partB := cloneRecords(records240)
	partB[125].AmountPaise = 9999999

	// C. Multiple dispersed mutations (Hour 1, 9, 20)
	canonC := cloneRecords(records240)
	partC := cloneRecords(records240)
	partC[15].AmountPaise = 8888888 // Hour 1
	partC[95].Currency = "USD"      // Hour 9
	if partC[205].EntryType == "DEBIT" {
		partC[205].EntryType = "CREDIT"
	} else {
		partC[205].EntryType = "DEBIT"
	} // Hour 20

	// D. Missing participant records (Hour 5, indices 50 and 51)
	canonD := cloneRecords(records240)
	partD := make([]reconciliation.CanonicalRecord, 0, len(records240)-2)
	partD = append(partD, records240[:50]...)
	partD = append(partD, records240[52:]...)

	// E. Extra participant records (Hour 23)
	canonE := cloneRecords(records240)
	partE := cloneRecords(records240)
	extra1 := reconciliation.CanonicalRecord{
		OperationID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("op-seed42-extra-1")),
		PaymentID:   uuid.NewSHA1(uuid.NameSpaceOID, []byte("pay-seed42-extra-1")),
		AccountID:   uuid.NewSHA1(uuid.NameSpaceOID, []byte("acc-seed42-extra-1")),
		EntryType:   "DEBIT",
		AmountPaise: 777000,
		Currency:    "INR",
		OccurredAt:  base.Add(23*time.Hour + 10*time.Minute).UTC(),
	}
	extra2 := reconciliation.CanonicalRecord{
		OperationID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("op-seed42-extra-2")),
		PaymentID:   uuid.NewSHA1(uuid.NameSpaceOID, []byte("pay-seed42-extra-2")),
		AccountID:   uuid.NewSHA1(uuid.NameSpaceOID, []byte("acc-seed42-extra-2")),
		EntryType:   "CREDIT",
		AmountPaise: 888000,
		Currency:    "INR",
		OccurredAt:  base.Add(23*time.Hour + 20*time.Minute).UTC(),
	}
	partE = append(partE, extra1, extra2)

	// F. Larger deterministic scaling dataset (1200 records across 48 hours)
	records1200 := generateDeterministicRecords(base, 1200, 48*time.Hour)
	canonF := cloneRecords(records1200)
	partF := cloneRecords(records1200)
	partF[310].AmountPaise = 5555555 // Hour 12
	partF[915].Currency = "EUR"      // Hour 36

	return []BenchmarkDataset{
		{
			Name:               "DatasetA_Identical",
			CaseType:           CaseIdentical,
			Scope:              scope24,
			CanonicalRecords:   canonA,
			ParticipantRecords: partA,
			ExpectedDiscs:      0,
		},
		{
			Name:               "DatasetB_SingleMutation",
			CaseType:           CaseSingleMutation,
			Scope:              scope24,
			CanonicalRecords:   canonB,
			ParticipantRecords: partB,
			ExpectedDiscs:      1,
		},
		{
			Name:               "DatasetC_MultipleDispersed",
			CaseType:           CaseMultipleDispersed,
			Scope:              scope24,
			CanonicalRecords:   canonC,
			ParticipantRecords: partC,
			ExpectedDiscs:      3,
		},
		{
			Name:               "DatasetD_MissingParticipant",
			CaseType:           CaseMissingParticipant,
			Scope:              scope24,
			CanonicalRecords:   canonD,
			ParticipantRecords: partD,
			ExpectedDiscs:      2,
		},
		{
			Name:               "DatasetE_ExtraParticipant",
			CaseType:           CaseExtraParticipant,
			Scope:              scope24,
			CanonicalRecords:   canonE,
			ParticipantRecords: partE,
			ExpectedDiscs:      2,
		},
		{
			Name:               "DatasetF_LargeScaling",
			CaseType:           CaseLargeScaling,
			Scope:              scope48,
			CanonicalRecords:   canonF,
			ParticipantRecords: partF,
			ExpectedDiscs:      2,
		},
	}
}

func makeBenchmarkParticipant(tb testing.TB, participantID string, records []reconciliation.CanonicalRecord) reconciliation.ReconciliationParticipant {
	tb.Helper()
	p, err := reconciliation.NewMemoryParticipant(participantID, participantID, time.Hour, records)
	if err != nil {
		tb.Fatalf("build test participant: %v", err)
	}
	return p
}

func newBenchmarkEngine(
	tb testing.TB,
	participants []string,
	canonicalFactory func(ctx context.Context, id string, scope reconciliation.Scope) (reconciliation.ReconciliationParticipant, error),
	participantFactory func(ctx context.Context, id string, scope reconciliation.Scope) (reconciliation.ReconciliationParticipant, error),
) (*reconciliation.Engine, *memoryRunRepository) {
	tb.Helper()
	repo := newMemoryRunRepository()
	knownParticipants := make(reconciliation.KnownParticipants)
	for _, p := range participants {
		knownParticipants[p] = true
	}
	return reconciliation.NewEngineWithRepo(knownParticipants, repo, canonicalFactory, participantFactory), repo
}

// sortDiscrepancies sorts discrepancies by BucketStart, Category, BucketKey, OperationID, and Field for deterministic checks.
func sortDiscrepancies(discs []reconciliation.Discrepancy) {
	sort.SliceStable(discs, func(i, j int) bool {
		if !discs[i].BucketStart.Equal(discs[j].BucketStart) {
			return discs[i].BucketStart.Before(discs[j].BucketStart)
		}
		if discs[i].MismatchCategory != discs[j].MismatchCategory {
			return discs[i].MismatchCategory < discs[j].MismatchCategory
		}
		if discs[i].BucketKey != discs[j].BucketKey {
			return discs[i].BucketKey < discs[j].BucketKey
		}
		opI := discs[i].Evidence["operation_id"]
		opJ := discs[j].Evidence["operation_id"]
		if opI != opJ {
			return opI < opJ
		}
		return discs[i].Evidence["field"] < discs[j].Evidence["field"]
	})
}

// compareDiscrepanciesDetailed verifies logical equivalence across all 7 non-volatile discrepancy fields:
// MismatchCategory, BucketKey, BucketStart, BucketWidthNs, operation_id, field, and reason.
// Volatile fields (ID, RunID, DetectedAt, duration) are deliberately excluded.
func compareDiscrepanciesDetailed(tb testing.TB, datasetName string, naiveDiscs, optDiscs []reconciliation.Discrepancy) bool {
	tb.Helper()
	if len(naiveDiscs) != len(optDiscs) {
		tb.Errorf("%s: discrepancy count mismatch: naive=%d, opt=%d", datasetName, len(naiveDiscs), len(optDiscs))
		return false
	}

	for i := range naiveDiscs {
		nD, oD := naiveDiscs[i], optDiscs[i]

		// 1. MismatchCategory
		if nD.MismatchCategory != oD.MismatchCategory {
			tb.Errorf("%s[%d]: MismatchCategory mismatch: naive=%q, opt=%q", datasetName, i, nD.MismatchCategory, oD.MismatchCategory)
			return false
		}

		// 2. BucketKey
		if nD.BucketKey != oD.BucketKey {
			tb.Errorf("%s[%d]: BucketKey mismatch: naive=%q, opt=%q", datasetName, i, nD.BucketKey, oD.BucketKey)
			return false
		}

		// 3. BucketStart
		if !nD.BucketStart.Equal(oD.BucketStart) {
			tb.Errorf("%s[%d]: BucketStart mismatch: naive=%v, opt=%v", datasetName, i, nD.BucketStart, oD.BucketStart)
			return false
		}

		// 4. BucketWidthNs
		if nD.BucketWidthNs != oD.BucketWidthNs {
			tb.Errorf("%s[%d]: BucketWidthNs mismatch: naive=%d, opt=%d", datasetName, i, nD.BucketWidthNs, oD.BucketWidthNs)
			return false
		}

		// 5. operation_id where present
		nOp, oOp := nD.Evidence["operation_id"], oD.Evidence["operation_id"]
		if nOp != oOp {
			tb.Errorf("%s[%d]: operation_id mismatch: naive=%q, opt=%q", datasetName, i, nOp, oOp)
			return false
		}

		// 6. field where present
		nField, oField := nD.Evidence["field"], oD.Evidence["field"]
		if nField != oField {
			tb.Errorf("%s[%d]: field mismatch: naive=%q, opt=%q", datasetName, i, nField, oField)
			return false
		}

		// 7. reason where present
		nReason, oReason := nD.Evidence["reason"], oD.Evidence["reason"]
		if nReason != oReason {
			tb.Errorf("%s[%d]: reason mismatch: naive=%q, opt=%q", datasetName, i, nReason, oReason)
			return false
		}
	}
	return true
}

// runBenchmarkComparison executes both naive and optimized algorithms against the same dataset
// and verifies exact logical equivalence.
func runBenchmarkComparison(tb testing.TB, ds BenchmarkDataset) BenchmarkComparisonResult {
	tb.Helper()
	ctx := context.Background()
	const pid = "BANK-A"

	// 1. Run Naive Baseline
	naiveReconciler := reconciliation.NewNaiveReconciler(time.Hour)
	naiveRes, err := naiveReconciler.Reconcile(ctx, pid, ds.Scope, ds.CanonicalRecords, ds.ParticipantRecords)
	if err != nil {
		tb.Fatalf("naive reconcile failed for %s: %v", ds.Name, err)
	}

	// 2. Run Optimized Merkle Engine
	var canonCounting, partCounting *countingParticipant
	engine, repo := newBenchmarkEngine(
		tb,
		[]string{pid},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			canonCounting = newCountingParticipant(makeBenchmarkParticipant(tb, id, ds.CanonicalRecords))
			return canonCounting, nil
		},
		func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			partCounting = newCountingParticipant(makeBenchmarkParticipant(tb, id, ds.ParticipantRecords))
			return partCounting, nil
		},
	)

	optStart := time.Now()
	optRun, err := engine.Execute(ctx, reconciliation.RunRequest{
		ParticipantID: pid,
		ScopeFrom:     ds.Scope.From,
		ScopeTo:       ds.Scope.To,
	})
	optDuration := time.Since(optStart)
	if err != nil {
		tb.Fatalf("optimized reconcile failed for %s: %v", ds.Name, err)
	}

	optDiscs := make([]reconciliation.Discrepancy, len(repo.discrepancies))
	copy(optDiscs, repo.discrepancies)

	// 3. Detailed Equivalence Verification
	naiveDiscs := make([]reconciliation.Discrepancy, len(naiveRes.Discrepancies))
	copy(naiveDiscs, naiveRes.Discrepancies)

	sortDiscrepancies(naiveDiscs)
	sortDiscrepancies(optDiscs)

	discrepanciesMatch := compareDiscrepanciesDetailed(tb, ds.Name, naiveDiscs, optDiscs)

	// 4. Capture Optimized Instrumentation
	nodesVisited := int64(0)
	recordsInspected := int64(0)
	if canonCounting != nil && partCounting != nil {
		nodesVisited = int64(canonCounting.getChildrenCalls + partCounting.getChildrenCalls)
		recordsInspected = int64(canonCounting.getRecordsCalls + partCounting.getRecordsCalls)
	}
	rootEqual := bytes.Equal(optRun.CanonicalRoot, optRun.ParticipantRoot)

	optInst := OptimizedInstrumentation{
		RecordsConsidered:  int64(len(ds.CanonicalRecords) + len(ds.ParticipantRecords)),
		RecordsInspected:   recordsInspected,
		NodesVisited:       nodesVisited,
		DiscrepanciesCount: optRun.DiscrepancyCount,
		Duration:           optDuration,
		RootEqual:          rootEqual,
	}

	return BenchmarkComparisonResult{
		DatasetName:            ds.Name,
		CaseType:               string(ds.CaseType),
		ScopeFrom:              ds.Scope.From,
		ScopeTo:                ds.Scope.To,
		CanonicalRecordCount:   len(ds.CanonicalRecords),
		ParticipantRecordCount: len(ds.ParticipantRecords),
		DiscrepancyCount:       len(naiveDiscs),
		DiscrepanciesMatch:     discrepanciesMatch,
		Naive:                  naiveRes.Instrumentation,
		Optimized:              optInst,
	}
}

// --- Focused Tests ---

func TestNaiveIdenticalCase(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(4 * time.Hour)}
	records := generateDeterministicRecords(base, 40, 4*time.Hour)

	naive := reconciliation.NewNaiveReconciler(time.Hour)
	res, err := naive.Reconcile(context.Background(), "BANK-A", scope, records, records)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.Discrepancies) != 0 {
		t.Fatalf("discrepancies = %d, want 0", len(res.Discrepancies))
	}
	if res.Instrumentation.RecordsConsidered != 80 {
		t.Fatalf("records considered = %d, want 80", res.Instrumentation.RecordsConsidered)
	}
	if res.Instrumentation.RecordsCompared != 80 {
		t.Fatalf("records compared = %d, want 80", res.Instrumentation.RecordsCompared)
	}
	if res.Instrumentation.DiscrepanciesCount != 0 {
		t.Fatalf("discrepancy count = %d, want 0", res.Instrumentation.DiscrepanciesCount)
	}
	if res.Instrumentation.Duration < 0 {
		t.Fatalf("invalid duration: %v", res.Instrumentation.Duration)
	}
}

func TestNaiveSingleMismatch(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(4 * time.Hour)}
	canon := generateDeterministicRecords(base, 40, 4*time.Hour)
	part := cloneRecords(canon)
	part[15].AmountPaise = 1234567

	naive := reconciliation.NewNaiveReconciler(time.Hour)
	res, err := naive.Reconcile(context.Background(), "BANK-A", scope, canon, part)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.Discrepancies) != 1 {
		t.Fatalf("discrepancies = %d, want 1", len(res.Discrepancies))
	}
	d := res.Discrepancies[0]
	if d.MismatchCategory != reconciliation.MismatchRecordDifference {
		t.Fatalf("category = %q, want %q", d.MismatchCategory, reconciliation.MismatchRecordDifference)
	}
	if d.Evidence["field"] != "amount_paise" {
		t.Fatalf("field = %q, want amount_paise", d.Evidence["field"])
	}
	if d.Evidence["operation_id"] != canon[15].OperationID.String() {
		t.Fatalf("operation_id = %q, want %q", d.Evidence["operation_id"], canon[15].OperationID.String())
	}
}

func TestNaiveMultipleMismatch(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(4 * time.Hour)}
	canon := generateDeterministicRecords(base, 40, 4*time.Hour)
	part := cloneRecords(canon)
	part[5].AmountPaise = 111111
	part[15].Currency = "USD"
	if part[25].EntryType == "DEBIT" {
		part[25].EntryType = "CREDIT"
	} else {
		part[25].EntryType = "DEBIT"
	}

	naive := reconciliation.NewNaiveReconciler(time.Hour)
	res, err := naive.Reconcile(context.Background(), "BANK-A", scope, canon, part)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res.Discrepancies) != 3 {
		t.Fatalf("discrepancies = %d, want 3", len(res.Discrepancies))
	}
	for _, d := range res.Discrepancies {
		if d.MismatchCategory != reconciliation.MismatchRecordDifference {
			t.Errorf("category = %q, want RECORD_MISMATCH", d.MismatchCategory)
		}
	}
}

func TestNaiveMissingExtraRecords(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	scope := reconciliation.Scope{From: base, To: base.Add(2 * time.Hour)}
	canon := generateDeterministicRecords(base, 20, 2*time.Hour)

	// Test missing
	partMissing := cloneRecords(canon[:18]) // last 2 missing
	naive := reconciliation.NewNaiveReconciler(time.Hour)
	resMissing, err := naive.Reconcile(context.Background(), "BANK-A", scope, canon, partMissing)
	if err != nil {
		t.Fatal(err)
	}
	if len(resMissing.Discrepancies) != 2 {
		t.Fatalf("missing discrepancies = %d, want 2", len(resMissing.Discrepancies))
	}
	for _, d := range resMissing.Discrepancies {
		if d.MismatchCategory != reconciliation.MismatchMissingRecord {
			t.Errorf("category = %q, want MISSING_PARTICIPANT_RECORD", d.MismatchCategory)
		}
		if d.Evidence["reason"] != "missing participant record" {
			t.Errorf("evidence reason = %q, want missing participant record", d.Evidence["reason"])
		}
	}

	// Test extra
	partExtra := cloneRecords(canon)
	extra := reconciliation.CanonicalRecord{
		OperationID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("extra-op")),
		PaymentID:   uuid.NewSHA1(uuid.NameSpaceOID, []byte("extra-pay")),
		AccountID:   uuid.NewSHA1(uuid.NameSpaceOID, []byte("extra-acc")),
		EntryType:   "DEBIT",
		AmountPaise: 999,
		Currency:    "INR",
		OccurredAt:  base.Add(30 * time.Minute),
	}
	partExtra = append(partExtra, extra)

	resExtra, err := naive.Reconcile(context.Background(), "BANK-A", scope, canon, partExtra)
	if err != nil {
		t.Fatal(err)
	}
	if len(resExtra.Discrepancies) != 1 {
		t.Fatalf("extra discrepancies = %d, want 1", len(resExtra.Discrepancies))
	}
	if resExtra.Discrepancies[0].MismatchCategory != reconciliation.MismatchExtraRecord {
		t.Errorf("category = %q, want EXTRA_PARTICIPANT_RECORD", resExtra.Discrepancies[0].MismatchCategory)
	}
	if resExtra.Discrepancies[0].Evidence["reason"] != "extra participant record" {
		t.Errorf("evidence reason = %q, want extra participant record", resExtra.Discrepancies[0].Evidence["reason"])
	}
}

func TestDeterministicBenchmarkFixtureGeneration(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	ds1 := GetBenchmarkDatasets(base)
	ds2 := GetBenchmarkDatasets(base)

	if len(ds1) != len(ds2) {
		t.Fatalf("dataset count mismatch: %d vs %d", len(ds1), len(ds2))
	}

	for i := range ds1 {
		d1, d2 := ds1[i], ds2[i]
		if d1.Name != d2.Name {
			t.Errorf("name mismatch at %d: %q vs %q", i, d1.Name, d2.Name)
		}
		if len(d1.CanonicalRecords) != len(d2.CanonicalRecords) {
			t.Errorf("canonical record count mismatch in %s: %d vs %d", d1.Name, len(d1.CanonicalRecords), len(d2.CanonicalRecords))
		}
		for j := range d1.CanonicalRecords {
			if d1.CanonicalRecords[j].OperationID != d2.CanonicalRecords[j].OperationID ||
				d1.CanonicalRecords[j].AmountPaise != d2.CanonicalRecords[j].AmountPaise ||
				!d1.CanonicalRecords[j].OccurredAt.Equal(d2.CanonicalRecords[j].OccurredAt) {
				t.Fatalf("non-deterministic record in %s at index %d", d1.Name, j)
			}
		}
	}
}

// TestBenchmarkInstrumentationPopulated strengthens validation across EVERY dataset (A through F):
// Validates:
// NAIVE:
//   - RecordsConsidered > 0
//   - RecordsCompared > 0
//   - DiscrepanciesCount == expected discrepancy count
//   - Duration >= 0
// OPTIMIZED:
//   - RecordsConsidered > 0
//   - RecordsInspected >= 0
//   - NodesVisited >= 0
//   - DiscrepanciesCount == expected discrepancy count
//   - Duration >= 0
//   - RootEqual is true for identical and false for divergent datasets
//   - NodesVisited / RecordsInspected are 0 when RootEqual == true (Merkle pruning) and > 0 when divergent
func TestBenchmarkInstrumentationPopulated(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	datasets := GetBenchmarkDatasets(base)

	for _, ds := range datasets {
		t.Run(ds.Name, func(t *testing.T) {
			result := runBenchmarkComparison(t, ds)

			// NAIVE assertions:
			if result.Naive.RecordsConsidered <= 0 {
				t.Errorf("%s: Naive.RecordsConsidered = %d, want > 0", ds.Name, result.Naive.RecordsConsidered)
			}
			if result.Naive.RecordsCompared <= 0 {
				t.Errorf("%s: Naive.RecordsCompared = %d, want > 0", ds.Name, result.Naive.RecordsCompared)
			}
			if result.Naive.DiscrepanciesCount != int64(ds.ExpectedDiscs) {
				t.Errorf("%s: Naive.DiscrepanciesCount = %d, want %d", ds.Name, result.Naive.DiscrepanciesCount, ds.ExpectedDiscs)
			}
			if result.Naive.Duration < 0 {
				t.Errorf("%s: Naive.Duration = %v, want >= 0", ds.Name, result.Naive.Duration)
			}

			// OPTIMIZED assertions:
			if result.Optimized.RecordsConsidered <= 0 {
				t.Errorf("%s: Optimized.RecordsConsidered = %d, want > 0", ds.Name, result.Optimized.RecordsConsidered)
			}
			if result.Optimized.RecordsInspected < 0 {
				t.Errorf("%s: Optimized.RecordsInspected = %d, want >= 0", ds.Name, result.Optimized.RecordsInspected)
			}
			if result.Optimized.NodesVisited < 0 {
				t.Errorf("%s: Optimized.NodesVisited = %d, want >= 0", ds.Name, result.Optimized.NodesVisited)
			}
			if result.Optimized.DiscrepanciesCount != int64(ds.ExpectedDiscs) {
				t.Errorf("%s: Optimized.DiscrepanciesCount = %d, want %d", ds.Name, result.Optimized.DiscrepanciesCount, ds.ExpectedDiscs)
			}
			if result.Optimized.Duration < 0 {
				t.Errorf("%s: Optimized.Duration = %v, want >= 0", ds.Name, result.Optimized.Duration)
			}

			// RootEqual verification:
			if ds.CaseType == CaseIdentical {
				if !result.Optimized.RootEqual {
					t.Errorf("%s: expected RootEqual = true for identical dataset, got false", ds.Name)
				}
				// Merkle pruning legitimately avoids traversal when roots match
				if result.Optimized.NodesVisited != 0 {
					t.Errorf("%s: expected NodesVisited = 0 for identical root early exit, got %d", ds.Name, result.Optimized.NodesVisited)
				}
				if result.Optimized.RecordsInspected != 0 {
					t.Errorf("%s: expected RecordsInspected = 0 for identical root early exit, got %d", ds.Name, result.Optimized.RecordsInspected)
				}
			} else {
				if result.Optimized.RootEqual {
					t.Errorf("%s: expected RootEqual = false for divergent dataset, got true", ds.Name)
				}
				// For divergent cases, traversal and leaf inspection MUST have occurred
				if result.Optimized.NodesVisited <= 0 {
					t.Errorf("%s: expected NodesVisited > 0 for divergent dataset, got %d", ds.Name, result.Optimized.NodesVisited)
				}
				if result.Optimized.RecordsInspected <= 0 {
					t.Errorf("%s: expected RecordsInspected > 0 for divergent dataset, got %d", ds.Name, result.Optimized.RecordsInspected)
				}
			}
		})
	}
}

// TestNaiveVsOptimizedEquivalence asserts exact logical equivalence between
// naive and optimized algorithms across all benchmark cases A through F.
// Compares: MismatchCategory, BucketKey, BucketStart, BucketWidthNs, operation_id, field, reason.
func TestNaiveVsOptimizedEquivalence(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	datasets := GetBenchmarkDatasets(base)

	for _, ds := range datasets {
		t.Run(ds.Name, func(t *testing.T) {
			// Record snapshots of input data before execution to verify no financial mutation
			origCanonLen := len(ds.CanonicalRecords)
			origPartLen := len(ds.ParticipantRecords)

			result := runBenchmarkComparison(t, ds)

			if !result.DiscrepanciesMatch {
				t.Fatalf("%s: naive and optimized discrepancies do not match across all 7 fields (MismatchCategory, BucketKey, BucketStart, BucketWidthNs, operation_id, field, reason)", ds.Name)
			}
			if result.DiscrepancyCount != ds.ExpectedDiscs {
				t.Fatalf("%s: discrepancyCount = %d, want %d", ds.Name, result.DiscrepancyCount, ds.ExpectedDiscs)
			}

			// Verify no financial mutation
			if len(ds.CanonicalRecords) != origCanonLen || len(ds.ParticipantRecords) != origPartLen {
				t.Fatalf("%s: input records mutated during reconciliation", ds.Name)
			}

			// Verify same normalized scope
			if !result.ScopeFrom.Equal(ds.Scope.From.UTC()) || !result.ScopeTo.Equal(ds.Scope.To.UTC()) {
				t.Fatalf("%s: normalized scope mismatch: [%v, %v) vs [%v, %v)", ds.Name, result.ScopeFrom, result.ScopeTo, ds.Scope.From, ds.Scope.To)
			}
		})
	}
}

// TestBenchmarkResearchEvidenceReport executes all benchmark cases and produces
// a structured research evidence report for M3-6.
func TestBenchmarkResearchEvidenceReport(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	datasets := GetBenchmarkDatasets(base)

	var results []BenchmarkComparisonResult
	for _, ds := range datasets {
		res := runBenchmarkComparison(t, ds)
		results = append(results, res)
	}

	reportJSON, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}

	t.Logf("\n=== M3-6 RECONCILIATION BENCHMARK RESEARCH EVIDENCE ===\n\n%s\n", string(reportJSON))

	// Also print a clean Markdown summary table
	t.Logf("\n| Dataset | Canon Recs | Part Recs | Discrepancies | Naive Considered | Naive Compared | Naive Time | Opt Inspected | Opt Visited | Opt Time |\n" +
		"|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range results {
		t.Logf("| %-25s | %5d | %5d | %2d | %5d | %5d | %10v | %5d | %3d | %10v |\n",
			r.DatasetName,
			r.CanonicalRecordCount,
			r.ParticipantRecordCount,
			r.DiscrepancyCount,
			r.Naive.RecordsConsidered,
			r.Naive.RecordsCompared,
			r.Naive.Duration,
			r.Optimized.RecordsInspected,
			r.Optimized.NodesVisited,
			r.Optimized.Duration,
		)
	}
}

// --- Go Benchmarks ---

// BenchmarkReconciliation measures cold-start reconciliation where participant instances are constructed per iteration.
func BenchmarkReconciliation(b *testing.B) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	datasets := GetBenchmarkDatasets(base)
	ctx := context.Background()

	for _, ds := range datasets {
		ds := ds

		// Naive Path
		reconciler := reconciliation.NewNaiveReconciler(time.Hour)
		b.Run(ds.Name+"/Naive", func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				res, err := reconciler.Reconcile(ctx, "BANK-A", ds.Scope, ds.CanonicalRecords, ds.ParticipantRecords)
				if err != nil {
					b.Fatal(err)
				}
				_ = res
			}
		})

		// Optimized Path (Engine and bounded store pre-constructed outside timed loop; participant factory constructs instances)
		store := newBenchmarkRunStore()
		knownParticipants := reconciliation.KnownParticipants{"BANK-A": true}
		engine := reconciliation.NewEngineWithRepo(
			knownParticipants,
			store,
			func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
				return makeBenchmarkParticipant(b, id, ds.CanonicalRecords), nil
			},
			func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
				return makeBenchmarkParticipant(b, id, ds.ParticipantRecords), nil
			},
		)
		req := reconciliation.RunRequest{
			ParticipantID: "BANK-A",
			ScopeFrom:     ds.Scope.From,
			ScopeTo:       ds.Scope.To,
		}

		b.Run(ds.Name+"/Optimized", func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				res, err := engine.Execute(ctx, req)
				if err != nil {
					b.Fatal(err)
				}
				_ = res
			}
		})
	}
}

// BenchmarkReconciliationPrebuilt measures the pure reconciliation query traversal phase
// when participant commitment state is already maintained (as in the live production architecture).
//
// TIMED REGION DOCUMENTATION:
//
// 1. NAIVE TIMED REGION:
//    reconciler.Reconcile(ctx, "BANK-A", ds.Scope, ds.CanonicalRecords, ds.ParticipantRecords)
//    - Scope validation and UTC normalization.
//    - Filtering canonical and participant records by normalized [From, To).
//    - Deterministic SortRecords on both datasets.
//    - Full O(N) map indexing and record-by-record comparison across all records.
//    - Discrepancy slice construction and execution timing.
//
// 2. OPTIMIZED TIMED REGION:
//    engine.Execute(ctx, req)
//    - Scope validation and UTC normalization.
//    - store.CreateRun (persisting RUNNING status in the bounded benchmarkRunStore).
//    - Participant GetRoot calls (retrieving pre-maintained commitments).
//    - Version validation and participant GetMetadata calls.
//    - Root hash equality check: if equal, stops immediately in O(1) and calls store.CompleteRun.
//    - If divergent: BFS queue traversal, logical region matching, subtree hash pruning,
//      GetRecords for divergent leaf buckets, compareBucketRecords, store.SaveDiscrepancy,
//      and store.CompleteRun.
//
// HARNESS FAIRNESS & SETUP CONTROLS:
//   - Pre-initialization: pCanon and pPart compute and cache their Merkle commitment states
//     before b.ResetTimer().
//   - Engine Reuse: The Engine and participant factories are constructed ONCE outside the timed loop.
//   - State Accumulation Prevention: benchmarkRunStore resets its discrepancy slice in O(1)
//     on each CreateRun call, avoiding unbounded slice growth across thousands of benchmark iterations.
//   - RunStore Persistence Limitation: store.CreateRun / CompleteRun calls remain inside the
//     optimized timed path as required by the production engine.Execute contract.
func BenchmarkReconciliationPrebuilt(b *testing.B) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	datasets := GetBenchmarkDatasets(base)
	ctx := context.Background()

	for _, ds := range datasets {
		ds := ds

		// 1. Pre-initialize participant state so the benchmark isolates reconciliation query execution
		pCanon := makeBenchmarkParticipant(b, "BANK-A", ds.CanonicalRecords)
		if _, err := pCanon.GetRoot(ctx, ds.Scope); err != nil {
			b.Fatal(err)
		}
		pPart := makeBenchmarkParticipant(b, "BANK-A", ds.ParticipantRecords)
		if _, err := pPart.GetRoot(ctx, ds.Scope); err != nil {
			b.Fatal(err)
		}

		// 2. Pre-create Naive Reconciler outside the timed loop
		reconciler := reconciliation.NewNaiveReconciler(time.Hour)

		b.Run(ds.Name+"/Naive", func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				res, err := reconciler.Reconcile(ctx, "BANK-A", ds.Scope, ds.CanonicalRecords, ds.ParticipantRecords)
				if err != nil {
					b.Fatal(err)
				}
				_ = res
			}
		})

		// 3. Pre-create Optimized Engine and bounded RunStore outside the timed loop
		store := newBenchmarkRunStore()
		knownParticipants := reconciliation.KnownParticipants{"BANK-A": true}
		engine := reconciliation.NewEngineWithRepo(
			knownParticipants,
			store,
			func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
				return pCanon, nil
			},
			func(ctx context.Context, id string, s reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
				return pPart, nil
			},
		)
		req := reconciliation.RunRequest{
			ParticipantID: "BANK-A",
			ScopeFrom:     ds.Scope.From,
			ScopeTo:       ds.Scope.To,
		}

		b.Run(ds.Name+"/OptimizedPrebuilt", func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				res, err := engine.Execute(ctx, req)
				if err != nil {
					b.Fatal(err)
				}
				_ = res
			}
		})
	}
}
