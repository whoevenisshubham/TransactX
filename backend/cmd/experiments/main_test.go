package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/transactx/backend/internal/experiments"
)

func TestEvaluateCrossBankReport_Success(t *testing.T) {
	report := &experiments.CrossBankRecoveryReport{
		TotalScenarios:         7,
		PassedCount:            7,
		FailedCount:            0,
		AllInvariantsSatisfied: true,
		FinancialSummary: experiments.CrossBankFinancialSummary{
			TotalViolations: 0,
		},
	}

	err := evaluateCrossBankReport(report)
	if err != nil {
		t.Fatalf("expected nil error for successful report, got: %v", err)
	}
}

func TestEvaluateCrossBankReport_FailedScenarios(t *testing.T) {
	report := &experiments.CrossBankRecoveryReport{
		TotalScenarios:         7,
		PassedCount:            6,
		FailedCount:            1,
		AllInvariantsSatisfied: false,
		FinancialSummary: experiments.CrossBankFinancialSummary{
			TotalViolations: 1,
		},
	}

	err := evaluateCrossBankReport(report)
	if err == nil {
		t.Fatalf("expected error for report with failed scenarios, got nil")
	}
	if !strings.Contains(err.Error(), "1 scenarios failed") {
		t.Errorf("expected error message mentioning failed scenarios, got: %v", err)
	}
}

func TestEvaluateCrossBankReport_ZeroFailedScenarios_InvariantsFailed(t *testing.T) {
	report := &experiments.CrossBankRecoveryReport{
		TotalScenarios:         7,
		PassedCount:            7,
		FailedCount:            0,
		AllInvariantsSatisfied: false,
		FinancialSummary: experiments.CrossBankFinancialSummary{
			TotalViolations: 2,
		},
	}

	err := evaluateCrossBankReport(report)
	if err == nil {
		t.Fatalf("expected error when financial invariants fail despite 0 scenario failures, got nil")
	}
	if !strings.Contains(err.Error(), "financial invariants not satisfied") {
		t.Errorf("expected error message to mention financial invariants, got: %v", err)
	}
	if !strings.Contains(err.Error(), "violations=2") {
		t.Errorf("expected error message to mention violations count, got: %v", err)
	}
}

func TestEvaluateCrossBankReport_Nil(t *testing.T) {
	err := evaluateCrossBankReport(nil)
	if err == nil {
		t.Fatalf("expected error for nil cross-bank report, got nil")
	}
	if !strings.Contains(err.Error(), "cross-bank recovery report is nil") {
		t.Errorf("expected error message mentioning nil report, got: %v", err)
	}
}

// 1. Successful complete results for experiments 1, 2, 3, and 5.
func TestEvaluateBenchmarkResult_SuccessfulCompleteResults(t *testing.T) {
	res1 := &experiments.ExperimentResult{
		ExperimentID: "EXP-1",
		Title:        "Experiment 1: Routing",
		Baseline: &experiments.ExperimentSummary{
			AllInvariantsSatisfied: true,
			Invariants:             []experiments.InvariantResult{{InvariantName: "Base Inv 1", Passed: true}},
		},
		Proposed: &experiments.ExperimentSummary{
			AllInvariantsSatisfied: true,
			Invariants:             []experiments.InvariantResult{{InvariantName: "Prop Inv 1", Passed: true}},
		},
	}
	res2 := &experiments.ExperimentResult{
		ExperimentID: "outage",
		Title:        "Experiment 2: Outage",
		Baseline: &experiments.ExperimentSummary{
			AllInvariantsSatisfied: true,
			Invariants:             []experiments.InvariantResult{{InvariantName: "Base Inv 2", Passed: true}},
		},
		Proposed: &experiments.ExperimentSummary{
			AllInvariantsSatisfied: true,
			Invariants:             []experiments.InvariantResult{{InvariantName: "Prop Inv 2", Passed: true}},
		},
	}
	res3 := &experiments.ExperimentResult{
		ExperimentID: "3",
		Title:        "Experiment 3: Latency",
		Baseline: &experiments.ExperimentSummary{
			AllInvariantsSatisfied: true,
			Invariants:             []experiments.InvariantResult{{InvariantName: "Base Inv 3", Passed: true}},
		},
		Proposed: &experiments.ExperimentSummary{
			AllInvariantsSatisfied: true,
			Invariants:             []experiments.InvariantResult{{InvariantName: "Prop Inv 3", Passed: true}},
		},
	}
	res5 := &experiments.ExperimentResult{
		ExperimentID: "EXP-5",
		Title:        "Experiment 5: Concurrency",
		Summary: &experiments.ExperimentSummary{
			AllInvariantsSatisfied: true,
			Invariants:             []experiments.InvariantResult{{InvariantName: "Concurrency Conservation", Passed: true}},
		},
	}

	for _, res := range []*experiments.ExperimentResult{res1, res2, res3, res5} {
		if err := evaluateBenchmarkResult(res); err != nil {
			t.Fatalf("expected nil error for complete result %s, got: %v", res.ExperimentID, err)
		}
	}

	// Verify aggregate helper succeeds on full slice
	if err := evaluateAllBenchmarkResults([]*experiments.ExperimentResult{res1, res2, res3, res5}); err != nil {
		t.Fatalf("expected nil error for evaluateAllBenchmarkResults, got: %v", err)
	}
}

