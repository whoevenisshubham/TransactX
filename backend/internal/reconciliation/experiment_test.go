package reconciliation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/transactx/backend/internal/reconciliation"
)

// TestReconciliationScaleEvidence is opt-in because it writes raw research data.
// Set M3_EVIDENCE_PATH to a new JSON file before running it.
func TestReconciliationScaleEvidence(t *testing.T) {
	path := os.Getenv("M3_EVIDENCE_PATH")
	if path == "" {
		t.Skip("set M3_EVIDENCE_PATH to record scale evidence")
	}
	const seed = 42
	const repetitions = 3
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	ctx := context.Background()
	type measurement struct {
		NaiveNs                int64 `json:"naiveNs"`
		MerkleNs               int64 `json:"merkleNs"`
		NaiveRecordsCompared   int64 `json:"naiveRecordsCompared"`
		MerkleGetChildrenCalls int   `json:"merkleGetChildrenCalls"`
		MerkleGetRecordsCalls  int   `json:"merkleGetRecordsCalls"`
		Discrepancies          int   `json:"discrepancies"`
		RootsEqual             bool  `json:"rootsEqual"`
	}
	type result struct {
		Records      int           `json:"records"`
		Scenario     string        `json:"scenario"`
		Measurements []measurement `json:"measurements"`
	}
	report := struct {
		GitCommit    string    `json:"gitCommit"`
		TimestampUTC time.Time `json:"timestampUtc"`
		OS           string    `json:"os"`
		CPU          string    `json:"cpu"`
		LogicalCPUs  int       `json:"logicalCpus"`
		GoVersion    string    `json:"goVersion"`
		Seed         int       `json:"seed"`
		Repetitions  int       `json:"repetitions"`
		Scope        string    `json:"scope"`
		Method       string    `json:"method"`
		Results      []result  `json:"results"`
	}{GitCommit: os.Getenv("M3_GIT_COMMIT"), TimestampUTC: time.Now().UTC(), OS: runtime.GOOS,
		CPU: os.Getenv("PROCESSOR_IDENTIFIER"), LogicalCPUs: runtime.NumCPU(), GoVersion: runtime.Version(),
		Seed: seed, Repetitions: repetitions, Scope: "[2026-09-01T00:00:00Z,2026-09-02T00:00:00Z)",
		Method: "prebuilt commitments; participant construction and warmup excluded; naive full sort/diff versus Merkle engine run including in-memory run store"}
	if report.GitCommit == "" {
		t.Fatal("M3_GIT_COMMIT is required")
	}
	scope := reconciliation.Scope{From: base, To: base.Add(24 * time.Hour)}
	for _, count := range []int{10000, 100000} {
		canonical := generateDeterministicRecords(base, count, 24*time.Hour)
		for _, scenario := range []string{"identical", "single", "clustered", "dispersed"} {
			participant := cloneRecords(canonical)
			switch scenario {
			case "single":
				participant[count/2].AmountPaise++
			case "clustered":
				for i := count / 2; i < count/2+10; i++ {
					participant[i].AmountPaise++
				}
			case "dispersed":
				for i := 1; i <= 10; i++ {
					participant[i*count/11].AmountPaise++
				}
			}
			canonP := makeBenchmarkParticipant(t, "BANK-A", canonical)
			partP := makeBenchmarkParticipant(t, "BANK-A", participant)
			canonRoot, err := canonP.GetRoot(ctx, scope)
			if err != nil {
				t.Fatal(err)
			}
			partRoot, err := partP.GetRoot(ctx, scope)
			if err != nil {
				t.Fatal(err)
			}
			naive := reconciliation.NewNaiveReconciler(time.Hour)
			r := result{Records: count, Scenario: scenario}
			for rep := -1; rep < repetitions; rep++ {
				store := newBenchmarkRunStore()
				canonCount := newCountingParticipant(canonP)
				partCount := newCountingParticipant(partP)
				engine := reconciliation.NewEngineWithRepo(reconciliation.KnownParticipants{"BANK-A": true}, store,
					func(context.Context, string, reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
						return canonCount, nil
					},
					func(context.Context, string, reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
						return partCount, nil
					})
				start := time.Now()
				naiveResult, err := naive.Reconcile(ctx, "BANK-A", scope, canonical, participant)
				naiveNs := time.Since(start).Nanoseconds()
				if err != nil {
					t.Fatal(err)
				}
				start = time.Now()
				merkleRun, err := engine.Execute(ctx, reconciliation.RunRequest{ParticipantID: "BANK-A", ScopeFrom: scope.From, ScopeTo: scope.To})
				merkleNs := time.Since(start).Nanoseconds()
				if err != nil {
					t.Fatal(err)
				}
				if int64(len(naiveResult.Discrepancies)) != merkleRun.DiscrepancyCount {
					t.Fatalf("%d %s: mismatch counts", count, scenario)
				}
				naiveDiscs := append([]reconciliation.Discrepancy(nil), naiveResult.Discrepancies...)
				merkleDiscs := append([]reconciliation.Discrepancy(nil), store.discrepancies...)
				sortDiscrepancies(naiveDiscs)
				sortDiscrepancies(merkleDiscs)
				if !compareDiscrepanciesDetailed(t, fmt.Sprintf("%d/%s", count, scenario), naiveDiscs, merkleDiscs) {
					t.Fatal("discrepancy evidence differs")
				}
				if rep >= 0 {
					r.Measurements = append(r.Measurements, measurement{NaiveNs: naiveNs, MerkleNs: merkleNs,
						NaiveRecordsCompared:   naiveResult.Instrumentation.RecordsCompared,
						MerkleGetChildrenCalls: canonCount.getChildrenCalls + partCount.getChildrenCalls,
						MerkleGetRecordsCalls:  canonCount.getRecordsCalls + partCount.getRecordsCalls,
						Discrepancies:          len(naiveDiscs), RootsEqual: bytes.Equal(canonRoot.Root, partRoot.Root)})
				}
			}
			report.Results = append(report.Results, r)
		}
	}
	sort.Slice(report.Results, func(i, j int) bool {
		if report.Results[i].Records != report.Results[j].Records {
			return report.Results[i].Records < report.Results[j].Records
		}
		return report.Results[i].Scenario < report.Results[j].Scenario
	})
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
}
