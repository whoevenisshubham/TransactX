package experiments_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
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

	lowerURL := strings.ToLower(dbURL)
	if strings.Contains(lowerURL, "/transactx") && !strings.Contains(lowerURL, "/transactx_") {
		t.Fatal("SAFETY VIOLATION: DATABASE_URL points to protected source database transactx")
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
	defer func() {
		if closeErr := harness.Close(); closeErr != nil {
			t.Errorf("harness.Close failed: %v", closeErr)
		}
	}()

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

func TestCrashMatrix_FinancialInvariants_DetectsParticipantPayloadMismatch(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping test")
	}

	lowerURL := strings.ToLower(dbURL)
	if strings.Contains(lowerURL, "/transactx") && !strings.Contains(lowerURL, "/transactx_") {
		t.Fatal("SAFETY VIOLATION: DATABASE_URL points to protected source database transactx")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := database.NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	harness, err := experiments.NewCrashMatrixHarness(pool, 42)
	if err != nil {
		t.Fatalf("initialize harness: %v", err)
	}
	defer func() {
		if closeErr := harness.Close(); closeErr != nil {
			t.Errorf("harness.Close failed: %v", closeErr)
		}
	}()

	// Initial check: baseline must be clean
	baseInv := harness.VerifyFinancialInvariants(ctx)
	if !baseInv.ParticipantUnique {
		t.Fatalf("baseline ParticipantUnique is already false: violations=%d", baseInv.TotalViolations)
	}

	// Deliberately inject a corrupted mismatch: payment_bank_operations vs bank_a.operations
	testPID := uuid.New()
	testOpID := uuid.New()
	testAccID := uuid.New()
	var bankAID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM banks WHERE code = 'BANK-A'`).Scan(&bankAID); err != nil {
		t.Fatalf("query bank a id: %v", err)
	}

	// Insert parent payment
	if _, err := pool.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state, source_bank_id, destination_bank_id)
		SELECT $1, u.id, a1.id, a2.id, 500, 'INR', 'ONLINE', 'PROCESSING', $2, $2
		FROM users u, accounts a1, accounts a2
		WHERE a1.id != a2.id LIMIT 1`, testPID, bankAID); err != nil {
		t.Fatalf("insert test payment: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM bank_a.operations WHERE operation_id = $1`, testOpID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM payment_bank_operations WHERE operation_id = $1`, testOpID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM payments WHERE id = $1`, testPID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM bank_a.accounts WHERE id = $1`, testAccID)
	}()

	// Insert central journal record with amount 500
	if _, err := pool.Exec(ctx, `
		INSERT INTO payment_bank_operations (id, payment_id, bank_id, operation_id, operation_type, status, amount_paise, currency)
		VALUES ($1, $2, $3, $4, 'HOLD', 'COMMITTED', 500, 'INR')`,
		uuid.New(), testPID, bankAID, testOpID); err != nil {
		t.Fatalf("insert central journal: %v", err)
	}

	// Insert participant account in bank_a
	_, _ = pool.Exec(ctx, `
		INSERT INTO bank_a.accounts (id, account_number, balance_paise, version, status)
		VALUES ($1, $2, 10000, 0, 'ACTIVE')`, testAccID, "ACC-TEST-"+testAccID.String()[:8])

	// Insert participant operation in bank_a with MISMATCHED amount (999 != 500)
	if _, err := pool.Exec(ctx, `
		INSERT INTO bank_a.operations (id, payment_id, operation_id, bank_id, idempotency_key, operation_type, status, account_id, amount_paise, currency)
		VALUES ($1, $2, $3, 'BANK-A', $4, 'HOLD', 'CONFIRMED', $5, 999, 'INR')`,
		uuid.New(), testPID, testOpID, testOpID.String(), testAccID); err != nil {
		t.Fatalf("insert bank_a operation: %v", err)
	}

	// Verify that harness detects the fault!
	faultInv := harness.VerifyFinancialInvariants(ctx)
	if faultInv.ParticipantUnique {
		t.Error("expected ParticipantUnique to be FALSE due to payload amount mismatch, but got true")
	}
	if faultInv.TotalViolations == 0 {
		t.Error("expected TotalViolations > 0 due to payload amount mismatch")
	}

	// Clean up injected mismatch
	_, _ = pool.Exec(ctx, `DELETE FROM bank_a.operations WHERE operation_id = $1`, testOpID)
	_, _ = pool.Exec(ctx, `DELETE FROM payment_bank_operations WHERE operation_id = $1`, testOpID)

	// Invariant must restore to clean
	restoredInv := harness.VerifyFinancialInvariants(ctx)
	if !restoredInv.ParticipantUnique {
		t.Errorf("expected ParticipantUnique to restore to TRUE, got false with %d violations", restoredInv.TotalViolations)
	}
}

func TestCrashMatrix_FinancialInvariants_DetectsDebitCreditImbalance(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping test")
	}

	lowerURL := strings.ToLower(dbURL)
	if strings.Contains(lowerURL, "/transactx") && !strings.Contains(lowerURL, "/transactx_") {
		t.Fatal("SAFETY VIOLATION: DATABASE_URL points to protected source database transactx")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := database.NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	harness, err := experiments.NewCrashMatrixHarness(pool, 42)
	if err != nil {
		t.Fatalf("initialize harness: %v", err)
	}
	defer func() {
		if closeErr := harness.Close(); closeErr != nil {
			t.Errorf("harness.Close failed: %v", closeErr)
		}
	}()

	baseInv := harness.VerifyFinancialInvariants(ctx)
	if !baseInv.DebitCreditConservation {
		t.Fatalf("baseline DebitCreditConservation is already false: violations=%d", baseInv.TotalViolations)
	}

	// Deliberately insert an unbalanced ledger entry (debit with no matching credit)
	testPaymentID := uuid.New()
	testTxID := uuid.New()
	testEntryID := uuid.New()
	var testAccID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts LIMIT 1`).Scan(&testAccID); err != nil {
		t.Fatalf("query test account: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state)
		SELECT $1, u.id, a1.id, a2.id, 77777, 'INR', 'ONLINE', 'COMPLETED'
		FROM users u, accounts a1, accounts a2
		WHERE a1.id != a2.id LIMIT 1`, testPaymentID); err != nil {
		t.Fatalf("insert test payment: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM payments WHERE id = $1`, testPaymentID)
	}()

	if _, err := pool.Exec(ctx, `
		INSERT INTO ledger_transactions (id, payment_id)
		VALUES ($1, $2)`, testTxID, testPaymentID); err != nil {
		t.Fatalf("insert test ledger transaction: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM ledger_entries WHERE id = $1`, testEntryID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM ledger_transactions WHERE id = $1`, testTxID)
	}()

	if _, err := pool.Exec(ctx, `
		INSERT INTO ledger_entries (id, ledger_transaction_id, account_id, entry_type, amount_paise)
		VALUES ($1, $2, $3, 'DEBIT', 77777)`, testEntryID, testTxID, testAccID); err != nil {
		t.Fatalf("insert unbalanced debit: %v", err)
	}

	// Verify that harness detects the fault!
	faultInv := harness.VerifyFinancialInvariants(ctx)
	if faultInv.DebitCreditConservation {
		t.Error("expected DebitCreditConservation to be FALSE due to unbalanced debit, but got true")
	}
	if faultInv.TotalViolations == 0 {
		t.Error("expected TotalViolations > 0 due to unbalanced debit")
	}

	// Clean up
	_, _ = pool.Exec(ctx, `DELETE FROM ledger_entries WHERE id = $1`, testEntryID)
	_, _ = pool.Exec(ctx, `DELETE FROM ledger_transactions WHERE id = $1`, testTxID)

	restoredInv := harness.VerifyFinancialInvariants(ctx)
	if !restoredInv.DebitCreditConservation {
		t.Errorf("expected DebitCreditConservation to restore to TRUE, got false with %d violations", restoredInv.TotalViolations)
	}
}

func TestCrashMatrix_Scenario_FailsOnInjectedCancelledContext(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping test")
	}

	lowerURL := strings.ToLower(dbURL)
	if strings.Contains(lowerURL, "/transactx") && !strings.Contains(lowerURL, "/transactx_") {
		t.Fatal("SAFETY VIOLATION: DATABASE_URL points to protected source database transactx")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := database.NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	harness, err := experiments.NewCrashMatrixHarness(pool, 42)
	if err != nil {
		t.Fatalf("initialize harness: %v", err)
	}
	defer func() {
		if closeErr := harness.Close(); closeErr != nil {
			t.Errorf("harness.Close failed: %v", closeErr)
		}
	}()

	// Create an already-cancelled context
	cancelledCtx, cancelNow := context.WithCancel(ctx)
	cancelNow()

	// Running a scenario with a cancelled context must fail closed, report Passed=false and non-empty ErrorMessage
	res := harness.RunScenario(cancelledCtx, "REC-01")
	if res.Passed {
		t.Error("expected scenario to fail on cancelled context, but passed")
	}
	if res.ErrorMessage == "" {
		t.Error("expected non-empty ErrorMessage when context is cancelled")
	}
	t.Logf("Verified fail-closed behavior on cancelled context: error=%s", res.ErrorMessage)
}