// 2. Missing baseline for a result that requires it (experiments 1, 2, 3).
func TestEvaluateBenchmarkResult_MissingBaseline(t *testing.T) {
	testIDs := []string{"EXP-1", "2", "latency"}
	for _, id := range testIDs {
		res := &experiments.ExperimentResult{
			ExperimentID: id,
			Title:        "Dual Required Shape Experiment",
			Baseline:     nil, // Missing baseline
			Proposed: &experiments.ExperimentSummary{
				AllInvariantsSatisfied: true,
				Invariants:             []experiments.InvariantResult{{InvariantName: "Prop Inv", Passed: true}},
			},
		}

		err := evaluateBenchmarkResult(res)
		if err == nil {
			t.Fatalf("expected error for experiment %s missing baseline, got nil", id)
		}
		if !strings.Contains(err.Error(), "missing required baseline summary") {
			t.Errorf("expected error message to identify missing baseline summary, got: %v", err)
		}
	}
}

// 3. Missing proposed result for a result that requires it (experiments 1, 2, 3).
func TestEvaluateBenchmarkResult_MissingProposed(t *testing.T) {
	testIDs := []string{"routing", "EXP-2", "3"}
	for _, id := range testIDs {
		res := &experiments.ExperimentResult{
			ExperimentID: id,
			Title:        "Dual Required Shape Experiment",
			Baseline: &experiments.ExperimentSummary{
				AllInvariantsSatisfied: true,
				Invariants:             []experiments.InvariantResult{{InvariantName: "Base Inv", Passed: true}},
			},
			Proposed: nil, // Missing proposed
		}

		err := evaluateBenchmarkResult(res)
		if err == nil {
			t.Fatalf("expected error for experiment %s missing proposed, got nil", id)
		}
		if !strings.Contains(err.Error(), "missing required proposed summary") {
			t.Errorf("expected error message to identify missing proposed summary, got: %v", err)
		}
	}
}

// 4. Missing summary for a result that requires it (experiment 5).
func TestEvaluateBenchmarkResult_MissingSummary(t *testing.T) {
	// Experiment 5 with Summary nil
	res := &experiments.ExperimentResult{
		ExperimentID: "EXP-5",
		Title:        "Experiment 5: Concurrency",
		Summary:      nil,
	}

	err := evaluateBenchmarkResult(res)
	if err == nil {
		t.Fatalf("expected error for experiment 5 missing summary, got nil")
	}
	if !strings.Contains(err.Error(), "missing required summary") {
		t.Errorf("expected error message to identify missing summary, got: %v", err)
	}

	// Verify fail-closed: providing Baseline and Proposed does not substitute for Summary
	resSubstituted := &experiments.ExperimentResult{
		ExperimentID: "concurrency",
		Title:        "Experiment 5: Concurrency",
		Baseline: &experiments.ExperimentSummary{
			AllInvariantsSatisfied: true,
		},
		Proposed: &experiments.ExperimentSummary{
			AllInvariantsSatisfied: true,
		},
		Summary: nil,
	}
	errSub := evaluateBenchmarkResult(resSubstituted)
	if errSub == nil {
		t.Fatalf("expected error when summary is missing even if baseline/proposed exist, got nil")
	}
	if !strings.Contains(errSub.Error(), "missing required summary") {
		t.Errorf("expected error message to strictly require summary, got: %v", errSub)
	}
}

