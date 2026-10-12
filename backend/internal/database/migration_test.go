package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func randomDBName(prefix string) string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(b))
}

func setupDisposableDB(t *testing.T) (string, func()) {
	t.Helper()
	baseURL := os.Getenv("DATABASE_URL")
	if baseURL == "" {
		t.Skip("DATABASE_URL is not set, skipping integration database test")
	}

	config, err := pgxpool.ParseConfig(baseURL)
	if err != nil {
		t.Fatalf("failed to parse DATABASE_URL: %v", err)
	}

	dbName := randomDBName("tx_mig_test")

	// Connect to default postgres to create database
	origDB := config.ConnConfig.Database
	config.ConnConfig.Database = "postgres"
	adminPool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("failed to connect to admin database: %v", err)
	}

	_, err = adminPool.Exec(context.Background(), fmt.Sprintf("CREATE DATABASE %s;", dbName))
	if err != nil {
		adminPool.Close()
		t.Fatalf("failed to create disposable database %s: %v", dbName, err)
	}

	config.ConnConfig.Database = dbName
	testPool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		_, _ = adminPool.Exec(context.Background(), fmt.Sprintf("DROP DATABASE %s WITH (FORCE);", dbName))
		adminPool.Close()
		t.Fatalf("failed to connect to new disposable database %s: %v", dbName, err)
	}
	testPool.Close()

	config.ConnConfig.Database = origDB

	cleanup := func() {
		// Drop test database with force
		_, _ = adminPool.Exec(context.Background(), fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE);", dbName))
		adminPool.Close()
	}

	// Build connection string for disposable database
	testDBURL := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		config.ConnConfig.User,
		config.ConnConfig.Password,
		config.ConnConfig.Host,
		config.ConnConfig.Port,
		dbName,
	)

	return testDBURL, cleanup
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

func TestMigrator_EmptyDBToCurrent(t *testing.T) {
	testDBURL, cleanup := setupDisposableDB(t)
	defer cleanup()

	pool, err := NewPool(context.Background(), testDBURL)
	if err != nil {
		t.Fatalf("failed to connect to test pool: %v", err)
	}
	defer pool.Close()

	migrationsDir := findRepoMigrationsDir(t)
	migrator, err := NewMigrator(pool, migrationsDir)
	if err != nil {
		t.Fatalf("failed to create migrator: %v", err)
	}

	applied, err := migrator.Up(context.Background())
	if err != nil {
		t.Fatalf("Migrator.Up failed on empty DB: %v", err)
	}
	if applied < 21 {
		t.Fatalf("expected at least 21 migrations applied, got %d", applied)
	}

	// Verify schema_migrations ledger rows
	status, err := migrator.Status(context.Background())
	if err != nil {
		t.Fatalf("Migrator.Status failed: %v", err)
	}
	if len(status.Applied) != applied {
		t.Fatalf("expected status.Applied == %d, got %d", applied, len(status.Applied))
	}
	if len(status.Pending) != 0 {
		t.Fatalf("expected 0 pending migrations, got %d", len(status.Pending))
	}

	// Verify one of the tables actually exists and works
	var count int
	err = pool.QueryRow(context.Background(), "SELECT count(*) FROM users").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query users table created by migrations: %v", err)
	}
}

func TestMigrator_SecondRunNoOp(t *testing.T) {
	testDBURL, cleanup := setupDisposableDB(t)
	defer cleanup()

	pool, err := NewPool(context.Background(), testDBURL)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer pool.Close()

	migrationsDir := findRepoMigrationsDir(t)
	migrator, err := NewMigrator(pool, migrationsDir)
	if err != nil {
		t.Fatalf("failed to create migrator: %v", err)
	}

	// First run
	firstApplied, err := migrator.Up(context.Background())
	if err != nil {
		t.Fatalf("first Up failed: %v", err)
	}
	if firstApplied == 0 {
		t.Fatal("expected migrations applied on first run")
	}

	// Second run must be no-op
	secondApplied, err := migrator.Up(context.Background())
	if err != nil {
		t.Fatalf("second Up failed: %v", err)
	}
	if secondApplied != 0 {
		t.Fatalf("expected 0 migrations on second run, got %d", secondApplied)
	}

	// Verify integrity remains intact
	if err := migrator.Verify(context.Background()); err != nil {
		t.Fatalf("Migrator.Verify failed after second run: %v", err)
	}
}

