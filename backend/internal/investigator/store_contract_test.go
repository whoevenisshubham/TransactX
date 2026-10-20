package investigator_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/database"
	"github.com/transactx/backend/internal/investigator"
	"github.com/transactx/backend/internal/investigator/storetest"
)

func TestMemoryInvestigationStoreConformanceHarness(t *testing.T) {
	storetest.Run(t, func(t *testing.T, limits investigator.UsageAllowance) investigator.InvestigationStore {
		t.Helper()
		store := investigator.NewMemoryInvestigationStore(time.Now)
		store.SetUsageLimits(limits.InputTokens, limits.OutputTokens, limits.CostMicrousd)
		return store
	})
}

func findRepoMigrationsDir(t *testing.T) string {
	t.Helper()
	candidates := []string{
		"../../migrations",
		"../migrations",
		"migrations",
		"backend/migrations",
	}
	for _, c := range candidates {
		clean := filepath.Clean(c)
		if info, err := os.Stat(clean); err == nil && info.IsDir() {
			abs, _ := filepath.Abs(clean)
			return abs
		}
	}
	t.Fatal("could not find migrations directory from test")
	return ""
}

func setupDisposableDB(t *testing.T, databaseURL string) (string, func()) {
	t.Helper()
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("failed to parse database URL: %v", err)
	}

	adminPool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("failed to connect to admin database: %v", err)
	}

	dbName := "tx_store_conf_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = adminPool.Exec(context.Background(), fmt.Sprintf("CREATE DATABASE %s;", dbName))
	if err != nil {
		adminPool.Close()
		t.Fatalf("failed to create disposable database %s: %v", dbName, err)
	}

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = adminPool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE);", dbName))
		adminPool.Close()
	}

	testDBURL := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		config.ConnConfig.User,
		config.ConnConfig.Password,
		config.ConnConfig.Host,
		config.ConnConfig.Port,
		dbName,
	)

	return testDBURL, cleanup
}

func TestPostgresInvestigationStoreConformance(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set: skipping PostgreSQL investigation store conformance test")
	}

	testDBURL, cleanup := setupDisposableDB(t, databaseURL)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, testDBURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	// Apply all canonical migrations (000001 through 000029) via authoritative Migrator
	migrationsDir := findRepoMigrationsDir(t)
	migrator, err := database.NewMigrator(pool, migrationsDir)
	if err != nil {
		t.Fatalf("new migrator: %v", err)
	}
	applied, err := migrator.Up(ctx)
	if err != nil {
		t.Fatalf("apply canonical migrations: %v", err)
	}
	if applied < 30 {
		t.Fatalf("expected at least 30 migrations applied, got %d", applied)
	}

	storetest.Run(t, func(t *testing.T, limits investigator.UsageAllowance) investigator.InvestigationStore {
		t.Helper()

		subCtx, subCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer subCancel()

		// Clean tables between subtests for complete test isolation against canonical schema
		if _, err := pool.Exec(subCtx, "TRUNCATE investigation_usage_reservations, investigation_jobs CASCADE;"); err != nil {
			t.Fatalf("truncate investigation tables: %v", err)
		}

		store, err := investigator.NewPostgresInvestigationStore(pool, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SetUsageLimits(subCtx, limits); err != nil {
			t.Fatal(err)
		}
		return store
	})
}

