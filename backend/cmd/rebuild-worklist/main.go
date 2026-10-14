package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/database"
	"github.com/transactx/backend/internal/projections"
)

func main() {
	preview := flag.Bool("preview", false, "Preview current counts and discrepancies without rebuilding")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		logger.Error("DATABASE_URL environment variable is required")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := database.NewPool(ctx, dbURL)
	if err != nil {
		logger.Error("failed to connect database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if *preview {
		runPreview(ctx, pool, logger)
		return
	}

	runRebuild(ctx, pool, logger)
}

func runPreview(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) {
	var authCount int64
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM payments").Scan(&authCount); err != nil {
		logger.Error("failed to count authoritative payments", "error", err)
		os.Exit(1)
	}

	var projCount int64
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM ops_payment_worklist").Scan(&projCount); err != nil {
		logger.Error("failed to count projected worklist", "error", err)
		os.Exit(1)
	}

	diff := authCount - projCount
	fmt.Printf("Worklist Projection Preview:\n")
	fmt.Printf("  Authoritative Payments: %d\n", authCount)
	fmt.Printf("  Projected Worklist:     %d\n", projCount)
	fmt.Printf("  Discrepancy (Missing):  %d\n", diff)
	if diff == 0 {
		fmt.Printf("  Status: IN_SYNC\n")
	} else {
		fmt.Printf("  Status: OUT_OF_SYNC (Rebuild recommended)\n")
	}
}

func runRebuild(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) {
	start := time.Now()
	logger.Info("starting worklist projection rebuild", "target", "ops_payment_worklist")

	// Acquire exclusive maintenance transaction advisory lock
	var locked bool
	err = tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1)", projections.WorklistMaintenanceLockKey).Scan(&locked)
	if err != nil || !locked {
		logger.Error("failed to acquire exclusive maintenance lock: another rebuild is in progress or workers are holding projection locks", "error", err)
		os.Exit(1)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		logger.Error("failed to begin transaction", "error", err)
		os.Exit(1)
	}
	defer tx.Rollback(ctx)

	// 1. Build temporary snapshot from authoritative tables
	createTempQuery := `
		CREATE TEMP TABLE tmp_rebuild_worklist ON COMMIT DROP AS
		SELECT p.id AS payment_id,
		       p.state AS current_state,
		       p.event_version,
		       p.amount_paise,
		       p.currency,
		       p.source_bank_id,
		       p.destination_bank_id,
		       p.route_bank_id,
		       p.created_at,
		       p.updated_at,
		       rj.status AS recovery_status,
		       COALESCE(rj.attempt_count, 0) AS recovery_attempts,
		       rj.last_reason_code AS last_recovery_reason,
		       rj.updated_at AS last_recovery_at,
		       NOW() AS projected_at
		FROM payments p
		LEFT JOIN recovery_jobs rj ON p.id = rj.payment_id`

	if _, err := tx.Exec(ctx, createTempQuery); err != nil {
		logger.Error("failed to create temporary snapshot table", "error", err)
		os.Exit(1)
	}

	var tempCount int64
	if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM tmp_rebuild_worklist").Scan(&tempCount); err != nil {
		logger.Error("failed to count temporary snapshot", "error", err)
		os.Exit(1)
	}

	var authCount int64
	if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM payments").Scan(&authCount); err != nil {
		logger.Error("failed to count authoritative payments", "error", err)
		os.Exit(1)
	}

	if tempCount != authCount {
		logger.Error("snapshot row count mismatch against source payments", "snapshot", tempCount, "source", authCount)
		os.Exit(1)
	}

	// 2. Atomically swap into ops_payment_worklist
	if _, err := tx.Exec(ctx, "TRUNCATE ops_payment_worklist"); err != nil {
		logger.Error("failed to truncate target projection table", "error", err)
		os.Exit(1)
	}

	insertQuery := `
		INSERT INTO ops_payment_worklist (
		    payment_id, current_state, event_version, amount_paise, currency,
		    source_bank_id, destination_bank_id, route_bank_id, created_at, updated_at,
		    recovery_status, recovery_attempts, last_recovery_reason, last_recovery_at,
		    projected_at
		) SELECT * FROM tmp_rebuild_worklist`

	tag, err := tx.Exec(ctx, insertQuery)
	if err != nil {
		logger.Error("failed to insert rebuilt projection rows", "error", err)
		os.Exit(1)
	}

	// 3. Commit the atomic swap
	if err := tx.Commit(ctx); err != nil {
		logger.Error("failed to commit rebuild transaction", "error", err)
		os.Exit(1)
	}

	duration := time.Since(start)
	logger.Info("worklist projection rebuild complete",
		"rows_rebuilt", tag.RowsAffected(),
		"duration_ms", duration.Milliseconds(),
	)

	// Record audit entry
	auditQuery := `
		INSERT INTO operational_audit_logs (
		    actor_id, actor_role, operation, entity_type, entity_id, reason_code, details, result
		) VALUES (
		    '00000000-0000-0000-0000-000000000000', 'OPS_ADMIN', 'REBUILD_WORKLIST',
		    'PROJECTION', 'ops_payment_worklist', 'M2_MAINTENANCE_REBUILD',
		    jsonb_build_object('rows', $1::bigint, 'durationMs', $2::bigint), 'SUCCESS'
		)`
	if _, err := pool.Exec(context.Background(), auditQuery, tag.RowsAffected(), duration.Milliseconds()); err != nil {
		logger.Error("failed to record operational audit log", "error", err)
	}

	fmt.Printf("Successfully rebuilt %d worklist rows in %v\n", tag.RowsAffected(), duration)
}
