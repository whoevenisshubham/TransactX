package experiments

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func handleTestCleanup(t *testing.T, harness *CrossBankRecoveryHarness) {
	if harness == nil {
		return
	}
	closeErr := harness.Close()
	if closeErr == nil {
		return
	}
	currentDB := harness.CurrentDB()
	contract, contractErr := ResolveOrchestratorContract(currentDB)
	if contractErr != nil {
		t.Logf("orchestrator contract check: %v", contractErr)
	}
	expectedToken := os.Getenv(EnvDisposableInvocationToken)
	shouldFail, isDelegated, diag := EvaluateTestCleanupOutcome(closeErr, contract, currentDB, expectedToken)
	if isDelegated {
		t.Logf("harness.Close: %s", diag)
	} else if shouldFail {
		t.Errorf("harness.Close failed: %s", diag)
	}
}

func TestCrossBankRecovery_FullMatrix(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping database-backed cross-bank recovery matrix test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := database.NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	currentDB, err := VerifyPoolIsDisposable(ctx, pool)
	if err != nil {
		t.Skipf("skipping database-backed cross-bank recovery test: database %q is not disposable: %v (run via backend/scripts/run-cross-bank-disposable.sh)", currentDB, err)
	}

	harness, err := NewCrossBankRecoveryHarness(pool, dbURL)
	if err != nil {
		t.Fatalf("init cross-bank recovery harness: %v", err)
	}
	defer handleTestCleanup(t, harness)

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

func TestCrossBankRecovery_ScenarioMetadata_NoProcessKillClaims(t *testing.T) {
	// Unit test: Verify that CB-01 and CB-02 report accurate in-process recovery metadata
	// and do not make false claims of operating-system process termination.
	cancCtx, cancel := context.WithCancel(context.Background())
	cancel()

	harness := &CrossBankRecoveryHarness{}
	res1 := harness.RunCB01_SeededRecoveryAfterJournalBeforeDispatch(cancCtx)
	if res1.ID != "CB-01" {
		t.Errorf("expected ID CB-01, got %s", res1.ID)
	}
	if strings.Contains(strings.ToLower(res1.Name), "killed") {
		t.Errorf("CB-01 Name should not claim process killed: %s", res1.Name)
	}
	if strings.Contains(strings.ToLower(res1.FaultBoundary), "killed") {
		t.Errorf("CB-01 FaultBoundary should not claim process killed: %s", res1.FaultBoundary)
	}
	if !strings.Contains(res1.Name, "In-process recovery") {
		t.Errorf("CB-01 Name should accurately reflect in-process recovery: %s", res1.Name)
	}

	res2 := harness.RunCB02_SeededRecoveryAfterHoldBeforeCredit(cancCtx)
	if res2.ID != "CB-02" {
		t.Errorf("expected ID CB-02, got %s", res2.ID)
	}
	if strings.Contains(strings.ToLower(res2.Name), "killed") {
		t.Errorf("CB-02 Name should not claim process killed: %s", res2.Name)
	}
	if strings.Contains(strings.ToLower(res2.FaultBoundary), "killed") {
		t.Errorf("CB-02 FaultBoundary should not claim process killed: %s", res2.FaultBoundary)
	}
	if !strings.Contains(res2.Name, "In-process recovery") {
		t.Errorf("CB-02 Name should accurately reflect in-process recovery: %s", res2.Name)
	}
}

func TestCrossBankRecovery_CB01_CB02_Focused(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping database-backed cross-bank test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	pool, err := database.NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	currentDB, err := VerifyPoolIsDisposable(ctx, pool)
	if err != nil {
		t.Skipf("skipping database-backed cross-bank recovery test: database %q is not disposable: %v (run via backend/scripts/run-cross-bank-disposable.sh)", currentDB, err)
	}

	harness, err := NewCrossBankRecoveryHarness(pool, dbURL)
	if err != nil {
		t.Fatalf("init cross-bank recovery harness: %v", err)
	}
	defer handleTestCleanup(t, harness)

	t.Run("CB-01_SeededRecoveryAfterJournalBeforeDispatch", func(t *testing.T) {
		res := harness.RunCB01_SeededRecoveryAfterJournalBeforeDispatch(ctx)
		if !res.Passed {
			t.Fatalf("CB-01 failed: %s", res.ErrorMessage)
		}
		if res.ObservedState != "COMPLETED" {
			t.Fatalf("expected COMPLETED, got %s", res.ObservedState)
		}
		for _, inv := range res.Invariants {
			if !inv.Passed {
				t.Errorf("invariant %s failed: %s", inv.InvariantName, inv.Details)
			}
		}
	})

	t.Run("CB-01_CancelledContext_FailsClosed", func(t *testing.T) {
		cancCtx, canc := context.WithCancel(ctx)
		canc()
		res := harness.RunCB01_SeededRecoveryAfterJournalBeforeDispatch(cancCtx)
		if res.Passed {
			t.Fatalf("expected CB-01 to fail closed on cancelled context, but passed")
		}
		if res.ErrorMessage == "" {
			t.Fatalf("expected error message on cancelled context failure")
		}
	})

	t.Run("CB-02_SeededRecoveryAfterHoldBeforeCredit", func(t *testing.T) {
		res := harness.RunCB02_SeededRecoveryAfterHoldBeforeCredit(ctx)
		if !res.Passed {
			t.Fatalf("CB-02 failed: %s", res.ErrorMessage)
		}
		if res.ObservedState != "COMPLETED" {
			t.Fatalf("expected COMPLETED, got %s", res.ObservedState)
		}
		for _, cp := range res.Checkpoints {
			if strings.Contains(strings.ToLower(cp), "killed") {
				t.Errorf("checkpoint %s contains 'killed' claim", cp)
			}
		}
		for _, inv := range res.Invariants {
			if !inv.Passed {
				t.Errorf("invariant %s failed: %s", inv.InvariantName, inv.Details)
			}
		}
	})

	t.Run("CB-02_CancelledContext_FailsClosed", func(t *testing.T) {
		cancCtx, canc := context.WithCancel(ctx)
		canc()
		res := harness.RunCB02_SeededRecoveryAfterHoldBeforeCredit(cancCtx)
		if res.Passed {
			t.Fatalf("expected CB-02 to fail closed on cancelled context, but passed")
		}
		if res.ErrorMessage == "" {
			t.Fatalf("expected error message on cancelled context failure")
		}
	})
}

type mockBackupRestoreRunner struct {
	dumpFn    func(ctx context.Context, host, port, user, password, srcDB, destFile string) ([]byte, error)
	dropFn    func(ctx context.Context, host, port, user, password, dbName string) ([]byte, error)
	createFn  func(ctx context.Context, host, port, user, password, dbName string) ([]byte, error)
	restoreFn func(ctx context.Context, host, port, user, password, dbName, srcFile string) ([]byte, error)
}

func (m *mockBackupRestoreRunner) RunPgDump(ctx context.Context, host, port, user, password, srcDB, destFile string) ([]byte, error) {
	if m.dumpFn != nil {
		return m.dumpFn(ctx, host, port, user, password, srcDB, destFile)
	}
	return nil, nil
}

func (m *mockBackupRestoreRunner) RunDropDB(ctx context.Context, host, port, user, password, dbName string) ([]byte, error) {
	if m.dropFn != nil {
		return m.dropFn(ctx, host, port, user, password, dbName)
	}
	return nil, nil
}

func (m *mockBackupRestoreRunner) RunCreateDB(ctx context.Context, host, port, user, password, dbName string) ([]byte, error) {
	if m.createFn != nil {
		return m.createFn(ctx, host, port, user, password, dbName)
	}
	return nil, nil
}

func (m *mockBackupRestoreRunner) RunPgRestore(ctx context.Context, host, port, user, password, dbName, srcFile string) ([]byte, error) {
	if m.restoreFn != nil {
		return m.restoreFn(ctx, host, port, user, password, dbName, srcFile)
	}
	return nil, nil
}

func TestCrossBankRecovery_CB07_DisposableDBSafetyValidation(t *testing.T) {
	// Unit test: Verify that target database validation refuses non-disposable,
	// system, source, or improperly prefixed database names.
	cases := []struct {
		targetDB  string
		srcDB     string
		shouldErr bool
	}{
		{"", "transactx", true},
		{"transactx", "transactx", true},
		{"postgres", "transactx", true},
		{"template0", "transactx", true},
		{"template1", "transactx", true},
		{"production_db", "transactx", true},
		{"my_custom_db", "transactx", true},
		{"transactx_cb_restore_12345", "transactx", false},
		{"transactx_disposable_test", "transactx", false},
		{"transactx_test_foo", "transactx", false},
	}

	for _, tc := range cases {
		err := validateDisposableDB(tc.targetDB, tc.srcDB)
		if tc.shouldErr && err == nil {
			t.Errorf("validateDisposableDB(%q, %q) expected error, got nil", tc.targetDB, tc.srcDB)
		}
		if !tc.shouldErr && err != nil {
			t.Errorf("validateDisposableDB(%q, %q) unexpected error: %v", tc.targetDB, tc.srcDB, err)
		}
	}
}

func TestCrossBankRecovery_Safety_ValidateDisposableDBName(t *testing.T) {
	// Unit test: Verify that ValidateDisposableDBName strictly rejects source, system,
	// empty, and non-prefixed database names, and only accepts allowed disposable prefixes.
	cases := []struct {
		dbName    string
		shouldErr bool
	}{
		{"", true},
		{"   ", true},
		{"transactx", true},
		{"TRANSACTX", true},
		{"postgres", true},
		{"template0", true},
		{"template1", true},
		{"production", true},
		{"transactx_prod", true},
		{"transactx_backup", true},
		{"transactx_disposable_123", false},
		{"transactx_cb_restore_456", false},
		{"transactx_test_unit", false},
	}

	for _, tc := range cases {
		err := ValidateDisposableDBName(tc.dbName)
		if tc.shouldErr && err == nil {
			t.Errorf("ValidateDisposableDBName(%q) expected error, got nil", tc.dbName)
		}
		if !tc.shouldErr && err != nil {
			t.Errorf("ValidateDisposableDBName(%q) unexpected error: %v", tc.dbName, err)
		}
	}
}

func TestCrossBankRecovery_Safety_HarnessRefusesNilPool(t *testing.T) {
	_, err := NewCrossBankRecoveryHarness(nil, "postgres://dummy")
	if err == nil {
		t.Fatalf("expected NewCrossBankRecoveryHarness to reject nil pool, got nil error")
	}
}

func TestCrossBankRecovery_Safety_VerifyPoolIsDisposable_RejectsNilPool(t *testing.T) {
	_, err := VerifyPoolIsDisposable(context.Background(), nil)
	if err == nil {
		t.Fatalf("expected VerifyPoolIsDisposable to reject nil pool, got nil error")
	}
}

func TestCrossBankRecovery_Safety_HarnessRefusesDestructiveActionsOnNilPool(t *testing.T) {
	// Verify that harness methods fail closed and do not perform unsafe execution
	var h *CrossBankRecoveryHarness
	_, err := h.SeedFixture(context.Background(), "test", 1000)
	if err == nil {
		t.Errorf("expected SeedFixture on nil harness to return error")
	}

	h2 := &CrossBankRecoveryHarness{}
	_, err = h2.SeedFixture(context.Background(), "test", 1000)
	if err == nil {
		t.Errorf("expected SeedFixture on empty harness to return error")
	}

	err = h2.insertPayment(context.Background(), &CrossBankFixture{}, "SUBMITTED")
	if err == nil {
		t.Errorf("expected insertPayment on empty harness to return error")
	}

	// CleanupTestUsers and Close on nil or empty harness must return safely without panic
	if err := h.CleanupTestUsers(context.Background()); err != nil {
		t.Errorf("expected CleanupTestUsers on nil harness to return nil, got: %v", err)
	}
	if err := h2.CleanupTestUsers(context.Background()); err != nil {
		t.Errorf("expected CleanupTestUsers on empty harness to return nil, got: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Errorf("expected Close on nil harness to return nil, got: %v", err)
	}
	if err := h2.Close(); err != nil {
		t.Errorf("expected Close on empty harness to return nil, got: %v", err)
	}
}

func TestCrossBankRecovery_CB07_TemplateCloneDiagnostic_DoesNotSatisfyBackupAcceptance(t *testing.T) {
	// Unit test: Verify that template cloning is classified as a diagnostic clone
	// and explicitly does NOT satisfy CB-07 backup-and-restore acceptance.
	harness := &CrossBankRecoveryHarness{}
	res := harness.RunCB07_TemplateCloneDiagnostic(context.Background())
	if res.Passed {
		t.Fatalf("template clone must never satisfy backup acceptance, but passed")
	}
	if res.ID != "CB-07-CLONE" {
		t.Errorf("expected ID CB-07-CLONE, got %s", res.ID)
	}
	if !strings.Contains(res.ErrorMessage, "does not satisfy backup-and-restore acceptance") {
		t.Errorf("expected error message explaining non-satisfaction of backup acceptance, got: %s", res.ErrorMessage)
	}
}

func TestCrossBankRecovery_CB07_NilPoolFailsClosed(t *testing.T) {
	// Unit test: Verify that CB-07 fails closed when the harness pool is nil.
	harness := &CrossBankRecoveryHarness{}
	res := harness.RunCB07_UnencryptedDumpRestorePendingPayment(context.Background())
	if res.Passed {
		t.Fatalf("expected nil pool to fail, but passed")
	}
	if !strings.Contains(res.ErrorMessage, "database pool is nil") {
		t.Errorf("expected 'database pool is nil' error, got: %s", res.ErrorMessage)
	}
}

func TestCrossBankRecovery_CB07_Metadata_ExplicitlyUnencryptedScope(t *testing.T) {
	// Unit test: Verify that CB-07 metadata does not claim encrypted disaster recovery
	// and accurately reflects non-encrypted PostgreSQL custom-format dump/restore.
	harness := &CrossBankRecoveryHarness{}
	res := harness.RunCB07_UnencryptedDumpRestorePendingPayment(context.Background())
	if res.ID != "CB-07" {
		t.Errorf("expected ID CB-07, got %s", res.ID)
	}
	lowerName := strings.ToLower(res.Name)
	if strings.Contains(lowerName, "encrypted") && !strings.Contains(lowerName, "unencrypted") && !strings.Contains(lowerName, "non-encrypted") {
		t.Errorf("CB-07 Name should not claim encryption: %s", res.Name)
	}
	if !strings.Contains(res.Name, "Unencrypted dump-and-restore") {
		t.Errorf("CB-07 Name should state unencrypted dump-and-restore: %s", res.Name)
	}
	if !strings.Contains(res.FaultBoundary, "does not satisfy encrypted DR acceptance") {
		t.Errorf("CB-07 FaultBoundary should explicitly disclaim encrypted DR acceptance: %s", res.FaultBoundary)
	}
}

func TestCrossBankRecovery_CB07_SeamFailures(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping database-backed CB-07 seam tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := database.NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	currentDB, err := VerifyPoolIsDisposable(ctx, pool)
	if err != nil {
		t.Skipf("skipping database-backed cross-bank recovery test: database %q is not disposable: %v (run via backend/scripts/run-cross-bank-disposable.sh)", currentDB, err)
	}

	harness, err := NewCrossBankRecoveryHarness(pool, dbURL)
	if err != nil {
		t.Fatalf("init harness: %v", err)
	}
	defer handleTestCleanup(t, harness)

	t.Run("BackupCommandFailure", func(t *testing.T) {
		harness.SetBackupRestoreRunner(&mockBackupRestoreRunner{
			dumpFn: func(ctx context.Context, host, port, user, password, srcDB, destFile string) ([]byte, error) {
				return []byte("pg_dump: connection to server failed"), fmt.Errorf("exit status 1")
			},
		})
		defer harness.SetBackupRestoreRunner(nil)

		res := harness.RunCB07_RestoreFromDisposableBackupPendingPayment(ctx)
		if res.Passed {
			t.Fatalf("expected CB-07 to fail when backup fails, but passed")
		}
		if !strings.Contains(res.ErrorMessage, "backup failed: pg_dump") {
			t.Errorf("expected backup failure error message, got: %s", res.ErrorMessage)
		}
		for _, cp := range res.Checkpoints {
			if cp == "pg_dump_custom_format_backup_completed" {
				t.Errorf("unexpected checkpoint pg_dump_custom_format_backup_completed on dump failure")
			}
		}
	})

	t.Run("DatabaseCreationFailure", func(t *testing.T) {
		harness.SetBackupRestoreRunner(&mockBackupRestoreRunner{
			dumpFn: func(ctx context.Context, host, port, user, password, srcDB, destFile string) ([]byte, error) {
				if err := os.WriteFile(destFile, []byte("mock-pg-dump-header"), 0644); err != nil {
					return nil, err
				}
				return []byte("dump ok"), nil
			},
			createFn: func(ctx context.Context, host, port, user, password, dbName string) ([]byte, error) {
				return []byte("createdb: permission denied"), fmt.Errorf("exit status 1")
			},
		})
		defer harness.SetBackupRestoreRunner(nil)

		res := harness.RunCB07_RestoreFromDisposableBackupPendingPayment(ctx)
		if res.Passed {
			t.Fatalf("expected CB-07 to fail when createdb fails, but passed")
		}
		if !strings.Contains(res.ErrorMessage, "database creation failed: createdb") {
			t.Errorf("expected createdb failure error message, got: %s", res.ErrorMessage)
		}
	})

	t.Run("RestoreCommandFailure", func(t *testing.T) {
		harness.SetBackupRestoreRunner(&mockBackupRestoreRunner{
			dumpFn: func(ctx context.Context, host, port, user, password, srcDB, destFile string) ([]byte, error) {
				if err := os.WriteFile(destFile, []byte("mock-pg-dump-header"), 0644); err != nil {
					return nil, err
				}
				return []byte("dump ok"), nil
			},
			createFn: func(ctx context.Context, host, port, user, password, dbName string) ([]byte, error) {
				return []byte("createdb ok"), nil
			},
			restoreFn: func(ctx context.Context, host, port, user, password, dbName, srcFile string) ([]byte, error) {
				return []byte("pg_restore: [archiver] input file is corrupt"), fmt.Errorf("exit status 1")
			},
		})
		defer harness.SetBackupRestoreRunner(nil)

		res := harness.RunCB07_RestoreFromDisposableBackupPendingPayment(ctx)
		if res.Passed {
			t.Fatalf("expected CB-07 to fail when pg_restore fails, but passed")
		}
		if !strings.Contains(res.ErrorMessage, "restore failed: pg_restore") {
			t.Errorf("expected pg_restore failure error message, got: %s", res.ErrorMessage)
		}
	})

	t.Run("RestoreCommandFatalErrorInOutput", func(t *testing.T) {
		harness.SetBackupRestoreRunner(&mockBackupRestoreRunner{
			dumpFn: func(ctx context.Context, host, port, user, password, srcDB, destFile string) ([]byte, error) {
				if err := os.WriteFile(destFile, []byte("mock-pg-dump-header"), 0644); err != nil {
					return nil, err
				}
				return []byte("dump ok"), nil
			},
			createFn: func(ctx context.Context, host, port, user, password, dbName string) ([]byte, error) {
				return []byte("createdb ok"), nil
			},
			restoreFn: func(ctx context.Context, host, port, user, password, dbName, srcFile string) ([]byte, error) {
				// Exit code 0, but output contains critical query errors
				return []byte("pg_restore: error: could not execute query: ERROR: relation already exists"), nil
			},
		})
		defer harness.SetBackupRestoreRunner(nil)

		res := harness.RunCB07_RestoreFromDisposableBackupPendingPayment(ctx)
		if res.Passed {
			t.Fatalf("expected CB-07 to fail when pg_restore outputs fatal errors, but passed")
		}
		if !strings.Contains(res.ErrorMessage, "restore completed with fatal errors") {
			t.Errorf("expected fatal errors message, got: %s", res.ErrorMessage)
		}
	})

	t.Run("CleanupFailureDoesNotMaskPrimaryError", func(t *testing.T) {
		harness.SetBackupRestoreRunner(&mockBackupRestoreRunner{
			dumpFn: func(ctx context.Context, host, port, user, password, srcDB, destFile string) ([]byte, error) {
				return []byte("pg_dump: disk full"), fmt.Errorf("exit status 1")
			},
			dropFn: func(ctx context.Context, host, port, user, password, dbName string) ([]byte, error) {
				return []byte("dropdb: network error"), fmt.Errorf("exit status 1")
			},
		})
		defer harness.SetBackupRestoreRunner(nil)

		res := harness.RunCB07_RestoreFromDisposableBackupPendingPayment(ctx)
		if res.Passed {
			t.Fatalf("expected CB-07 to fail, but passed")
		}
		// Primary error must be the dump failure, not masked by the drop failure
		if !strings.Contains(res.ErrorMessage, "backup failed: pg_dump") {
			t.Errorf("primary error was masked by cleanup error: %s", res.ErrorMessage)
		}
	})

	t.Run("RestoredDataMissingPaymentFailsClosed", func(t *testing.T) {
		// Simulate a situation where pg_restore succeeded, but restored into an empty
		// or unpopulated database where the payment record does not exist.
		// We use a mock create/restore that creates a real empty disposable database.
		disposableTestDB := fmt.Sprintf("transactx_disposable_empty_%d", time.Now().UnixNano()%1000000)
		harness.SetBackupRestoreRunner(&mockBackupRestoreRunner{
			dumpFn: func(ctx context.Context, host, port, user, password, srcDB, destFile string) ([]byte, error) {
				if err := os.WriteFile(destFile, []byte("mock-dump"), 0644); err != nil {
					return nil, err
				}
				return []byte("ok"), nil
			},
			createFn: func(ctx context.Context, host, port, user, password, dbName string) ([]byte, error) {
				// Create a real empty database using postgres default runner
				runner := &DefaultBackupRestoreRunner{}
				return runner.RunCreateDB(ctx, host, port, user, password, dbName)
			},
			restoreFn: func(ctx context.Context, host, port, user, password, dbName, srcFile string) ([]byte, error) {
				// Pretend restore succeeded without populating payments table
				return []byte("restore completed"), nil
			},
		})
		defer harness.SetBackupRestoreRunner(nil)

		res := harness.RunCB07_RestoreFromDisposableBackupPendingPayment(ctx)
		if res.Passed {
			t.Fatalf("expected missing payment data in restored DB to fail, but passed")
		}
		if !strings.Contains(res.ErrorMessage, "restored DB missing expected payment") {
			t.Errorf("expected missing expected payment error, got: %s", res.ErrorMessage)
		}
		_ = disposableTestDB
	})
}

func TestCrossBankRecovery_CB07_FullRestoreEndToEnd(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping database-backed CB-07 full restore test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := database.NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	currentDB, err := VerifyPoolIsDisposable(ctx, pool)
	if err != nil {
		t.Skipf("skipping database-backed cross-bank recovery test: database %q is not disposable: %v (run via backend/scripts/run-cross-bank-disposable.sh)", currentDB, err)
	}

	harness, err := NewCrossBankRecoveryHarness(pool, dbURL)
	if err != nil {
		t.Fatalf("init harness: %v", err)
	}
	defer handleTestCleanup(t, harness)

	res := harness.RunCB07_RestoreFromDisposableBackupPendingPayment(ctx)
	if !res.Passed {
		t.Fatalf("CB-07 full restore failed: %s", res.ErrorMessage)
	}
	if res.ObservedState != "COMPLETED" {
		t.Errorf("expected COMPLETED, got %s", res.ObservedState)
	}
	for _, inv := range res.Invariants {
		if !inv.Passed {
			t.Errorf("invariant %s failed: %s", inv.InvariantName, inv.Details)
		}
	}
}

// Database-independent unit tests for cross-bank cleanup error classification and lifecycle interpretation.

func TestCrossBankRecovery_IsImmutableHistoryCleanupError(t *testing.T) {
	// 1. Nil error -> false
	if IsImmutableHistoryCleanupError(nil) {
		t.Errorf("expected nil error to report false")
	}

	// 2. Sentinel ErrImmutableHistoryBlockedCleanup -> true
	if !IsImmutableHistoryCleanupError(ErrImmutableHistoryBlockedCleanup) {
		t.Errorf("expected ErrImmutableHistoryBlockedCleanup to report true")
	}

	// 3. Wrapped ErrImmutableHistoryBlockedCleanup -> true
	wrapped := fmt.Errorf("cleanup failed: %w", ErrImmutableHistoryBlockedCleanup)
	if !IsImmutableHistoryCleanupError(wrapped) {
		t.Errorf("expected wrapped ErrImmutableHistoryBlockedCleanup to report true")
	}

	// 4. PostgreSQL foreign key error referencing payment_state_transitions -> true
	pgFKMsg := "ERROR: update or delete on table \"payments\" violates foreign key constraint \"payment_state_transitions_payment_id_fkey\" on table \"payment_state_transitions\" (SQLSTATE 23503)"
	if !IsImmutableHistoryCleanupError(errors.New(pgFKMsg)) {
		t.Errorf("expected PostgreSQL 23503 on payment_state_transitions to report true")
	}

	// 5. Unrelated foreign key error (not on payment_state_transitions) -> false
	unrelatedFK := "ERROR: update or delete on table \"payments\" violates foreign key constraint \"bank_a_operations_payment_id_fkey\" on table \"bank_a.operations\" (SQLSTATE 23503)"
	if IsImmutableHistoryCleanupError(errors.New(unrelatedFK)) {
		t.Errorf("expected unrelated FK violation to report false")
	}

	// 6. Generic database errors -> false
	if IsImmutableHistoryCleanupError(errors.New("connection reset by peer")) {
		t.Errorf("expected connection error to report false")
	}
	if IsImmutableHistoryCleanupError(errors.New("syntax error at or near FROM")) {
		t.Errorf("expected syntax error to report false")
	}
	if IsImmutableHistoryCleanupError(errors.New("commit cleanup transaction: deadlock detected")) {
		t.Errorf("expected deadlock error to report false")
	}
}

func TestCrossBankRecovery_ClassifyTeardownError(t *testing.T) {
	// 1. Clean (nil error)
	if status := ClassifyTeardownError(nil); status != TeardownClean {
		t.Errorf("expected TeardownClean for nil error, got: %s", status)
	}

	// 2. Requires external drop (sentinel)
	if status := ClassifyTeardownError(ErrImmutableHistoryBlockedCleanup); status != TeardownRequiresExternalDrop {
		t.Errorf("expected TeardownRequiresExternalDrop for sentinel, got: %s", status)
	}

	// 3. Requires external drop (wrapped PG FK)
	wrappedPGFK := fmt.Errorf("cleanup table payments: %w", errors.New("violates foreign key constraint on payment_state_transitions (SQLSTATE 23503)"))
	if status := ClassifyTeardownError(wrappedPGFK); status != TeardownRequiresExternalDrop {
		t.Errorf("expected TeardownRequiresExternalDrop for PG FK on payment_state_transitions, got: %s", status)
	}

	// 4. Unexpected cleanup errors
	unexpectedErrs := []error{
		errors.New("connection refused"),
		errors.New("syntax error at or near FROM"),
		errors.New("table bank_a.ledger_entries does not exist"),
		errors.New("commit cleanup transaction: connection closed"),
	}
	for _, uErr := range unexpectedErrs {
		if status := ClassifyTeardownError(uErr); status != TeardownUnexpectedError {
			t.Errorf("expected TeardownUnexpectedError for %q, got: %s", uErr.Error(), status)
		}
	}
}

func TestCrossBankRecovery_ErrorsJoinPreservesImmutableHistorySentinel(t *testing.T) {
	// Verify that harness.Close error aggregation via errors.Join preserves ErrImmutableHistoryBlockedCleanup
	serverErr := errors.New("server A shutdown timeout")
	cleanupErr := fmt.Errorf("%w: cleanup table payments: FK violation", ErrImmutableHistoryBlockedCleanup)

	joined := errors.Join(serverErr, cleanupErr)
	if joined == nil {
		t.Fatalf("expected non-nil joined error")
	}

	// Verify both underlying errors remain discoverable via errors.Is
	if !errors.Is(joined, ErrImmutableHistoryBlockedCleanup) {
		t.Errorf("expected errors.Is(joined, ErrImmutableHistoryBlockedCleanup) == true")
	}
	if !errors.Is(joined, serverErr) {
		t.Errorf("expected errors.Is(joined, serverErr) == true")
	}

	// Verify teardown classification works on joined errors
	if status := ClassifyTeardownError(joined); status != TeardownRequiresExternalDrop {
		t.Errorf("expected TeardownRequiresExternalDrop for joined error, got: %s", status)
	}
}

func TestCrossBankRecovery_ResolveOrchestratorContract(t *testing.T) {
	targetDB := "transactx_disposable_ci_test_123"

	// 1. Un-orchestrated: TRANSACTX_DISPOSABLE_DB_ORCHESTRATED not set
	t.Run("un-orchestrated, env var not set", func(t *testing.T) {
		t.Setenv(EnvDisposableDBOrchestrated, "")
		t.Setenv(EnvDisposableContractFile, "")
		t.Setenv(EnvDisposableOrchestratorContract, "")
		contract, err := ResolveOrchestratorContract(targetDB)
		if err == nil {
			t.Fatalf("expected error when TRANSACTX_DISPOSABLE_DB_ORCHESTRATED is unset, got contract: %+v", contract)
		}
	})

	// 2. Un-orchestrated: TRANSACTX_DISPOSABLE_DB_ORCHESTRATED = "0"
	t.Run("un-orchestrated, env var is 0", func(t *testing.T) {
		t.Setenv(EnvDisposableDBOrchestrated, "0")
		t.Setenv(EnvDisposableContractFile, "")
		t.Setenv(EnvDisposableOrchestratorContract, "")
		contract, err := ResolveOrchestratorContract(targetDB)
		if err == nil {
			t.Fatalf("expected error when TRANSACTX_DISPOSABLE_DB_ORCHESTRATED is '0', got contract: %+v", contract)
		}
	})

	// 3. Env var set to "1" alone, but invocation token missing
	t.Run("orchestration env var set alone without invocation token", func(t *testing.T) {
		t.Setenv(EnvDisposableDBOrchestrated, "1")
		t.Setenv(EnvDisposableInvocationToken, "")
		t.Setenv(EnvDisposableContractFile, "")
		t.Setenv(EnvDisposableOrchestratorContract, "")
		contract, err := ResolveOrchestratorContract(targetDB)
		if err == nil {
			t.Fatalf("expected error when env var set alone without invocation token, got contract: %+v", contract)
		}
		if !strings.Contains(err.Error(), "orchestrator invocation token missing") {
			t.Errorf("expected error message to explain invocation token is missing, got: %v", err)
		}
	})

	// 3b. Env var set to "1" with invocation token, but neither file nor inline contract provided
	t.Run("orchestration env var set with token but without contract marker", func(t *testing.T) {
		t.Setenv(EnvDisposableDBOrchestrated, "1")
		t.Setenv(EnvDisposableInvocationToken, "test_token_12345678")
		t.Setenv(EnvDisposableContractFile, "")
		t.Setenv(EnvDisposableOrchestratorContract, "")
		contract, err := ResolveOrchestratorContract(targetDB)
		if err == nil {
			t.Fatalf("expected error when env var set without contract file, got contract: %+v", contract)
		}
		if !strings.Contains(err.Error(), "environment variable alone does not establish teardown ownership") {
			t.Errorf("expected error message to explain environment variable alone is insufficient, got: %v", err)
		}
	})

	// 4. Contract file does not exist
	t.Run("contract file does not exist", func(t *testing.T) {
		t.Setenv(EnvDisposableDBOrchestrated, "1")
		t.Setenv(EnvDisposableInvocationToken, "test_token_12345678")
		t.Setenv(EnvDisposableContractFile, filepath.Join(t.TempDir(), "nonexistent_contract.json"))
		t.Setenv(EnvDisposableOrchestratorContract, "")
		contract, err := ResolveOrchestratorContract(targetDB)
		if err == nil {
			t.Fatalf("expected error for non-existent contract file, got contract: %+v", contract)
		}
	})

	// 5. Contract file is empty
	t.Run("contract file is empty", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "empty_contract.json")
		if err := os.WriteFile(tmpFile, []byte("   \n"), 0600); err != nil {
			t.Fatalf("write empty contract: %v", err)
		}
		t.Setenv(EnvDisposableDBOrchestrated, "1")
		t.Setenv(EnvDisposableInvocationToken, "test_token_12345678")
		t.Setenv(EnvDisposableContractFile, tmpFile)
		t.Setenv(EnvDisposableOrchestratorContract, "")
		contract, err := ResolveOrchestratorContract(targetDB)
		if err == nil {
			t.Fatalf("expected error for empty contract file, got contract: %+v", contract)
		}
	})

	// 6. Contract file has malformed JSON
	t.Run("contract file has malformed JSON", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "malformed_contract.json")
		if err := os.WriteFile(tmpFile, []byte("{broken_json"), 0600); err != nil {
			t.Fatalf("write malformed contract: %v", err)
		}
		t.Setenv(EnvDisposableDBOrchestrated, "1")
		t.Setenv(EnvDisposableInvocationToken, "test_token_12345678")
		t.Setenv(EnvDisposableContractFile, tmpFile)
		t.Setenv(EnvDisposableOrchestratorContract, "")
		contract, err := ResolveOrchestratorContract(targetDB)
		if err == nil {
			t.Fatalf("expected error for malformed JSON, got contract: %+v", contract)
		}
	})

	// 7. Contract version unsupported
	t.Run("contract version unsupported", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "wrong_version.json")
		payload := `{"version":"tx.orchestrator.contract.v99","database":"` + targetDB + `","invocationToken":"test_token_12345678","orchestratorPid":1234,"createdAt":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
		if err := os.WriteFile(tmpFile, []byte(payload), 0600); err != nil {
			t.Fatalf("write wrong version contract: %v", err)
		}
		t.Setenv(EnvDisposableDBOrchestrated, "1")
		t.Setenv(EnvDisposableInvocationToken, "test_token_12345678")
		t.Setenv(EnvDisposableContractFile, tmpFile)
		t.Setenv(EnvDisposableOrchestratorContract, "")
		contract, err := ResolveOrchestratorContract(targetDB)
		if err == nil {
			t.Fatalf("expected error for wrong contract version, got contract: %+v", contract)
		}
		if !strings.Contains(err.Error(), "unsupported orchestrator contract version") {
			t.Errorf("expected error about unsupported version, got: %v", err)
		}
	})

	// 8. Contract orchestrator PID <= 0
	t.Run("contract invalid PID", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "invalid_pid.json")
		payload := `{"version":"tx.orchestrator.contract.v1","database":"` + targetDB + `","invocationToken":"test_token_12345678","orchestratorPid":0,"createdAt":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
		if err := os.WriteFile(tmpFile, []byte(payload), 0600); err != nil {
			t.Fatalf("write invalid pid contract: %v", err)
		}
		t.Setenv(EnvDisposableDBOrchestrated, "1")
		t.Setenv(EnvDisposableInvocationToken, "test_token_12345678")
		t.Setenv(EnvDisposableContractFile, tmpFile)
		t.Setenv(EnvDisposableOrchestratorContract, "")
		contract, err := ResolveOrchestratorContract(targetDB)
		if err == nil {
			t.Fatalf("expected error for non-positive orchestrator PID, got contract: %+v", contract)
		}
	})

	// 9. Contract database name mismatch
	t.Run("contract database mismatch", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "mismatch_contract.json")
		payload := `{"version":"tx.orchestrator.contract.v1","database":"transactx_disposable_ci_other","invocationToken":"test_token_12345678","orchestratorPid":1234,"createdAt":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
		if err := os.WriteFile(tmpFile, []byte(payload), 0600); err != nil {
			t.Fatalf("write mismatch contract: %v", err)
		}
		t.Setenv(EnvDisposableDBOrchestrated, "1")
		t.Setenv(EnvDisposableInvocationToken, "test_token_12345678")
		t.Setenv(EnvDisposableContractFile, tmpFile)
		t.Setenv(EnvDisposableOrchestratorContract, "")
		contract, err := ResolveOrchestratorContract(targetDB)
		if err == nil {
			t.Fatalf("expected error when contract database does not match targetDB, got contract: %+v", contract)
		}
		if !strings.Contains(err.Error(), "ownership binding mismatch") {
			t.Errorf("expected error about ownership binding mismatch, got: %v", err)
		}
	})

	// 10. Contract createdAt malformed
	t.Run("contract createdAt malformed", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "malformed_created.json")
		payload := `{"version":"tx.orchestrator.contract.v1","database":"` + targetDB + `","invocationToken":"test_token_12345678","orchestratorPid":1234,"createdAt":"not-a-timestamp"}`
		if err := os.WriteFile(tmpFile, []byte(payload), 0600); err != nil {
			t.Fatalf("write contract: %v", err)
		}
		t.Setenv(EnvDisposableDBOrchestrated, "1")
		t.Setenv(EnvDisposableInvocationToken, "test_token_12345678")
		t.Setenv(EnvDisposableContractFile, tmpFile)
		t.Setenv(EnvDisposableOrchestratorContract, "")
		_, err := ResolveOrchestratorContract(targetDB)
		if err == nil || !strings.Contains(err.Error(), "invalid createdAt timestamp") {
			t.Fatalf("expected error for malformed createdAt timestamp, got: %v", err)
		}
	})

	// 11. Contract createdAt in future
	t.Run("contract createdAt in future", func(t *testing.T) {
		futureTime := time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339)
		tmpFile := filepath.Join(t.TempDir(), "future_created.json")
		payload := `{"version":"tx.orchestrator.contract.v1","database":"` + targetDB + `","invocationToken":"test_token_12345678","orchestratorPid":1234,"createdAt":"` + futureTime + `"}`
		if err := os.WriteFile(tmpFile, []byte(payload), 0600); err != nil {
			t.Fatalf("write contract: %v", err)
		}
		t.Setenv(EnvDisposableDBOrchestrated, "1")
		t.Setenv(EnvDisposableInvocationToken, "test_token_12345678")
		t.Setenv(EnvDisposableContractFile, tmpFile)
		t.Setenv(EnvDisposableOrchestratorContract, "")
		_, err := ResolveOrchestratorContract(targetDB)
		if err == nil || !strings.Contains(err.Error(), "clock skew violation") {
			t.Fatalf("expected error for future contract timestamp, got: %v", err)
		}
	})

	// 12. Contract createdAt stale (> 1 hour old)
	t.Run("contract createdAt stale", func(t *testing.T) {
		staleTime := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
		tmpFile := filepath.Join(t.TempDir(), "stale_created.json")
		payload := `{"version":"tx.orchestrator.contract.v1","database":"` + targetDB + `","invocationToken":"test_token_12345678","orchestratorPid":1234,"createdAt":"` + staleTime + `"}`
		if err := os.WriteFile(tmpFile, []byte(payload), 0600); err != nil {
			t.Fatalf("write contract: %v", err)
		}
		t.Setenv(EnvDisposableDBOrchestrated, "1")
		t.Setenv(EnvDisposableInvocationToken, "test_token_12345678")
		t.Setenv(EnvDisposableContractFile, tmpFile)
		t.Setenv(EnvDisposableOrchestratorContract, "")
		_, err := ResolveOrchestratorContract(targetDB)
		if err == nil || !strings.Contains(err.Error(), "stale") {
			t.Fatalf("expected error for stale contract timestamp, got: %v", err)
		}
	})

	// 13. Inline contract JSON rejected without contract file
	t.Run("inline JSON contract rejected", func(t *testing.T) {
		payload := `{"version":"tx.orchestrator.contract.v1","database":"` + targetDB + `","invocationToken":"test_token_12345678","orchestratorPid":8888,"createdAt":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
		t.Setenv(EnvDisposableDBOrchestrated, "1")
		t.Setenv(EnvDisposableInvocationToken, "test_token_12345678")
		t.Setenv(EnvDisposableContractFile, "")
		t.Setenv(EnvDisposableOrchestratorContract, payload)
		_, err := ResolveOrchestratorContract(targetDB)
		if err == nil || !strings.Contains(err.Error(), "inline JSON contract") {
			t.Fatalf("expected rejection of inline JSON contract without file, got: %v", err)
		}
	})

	// 14. Contract invocation token missing/empty in file
	t.Run("contract invocation token empty in file", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "empty_token_contract.json")
		payload := `{"version":"tx.orchestrator.contract.v1","database":"` + targetDB + `","invocationToken":"","orchestratorPid":1234,"createdAt":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
		if err := os.WriteFile(tmpFile, []byte(payload), 0600); err != nil {
			t.Fatalf("write contract: %v", err)
		}
		t.Setenv(EnvDisposableDBOrchestrated, "1")
		t.Setenv(EnvDisposableInvocationToken, "test_token_12345678")
		t.Setenv(EnvDisposableContractFile, tmpFile)
		t.Setenv(EnvDisposableOrchestratorContract, "")
		_, err := ResolveOrchestratorContract(targetDB)
		if err == nil || !strings.Contains(err.Error(), "contract invocation token is empty") {
			t.Fatalf("expected error for empty invocation token in contract, got: %v", err)
		}
	})

	// 15. Contract invocation token mismatched with environment
	t.Run("contract invocation token mismatch with env", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "mismatched_token_contract.json")
		payload := `{"version":"tx.orchestrator.contract.v1","database":"` + targetDB + `","invocationToken":"test_token_11111111","orchestratorPid":1234,"createdAt":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
		if err := os.WriteFile(tmpFile, []byte(payload), 0600); err != nil {
			t.Fatalf("write contract: %v", err)
		}
		t.Setenv(EnvDisposableDBOrchestrated, "1")
		t.Setenv(EnvDisposableInvocationToken, "test_token_22222222")
		t.Setenv(EnvDisposableContractFile, tmpFile)
		t.Setenv(EnvDisposableOrchestratorContract, "")
		_, err := ResolveOrchestratorContract(targetDB)
		if err == nil || !strings.Contains(err.Error(), "invocation binding mismatch") {
			t.Fatalf("expected error for token mismatch, got: %v", err)
		}
	})

	// 16. Valid contract file matching targetDB and invocation token with fresh timestamp
	t.Run("valid contract file and matching token", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "valid_contract.json")
		freshTime := time.Now().UTC().Format(time.RFC3339)
		validToken := "test_token_abcdef12"
		payload := `{"version":"tx.orchestrator.contract.v1","database":"` + targetDB + `","invocationToken":"` + validToken + `","orchestratorPid":4567,"createdAt":"` + freshTime + `"}`
		if err := os.WriteFile(tmpFile, []byte(payload), 0600); err != nil {
			t.Fatalf("write valid contract: %v", err)
		}
		t.Setenv(EnvDisposableDBOrchestrated, "1")
		t.Setenv(EnvDisposableInvocationToken, validToken)
		t.Setenv(EnvDisposableContractFile, tmpFile)
		t.Setenv(EnvDisposableOrchestratorContract, "")
		contract, err := ResolveOrchestratorContract(targetDB)
		if err != nil {
			t.Fatalf("expected nil error for valid contract file, got: %v", err)
		}
		if contract.Database != targetDB || contract.OrchestratorPID != 4567 || contract.Version != OrchestratorContractVersion || contract.InvocationToken != validToken {
			t.Errorf("unexpected contract fields: %+v", contract)
		}
	})
}