func TestPostgresInvestigationStoreRejectsCorruptPayload(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set: skipping PostgreSQL investigation store test")
	}

	testDBURL, cleanup := setupDisposableDB(t, databaseURL)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, testDBURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	migrationsDir := findRepoMigrationsDir(t)
	migrator, err := database.NewMigrator(pool, migrationsDir)
	if err != nil {
		t.Fatalf("new migrator: %v", err)
	}
	applied, err := migrator.Up(ctx)
	if err != nil {
		t.Fatalf("apply canonical migrations: %v", err)
	}
	if applied < 30 {
		t.Fatalf("expected at least 30 migrations applied, got %d", applied)
	}

	limits := investigator.UsageAllowance{
		InputTokens: 100, OutputTokens: 100, CostMicrousd: 1_000,
	}
	store, err := investigator.NewPostgresInvestigationStore(pool, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetUsageLimits(ctx, limits); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	scope := investigator.TrustedScope{
		ActorID: uuid.NewString(), Role: "OPS_VIEWER", SelectedPaymentID: uuid.NewString(),
	}
	request := investigator.NewJob{
		Scope: scope, PaymentID: scope.SelectedPaymentID, Question: "What happened?",
		IdempotencyKey: "corrupt-payload", RequestHash: strings.Repeat("a", 64),
		Mode: investigator.ModeEvidenceOnly, CreatedAt: now, DeadlineAt: now.Add(time.Minute),
	}
	job, created, err := store.Create(ctx, request)
	if err != nil || !created {
		t.Fatalf("create job: created=%t err=%v", created, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE investigation_jobs SET job_payload = '{}'::jsonb WHERE id = $1`, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Create(ctx, request); err == nil {
		t.Fatal("idempotent replay accepted corrupt stored payload")
	}
	if _, err := store.Get(ctx, scope, job.ID); err == nil {
		t.Fatal("get accepted corrupt stored payload")
	}
	if _, err := store.Claim(ctx, job.ID, "worker-corrupt", time.Minute); err == nil {
		t.Fatal("claim accepted corrupt stored payload")
	}
	var status string
	var leaseEpoch int64
	var leaseOwner *string
	if err := pool.QueryRow(ctx, `SELECT status, lease_epoch, lease_owner FROM investigation_jobs WHERE id = $1`, job.ID).Scan(&status, &leaseEpoch, &leaseOwner); err != nil {
		t.Fatal(err)
	}
	if status != investigator.StatusQueued || leaseEpoch != 0 || leaseOwner != nil {
		t.Fatalf("corrupt claim mutated row: status=%s epoch=%d owner=%v", status, leaseEpoch, leaseOwner)
	}
}

func TestPostgresInvestigationStoreUpgradedDatabaseConformance(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set: skipping PostgreSQL investigation store upgraded database test")
	}

	testDBURL, cleanup := setupDisposableDB(t, databaseURL)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, testDBURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	realMigrationsDir := findRepoMigrationsDir(t)

	// Step 1: Create a temp dir with ONLY migrations 000001 through 000029
	tempDir := t.TempDir()
	entries, err := os.ReadDir(realMigrationsDir)
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || len(entry.Name()) < 7 || entry.Name()[:6] > "000029" {
			continue // A genuine pre-30 schema, excluding every later migration.
		}
		data, err := os.ReadFile(filepath.Join(realMigrationsDir, entry.Name()))
		if err != nil {
			t.Fatalf("read migration %s: %v", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(tempDir, entry.Name()), data, 0644); err != nil {
			t.Fatalf("write migration %s to temp: %v", entry.Name(), err)
		}
	}

	// Step 2: Apply migrations up to 000029
	preMigrator, err := database.NewMigrator(pool, tempDir)
	if err != nil {
		t.Fatalf("new pre-migrator: %v", err)
	}
	applied, err := preMigrator.Up(ctx)
	if err != nil {
		t.Fatalf("apply pre-upgrade migrations: %v", err)
	}
	if applied != 29 {
		t.Fatalf("expected 29 pre-upgrade migrations, got %d", applied)
	}
	var premature bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('auth_external_identities') IS NOT NULL OR to_regclass('gemini_free_attempts') IS NOT NULL`).Scan(&premature); err != nil || premature {
		t.Fatalf("pre-30 fixture contains later schema: %v", err)
	}

	// Step 3: Seed legacy data in pre-upgrade schema state:
	// A) A queued legacy job with empty request_hash and empty job_payload
	legacyQueuedJobID := uuid.NewString()
	legacyPaymentID := uuid.NewString()
	legacyActorID := uuid.NewString()
	_, err = pool.Exec(ctx, `
		INSERT INTO investigation_jobs (id, payment_id, actor_id, status, mode, question, idempotency_key, created_at, role, request_hash, job_payload)
		VALUES ($1, $2, $3, 'queued', 'evidence_only', 'Legacy queued question', 'legacy-key-1', NOW() - INTERVAL '10 minutes', 'OPS_VIEWER', '', '{}'::jsonb)`,
		legacyQueuedJobID, legacyPaymentID, legacyActorID)
	if err != nil {
		t.Fatalf("seed legacy queued job: %v", err)
	}

	// B) A completed legacy job with report, question, usage, empty request_hash and empty job_payload
	legacyCompletedJobID := uuid.NewString()
	legacyCompletedReport := `{"summary":"Historical completed report","findings":[],"citations":[]}`
	legacyCompletedUsage := `{"inputTokens":250,"outputTokens":120}`
	_, err = pool.Exec(ctx, `
		INSERT INTO investigation_jobs (id, payment_id, actor_id, status, mode, question, idempotency_key, created_at, completed_at, role, request_hash, job_payload, report, usage)
		VALUES ($1, $2, $3, 'completed', 'evidence_only', 'Legacy completed question', 'legacy-key-2', NOW() - INTERVAL '1 hour', NOW() - INTERVAL '50 minutes', 'OPS_VIEWER', '', '{}'::jsonb, $4::jsonb, $5::jsonb)`,
		legacyCompletedJobID, legacyPaymentID, legacyActorID, legacyCompletedReport, legacyCompletedUsage)
	if err != nil {
		t.Fatalf("seed legacy completed job: %v", err)
	}

	// C) A legacy usage reservation using single-dimension tokens
	legacyResID := uuid.NewString()
	_, err = pool.Exec(ctx, `
		INSERT INTO investigation_usage_reservations (id, job_id, actor_id, provider, model, reserved_tokens, actual_tokens, status, reserved_input, reserved_output)
		VALUES ($1, $2, $3, 'mock-provider', 'mock-model', 500, 420, 'completed', 0, 0)`,
		legacyResID, legacyCompletedJobID, legacyActorID)
	if err != nil {
		t.Fatalf("seed legacy reservation: %v", err)
	}

	// Step 4: Run migration 000030 and all later additive migrations using the full canonical migrations directory
	upgradeMigrator, err := database.NewMigrator(pool, realMigrationsDir)
	if err != nil {
		t.Fatalf("new upgrade migrator: %v", err)
	}
	// Verify that previous 29 migrations have matching checksums
	if err := upgradeMigrator.Verify(ctx); err != nil {
		t.Fatalf("checksum verification of applied migrations failed: %v", err)
	}
	status, err := upgradeMigrator.Status(ctx)
	if err != nil {
		t.Fatalf("check migration status: %v", err)
	}
	if len(status.Pending) < 3 || status.Pending[0].Version != "000030" || status.Pending[1].Version != "000031" || status.Pending[2].Version != "000032" {
		t.Fatalf("expected contiguous latest-schema upgrade starting 30/31/32, got %+v", status.Pending)
	}
	expectedPending := len(status.Pending)
	upgraded, err := upgradeMigrator.Up(ctx)
	if err != nil {
		t.Fatalf("apply upgrade migration 000030: %v", err)
	}
	if upgraded != expectedPending {
		t.Fatalf("expected %d pending migrations applied, got %d", expectedPending, upgraded)
	}
	// Verify all latest migrations pass checksum verification and 0 pending
	if err := upgradeMigrator.Verify(ctx); err != nil {
		t.Fatalf("checksum verification failed after upgrade: %v", err)
	}
	latestStatus, err := upgradeMigrator.Status(ctx)
	if err != nil || len(latestStatus.Pending) != 0 {
		t.Fatalf("latest schema not fully applied: %v", err)
	}
	var latest bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('auth_external_identities') IS NOT NULL AND to_regclass('gemini_free_attempts') IS NOT NULL AND EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='gemini_free_admission' AND column_name='disabled' AND is_nullable='NO') AND (SELECT is_nullable='YES' FROM information_schema.columns WHERE table_schema='public' AND table_name='users' AND column_name='password_hash')`).Scan(&latest); err != nil || !latest {
		t.Fatalf("latest auth/provider schema missing: %v", err)
	}

	// Step 5: Assert legacy data migration invariants
	// A) Active legacy job was cancelled and marked unreplayable
	var qStatus, qErrCode string
	if err := pool.QueryRow(ctx, `SELECT status, COALESCE(error_code, '') FROM investigation_jobs WHERE id = $1`, legacyQueuedJobID).Scan(&qStatus, &qErrCode); err != nil {
		t.Fatalf("query legacy queued job: %v", err)
	}
	if qStatus != "cancelled" || qErrCode != "MIGRATION_LEGACY_UNREPLAYABLE" {
		t.Fatalf("legacy queued job was not safely cancelled: status=%s, errCode=%s", qStatus, qErrCode)
	}

	// B) Historical completed job was backfilled with complete validated job_payload
	var cReqHash string
	var cPayload []byte
	if err := pool.QueryRow(ctx, `SELECT request_hash, job_payload FROM investigation_jobs WHERE id = $1`, legacyCompletedJobID).Scan(&cReqHash, &cPayload); err != nil {
		t.Fatalf("query legacy completed job: %v", err)
	}
	if cReqHash == "" {
		t.Fatal("legacy completed job request_hash was not backfilled")
	}
	store, err := investigator.NewPostgresInvestigationStore(pool, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	// Verify that store.Get on the upgraded database can cleanly read the historical completed job
	scope := investigator.TrustedScope{
		ActorID: legacyActorID, Role: "OPS_VIEWER", SelectedPaymentID: legacyPaymentID,
	}
	retrievedJob, err := store.Get(ctx, scope, legacyCompletedJobID)
	if err != nil {
		t.Fatalf("store.Get on backfilled historical job failed: %v", err)
	}
	if retrievedJob.Status != investigator.StatusCompleted || retrievedJob.Report == nil {
		t.Fatalf("historical job report lost during migration: status=%s, report=%v", retrievedJob.Status, retrievedJob.Report)
	}

	// C) Historical usage accounting was preserved into decoupled dimensions
	var resInput, actInput int64
	if err := pool.QueryRow(ctx, `SELECT reserved_input, actual_input FROM investigation_usage_reservations WHERE id = $1`, legacyResID).Scan(&resInput, &actInput); err != nil {
		t.Fatalf("query upgraded reservation: %v", err)
	}
	if resInput != 500 || actInput != 420 {
		t.Fatalf("historical token accounting was lost: reserved_input=%d (want 500), actual_input=%d (want 420)", resInput, actInput)
	}

	// Step 6: Run full store conformance suite on the upgraded database
	storetest.Run(t, func(t *testing.T, limits investigator.UsageAllowance) investigator.InvestigationStore {
		t.Helper()

		subCtx, subCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer subCancel()

		// Clean tables between subtests for complete test isolation against upgraded schema
		if _, err := pool.Exec(subCtx, "TRUNCATE investigation_usage_reservations, investigation_jobs CASCADE;"); err != nil {
			t.Fatalf("truncate investigation tables: %v", err)
		}

		s, err := investigator.NewPostgresInvestigationStore(pool, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SetUsageLimits(subCtx, limits); err != nil {
			t.Fatal(err)
		}
		return s
	})
}
