package experiments_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/transactx/backend/internal/database"
	"github.com/transactx/backend/internal/experiments"
)

func findArtifactsDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "artifacts"
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return filepath.Join(dir, "artifacts")
		}
		if _, err := os.Stat(filepath.Join(dir, "backend", "go.mod")); err == nil {
			return filepath.Join(dir, "artifacts")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Join("..", "..", "artifacts")
}

func TestCrashMatrix_FullRecoveryAndEventMatrix(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping live crash matrix test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	pool, err := database.NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	harness, err := experiments.NewCrashMatrixHarness(pool, 42)
	if err != nil {
		t.Fatalf("initialize crash matrix harness: %v", err)
	}
	defer harness.Close()

	t.Log("Starting Crash Matrix & Event Restart Verification (B12)...")
	report, err := harness.RunAllScenarios(ctx)
	if err != nil {
		t.Fatalf("RunAllScenarios returned error: %v", err)
	}

	t.Logf("Executed %d scenarios: %d passed, %d failed", report.TotalScenarios, report.PassedCount, report.FailedCount)

	// Verify all 20 scenarios passed
	if report.TotalScenarios != 20 {
		t.Errorf("expected 20 scenarios, executed %d", report.TotalScenarios)
	}

	for _, s := range report.Scenarios {
		if !s.Passed {
			t.Errorf("Scenario [%s] %s FAILED: %s", s.ID, s.Name, s.ErrorMessage)
			for _, inv := range s.Invariants {
				if !inv.Passed {
					t.Errorf("  - Invariant failed: %s (%s)", inv.InvariantName, inv.Details)
				}
			}
		} else {
			t.Logf("PASS [%s] %s (%.2f ms, state=%s)", s.ID, s.Name, s.DurationMs, s.ObservedState)
		}
	}

	if report.FailedCount > 0 {
		t.Fatalf("%d scenarios failed", report.FailedCount)
	}

	if !report.AllInvariantsSatisfied {
		t.Error("report.AllInvariantsSatisfied is false")
	}

	// Verify overall financial invariants
	fs := report.FinancialSummary
	t.Logf("Financial Invariants Summary: NonnegativeBalances=%t, DebitCreditConservation=%t, SingleLedger=%t, Violations=%d",
		fs.NonnegativeBalances, fs.DebitCreditConservation, fs.SingleLedgerPerPayment, fs.TotalViolations)

	if !fs.NonnegativeBalances {
		t.Error("financial invariant violated: negative balance found")
	}
	if !fs.DebitCreditConservation {
		t.Errorf("financial invariant violated: debit/credit not conserved (debits=%d, credits=%d)",
			fs.TotalLedgerDebitsPaise, fs.TotalLedgerCreditsPaise)
	}
	if !fs.SingleLedgerPerPayment {
		t.Error("financial invariant violated: duplicate ledger transaction per payment found")
	}
	if !fs.IdempotencyUnique {
		t.Error("financial invariant violated: duplicate idempotency keys found")
	}
	if !fs.ParticipantUnique {
		t.Error("financial invariant violated: duplicate participant operations found")
	}
	if fs.TotalViolations > 0 {
		t.Errorf("total financial violations: %d", fs.TotalViolations)
	}

	// Persist verifiable artifacts
	artifactsDir := findArtifactsDir()
	jsonPath, csvPath, err := report.SaveArtifacts(artifactsDir)
	if err != nil {
		t.Fatalf("save artifacts: %v", err)
	}
	t.Logf("Saved crash matrix JSON artifact: %s", jsonPath)
	t.Logf("Saved crash matrix CSV artifact:  %s", csvPath)
	t.Logf("Report SHA256 digest: %s", report.ReportSHA256)

	if report.ReportSHA256 == "" {
		t.Error("expected non-empty ReportSHA256")
	}
}
