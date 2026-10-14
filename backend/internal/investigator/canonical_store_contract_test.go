package investigator_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/investigator"
	"github.com/transactx/backend/internal/investigator/storetest"
)

const (
	canonicalStoreDatabaseURLEnv = "M3_CANONICAL_STORE_DATABASE_URL"
	canonicalStoreResetNameEnv   = "M3_CANONICAL_STORE_RESET_DATABASE"
)

// TestPostgresInvestigationStoreCanonicalSchemaConformance runs the reusable
// store contract against tables created by the canonical migrations. Unlike
// TestPostgresInvestigationStoreConformance, it creates no schema or DDL.
//
// This test truncates the three investigator store tables. It is deliberately
// opt-in and refuses to run unless the caller repeats the connected database's
// exact name and that name ends in _test.
func TestPostgresInvestigationStoreCanonicalSchemaConformance(t *testing.T) {
	databaseURL := os.Getenv(canonicalStoreDatabaseURLEnv)
	expectedDatabase := os.Getenv(canonicalStoreResetNameEnv)
	if databaseURL == "" && expectedDatabase == "" {
		t.Skipf("%s and %s are not set: skipping canonical-schema store conformance", canonicalStoreDatabaseURLEnv, canonicalStoreResetNameEnv)
	}
	if databaseURL == "" || expectedDatabase == "" {
		t.Fatalf("%s and %s must both be set", canonicalStoreDatabaseURLEnv, canonicalStoreResetNameEnv)
	}
	if !strings.HasSuffix(strings.ToLower(expectedDatabase), "_test") {
		t.Fatalf("%s must name a dedicated database ending in _test", canonicalStoreResetNameEnv)
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse canonical store database URL: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = "public"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect canonical store database: %v", err)
	}
	t.Cleanup(pool.Close)

	var actualDatabase string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&actualDatabase); err != nil {
		t.Fatalf("read canonical store database name: %v", err)
	}
	if actualDatabase != expectedDatabase {
		t.Fatalf("refusing reset: connected database %q differs from %s=%q", actualDatabase, canonicalStoreResetNameEnv, expectedDatabase)
	}

	lockConnection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire canonical store lock connection: %v", err)
	}
	var locked bool
	if err := lockConnection.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('transactx-m3-canonical-store-contract'))`).Scan(&locked); err != nil {
		lockConnection.Release()
		t.Fatalf("acquire canonical store advisory lock: %v", err)
	}
	if !locked {
		lockConnection.Release()
		t.Fatal("another canonical store conformance run holds the database lock")
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := lockConnection.Exec(cleanupCtx, `SELECT pg_advisory_unlock(hashtext('transactx-m3-canonical-store-contract'))`); err != nil {
			t.Errorf("release canonical store advisory lock: %v", err)
		}
		lockConnection.Release()
	})

	assertCanonicalStoreSchema(t, pool)
	storetest.Run(t, func(t *testing.T, limits investigator.UsageAllowance) investigator.InvestigationStore {
		t.Helper()
		resetCanonicalStoreTables(t, pool)
		t.Cleanup(func() { resetCanonicalStoreTables(t, pool) })
		store, err := investigator.NewPostgresInvestigationStore(pool, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		setCtx, setCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer setCancel()
		if err := store.SetUsageLimits(setCtx, limits); err != nil {
			t.Fatalf("set canonical usage limits: %v", err)
		}
		return store
	})
}

func resetCanonicalStoreTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `
		TRUNCATE TABLE investigation_usage_reservations,
			investigation_jobs,
			investigation_usage_limits`); err != nil {
		t.Fatalf("reset canonical investigation store tables: %v", err)
	}
}

func assertCanonicalStoreSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	type columnContract struct {
		table    string
		column   string
		nullable bool
	}
	required := []columnContract{
		{table: "investigation_jobs", column: "id"},
		{table: "investigation_jobs", column: "actor_id"},
		{table: "investigation_jobs", column: "role"},
		{table: "investigation_jobs", column: "payment_id"},
		{table: "investigation_jobs", column: "idempotency_key"},
		{table: "investigation_jobs", column: "request_hash"},
		{table: "investigation_jobs", column: "status"},
		{table: "investigation_jobs", column: "mode"},
		{table: "investigation_jobs", column: "question"},
		{table: "investigation_jobs", column: "lease_owner", nullable: true},
		{table: "investigation_jobs", column: "lease_epoch"},
		{table: "investigation_jobs", column: "lease_expires_at", nullable: true},
		{table: "investigation_jobs", column: "deadline_at", nullable: true},
		{table: "investigation_jobs", column: "created_at"},
		{table: "investigation_jobs", column: "completed_at", nullable: true},
		{table: "investigation_jobs", column: "provider_call_pending"},
		{table: "investigation_jobs", column: "job_payload"},
		{table: "investigation_usage_reservations", column: "id"},
		{table: "investigation_usage_reservations", column: "job_id"},
		{table: "investigation_usage_reservations", column: "reserved_input"},
		{table: "investigation_usage_reservations", column: "reserved_output"},
		{table: "investigation_usage_reservations", column: "reserved_cost_microusd"},
		{table: "investigation_usage_reservations", column: "actual_input", nullable: true},
		{table: "investigation_usage_reservations", column: "actual_output", nullable: true},
		{table: "investigation_usage_reservations", column: "actual_cost_microusd", nullable: true},
		{table: "investigation_usage_reservations", column: "status"},
		{table: "investigation_usage_reservations", column: "created_at"},
		{table: "investigation_usage_reservations", column: "updated_at"},
		{table: "investigation_usage_limits", column: "id"},
		{table: "investigation_usage_limits", column: "input_limit"},
		{table: "investigation_usage_limits", column: "output_limit"},
		{table: "investigation_usage_limits", column: "cost_limit_microusd"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := pool.Query(ctx, `
		SELECT table_name, column_name, is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = ANY($1::text[])`, []string{
		"investigation_jobs", "investigation_usage_reservations", "investigation_usage_limits",
	})
	if err != nil {
		t.Fatalf("inspect canonical store columns: %v", err)
	}
	defer rows.Close()
	observed := make(map[string]bool)
	for rows.Next() {
		var table, column, nullable string
		if err := rows.Scan(&table, &column, &nullable); err != nil {
			t.Fatalf("scan canonical store column: %v", err)
		}
		observed[table+"."+column] = nullable == "YES"
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("inspect canonical store columns: %v", err)
	}
	for _, contract := range required {
		key := contract.table + "." + contract.column
		nullable, ok := observed[key]
		if !ok {
			t.Errorf("canonical store column missing: %s", key)
			continue
		}
		if nullable != contract.nullable {
			t.Errorf("canonical store nullability mismatch: %s nullable=%t, want %t", key, nullable, contract.nullable)
		}
	}
	for _, legacy := range []string{"actor_id", "provider", "model", "reserved_tokens"} {
		if nullable, ok := observed["investigation_usage_reservations."+legacy]; ok && !nullable {
			t.Errorf("legacy reservation column must be nullable or removed: investigation_usage_reservations.%s", legacy)
		}
	}
	var uniqueJobID bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_index index_record
			JOIN pg_class table_record ON table_record.oid = index_record.indrelid
			JOIN pg_namespace schema_record ON schema_record.oid = table_record.relnamespace
			WHERE schema_record.nspname = 'public'
			  AND table_record.relname = 'investigation_usage_reservations'
			  AND index_record.indisunique
			  AND ARRAY(
				SELECT attribute.attname
				FROM unnest(index_record.indkey) WITH ORDINALITY AS indexed(attnum, position)
				JOIN pg_attribute attribute
				  ON attribute.attrelid = table_record.oid
				 AND attribute.attnum = indexed.attnum
				WHERE indexed.position <= index_record.indnkeyatts
				ORDER BY indexed.position
			  ) = ARRAY['job_id']::name[]
		)`).Scan(&uniqueJobID); err != nil {
		t.Fatalf("inspect canonical reservation uniqueness: %v", err)
	}
	if !uniqueJobID {
		t.Error("canonical store requires a unique reservation index or constraint on job_id")
	}
	if t.Failed() {
		t.FailNow()
	}
}
