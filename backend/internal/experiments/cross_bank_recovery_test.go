package experiments

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/transactx/backend/internal/database"
)

func findTestRepoRoot() string {
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
			return filepath.Dir(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return cwd
}

func TestCrossBankRecovery_FullMatrix(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres@localhost:5432/transactx?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := database.NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	harness, err := NewCrossBankRecoveryHarness(pool, dbURL)
	if err != nil {
		t.Fatalf("init cross-bank recovery harness: %v", err)
	}
	defer harness.Close()

	report, err := harness.RunAll(ctx)
	if err != nil {
		t.Fatalf("run cross-bank scenarios failed: %v", err)
	}

	t.Logf("Cross-Bank Recovery Results: %d Total | %d Passed | %d Failed | All Invariants: %t",
		report.TotalScenarios, report.PassedCount, report.FailedCount, report.AllInvariantsSatisfied)

	for _, sc := range report.Scenarios {
		t.Logf("  [%s] %s (%.2f ms) - Passed: %t", sc.ID, sc.Name, sc.DurationMs, sc.Passed)
		if !sc.Passed {
			t.Errorf("Scenario %s (%s) FAILED: %s", sc.ID, sc.Name, sc.ErrorMessage)
		}
	}

	if report.FailedCount > 0 {
		t.Fatalf("expected 0 scenario failures, got %d", report.FailedCount)
	}

	if !report.AllInvariantsSatisfied {
		t.Fatalf("financial invariants violated: %+v", report.FinancialSummary)
	}

	// Persist artifacts
	rootDir := findTestRepoRoot()
	jsonPath, csvPath, err := report.SaveArtifacts(filepath.Join(rootDir, "artifacts"))
	if err != nil {
		t.Fatalf("save artifacts failed: %v", err)
	}
	t.Logf("Saved Artifacts:\n  JSON: %s\n  CSV: %s\n  SHA256: %s", jsonPath, csvPath, report.ReportSHA256)
}
