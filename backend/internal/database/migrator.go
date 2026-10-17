package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// DefaultAdvisoryLockID is the stable 64-bit advisory lock key used for migration mutual exclusion.
	DefaultAdvisoryLockID int64 = 839217491047120391

	// DefaultLockTimeout is the maximum time to wait for the migration advisory lock.
	DefaultLockTimeout = 15 * time.Second

	// SchemaMigrationsTable is the authoritative ledger table for applied migrations.
	SchemaMigrationsTable = "schema_migrations"
)

var (
	ErrChecksumMismatch          = errors.New("migration checksum mismatch")
	ErrUnknownOrdering           = errors.New("unknown migration ordering")
	ErrLockAcquisitionTimeout    = errors.New("failed to acquire migration advisory lock within timeout")
	ErrBaselineSchemaNotVerified = errors.New("baseline schema verification failed: required baseline tables are missing")
	ErrInvalidMigrationFile      = errors.New("invalid migration filename format")
)

// MigrationRecord represents an entry in the schema_migrations ledger.
type MigrationRecord struct {
	Version         string    `json:"version"`
	Name            string    `json:"name"`
	Checksum        string    `json:"checksum"`
	AppliedAt       time.Time `json:"appliedAt"`
	ExecutionTimeMs int64     `json:"executionTimeMs"`
}

// MigrationFile represents a parsed .up.sql file on disk.
type MigrationFile struct {
	Version       string
	Name          string
	Path          string
	Checksum      string
	Content       string
	NoTransaction bool
}

// MigrationStatus captures the current migration state of the database.
type MigrationStatus struct {
	CurrentVersion string            `json:"currentVersion"`
	Applied        []MigrationRecord `json:"applied"`
	Pending        []MigrationFile   `json:"pending"`
	TotalFiles     int               `json:"totalFiles"`
}

// Migrator manages schema migrations with checksum verification and advisory locking.
type Migrator struct {
	pool          *pgxpool.Pool
	migrationsDir string
	lockID        int64
	lockTimeout   time.Duration
	targetDB      string
	logger        *slog.Logger
}

// MigratorOption allows customizing Migrator configuration.
type MigratorOption func(*Migrator)

// WithLockTimeout sets the advisory lock timeout.
func WithLockTimeout(timeout time.Duration) MigratorOption {
	return func(m *Migrator) {
		if timeout > 0 {
			m.lockTimeout = timeout
		}
	}
}

// WithAdvisoryLockID sets a custom advisory lock ID.
func WithAdvisoryLockID(lockID int64) MigratorOption {
	return func(m *Migrator) {
		m.lockID = lockID
	}
}

// WithTargetDatabase sets the expected database name for target validation.
func WithTargetDatabase(dbName string) MigratorOption {
	return func(m *Migrator) {
		m.targetDB = dbName
	}
}

// WithLogger sets a structured logger.
func WithLogger(logger *slog.Logger) MigratorOption {
	return func(m *Migrator) {
		if logger != nil {
			m.logger = logger
		}
	}
}

// NewMigrator initializes a new Migrator instance.
func NewMigrator(pool *pgxpool.Pool, migrationsDir string, opts ...MigratorOption) (*Migrator, error) {
	if pool == nil {
		return nil, errors.New("migrator requires a non-nil database pool")
	}
	cleanDir := filepath.Clean(migrationsDir)
	info, err := os.Stat(cleanDir)
	if err != nil {
		return nil, fmt.Errorf("migrations directory inaccessible: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("migrations path %s is not a directory", cleanDir)
	}

	m := &Migrator{
		pool:          pool,
		migrationsDir: cleanDir,
		lockID:        DefaultAdvisoryLockID,
		lockTimeout:   DefaultLockTimeout,
		logger:        slog.Default(),
	}

	for _, opt := range opts {
		opt(m)
	}

	return m, nil
}

// LoadMigrationFiles loads and validates all *.up.sql migration files in the migrations directory.
func (m *Migrator) LoadMigrationFiles() ([]MigrationFile, error) {
	entries, err := os.ReadDir(m.migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations directory: %w", err)
	}

	var files []MigrationFile
	seenVersions := make(map[string]string)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}

		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) < 2 || parts[0] == "" {
			return nil, fmt.Errorf("%w: %s must start with numeric prefix followed by underscore", ErrInvalidMigrationFile, entry.Name())
		}
		version := parts[0]

		if prev, exists := seenVersions[version]; exists {
			return nil, fmt.Errorf("duplicate migration version %s found in %s and %s", version, prev, entry.Name())
		}
		seenVersions[version] = entry.Name()

		filePath := filepath.Join(m.migrationsDir, entry.Name())
		rawContent, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read migration file %s: %w", entry.Name(), err)
		}

		sum := sha256.Sum256(rawContent)
		checksum := hex.EncodeToString(sum[:])
		contentStr := strings.TrimPrefix(string(rawContent), "\ufeff")
		noTx := strings.Contains(contentStr, "-- transactx:no-transaction")

		files = append(files, MigrationFile{
			Version:       version,
			Name:          entry.Name(),
			Path:          filePath,
			Checksum:      checksum,
			Content:       contentStr,
			NoTransaction: noTx,
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Version < files[j].Version
	})

	return files, nil
}

