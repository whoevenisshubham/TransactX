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
	if applied < 29 {
		t.Fatalf("expected at least 29 migrations applied, got %d", applied)
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
	if applied < 29 {
		t.Fatalf("expected at least 29 migrations applied, got %d", applied)
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