func TestMigrator_ConcurrentMigrationAttempts(t *testing.T) {
	testDBURL, cleanup := setupDisposableDB(t)
	defer cleanup()

	pool, err := NewPool(context.Background(), testDBURL)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer pool.Close()

	migrationsDir := findRepoMigrationsDir(t)

	var wg sync.WaitGroup
	results := make([]error, 2)
	appliedCounts := make([]int, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			m, err := NewMigrator(pool, migrationsDir, WithLockTimeout(10*time.Second))
			if err != nil {
				results[idx] = err
				return
			}
			cnt, err := m.Up(context.Background())
			results[idx] = err
			appliedCounts[idx] = cnt
		}(i)
	}
	wg.Wait()

	for i, r := range results {
		if r != nil {
			t.Fatalf("concurrent runner %d encountered error: %v", i, r)
		}
	}

	totalApplied := appliedCounts[0] + appliedCounts[1]
	status, err := (&Migrator{pool: pool, migrationsDir: migrationsDir}).Status(context.Background())
	if err != nil {
		t.Fatalf("Status check failed: %v", err)
	}

	// One runner must apply the migrations and the other runner must see 0 pending (no-op)
	if totalApplied != len(status.Applied) {
		t.Fatalf("expected totalApplied (%d) == len(status.Applied) (%d)", totalApplied, len(status.Applied))
	}
}

func TestMigrator_ChecksumAlterationDetected(t *testing.T) {
	testDBURL, cleanup := setupDisposableDB(t)
	defer cleanup()

	pool, err := NewPool(context.Background(), testDBURL)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer pool.Close()

	tempDir := t.TempDir()

	// Create 2 test migrations
	m1 := "000001_initial.up.sql"
	m2 := "000002_add_table.up.sql"

	_ = os.WriteFile(filepath.Join(tempDir, m1), []byte("CREATE TABLE test_table_1 (id INT PRIMARY KEY);"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, m2), []byte("CREATE TABLE test_table_2 (id INT PRIMARY KEY);"), 0644)

	migrator, err := NewMigrator(pool, tempDir)
	if err != nil {
		t.Fatalf("failed to create migrator: %v", err)
	}

	applied, err := migrator.Up(context.Background())
	if err != nil || applied != 2 {
		t.Fatalf("expected 2 migrations applied, got %d, err: %v", applied, err)
	}

	// Tamper with m1 on disk
	_ = os.WriteFile(filepath.Join(tempDir, m1), []byte("CREATE TABLE test_table_1 (id INT PRIMARY KEY, tampered INT);"), 0644)

	// Now running Up or Verify must detect checksum mismatch
	_, err = migrator.Up(context.Background())
	if err == nil {
		t.Fatal("expected checksum mismatch error on tampered file, got nil")
	}

	if err := migrator.Verify(context.Background()); err == nil {
		t.Fatal("expected Verify to fail on tampered file, got nil")
	}
}

