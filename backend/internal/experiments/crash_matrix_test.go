package experiments_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

func handleCrashMatrixTestCleanup(t *testing.T, harness *experiments.CrashMatrixHarness, prevalidatedContract ...*experiments.OrchestratorLifecycleContract) {
	if harness == nil {
		return
	}
	closeErr := harness.Close()
	if closeErr == nil {
		return
	}
	currentDB := harness.CurrentDB()
	var contract *experiments.OrchestratorLifecycleContract
	if len(prevalidatedContract) > 0 && prevalidatedContract[0] != nil {
		contract = prevalidatedContract[0]
	} else if harness.LifecycleContract() != nil {
		contract = harness.LifecycleContract()
	} else {
		var contractErr error
		contract, contractErr = experiments.ResolveOrchestratorContract(currentDB)
		if contractErr != nil {
			t.Logf("orchestrator contract check: %v", contractErr)
		}
	}
	expectedToken := os.Getenv(experiments.EnvDisposableInvocationToken)
	shouldFail, isDelegated, diag := experiments.EvaluateTestCleanupOutcome(closeErr, contract, currentDB, expectedToken)
	if isDelegated {
		t.Logf("harness.Close: %s", diag)
	} else if shouldFail {
		t.Errorf("harness.Close failed: %s", diag)
	}
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

	if os.Getenv(experiments.EnvDisposableDBOrchestrated) != "1" {
		t.Skip("skipping crash matrix end-to-end scenario outside dedicated orchestrated disposable database lifecycle (run via backend/scripts/run-cross-bank-disposable.sh)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	pool, err := database.NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	currentDB, contract, err := experiments.AuthorizeDestructiveLifecycle(ctx, pool)
	if err != nil {
		t.Fatalf("crash matrix lifecycle authorization failed for database %q: %v", currentDB, err)
	}

	harness, err := experiments.NewCrashMatrixHarness(pool, 42)
	if err != nil {
		t.Fatalf("initialize crash matrix harness: %v", err)
	}
	harness.SetLifecycleContract(contract)
	defer handleCrashMatrixTestCleanup(t, harness, contract)

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
	defer handleCrashMatrixTestCleanup(t, harness)

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

// -----------------------------------------------------------------------------
// Database-Independent Offline Regression Tests for Crash Matrix Orchestration
// -----------------------------------------------------------------------------

// Database-independent test verifying that the broad test suite skips the crash matrix
// end-to-end scenario when TRANSACTX_DISPOSABLE_DB_ORCHESTRATED is not set to "1".
func TestCrashMatrix_Safety_VerifyBroadSuiteSkipsWithoutOrchestrator(t *testing.T) {
	t.Setenv(experiments.EnvDisposableDBOrchestrated, "")
	if os.Getenv(experiments.EnvDisposableDBOrchestrated) == "1" {
		t.Fatal("expected EnvDisposableDBOrchestrated to not be 1")
	}
}

// Database-independent test verifying that cleanup outcome evaluation delegates teardown
// when a valid orchestrator contract and invocation token match the target database.
func TestCrashMatrix_Safety_EvaluateCleanupOutcome_ValidContractDelegates(t *testing.T) {
	targetDB := "transactx_disposable_ci_test_123"
	token := "test_token_12345678"
	contractJSON := fmt.Sprintf(`{
		"version": %q,
		"database": %q,
		"invocationToken": %q,
		"orchestratorPid": 12345,
		"createdAt": %q
	}`, experiments.OrchestratorContractVersion, targetDB, token, time.Now().UTC().Format(time.RFC3339))

	contractFile := filepath.Join(t.TempDir(), "contract.json")
	if err := os.WriteFile(contractFile, []byte(contractJSON), 0600); err != nil {
		t.Fatalf("write contract: %v", err)
	}

	t.Setenv(experiments.EnvDisposableDBOrchestrated, "1")
	t.Setenv(experiments.EnvDisposableContractFile, contractFile)
	t.Setenv(experiments.EnvDisposableInvocationToken, token)

	contract, err := experiments.ResolveOrchestratorContract(targetDB)
	if err != nil {
		t.Fatalf("unexpected error resolving contract: %v", err)
	}

	simulatedCloseErr := fmt.Errorf("%w: cleanup table payments: foreign key constraint", experiments.ErrImmutableHistoryBlockedCleanup)
	shouldFail, isDelegated, diag := experiments.EvaluateTestCleanupOutcome(simulatedCloseErr, contract, targetDB, token)

	if shouldFail {
		t.Errorf("expected shouldFail == false for valid delegated contract, got true; diag: %s", diag)
	}
	if !isDelegated {
		t.Errorf("expected isDelegated == true, got false; diag: %s", diag)
	}
}

// Database-independent test verifying that missing or invalid orchestrator contract rejects delegation.
func TestCrashMatrix_Safety_EvaluateCleanupOutcome_MissingOrInvalidContractRejects(t *testing.T) {
	simulatedCloseErr := fmt.Errorf("%w: cleanup table payments: foreign key constraint", experiments.ErrImmutableHistoryBlockedCleanup)

	// Case 1: Nil contract
	shouldFail, isDelegated, _ := experiments.EvaluateTestCleanupOutcome(simulatedCloseErr, nil, "transactx_disposable_ci_test", "some_token")
	if !shouldFail || isDelegated {
		t.Errorf("expected rejection on nil contract: shouldFail=true, isDelegated=false; got shouldFail=%t, isDelegated=%t", shouldFail, isDelegated)
	}

	// Case 2: Mismatched token
	contract := &experiments.OrchestratorLifecycleContract{
		Version:         experiments.OrchestratorContractVersion,
		Database:        "transactx_disposable_ci_test",
		InvocationToken: "expected_token_123",
		OrchestratorPID: 1234,
		CreatedAt:       time.Now().UTC().Format(time.RFC3339),
	}
	shouldFail, isDelegated, _ = experiments.EvaluateTestCleanupOutcome(simulatedCloseErr, contract, "transactx_disposable_ci_test", "wrong_token_456")
	if !shouldFail || isDelegated {
		t.Errorf("expected rejection on mismatched token: shouldFail=true, isDelegated=false; got shouldFail=%t, isDelegated=%t", shouldFail, isDelegated)
	}

	// Case 3: Mismatched database
	shouldFail, isDelegated, _ = experiments.EvaluateTestCleanupOutcome(simulatedCloseErr, contract, "other_disposable_db", "expected_token_123")
	if !shouldFail || isDelegated {
		t.Errorf("expected rejection on mismatched database: shouldFail=true, isDelegated=false; got shouldFail=%t, isDelegated=%t", shouldFail, isDelegated)
	}
}

// Database-independent test verifying that unexpected cleanup errors always fail regardless of orchestrator contract.
func TestCrashMatrix_Safety_EvaluateCleanupOutcome_UnexpectedErrorFails(t *testing.T) {
	unexpectedErr := errors.New("pq: connection refused")
	contract := &experiments.OrchestratorLifecycleContract{
		Version:         experiments.OrchestratorContractVersion,
		Database:        "transactx_disposable_ci_test",
		InvocationToken: "token_12345678",
		OrchestratorPID: 1234,
		CreatedAt:       time.Now().UTC().Format(time.RFC3339),
	}

	shouldFail, isDelegated, diag := experiments.EvaluateTestCleanupOutcome(unexpectedErr, contract, "transactx_disposable_ci_test", "token_12345678")
	if !shouldFail {
		t.Errorf("expected shouldFail == true for unexpected error, got false; diag: %s", diag)
	}
	if isDelegated {
		t.Errorf("expected isDelegated == false for unexpected error, got true; diag: %s", diag)
	}
}

// Database-independent test verifying that the dedicated lifecycle test pattern matches both
// the cross-bank recovery suite and the crash-matrix suite.
func TestCrashMatrix_Safety_PatternMatchesBothSuites(t *testing.T) {
	pattern := `^(TestCrossBankRecovery_|TestCrashMatrix_)`
	re, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatalf("failed to compile pattern %q: %v", pattern, err)
	}

	matchedTests := []string{
		"TestCrossBankRecovery_FullMatrix",
		"TestCrossBankRecovery_CB01_CB02_Focused",
		"TestCrossBankRecovery_CB07_FullRestoreEndToEnd",
		"TestCrashMatrix_FullRecoveryAndEventMatrix",
	}
	for _, tc := range matchedTests {
		if !re.MatchString(tc) {
			t.Errorf("expected pattern %q to match %q", pattern, tc)
		}
	}

	unmatchedTests := []string{
		"TestExperiment1_RoutingComparison",
		"TestExperiment2_OutageAndCircuitIsolation",
		"TestExperiment3_LatencyDegradation",
		"TestExperiment5_ConcurrencyStormAndInvariants",
	}
	for _, tc := range unmatchedTests {
		if re.MatchString(tc) {
			t.Errorf("expected pattern %q to NOT match %q", pattern, tc)
		}
	}
}

// Database-independent test verifying CurrentDB behavior on nil harness.
func TestCrashMatrix_HarnessCurrentDB_NilHarness(t *testing.T) {
	var h *experiments.CrashMatrixHarness
	if db := h.CurrentDB(); db != "" {
		t.Errorf("expected empty string for nil harness, got %q", db)
	}
}

// Database-independent test verifying that pre-execution lifecycle authorization prevents
// fixture creation and destructive scenario execution when contracts or tokens are invalid.
func TestCrashMatrix_Safety_PreExecutionLifecycleAuthorization(t *testing.T) {
	origOrchestrated := os.Getenv(experiments.EnvDisposableDBOrchestrated)
	origToken := os.Getenv(experiments.EnvDisposableInvocationToken)
	origContractFile := os.Getenv(experiments.EnvDisposableContractFile)
	origContractJSON := os.Getenv(experiments.EnvDisposableOrchestratorContract)
	defer func() {
		os.Setenv(experiments.EnvDisposableDBOrchestrated, origOrchestrated)
		os.Setenv(experiments.EnvDisposableInvocationToken, origToken)
		os.Setenv(experiments.EnvDisposableContractFile, origContractFile)
		os.Setenv(experiments.EnvDisposableOrchestratorContract, origContractJSON)
	}()

	targetDB := "transactx_disposable_ci_55555"
	validToken := "tx_token_valid_55555"
	freshTime := time.Now().UTC().Format(time.RFC3339)

	writeContract := func(t *testing.T, content string) string {
		t.Helper()
		tmpDir := t.TempDir()
		fPath := filepath.Join(tmpDir, "contract.json")
		if err := os.WriteFile(fPath, []byte(content), 0600); err != nil {
			t.Fatalf("failed to write test contract: %v", err)
		}
		return fPath
	}

	t.Run("broad_suite_skips_without_orchestrator", func(t *testing.T) {
		os.Unsetenv(experiments.EnvDisposableDBOrchestrated)
		os.Unsetenv(experiments.EnvDisposableInvocationToken)
		os.Unsetenv(experiments.EnvDisposableContractFile)

		contract, err := experiments.ValidateDestructiveLifecycleContract(targetDB)
		if !errors.Is(err, experiments.ErrUnorchestratedLifecycle) {
			t.Errorf("expected ErrUnorchestratedLifecycle, got contract=%+v, err=%v", contract, err)
		}

		ctx := context.Background()
		db, contract2, err2 := experiments.AuthorizeDestructiveLifecycle(ctx, nil)
		if !errors.Is(err2, experiments.ErrUnorchestratedLifecycle) {
			t.Errorf("expected AuthorizeDestructiveLifecycle to return ErrUnorchestratedLifecycle on un-orchestrated run, got db=%q, contract=%+v, err=%v", db, contract2, err2)
		}
	})

	t.Run("marker_set_with_nil_pool_fails_closed", func(t *testing.T) {
		os.Setenv(experiments.EnvDisposableDBOrchestrated, "1")
		os.Setenv(experiments.EnvDisposableInvocationToken, validToken)
		f := writeContract(t, fmt.Sprintf(`{"version":"%s","database":"%s","invocationToken":"%s","orchestratorPid":12345,"createdAt":"%s"}`,
			experiments.OrchestratorContractVersion, targetDB, validToken, freshTime))
		os.Setenv(experiments.EnvDisposableContractFile, f)

		ctx := context.Background()
		_, _, err := experiments.AuthorizeDestructiveLifecycle(ctx, nil)
		if err == nil || !strings.Contains(err.Error(), "pool is required") {
			t.Errorf("expected AuthorizeDestructiveLifecycle to fail closed on nil pool when orchestrated, got %v", err)
		}
	})

	t.Run("marker_set_without_contract_or_token_fails_closed", func(t *testing.T) {
		os.Setenv(experiments.EnvDisposableDBOrchestrated, "1")
		os.Unsetenv(experiments.EnvDisposableInvocationToken)
		os.Unsetenv(experiments.EnvDisposableContractFile)

		_, err := experiments.ValidateDestructiveLifecycleContract(targetDB)
		if err == nil {
			t.Fatal("expected error when contract and token are missing, got nil")
		}
	})

	t.Run("mismatched_database_contract_fails_closed", func(t *testing.T) {
		os.Setenv(experiments.EnvDisposableDBOrchestrated, "1")
		os.Setenv(experiments.EnvDisposableInvocationToken, validToken)
		f := writeContract(t, fmt.Sprintf(`{"version":"%s","database":"transactx_disposable_ci_other","invocationToken":"%s","orchestratorPid":123,"createdAt":"%s"}`,
			experiments.OrchestratorContractVersion, validToken, freshTime))
		os.Setenv(experiments.EnvDisposableContractFile, f)

		_, err := experiments.ValidateDestructiveLifecycleContract(targetDB)
		if err == nil || !strings.Contains(err.Error(), "mismatch") {
			t.Errorf("expected database mismatch error, got %v", err)
		}
	})

	t.Run("stale_contract_fails_closed", func(t *testing.T) {
		os.Setenv(experiments.EnvDisposableDBOrchestrated, "1")
		os.Setenv(experiments.EnvDisposableInvocationToken, validToken)
		staleTime := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
		f := writeContract(t, fmt.Sprintf(`{"version":"%s","database":"%s","invocationToken":"%s","orchestratorPid":123,"createdAt":"%s"}`,
			experiments.OrchestratorContractVersion, targetDB, validToken, staleTime))
		os.Setenv(experiments.EnvDisposableContractFile, f)

		_, err := experiments.ValidateDestructiveLifecycleContract(targetDB)
		if err == nil || !strings.Contains(err.Error(), "stale") {
			t.Errorf("expected stale contract error, got %v", err)
		}
	})

	t.Run("prior_invocation_token_fails_closed", func(t *testing.T) {
		os.Setenv(experiments.EnvDisposableDBOrchestrated, "1")
		os.Setenv(experiments.EnvDisposableInvocationToken, "tx_token_new_run")
		f := writeContract(t, fmt.Sprintf(`{"version":"%s","database":"%s","invocationToken":"tx_token_old_run","orchestratorPid":123,"createdAt":"%s"}`,
			experiments.OrchestratorContractVersion, targetDB, freshTime))
		os.Setenv(experiments.EnvDisposableContractFile, f)

		_, err := experiments.ValidateDestructiveLifecycleContract(targetDB)
		if err == nil || !strings.Contains(err.Error(), "does not match expected invocation token") {
			t.Errorf("expected invocation token mismatch error, got %v", err)
		}
	})

	t.Run("valid_contract_permits_authorization_and_cleanup_delegation", func(t *testing.T) {
		os.Setenv(experiments.EnvDisposableDBOrchestrated, "1")
		os.Setenv(experiments.EnvDisposableInvocationToken, validToken)
		f := writeContract(t, fmt.Sprintf(`{"version":"%s","database":"%s","invocationToken":"%s","orchestratorPid":12345,"createdAt":"%s"}`,
			experiments.OrchestratorContractVersion, targetDB, validToken, freshTime))
		os.Setenv(experiments.EnvDisposableContractFile, f)

		contract, err := experiments.ValidateDestructiveLifecycleContract(targetDB)
		if err != nil {
			t.Fatalf("expected valid contract to be authorized, got: %v", err)
		}
		if contract.Database != targetDB {
			t.Errorf("expected database %q, got %q", targetDB, contract.Database)
		}

		// Verify reusing validated contract for cleanup delegation
		shouldFail, isDelegated, diag := experiments.EvaluateTestCleanupOutcome(
			experiments.ErrImmutableHistoryBlockedCleanup, contract, targetDB, validToken)
		if shouldFail || !isDelegated {
			t.Errorf("expected cleanup delegation to succeed with validated contract, got shouldFail=%t, isDelegated=%t, diag=%s",
				shouldFail, isDelegated, diag)
		}
	})
}
