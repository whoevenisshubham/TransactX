package experiments

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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