func TestMigrator_FailedMigrationRollback(t *testing.T) {
	testDBURL, cleanup := setupDisposableDB(t)
	defer cleanup()

	pool, err := NewPool(context.Background(), testDBURL)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer pool.Close()

	tempDir := t.TempDir()

	m1 := "000001_good.up.sql"
	m2 := "000002_bad_syntax.up.sql"

	_ = os.WriteFile(filepath.Join(tempDir, m1), []byte("CREATE TABLE good_table (id INT PRIMARY KEY);"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, m2), []byte("CREATE TABLE bad_table (id INT INVALID SYNTAX HERE);"), 0644)

	migrator, err := NewMigrator(pool, tempDir)
	if err != nil {
		t.Fatalf("failed to create migrator: %v", err)
	}

	applied, err := migrator.Up(context.Background())
	if err == nil {
		t.Fatal("expected migration 000002 to fail with syntax error, got nil")
	}
	if applied != 1 {
		t.Fatalf("expected 1 migration applied (000001), got %d", applied)
	}

	// Verify that bad_table was NOT created and 000002 is NOT in ledger
	var badTableExists bool
	_ = pool.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables WHERE table_name = 'bad_table'
		);
	`).Scan(&badTableExists)
	if badTableExists {
		t.Fatal("bad_table should have rolled back and not exist")
	}

	status, err := migrator.Status(context.Background())
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if len(status.Applied) != 1 || status.Applied[0].Version != "000001" {
		t.Fatalf("expected only 000001 in applied, got: %+v", status.Applied)
	}
	if len(status.Pending) != 1 || status.Pending[0].Version != "000002" {
		t.Fatalf("expected 000002 still pending, got: %+v", status.Pending)
	}
}

func TestMigrator_BaselineRejectionOnEmptyDB(t *testing.T) {
	testDBURL, cleanup := setupDisposableDB(t)
	defer cleanup()

	pool, err := NewPool(context.Background(), testDBURL)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer pool.Close()

	migrationsDir := findRepoMigrationsDir(t)
	migrator, err := NewMigrator(pool, migrationsDir)
	if err != nil {
		t.Fatalf("failed to create migrator: %v", err)
	}

	// Baseline on an empty database must fail schema verification
	err = migrator.Baseline(context.Background(), "000021")
	if err == nil {
		t.Fatal("expected Baseline on empty database to fail, got nil")
	}
}

func TestMigrator_BaselineSuccessOnPopulatedDB(t *testing.T) {
	testDBURL, cleanup := setupDisposableDB(t)
	defer cleanup()

	pool, err := NewPool(context.Background(), testDBURL)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer pool.Close()

	migrationsDir := findRepoMigrationsDir(t)

	// Simulate untracked existing database by applying SQL files without schema_migrations table
	files, err := (&Migrator{migrationsDir: migrationsDir}).LoadMigrationFiles()
	if err != nil {
		t.Fatalf("failed to load files: %v", err)
	}

	for _, f := range files {
		_, err := pool.Exec(context.Background(), f.Content)
		if err != nil {
			t.Fatalf("failed executing raw migration %s: %v", f.Name, err)
		}
	}

	// Drop schema_migrations if any raw script created it (none should have)
	_, _ = pool.Exec(context.Background(), "DROP TABLE IF EXISTS schema_migrations")

	migrator, err := NewMigrator(pool, migrationsDir)
	if err != nil {
		t.Fatalf("failed to create migrator: %v", err)
	}

	// Baseline up to 000021
	err = migrator.Baseline(context.Background(), "000021")
	if err != nil {
		t.Fatalf("Baseline failed on populated database: %v", err)
	}

	// Now Up should be a no-op
	applied, err := migrator.Up(context.Background())
	if err != nil {
		t.Fatalf("Up failed after baseline: %v", err)
	}
	if applied != 0 {
		t.Fatalf("expected 0 applied after baseline, got %d", applied)
	}

	// Status should show all 21 applied
	status, err := migrator.Status(context.Background())
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if len(status.Applied) != 21 {
		t.Fatalf("expected 21 applied, got %d", len(status.Applied))
	}
}

func TestMigrator_TargetDatabaseValidation(t *testing.T) {
	testDBURL, cleanup := setupDisposableDB(t)
	defer cleanup()

	pool, err := NewPool(context.Background(), testDBURL)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer pool.Close()

	migrationsDir := findRepoMigrationsDir(t)
	migrator, err := NewMigrator(pool, migrationsDir, WithTargetDatabase("wrong_expected_db"))
	if err != nil {
		t.Fatalf("failed to create migrator: %v", err)
	}

	_, err = migrator.Up(context.Background())
	if err == nil {
		t.Fatal("expected target database mismatch error, got nil")
	}
}
