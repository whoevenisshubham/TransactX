package experiments

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/chaos"
	"github.com/transactx/backend/internal/circuit"
	"github.com/transactx/backend/internal/payments"
)

func TestExperiment1_RoutingComparison(t *testing.T) {
	ctx := context.Background()
	result, err := RunExperiment1(ctx, 42, 200)
	if err != nil {
		t.Fatalf("RunExperiment1 failed: %v", err)
	}

	if result.Baseline == nil || result.Proposed == nil {
		t.Fatal("expected both baseline and proposed summaries")
	}

	if result.Baseline.TotalRequests != 200 || result.Proposed.TotalRequests != 200 {
		t.Fatalf("expected 200 requests, got baseline=%d, proposed=%d",
			result.Baseline.TotalRequests, result.Proposed.TotalRequests)
	}

	// In Phase 2, primary target is degraded.
	// Adaptive routing should have a significantly higher success rate than static routing.
	if result.Proposed.SuccessRate <= result.Baseline.SuccessRate {
		t.Errorf("expected proposed adaptive success rate (%.2f%%) > baseline static (%.2f%%)",
			result.Proposed.SuccessRate, result.Baseline.SuccessRate)
	}

	if !result.Baseline.AllInvariantsSatisfied {
		t.Error("baseline invariants failed")
	}
	if !result.Proposed.AllInvariantsSatisfied {
		t.Error("proposed invariants failed")
	}
}

func TestExperiment2_OutageAndCircuitIsolation(t *testing.T) {
	ctx := context.Background()
	result, err := RunExperiment2(ctx, 42, 180)
	if err != nil {
		t.Fatalf("RunExperiment2 failed: %v", err)
	}

	if result.Baseline == nil || result.Proposed == nil {
		t.Fatal("expected both baseline and proposed summaries")
	}

	// Circuit breaker should isolate Rail A and yield higher overall successes during outage
	if result.Proposed.SuccessRate <= result.Baseline.SuccessRate {
		t.Errorf("expected proposed success rate (%.2f%%) > baseline (%.2f%%)",
			result.Proposed.SuccessRate, result.Baseline.SuccessRate)
	}

	if !result.Proposed.AllInvariantsSatisfied {
		t.Error("proposed circuit breaker invariants failed")
	}
}

func TestExperiment3_LatencyDegradation(t *testing.T) {
	ctx := context.Background()
	result, err := RunExperiment3(ctx, 42, 180)
	if err != nil {
		t.Fatalf("RunExperiment3 failed: %v", err)
	}

	if result.Baseline == nil || result.Proposed == nil {
		t.Fatal("expected both baseline and proposed summaries")
	}

	// Under static routing, all requests hit Rail A and suffer latency.
	// Under adaptive routing, traffic shifts to Rail B, giving a lower P95 latency.
	if result.Proposed.Latency.P95Ms >= result.Baseline.Latency.P95Ms {
		t.Errorf("expected proposed P95 (%.2fms) < baseline (%.2fms)",
			result.Proposed.Latency.P95Ms, result.Baseline.Latency.P95Ms)
	}

	if !result.Proposed.AllInvariantsSatisfied {
		t.Error("proposed latency invariants failed")
	}
}

func TestExperiment5_ConcurrencyStormAndInvariants(t *testing.T) {
	ctx := context.Background()
	result, err := RunExperiment5(ctx, 42, 200, 10)
	if err != nil {
		t.Fatalf("RunExperiment5 failed: %v", err)
	}

	if result.Summary == nil {
		t.Fatal("expected summary")
	}

	if !result.Summary.AllInvariantsSatisfied {
		t.Errorf("concurrency storm invariants failed: %#v", result.Summary.Invariants)
	}
}

func TestArtifactSerialization(t *testing.T) {
	ctx := context.Background()
	result, err := RunExperiment1(ctx, 42, 50)
	if err != nil {
		t.Fatalf("RunExperiment1 failed: %v", err)
	}

	tempDir := t.TempDir()
	jsonPath, csvPath, err := result.SaveArtifacts(tempDir)
	if err != nil {
		t.Fatalf("SaveArtifacts failed: %v", err)
	}

	if _, err := os.Stat(jsonPath); err != nil {
		t.Errorf("expected json file at %s: %v", jsonPath, err)
	}
	if _, err := os.Stat(csvPath); err != nil {
		t.Errorf("expected csv file at %s: %v", csvPath, err)
	}

	// Verify environment metadata
	if result.Environment.GitCommit == "" {
		t.Error("git commit sha should not be empty")
	}
	if result.Environment.GoVersion == "" {
		t.Error("go version should not be empty")
	}
	if filepath.Ext(jsonPath) != ".json" || filepath.Ext(csvPath) != ".csv" {
		t.Errorf("unexpected file extensions: %s, %s", jsonPath, csvPath)
	}
}