// ensureLedgerTable creates the schema_migrations table if it does not exist.
func (m *Migrator) ensureLedgerTable(ctx context.Context, tx pgx.Tx) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			version VARCHAR(64) PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			checksum VARCHAR(64) NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			execution_time_ms BIGINT NOT NULL DEFAULT 0
		);
	`, SchemaMigrationsTable)
	_, err := tx.Exec(ctx, query)
	return err
}

// fetchAppliedRecords queries the schema_migrations ledger on a specific connection or transaction.
func (m *Migrator) fetchAppliedRecords(ctx context.Context, q interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}) ([]MigrationRecord, error) {
	query := fmt.Sprintf(`
		SELECT version, name, checksum, applied_at, execution_time_ms
		FROM %s
		ORDER BY version ASC;
	`, SchemaMigrationsTable)

	rows, err := q.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []MigrationRecord
	for rows.Next() {
		var rec MigrationRecord
		if err := rows.Scan(&rec.Version, &rec.Name, &rec.Checksum, &rec.AppliedAt, &rec.ExecutionTimeMs); err != nil {
			return nil, err
		}
		records = append(records, rec)
	}

	return records, rows.Err()
}

// acquireAdvisoryLock acquires the session-level advisory lock on a dedicated connection.
func (m *Migrator) acquireAdvisoryLock(ctx context.Context, conn *pgxpool.Conn) error {
	deadline := time.Now().Add(m.lockTimeout)
	for {
		var acquired bool
		err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", m.lockID).Scan(&acquired)
		if err != nil {
			return fmt.Errorf("error attempting advisory lock: %w", err)
		}
		if acquired {
			return nil
		}

		if time.Now().After(deadline) {
			return ErrLockAcquisitionTimeout
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// releaseAdvisoryLock releases the session-level advisory lock on the dedicated connection.
func (m *Migrator) releaseAdvisoryLock(ctx context.Context, conn *pgxpool.Conn) error {
	var released bool
	err := conn.QueryRow(ctx, "SELECT pg_advisory_unlock($1)", m.lockID).Scan(&released)
	if err != nil {
		return fmt.Errorf("error releasing advisory lock: %w", err)
	}
	if !released {
		return errors.New("advisory lock was not held when release was attempted")
	}
	return nil
}

// validateTargetDatabase verifies the connected database matches the target restriction if specified.
func (m *Migrator) validateTargetDatabase(ctx context.Context, conn *pgxpool.Conn) error {
	if m.targetDB == "" {
		return nil
	}
	var currentDB string
	if err := conn.QueryRow(ctx, "SELECT current_database()").Scan(&currentDB); err != nil {
		return fmt.Errorf("failed to query current_database(): %w", err)
	}
	if currentDB != m.targetDB {
		return fmt.Errorf("target database mismatch: connected to %q, expected %q", currentDB, m.targetDB)
	}
	return nil
}

// Up applies all unapplied migrations sequentially and transactionally.
func (m *Migrator) Up(ctx context.Context) (int, error) {
	conn, err := m.pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to acquire dedicated connection for migration: %w", err)
	}
	defer conn.Release()

	if err := m.validateTargetDatabase(ctx, conn); err != nil {
		return 0, err
	}

	if err := m.acquireAdvisoryLock(ctx, conn); err != nil {
		return 0, err
	}
	defer func() {
		_ = m.releaseAdvisoryLock(context.Background(), conn)
	}()

	initTx, err := conn.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to begin init transaction: %w", err)
	}
	if err := m.ensureLedgerTable(ctx, initTx); err != nil {
		_ = initTx.Rollback(ctx)
		return 0, fmt.Errorf("failed to ensure ledger table: %w", err)
	}
	if err := initTx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("failed to commit init transaction: %w", err)
	}

	files, err := m.LoadMigrationFiles()
	if err != nil {
		return 0, err
	}

	applied, err := m.fetchAppliedRecords(ctx, conn)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch applied migration records: %w", err)
	}

	// Verify checksums and ordering for already-applied migrations
	appliedMap := make(map[string]MigrationRecord)
	for i, rec := range applied {
		if i >= len(files) {
			return 0, fmt.Errorf("%w: recorded version %s has no matching file on disk", ErrUnknownOrdering, rec.Version)
		}
		if files[i].Version != rec.Version {
			return 0, fmt.Errorf("%w: expected file version %s at index %d, but recorded version is %s", ErrUnknownOrdering, files[i].Version, i, rec.Version)
		}
		if files[i].Checksum != rec.Checksum {
			return 0, fmt.Errorf("%w for version %s (%s): file checksum is %s, recorded checksum is %s",
				ErrChecksumMismatch, rec.Version, rec.Name, files[i].Checksum, rec.Checksum)
		}
		appliedMap[rec.Version] = rec
	}

	// Apply pending migrations
	var appliedCount int
	for _, file := range files {
		if _, exists := appliedMap[file.Version]; exists {
			continue
		}

		m.logger.Info("applying migration",
			slog.String("version", file.Version),
			slog.String("name", file.Name),
		)

		start := time.Now()

		if file.NoTransaction {
			if _, err := conn.Exec(ctx, file.Content); err != nil {
				return appliedCount, fmt.Errorf("failed applying non-transactional migration %s: %w", file.Version, err)
			}
			execDuration := time.Since(start).Milliseconds()
			insertQuery := fmt.Sprintf(`
				INSERT INTO %s (version, name, checksum, applied_at, execution_time_ms)
				VALUES ($1, $2, $3, NOW(), $4)
			`, SchemaMigrationsTable)
			if _, err := conn.Exec(ctx, insertQuery, file.Version, file.Name, file.Checksum, execDuration); err != nil {
				return appliedCount, fmt.Errorf("failed recording non-transactional migration %s in ledger: %w", file.Version, err)
			}
		} else {
			tx, err := conn.Begin(ctx)
			if err != nil {
				return appliedCount, fmt.Errorf("failed to begin transaction for migration %s: %w", file.Version, err)
			}

			if _, err := tx.Exec(ctx, file.Content); err != nil {
				_ = tx.Rollback(ctx)
				return appliedCount, fmt.Errorf("failed applying migration %s (%s): %w", file.Version, file.Name, err)
			}

			execDuration := time.Since(start).Milliseconds()
			insertQuery := fmt.Sprintf(`
				INSERT INTO %s (version, name, checksum, applied_at, execution_time_ms)
				VALUES ($1, $2, $3, NOW(), $4)
			`, SchemaMigrationsTable)
			if _, err := tx.Exec(ctx, insertQuery, file.Version, file.Name, file.Checksum, execDuration); err != nil {
				_ = tx.Rollback(ctx)
				return appliedCount, fmt.Errorf("failed recording migration %s in ledger: %w", file.Version, err)
			}

			if err := tx.Commit(ctx); err != nil {
				return appliedCount, fmt.Errorf("failed committing migration %s: %w", file.Version, err)
			}
		}

		elapsed := time.Since(start)
		m.logger.Info("migration applied successfully",
			slog.String("version", file.Version),
			slog.String("name", file.Name),
			slog.Duration("duration", elapsed),
		)
		appliedCount++
	}

	return appliedCount, nil
}

// Status returns the current migration status without applying any changes.
func (m *Migrator) Status(ctx context.Context) (*MigrationStatus, error) {
	conn, err := m.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	files, err := m.LoadMigrationFiles()
	if err != nil {
		return nil, err
	}

	var tableExists bool
	checkQuery := `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables 
			WHERE table_name = $1
		);
	`
	if err := conn.QueryRow(ctx, checkQuery, SchemaMigrationsTable).Scan(&tableExists); err != nil {
		return nil, err
	}

	var applied []MigrationRecord
	if tableExists {
		applied, err = m.fetchAppliedRecords(ctx, conn)
		if err != nil {
			return nil, err
		}
	}

	appliedSet := make(map[string]bool)
	var currentVersion string
	for _, rec := range applied {
		appliedSet[rec.Version] = true
		currentVersion = rec.Version
	}

	var pending []MigrationFile
	for _, file := range files {
		if !appliedSet[file.Version] {
			pending = append(pending, file)
		}
	}

	return &MigrationStatus{
		CurrentVersion: currentVersion,
		Applied:        applied,
		Pending:        pending,
		TotalFiles:     len(files),
	}, nil
}

// Verify checks that all recorded migrations match on-disk file checksums and order.
func (m *Migrator) Verify(ctx context.Context) error {
	conn, err := m.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	files, err := m.LoadMigrationFiles()
	if err != nil {
		return err
	}

	applied, err := m.fetchAppliedRecords(ctx, conn)
	if err != nil {
		return err
	}

	for i, rec := range applied {
		if i >= len(files) {
			return fmt.Errorf("%w: recorded version %s has no matching file on disk", ErrUnknownOrdering, rec.Version)
		}
		if files[i].Version != rec.Version {
			return fmt.Errorf("%w: file version %s vs recorded version %s", ErrUnknownOrdering, files[i].Version, rec.Version)
		}
		if files[i].Checksum != rec.Checksum {
			return fmt.Errorf("%w for version %s: disk=%s recorded=%s", ErrChecksumMismatch, rec.Version, files[i].Checksum, rec.Checksum)
		}
	}

	return nil
}

// Baseline records existing migrations up to baselineVersion as applied without executing their SQL.
// It verifies that the database actually contains the expected core tables before establishing the baseline.
func (m *Migrator) Baseline(ctx context.Context, baselineVersion string) error {
	conn, err := m.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	if err := m.validateTargetDatabase(ctx, conn); err != nil {
		return err
	}

	if err := m.acquireAdvisoryLock(ctx, conn); err != nil {
		return err
	}
	defer func() {
		_ = m.releaseAdvisoryLock(context.Background(), conn)
	}()

	// 1. Verify schema: ensure core baseline tables actually exist in the database
	requiredTables := []string{
		"users",
		"accounts",
		"banks",
		"payments",
		"ledger_transactions",
		"ledger_entries",
		"idempotency_records",
		"payment_state_transitions",
	}

	for _, tbl := range requiredTables {
		var exists bool
		checkQuery := `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.tables 
				WHERE table_schema = 'public' AND table_name = $1
			);
		`
		if err := conn.QueryRow(ctx, checkQuery, tbl).Scan(&exists); err != nil {
			return fmt.Errorf("error verifying table %s: %w", tbl, err)
		}
		if !exists {
			return fmt.Errorf("%w: table %q does not exist", ErrBaselineSchemaNotVerified, tbl)
		}
	}

	// 2. Ensure schema_migrations table exists
	initTx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	if err := m.ensureLedgerTable(ctx, initTx); err != nil {
		_ = initTx.Rollback(ctx)
		return fmt.Errorf("failed to ensure ledger table: %w", err)
	}
	if err := initTx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit ledger table init: %w", err)
	}

	files, err := m.LoadMigrationFiles()
	if err != nil {
		return err
	}

	var foundTarget bool
	for _, f := range files {
		if f.Version == baselineVersion {
			foundTarget = true
			break
		}
	}
	if !foundTarget {
		return fmt.Errorf("baseline version %q is not found in migration files", baselineVersion)
	}

	// 3. Insert records for all files up to baselineVersion
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin baseline transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, file := range files {
		if file.Version > baselineVersion {
			break
		}

		insertQuery := fmt.Sprintf(`
			INSERT INTO %s (version, name, checksum, applied_at, execution_time_ms)
			VALUES ($1, $2, $3, NOW(), 0)
			ON CONFLICT (version) DO NOTHING;
		`, SchemaMigrationsTable)

		if _, err := tx.Exec(ctx, insertQuery, file.Version, file.Name, file.Checksum); err != nil {
			return fmt.Errorf("failed to record baseline migration %s: %w", file.Version, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit baseline: %w", err)
	}

	m.logger.Info("baseline established successfully",
		slog.String("baselineVersion", baselineVersion),
	)

	return nil
}
