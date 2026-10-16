package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/transactx/backend/internal/database"
	"github.com/transactx/backend/internal/experiments"
)

func findRepoRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, "backend", "go.mod")); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			// Inside backend directory
			parent := filepath.Dir(dir)
			return parent
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return cwd
}

func main() {
	experimentFlag := flag.String("experiment", "all", "Experiment to run: 1|2|3|5|crash|all (or routing|outage|latency|concurrency|crash)")
	outputDirFlag := flag.String("output-dir", "", "Directory to write artifacts into (defaults to <repo_root>/artifacts)")
	seedFlag := flag.Int64("seed", 42, "Deterministic seed for simulation")
	requestsFlag := flag.Int("requests", 300, "Total number of operations/requests per experiment")
	concurrencyFlag := flag.Int("concurrency", 10, "Concurrency level for Experiment 5")
	flag.Parse()

	outputDir := *outputDirFlag
	if outputDir == "" {
		repoRoot := findRepoRoot()
		outputDir = filepath.Join(repoRoot, "artifacts")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fmt.Println("================================================================================")
	fmt.Println("  TransactX M2-8: Resilience Experiments Runner")
	fmt.Println("================================================================================")
	fmt.Printf("Seed:         %d\n", *seedFlag)
	fmt.Printf("Requests:     %d\n", *requestsFlag)
	fmt.Printf("Concurrency:  %d\n", *concurrencyFlag)
	fmt.Printf("Output Dir:   %s\n", outputDir)
	fmt.Println("--------------------------------------------------------------------------------")

	selected := strings.ToLower(strings.TrimSpace(*experimentFlag))
	runAll := selected == "all"

	// Crash Matrix Experiment (B12): Run recovery fault boundaries and event restart scenarios against PostgreSQL
	if selected == "crash" || selected == "matrix" {
		if err := runCrashMatrix(ctx, outputDir, *seedFlag); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Cross-Bank Recovery Matrix: Native cross-bank failure and lost-reply scenarios
	if selected == "crossbank" || selected == "cb" {
		if err := runCrossBankRecovery(ctx, outputDir); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	var results []*experiments.ExperimentResult

	// Experiment 1: Routing
	if runAll || selected == "1" || selected == "routing" {
		fmt.Println("\n[EXPERIMENT 1] Static Routing vs Health-Aware Routing")
		res, err := experiments.RunExperiment1(ctx, *seedFlag, *requestsFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error running Experiment 1: %v\n", err)
			os.Exit(1)
		}
		printSummary("Baseline (Static)", res.Baseline)
		printSummary("Proposed (Adaptive)", res.Proposed)
		results = append(results, res)
	}

	// Experiment 2: Outage
	if runAll || selected == "2" || selected == "outage" {
		fmt.Println("\n[EXPERIMENT 2] Bank Outage and Circuit Isolation")
		res, err := experiments.RunExperiment2(ctx, *seedFlag, *requestsFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error running Experiment 2: %v\n", err)
			os.Exit(1)
		}
		printSummary("Baseline (Static)", res.Baseline)
		printSummary("Proposed (Circuit-Aware)", res.Proposed)
		results = append(results, res)
	}

	// Experiment 3: Latency
	if runAll || selected == "3" || selected == "latency" {
		fmt.Println("\n[EXPERIMENT 3] Latency Degradation and Traffic Shift")
		res, err := experiments.RunExperiment3(ctx, *seedFlag, *requestsFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error running Experiment 3: %v\n", err)
			os.Exit(1)
		}
		printSummary("Baseline (Static)", res.Baseline)
		printSummary("Proposed (Health-Aware)", res.Proposed)
		results = append(results, res)
	}

	// Experiment 5: Concurrency Storm
	if runAll || selected == "5" || selected == "concurrency" {
		fmt.Println("\n[EXPERIMENT 5] Concurrency Storm and Financial Invariants")
		res, err := experiments.RunExperiment5(ctx, *seedFlag, *requestsFlag, *concurrencyFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error running Experiment 5: %v\n", err)
			os.Exit(1)
		}
		printSummary("Concurrency Storm", res.Summary)
		results = append(results, res)
	}

	fmt.Println("\n================================================================================")
	fmt.Println("  Persisting Raw Benchmark Artifacts")
	fmt.Println("================================================================================")
	for _, res := range results {
		jsonPath, csvPath, err := res.SaveArtifacts(outputDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to save artifacts for %s: %v\n", res.ExperimentID, err)
			os.Exit(1)
		}
		fmt.Printf("[%s]\n  JSON: %s\n  CSV:  %s\n", res.ExperimentID, jsonPath, csvPath)
	}

	if err := evaluateAllBenchmarkResults(results); err != nil {
		fmt.Fprintf(os.Stderr, "\nBenchmark verification failed:\n%v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nExperiments completed successfully with all invariants satisfied.")
}

func printSummary(label string, s *experiments.ExperimentSummary) {
	if s == nil {
		return
	}
	fmt.Printf("  %s:\n", label)
	fmt.Printf("    Total Requests:     %d\n", s.TotalRequests)
	fmt.Printf("    Successes:          %d\n", s.SuccessCount)
	fmt.Printf("    Failures:           %d\n", s.FailureCount)
	if s.PendingCount > 0 {
		fmt.Printf("    Pending:            %d\n", s.PendingCount)
	}
	fmt.Printf("    Success Rate:       %.2f%%\n", s.SuccessRate)
	if s.IsolationTimeMs > 0 {
		fmt.Printf("    Isolation Time:     %.2f ms\n", s.IsolationTimeMs)
	}
	if s.RecoveryTimeMs > 0 {
		fmt.Printf("    Recovery Time:      %.2f ms\n", s.RecoveryTimeMs)
	}
	if s.Latency.P50Ms > 0 || s.Latency.P95Ms > 0 {
		fmt.Printf("    Latency P50/P95/P99: %.2f ms / %.2f ms / %.2f ms (Max: %.2f ms)\n",
			s.Latency.P50Ms, s.Latency.P95Ms, s.Latency.P99Ms, s.Latency.MaxMs)
	}
	if len(s.TrafficShare) > 0 {
		fmt.Print("    Traffic Share:      ")
		for i, ts := range s.TrafficShare {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Printf("%s: %d (%.1f%%)", ts.TargetID, ts.Count, ts.Percentage)
		}
		fmt.Println()
	}
	if len(s.AdditionalMetrics) > 0 {
		for k, v := range s.AdditionalMetrics {
			fmt.Printf("    %s: %v\n", k, v)
		}
	}
	if len(s.Invariants) > 0 {
		fmt.Printf("    Invariants:         All Satisfied = %t\n", s.AllInvariantsSatisfied)
		for _, inv := range s.Invariants {
			status := "PASS"
			if !inv.Passed {
				status = "FAIL"
			}
			fmt.Printf("      [%s] %s: %s\n", status, inv.InvariantName, inv.Details)
		}
	}
}

func runCrashMatrix(ctx context.Context, outputDir string, seed int64) (err error) {
	fmt.Println("\n[CRASH MATRIX] Recovery Fault Boundaries & Event Restart Matrix (B12)")
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return fmt.Errorf("DATABASE_URL environment variable is required to run crash matrix experiment (must point to an isolated disposable database)")
	}
	pool, poolErr := database.NewPool(ctx, dbURL)
	if poolErr != nil {
		return fmt.Errorf("connecting to database: %w", poolErr)
	}
	defer pool.Close()

	harness, initErr := experiments.NewCrashMatrixHarness(pool, seed)
	if initErr != nil {
		return fmt.Errorf("initializing crash matrix harness: %w", initErr)
	}
	defer func() {
		if closeErr := harness.Close(); closeErr != nil {
			err = combineExperimentAndCleanupError("crash matrix", err, closeErr)
		}
	}()

	report, runErr := harness.RunAllScenarios(ctx)
	if runErr != nil {
		return fmt.Errorf("running crash matrix scenarios: %w", runErr)
	}

	fmt.Printf("\nCrash Matrix Results: %d Total | %d Passed | %d Failed | All Invariants: %t\n",
		report.TotalScenarios, report.PassedCount, report.FailedCount, report.AllInvariantsSatisfied)
	for _, s := range report.Scenarios {
		status := "PASS"
		if !s.Passed {
			status = "FAIL"
		}
		fmt.Printf("  [%s] %-8s %-50s (%.2f ms, state=%s)\n", status, s.ID, s.Name, s.DurationMs, s.ObservedState)
	}

	jsonPath, csvPath, saveErr := report.SaveArtifacts(outputDir)
	if saveErr != nil {
		return fmt.Errorf("saving crash matrix artifacts: %w", saveErr)
	}
	fmt.Printf("\nSaved Artifacts:\n  JSON: %s\n  CSV:  %s\n  SHA256: %s\n", jsonPath, csvPath, report.ReportSHA256)

	if report.FailedCount > 0 || !report.AllInvariantsSatisfied {
		return fmt.Errorf("crash matrix verification failed: %d scenarios failed, all invariants satisfied: %t",
			report.FailedCount, report.AllInvariantsSatisfied)
	}

	return nil
}

func runCrossBankRecovery(ctx context.Context, outputDir string) (err error) {
	fmt.Println("\n[CROSS-BANK RECOVERY] Native Cross-Bank and Lost-Reply Recovery Matrix")
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return fmt.Errorf("DATABASE_URL environment variable is required to run cross-bank recovery experiment (must point to an isolated disposable database)")
	}
	pool, poolErr := database.NewPool(ctx, dbURL)
	if poolErr != nil {
		return fmt.Errorf("connecting to database: %w", poolErr)
	}
	defer pool.Close()

	harness, initErr := experiments.NewCrossBankRecoveryHarness(pool, dbURL)
	if initErr != nil {
		return fmt.Errorf("initializing cross-bank recovery harness: %w", initErr)
	}
	defer func() {
		if closeErr := harness.Close(); closeErr != nil {
			err = combineExperimentAndCleanupError("cross-bank recovery", err, closeErr)
		}
	}()

	report, runErr := harness.RunAll(ctx)
	if runErr != nil {
		return fmt.Errorf("running cross-bank recovery scenarios: %w", runErr)
	}

	fmt.Printf("\nCross-Bank Recovery Results: %d Total | %d Passed | %d Failed | All Invariants: %t\n",
		report.TotalScenarios, report.PassedCount, report.FailedCount, report.AllInvariantsSatisfied)
	for _, s := range report.Scenarios {
		status := "PASS"
		if !s.Passed {
			status = "FAIL"
		}
		fmt.Printf("  [%s] %-8s %-55s (%.2f ms, state=%s)\n", status, s.ID, s.Name, s.DurationMs, s.ObservedState)
	}

	jsonPath, csvPath, saveErr := report.SaveArtifacts(outputDir)
	if saveErr != nil {
		return fmt.Errorf("saving cross-bank recovery artifacts: %w", saveErr)
	}
	fmt.Printf("\nSaved Artifacts:\n  JSON: %s\n  CSV:  %s\n  SHA256: %s\n", jsonPath, csvPath, report.ReportSHA256)

	if evalErr := evaluateCrossBankReport(report); evalErr != nil {
		return evalErr
	}

	return nil
}

func evaluateCrossBankReport(report *experiments.CrossBankRecoveryReport) error {
	if report == nil {
		return fmt.Errorf("cross-bank recovery report is nil")
	}
	if report.FailedCount > 0 && !report.AllInvariantsSatisfied {
		return fmt.Errorf("cross-bank recovery verification failed: %d scenarios failed, all invariants satisfied: false (violations=%d)",
			report.FailedCount, report.FinancialSummary.TotalViolations)
	}
	if report.FailedCount > 0 {
		return fmt.Errorf("cross-bank recovery verification failed: %d scenarios failed", report.FailedCount)
	}
	if !report.AllInvariantsSatisfied {
		return fmt.Errorf("cross-bank recovery verification failed: 0 scenarios failed, but financial invariants not satisfied (violations=%d)",
			report.FinancialSummary.TotalViolations)
	}
	return nil
}

func failedInvariantDetails(s *experiments.ExperimentSummary) string {
	if s == nil {
		return "summary is nil"
	}
	var failures []string
	for _, inv := range s.Invariants {
		if !inv.Passed {
			failures = append(failures, fmt.Sprintf("%s [%s]", inv.InvariantName, inv.Details))
		}
	}
	if len(failures) > 0 {
		return strings.Join(failures, "; ")
	}
	return "AllInvariantsSatisfied is false"
}

// combineExperimentAndCleanupError combines an experiment failure with a subsequent cleanup failure.
// If cleanup succeeded, it returns the experiment error unchanged (or nil if both succeeded).
// If the experiment succeeded but cleanup failed, it returns the contextually wrapped cleanup error.
// If both failed, it uses errors.Join to combine the primary experiment error and the
// contextually wrapped cleanup error. This guarantees that:
//   - errors.Is(result, expErr) is true
//   - errors.Is(result, cleanupErr) is true
//   - The diagnostic identifies the operation name and retains cleanup context.
func combineExperimentAndCleanupError(opName string, expErr, cleanupErr error) error {
	if cleanupErr == nil {
		return expErr
	}
	cleanupCtxErr := fmt.Errorf("%s cleanup error: %w", opName, cleanupErr)
	if expErr == nil {
		return cleanupCtxErr
	}
	return errors.Join(expErr, cleanupCtxErr)
}

// evaluateBenchmarkResult validates that a benchmark result matches its experiment-specific
// required shape and that all required invariant flags are satisfied.
//
// Required result shapes:
//   - Experiment 1 (routing / "EXP-1" / "1"): requires both Baseline and Proposed summaries.
//   - Experiment 2 (outage / "EXP-2" / "2"): requires both Baseline and Proposed summaries.
//   - Experiment 3 (latency / "EXP-3" / "3"): requires both Baseline and Proposed summaries.
//   - Experiment 5 (concurrency / "EXP-5" / "5"): requires Summary.
//
// Policy for unknown / unsupported experiment IDs:
//
//	Fails closed with an explicit error. An unknown experiment ID cannot have its required
//	component shape inferred or guaranteed, so it is strictly rejected rather than
//	defaulting or guessing.
func evaluateBenchmarkResult(res *experiments.ExperimentResult) error {
	if res == nil {
		return fmt.Errorf("benchmark result is nil")
	}

	normID := strings.ToUpper(strings.TrimSpace(res.ExperimentID))
	switch normID {
	case "EXP-1", "1", "ROUTING", "EXP-2", "2", "OUTAGE", "EXP-3", "3", "LATENCY":
		if res.Baseline == nil {
			return fmt.Errorf("experiment %s (%s): missing required baseline summary", res.ExperimentID, res.Title)
		}
		if res.Proposed == nil {
			return fmt.Errorf("experiment %s (%s): missing required proposed summary", res.ExperimentID, res.Title)
		}
		var invFailures []string
		if !res.Baseline.AllInvariantsSatisfied {
			invFailures = append(invFailures, fmt.Sprintf("baseline invariants not satisfied: %s", failedInvariantDetails(res.Baseline)))
		}
		if !res.Proposed.AllInvariantsSatisfied {
			invFailures = append(invFailures, fmt.Sprintf("proposed invariants not satisfied: %s", failedInvariantDetails(res.Proposed)))
		}
		if len(invFailures) > 0 {
			return fmt.Errorf("experiment %s (%s): %s", res.ExperimentID, res.Title, strings.Join(invFailures, "; "))
		}
		return nil

	case "EXP-5", "5", "CONCURRENCY":
		if res.Summary == nil {
			return fmt.Errorf("experiment %s (%s): missing required summary", res.ExperimentID, res.Title)
		}
		if !res.Summary.AllInvariantsSatisfied {
			return fmt.Errorf("experiment %s (%s): summary invariants not satisfied: %s",
				res.ExperimentID, res.Title, failedInvariantDetails(res.Summary))
		}
		return nil

	default:
		return fmt.Errorf("experiment %s (%s): unsupported experiment ID; cannot verify required benchmark result shape",
			res.ExperimentID, res.Title)
	}
}

func evaluateAllBenchmarkResults(results []*experiments.ExperimentResult) error {
	if len(results) == 0 {
		return fmt.Errorf("no benchmark results to evaluate")
	}
	var failureMessages []string
	for _, res := range results {
		if err := evaluateBenchmarkResult(res); err != nil {
			failureMessages = append(failureMessages, err.Error())
		}
	}
	if len(failureMessages) > 0 {
		return fmt.Errorf("%s", strings.Join(failureMessages, "\n"))
	}
	return nil
}