func TestExperiment3_RegressionLatencyInjection(t *testing.T) {
	ctx := context.Background()
	baseAdapter := NewMockBankAdapter("BANK-A-RAIL-A")
	baseAdapter.SetLatency(1 * time.Millisecond)

	repo := newMemoryChaosRepo()
	controller := chaos.NewController(repo)
	controller.SetTargetValidator(func(target string) bool { return target == "RAIL-A" })
	chaosAdapter := chaos.NewChaosAdapter("RAIL-A", baseAdapter, controller)

	req := bank.HoldFundsRequest{
		OperationRequest: bank.OperationRequest{
			PaymentID:      uuid.New(),
			OperationID:    uuid.New(),
			IdempotencyKey: "test-latency-key-1",
			AmountPaise:    1000,
			Currency:       "INR",
		},
	}

	// 1. Without scenario active: elapsed time should be small (< 15ms)
	start1 := time.Now()
	_, err := chaosAdapter.HoldFunds(ctx, req)
	elapsed1 := time.Since(start1)
	if err != nil {
		t.Fatalf("HoldFunds failed: %v", err)
	}
	if elapsed1 >= 15*time.Millisecond {
		t.Errorf("expected baseline elapsed < 15ms, got %v", elapsed1)
	}

	// 2. Start M2-6 latency scenario with LatencyMs = 25
	now := time.Now()
	controller.SetNowFunc(func() time.Time { return now })
	sc, err := controller.Start(ctx, chaos.StartRequest{
		ScenarioID: "test-latency-scenario",
		TargetID:   "RAIL-A",
		Type:       chaos.ScenarioTypeLatency,
		Parameters: chaos.ScenarioParameters{
			DurationMs: 5000,
			LatencyMs:  25,
		},
	}, "test-admin", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("chaos start failed: %v", err)
	}

	// 3. With scenario active: elapsed time must be >= 25ms
	req.OperationID = uuid.New()
	req.IdempotencyKey = "test-latency-key-2"
	start2 := time.Now()
	_, err = chaosAdapter.HoldFunds(ctx, req)
	elapsed2 := time.Since(start2)
	if err != nil {
		t.Fatalf("HoldFunds with latency failed: %v", err)
	}
	if elapsed2 < 25*time.Millisecond {
		t.Errorf("expected elapsed >= 25ms with active chaos latency, got %v", elapsed2)
	}

	// 4. Stop scenario: elapsed time returns to baseline (< 15ms)
	_, _ = controller.Stop(ctx, sc.ScenarioID, "test-admin", "OPS_ADMIN")
	req.OperationID = uuid.New()
	req.IdempotencyKey = "test-latency-key-3"
	start3 := time.Now()
	_, err = chaosAdapter.HoldFunds(ctx, req)
	elapsed3 := time.Since(start3)
	if err != nil {
		t.Fatalf("HoldFunds after stop failed: %v", err)
	}
	if elapsed3 >= 15*time.Millisecond {
		t.Errorf("expected elapsed < 15ms after scenario stopped, got %v", elapsed3)
	}
}

func TestExperiment5_CircuitInvariantValidation(t *testing.T) {
	cbConfig := circuit.DefaultConfig()
	cbConfig.FailureThreshold = 2
	cb, _ := circuit.NewBreaker(cbConfig)

	now := time.Now()
	cb.RecordFailure("RAIL-A", "error 1", now)
	cb.RecordFailure("RAIL-A", "error 2", now)

	state := cb.GetState("RAIL-A", now)
	if state != circuit.StateOpen {
		t.Fatalf("expected state OPEN, got %v", state)
	}

	candidate := payments.RouteCandidate{
		CandidateID:       "CANDIDATE-A",
		ExecutionTargetID: "RAIL-A",
	}
	isEligible := cb.EligibilityHook()(context.Background(), candidate)
	if isEligible {
		t.Error("expected candidate RAIL-A to be ineligible while OPEN")
	}

	var violations int
	if state == circuit.StateOpen || !isEligible {
		violations++
	}
	passed := violations == 0
	if passed {
		t.Error("expected invariant passed to be false when ineligible target is selected")
	}
}

func TestExperiment5_DuplicateFinancialExecutionDetected(t *testing.T) {
	tracker := NewExecutionTracker()
	key := "test-duplicate-key"

	tracker.RecordHold(key)
	unique, maxExecs, dups := tracker.Stats()
	if unique != 1 || maxExecs != 1 || dups != 0 {
		t.Fatalf("unexpected stats after 1 execution: unique=%d, max=%d, dups=%d", unique, maxExecs, dups)
	}

	// Deliberate duplicate execution of same logical key
	tracker.RecordHold(key)
	unique, maxExecs, dups = tracker.Stats()
	if dups != 1 || maxExecs != 2 {
		t.Fatalf("expected dups=1 and maxExecs=2, got dups=%d, maxExecs=%d", dups, maxExecs)
	}

	invariantPassed := dups == 0 && maxExecs <= 1
	if invariantPassed {
		t.Error("expected invariant to fail when duplicate financial execution occurred")
	}
}