// 5. Each required component reporting failed invariants.
func TestEvaluateBenchmarkResult_ComponentInvariantFailures(t *testing.T) {
	// Component 1: Baseline invariant fails (Experiment 1/2/3)
	resBaseFail := &experiments.ExperimentResult{
		ExperimentID: "EXP-1",
		Title:        "Experiment 1: Routing",
		Baseline: &experiments.ExperimentSummary{
			AllInvariantsSatisfied: false,
			Invariants: []experiments.InvariantResult{
				{InvariantName: "Traffic balance", Passed: false, Details: "unbalanced routing"},
			},
		},
		Proposed: &experiments.ExperimentSummary{
			AllInvariantsSatisfied: true,
			Invariants: []experiments.InvariantResult{
				{InvariantName: "Adaptive isolation", Passed: true},
			},
		},
	}
	err := evaluateBenchmarkResult(resBaseFail)
	if err == nil {
		t.Fatalf("expected error when baseline invariants fail, got nil")
	}
	if !strings.Contains(err.Error(), "baseline invariants not satisfied") {
		t.Errorf("expected error message to identify baseline invariants failure, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Traffic balance [unbalanced routing]") {
		t.Errorf("expected error message to include invariant detail, got: %v", err)
	}

	// Component 2: Proposed invariant fails (Experiment 1/2/3)
	resPropFail := &experiments.ExperimentResult{
		ExperimentID: "outage",
		Title:        "Experiment 2: Outage",
		Baseline: &experiments.ExperimentSummary{
			AllInvariantsSatisfied: true,
			Invariants: []experiments.InvariantResult{
				{InvariantName: "Baseline routing", Passed: true},
			},
		},
		Proposed: &experiments.ExperimentSummary{
			AllInvariantsSatisfied: false,
			Invariants: []experiments.InvariantResult{
				{InvariantName: "Circuit breaker trip", Passed: false, Details: "breaker did not trip"},
			},
		},
	}
	err = evaluateBenchmarkResult(resPropFail)
	if err == nil {
		t.Fatalf("expected error when proposed invariants fail, got nil")
	}
	if !strings.Contains(err.Error(), "proposed invariants not satisfied") {
		t.Errorf("expected error message to identify proposed invariants failure, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Circuit breaker trip [breaker did not trip]") {
		t.Errorf("expected error message to include invariant detail, got: %v", err)
	}

	// Component 3: Both Baseline and Proposed fail (Experiment 1/2/3)
	resBothFail := &experiments.ExperimentResult{
		ExperimentID: "latency",
		Title:        "Experiment 3: Latency",
		Baseline: &experiments.ExperimentSummary{
			AllInvariantsSatisfied: false,
			Invariants: []experiments.InvariantResult{
				{InvariantName: "Base SLA", Passed: false, Details: "base breach"},
			},
		},
		Proposed: &experiments.ExperimentSummary{
			AllInvariantsSatisfied: false,
			Invariants: []experiments.InvariantResult{
				{InvariantName: "Adaptive SLA", Passed: false, Details: "adaptive breach"},
			},
		},
	}
	err = evaluateBenchmarkResult(resBothFail)
	if err == nil {
		t.Fatalf("expected error when both fail, got nil")
	}
	if !strings.Contains(err.Error(), "baseline invariants not satisfied") || !strings.Contains(err.Error(), "proposed invariants not satisfied") {
		t.Errorf("expected error message to identify both baseline and proposed failures, got: %v", err)
	}

	// Component 4: Summary invariant fails (Experiment 5)
	resSummaryFail := &experiments.ExperimentResult{
		ExperimentID: "EXP-5",
		Title:        "Experiment 5: Concurrency",
		Summary: &experiments.ExperimentSummary{
			AllInvariantsSatisfied: false,
			Invariants: []experiments.InvariantResult{
				{InvariantName: "Financial conservation", Passed: false, Details: "imbalance of 500 paise"},
			},
		},
	}
	err = evaluateBenchmarkResult(resSummaryFail)
	if err == nil {
		t.Fatalf("expected error when summary invariants fail, got nil")
	}
	if !strings.Contains(err.Error(), "summary invariants not satisfied") {
		t.Errorf("expected error message to identify summary invariants failure, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Financial conservation [imbalance of 500 paise]") {
		t.Errorf("expected error message to include invariant detail, got: %v", err)
	}
}

// 6. An unsupported experiment ID, according to the chosen fail-closed policy.
func TestEvaluateBenchmarkResult_UnsupportedExperimentID(t *testing.T) {
	unsupportedIDs := []string{"EXP-99", "unknown", "4", "", "   "}
	for _, id := range unsupportedIDs {
		res := &experiments.ExperimentResult{
			ExperimentID: id,
			Title:        "Unsupported Experiment",
			Baseline: &experiments.ExperimentSummary{
				AllInvariantsSatisfied: true,
			},
			Proposed: &experiments.ExperimentSummary{
				AllInvariantsSatisfied: true,
			},
			Summary: &experiments.ExperimentSummary{
				AllInvariantsSatisfied: true,
			},
		}

		err := evaluateBenchmarkResult(res)
		if err == nil {
			t.Fatalf("expected error for unsupported experiment ID %q, got nil", id)
		}
		if !strings.Contains(err.Error(), "unsupported experiment ID") {
			t.Errorf("expected error message to indicate unsupported experiment ID, got: %v", err)
		}
	}
}

// 7. An empty aggregate result slice.
func TestEvaluateAllBenchmarkResults_EmptySlice(t *testing.T) {
	// Empty slice
	err := evaluateAllBenchmarkResults([]*experiments.ExperimentResult{})
	if err == nil {
		t.Fatalf("expected error for empty slice, got nil")
	}
	if !strings.Contains(err.Error(), "no benchmark results to evaluate") {
		t.Errorf("expected error message mentioning no benchmark results, got: %v", err)
	}

	// Nil slice
	err = evaluateAllBenchmarkResults(nil)
	if err == nil {
		t.Fatalf("expected error for nil slice, got nil")
	}
	if !strings.Contains(err.Error(), "no benchmark results to evaluate") {
		t.Errorf("expected error message mentioning no benchmark results, got: %v", err)
	}
}

// Guard: Nil benchmark result
func TestEvaluateBenchmarkResult_NilResult(t *testing.T) {
	err := evaluateBenchmarkResult(nil)
	if err == nil {
		t.Fatalf("expected error for nil benchmark result, got nil")
	}
	if !strings.Contains(err.Error(), "benchmark result is nil") {
		t.Errorf("expected error message mentioning nil result, got: %v", err)
	}
}

// Database-independent unit test for cleanup-error aggregation helper.
func TestCombineExperimentAndCleanupError(t *testing.T) {
	errExp := errors.New("primary experiment failure")
	errCleanup := errors.New("cleanup connection closed")

	// 1. Both nil -> returns nil
	if err := combineExperimentAndCleanupError("test-op", nil, nil); err != nil {
		t.Errorf("expected nil when both errors are nil, got: %v", err)
	}

	// 2. Experiment error only -> returns experiment error unchanged
	if err := combineExperimentAndCleanupError("test-op", errExp, nil); !errors.Is(err, errExp) {
		t.Errorf("expected errExp when cleanup is nil, got: %v", err)
	}

	// 3. Cleanup error only -> returns cleanup error with operation context
	errOnlyCleanup := combineExperimentAndCleanupError("test-op", nil, errCleanup)
	if errOnlyCleanup == nil {
		t.Fatalf("expected non-nil error when cleanup fails, got nil")
	}
	if !errors.Is(errOnlyCleanup, errCleanup) {
		t.Errorf("expected wrapped cleanup error, got: %v", errOnlyCleanup)
	}
	if !strings.Contains(errOnlyCleanup.Error(), "test-op cleanup error") {
		t.Errorf("expected error to mention operation name, got: %v", errOnlyCleanup)
	}

	// 4. Both experiment and cleanup error -> returns combined error preserving both errors via errors.Is
	errCombined := combineExperimentAndCleanupError("test-op", errExp, errCleanup)
	if errCombined == nil {
		t.Fatalf("expected combined error, got nil")
	}
	if !errors.Is(errCombined, errExp) {
		t.Errorf("expected errors.Is(errCombined, errExp) == true, got false (err: %v)", errCombined)
	}
	if !errors.Is(errCombined, errCleanup) {
		t.Errorf("expected errors.Is(errCombined, errCleanup) == true, got false (err: %v)", errCombined)
	}
	if !strings.Contains(errCombined.Error(), "primary experiment failure") {
		t.Errorf("expected combined error to include primary experiment error string, got: %v", errCombined)
	}
	if !strings.Contains(errCombined.Error(), "test-op cleanup error") {
		t.Errorf("expected combined error to include cleanup operation context, got: %v", errCombined)
	}
	if !strings.Contains(errCombined.Error(), "cleanup connection closed") {
		t.Errorf("expected combined error to include cleanup error detail, got: %v", errCombined)
	}
}

// Database-independent unit test verifying that cleanup failure prevents overall success status.
func TestCleanupFailurePreventsOverallSuccess(t *testing.T) {
	// Scenario A: Experiment succeeded (nil error), but cleanup failed
	errCleanup := errors.New("foreign key constraint violation on payments")
	errResult := combineExperimentAndCleanupError("cross-bank recovery", nil, errCleanup)
	if errResult == nil {
		t.Fatalf("expected cleanup failure to cause non-nil overall error, got nil")
	}
	if !errors.Is(errResult, errCleanup) {
		t.Errorf("expected overall error to match cleanup error via errors.Is, got: %v", errResult)
	}
	if !strings.Contains(errResult.Error(), "cross-bank recovery cleanup error") {
		t.Errorf("expected error to include operation context, got: %v", errResult)
	}

	// Scenario B: Experiment failed AND cleanup failed
	errExp := errors.New("experiment invariant violation")
	errBoth := combineExperimentAndCleanupError("crash matrix", errExp, errCleanup)
	if errBoth == nil {
		t.Fatalf("expected non-nil error when both fail, got nil")
	}
	if !errors.Is(errBoth, errExp) {
		t.Errorf("expected errors.Is(errBoth, errExp) == true, got false")
	}
	if !errors.Is(errBoth, errCleanup) {
		t.Errorf("expected errors.Is(errBoth, errCleanup) == true, got false")
	}
	// Verify that the original error is preserved and not replaced by cleanup error
	if !strings.Contains(errBoth.Error(), "experiment invariant violation") {
		t.Errorf("expected combined error to retain original experiment error, got: %v", errBoth)
	}
	if !strings.Contains(errBoth.Error(), "crash matrix cleanup error") {
		t.Errorf("expected combined error to retain cleanup error context, got: %v", errBoth)
	}
}

// Database-independent unit test verifying that ErrImmutableHistoryBlockedCleanup is preserved
// and identifiable via errors.Is when combined with an experiment result error.
func TestCombineExperimentAndCleanupError_ImmutableHistoryBlocked(t *testing.T) {
	errExp := errors.New("cross-bank scenario CB-01 failed: hold timeout expired")
	errBlocked := fmt.Errorf("%w: cleanup table payments: violates FK on payment_state_transitions", experiments.ErrImmutableHistoryBlockedCleanup)

	// Combine both
	combined := combineExperimentAndCleanupError("cross-bank recovery", errExp, errBlocked)
	if combined == nil {
		t.Fatalf("expected non-nil combined error")
	}

	// 1. Original experiment error is preserved
	if !errors.Is(combined, errExp) {
		t.Errorf("expected errors.Is(combined, errExp) == true")
	}

	// 2. Immutable history blocked cleanup sentinel is preserved
	if !errors.Is(combined, experiments.ErrImmutableHistoryBlockedCleanup) {
		t.Errorf("expected errors.Is(combined, experiments.ErrImmutableHistoryBlockedCleanup) == true")
	}

	// 3. Operation context is preserved in error string
	if !strings.Contains(combined.Error(), "cross-bank recovery cleanup error") {
		t.Errorf("expected combined error to contain operation context, got: %v", combined)
	}
	if !strings.Contains(combined.Error(), "hold timeout expired") {
		t.Errorf("expected combined error to contain original experiment error string, got: %v", combined)
	}

	// 4. Teardown status classification works on the combined error
	if status := experiments.ClassifyTeardownError(combined); status != experiments.TeardownRequiresExternalDrop {
		t.Errorf("expected ClassifyTeardownError on combined error to return TeardownRequiresExternalDrop, got: %s", status)
	}
}