func TestCrossBankRecovery_EvaluateTestCleanupOutcome(t *testing.T) {
	// Unit test: Verify that EvaluateTestCleanupOutcome correctly distinguishes
	// un-orchestrated vs orchestrated lifecycle contracts, handles typed sentinel and PG FK errors,
	// validates contract ownership against targetDB and invocation token, and strictly refuses to convert unexpected
	// cleanup errors into successes.
	targetDB := "transactx_disposable_ci_test"
	otherDB := "transactx_disposable_ci_other"
	validToken := "valid_test_token_123"

	freshTime := time.Now().UTC().Format(time.RFC3339)
	staleTime := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	futureTime := time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339)

	validContract := &OrchestratorLifecycleContract{
		Version:         OrchestratorContractVersion,
		Database:        targetDB,
		InvocationToken: validToken,
		OrchestratorPID: 1234,
		CreatedAt:       freshTime,
	}

	prevRunContract := &OrchestratorLifecycleContract{
		Version:         OrchestratorContractVersion,
		Database:        targetDB,
		InvocationToken: "previous_invocation_token_999",
		OrchestratorPID: 1234,
		CreatedAt:       freshTime,
	}

	staleContract := &OrchestratorLifecycleContract{
		Version:         OrchestratorContractVersion,
		Database:        targetDB,
		InvocationToken: validToken,
		OrchestratorPID: 1234,
		CreatedAt:       staleTime,
	}

	futureContract := &OrchestratorLifecycleContract{
		Version:         OrchestratorContractVersion,
		Database:        targetDB,
		InvocationToken: validToken,
		OrchestratorPID: 1234,
		CreatedAt:       futureTime,
	}

	mismatchedContract := &OrchestratorLifecycleContract{
		Version:         OrchestratorContractVersion,
		Database:        otherDB,
		InvocationToken: validToken,
		OrchestratorPID: 1234,
		CreatedAt:       freshTime,
	}

	malformedContract := &OrchestratorLifecycleContract{
		Version:         "unsupported_v0",
		Database:        targetDB,
		InvocationToken: validToken,
		OrchestratorPID: -1,
		CreatedAt:       freshTime,
	}

	noTokenContract := &OrchestratorLifecycleContract{
		Version:         OrchestratorContractVersion,
		Database:        targetDB,
		InvocationToken: "",
		OrchestratorPID: 1234,
		CreatedAt:       freshTime,
	}

	tests := []struct {
		name          string
		err           error
		contract      *OrchestratorLifecycleContract
		targetDB      string
		expectedToken string
		wantFail      bool
		wantDelegated bool
	}{
		// Clean / nil error
		{"nil error, nil contract, no token", nil, nil, targetDB, "", false, false},
		{"nil error, valid contract, matching token", nil, validContract, targetDB, validToken, false, false},

		// Outcome 4: Immutable history blocks with valid contract matching target DB and token -> delegated
		{"sentinel FK, valid contract matching DB and token", ErrImmutableHistoryBlockedCleanup, validContract, targetDB, validToken, false, true},
		{"wrapped sentinel FK, valid contract matching DB and token", fmt.Errorf("cleanup: %w", ErrImmutableHistoryBlockedCleanup), validContract, targetDB, validToken, false, true},
		{"raw PG FK 23503, valid contract matching DB and token", errors.New("update or delete on table \"payments\" violates foreign key constraint \"payment_state_transitions_payment_id_fkey\" on table \"payment_state_transitions\" (SQLSTATE 23503)"), validContract, targetDB, validToken, false, true},

		// Outcome 1: Orchestration marker set with no contract or invocation token: reject delegation
		{"sentinel FK, nil contract, empty token (un-orchestrated)", ErrImmutableHistoryBlockedCleanup, nil, targetDB, "", true, false},
		{"wrapped sentinel FK, nil contract, empty token", fmt.Errorf("cleanup: %w", ErrImmutableHistoryBlockedCleanup), nil, targetDB, "", true, false},
		{"raw PG FK 23503, nil contract, empty token", errors.New("update or delete on table \"payments\" violates foreign key constraint \"payment_state_transitions_payment_id_fkey\" on table \"payment_state_transitions\" (SQLSTATE 23503)"), nil, targetDB, "", true, false},

		// Outcome 2: Valid contract but absent or mismatched invocation token: reject delegation
		{"sentinel FK, valid contract, absent expected token", ErrImmutableHistoryBlockedCleanup, validContract, targetDB, "", true, false},
		{"sentinel FK, valid contract, mismatched expected token", ErrImmutableHistoryBlockedCleanup, validContract, targetDB, "different_token_abc123", true, false},
		{"sentinel FK, contract with empty token, provided expected token", ErrImmutableHistoryBlockedCleanup, noTokenContract, targetDB, validToken, true, false},

		// Outcome 3: Contract token from a previous invocation: reject delegation
		{"sentinel FK, previous invocation token", ErrImmutableHistoryBlockedCleanup, prevRunContract, targetDB, validToken, true, false},

		// Target database mismatch or contract invalidity: reject delegation
		{"sentinel FK, mismatched contract database", ErrImmutableHistoryBlockedCleanup, mismatchedContract, targetDB, validToken, true, false},
		{"raw PG FK 23503, mismatched contract database", errors.New("update or delete on table \"payments\" violates foreign key constraint \"payment_state_transitions_payment_id_fkey\" on table \"payment_state_transitions\" (SQLSTATE 23503)"), mismatchedContract, targetDB, validToken, true, false},
		{"sentinel FK, malformed contract", ErrImmutableHistoryBlockedCleanup, malformedContract, targetDB, validToken, true, false},
		{"sentinel FK, stale contract", ErrImmutableHistoryBlockedCleanup, staleContract, targetDB, validToken, true, false},
		{"sentinel FK, future contract", ErrImmutableHistoryBlockedCleanup, futureContract, targetDB, validToken, true, false},

		// Outcome 5: Unexpected SQL or connection error: fail unconditionally
		{"unexpected connection refused, nil contract, no token", errors.New("connection refused"), nil, targetDB, "", true, false},
		{"unexpected connection refused, valid contract and matching token", errors.New("connection refused"), validContract, targetDB, validToken, true, false},
		{"unexpected syntax error, valid contract and matching token", errors.New("syntax error at or near FROM"), validContract, targetDB, validToken, true, false},
		{"unexpected unrelated FK, valid contract and matching token", errors.New("violates foreign key constraint bank_a_operations_payment_id_fkey on table bank_a.operations"), validContract, targetDB, validToken, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFail, gotDelegated, diag := EvaluateTestCleanupOutcome(tt.err, tt.contract, tt.targetDB, tt.expectedToken)
			if gotFail != tt.wantFail {
				t.Errorf("EvaluateTestCleanupOutcome(%v, %+v, %s, %s) gotFail = %t, want %t (diag: %s)", tt.err, tt.contract, tt.targetDB, tt.expectedToken, gotFail, tt.wantFail, diag)
			}
			if gotDelegated != tt.wantDelegated {
				t.Errorf("EvaluateTestCleanupOutcome(%v, %+v, %s, %s) gotDelegated = %t, want %t (diag: %s)", tt.err, tt.contract, tt.targetDB, tt.expectedToken, gotDelegated, tt.wantDelegated, diag)
			}
			if diag == "" {
				t.Errorf("EvaluateTestCleanupOutcome(%v, %+v, %s, %s) returned empty diag string", tt.err, tt.contract, tt.targetDB, tt.expectedToken)
			}
		})
	}
}
