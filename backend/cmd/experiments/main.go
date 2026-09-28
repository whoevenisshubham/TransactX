package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	experimentFlag := flag.String("experiment", "all", "Experiment to run: 1|2|3|5|all (or routing|outage|latency|concurrency)")
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
