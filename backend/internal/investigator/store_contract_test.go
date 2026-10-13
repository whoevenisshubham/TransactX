package investigator_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

func TestPostgresInvestigationStoreConformance(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set: skipping PostgreSQL investigation store conformance test")
	}

	adminCtx, adminCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer adminCancel()

	admin, err := pgxpool.New(adminCtx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()

	storetest.Run(t, func(t *testing.T, limits investigator.UsageAllowance) investigator.InvestigationStore {
		t.Helper()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		schema := "tx_store_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		identifier := pgx.Identifier{schema}.Sanitize()
		if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cleanupCancel()
			if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
				t.Errorf("drop disposable store schema: %v", err)
			}
		})

		config, err := pgxpool.ParseConfig(databaseURL)
		if err != nil {
			t.Fatal(err)
		}
		config.ConnConfig.RuntimeParams["search_path"] = schema
		pool, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)

		if _, err := pool.Exec(ctx, postgresInvestigationStoreTestDDL); err != nil {
			t.Fatal(err)
		}
		store, err := investigator.NewPostgresInvestigationStore(pool, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SetUsageLimits(ctx, limits); err != nil {
			t.Fatal(err)
		}
		return store
	})
}

// Test-only DDL exercises the adapter contract. M2 owns canonical migration
// 000027; production composition must never execute this fixture schema.
const postgresInvestigationStoreTestDDL = `
CREATE TABLE investigation_jobs (
    id UUID PRIMARY KEY,
    actor_id UUID NOT NULL,
    role TEXT NOT NULL,
    payment_id UUID NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'completed', 'failed', 'cancelled')),
    mode TEXT NOT NULL,
    question TEXT NOT NULL,
    lease_owner TEXT,
    lease_epoch BIGINT NOT NULL DEFAULT 0,
    lease_expires_at TIMESTAMPTZ,
    deadline_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    provider_call_pending BOOLEAN NOT NULL DEFAULT FALSE,
    job_payload JSONB NOT NULL
);
CREATE UNIQUE INDEX idx_investigation_jobs_actor_idemp ON investigation_jobs (actor_id, idempotency_key);
CREATE INDEX idx_investigation_jobs_active ON investigation_jobs (status) WHERE status IN ('queued', 'running');

CREATE TABLE investigation_usage_reservations (
    id UUID PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES investigation_jobs(id) ON DELETE CASCADE,
    reserved_input BIGINT NOT NULL CHECK (reserved_input >= 0),
    reserved_output BIGINT NOT NULL CHECK (reserved_output >= 0),
    reserved_cost_microusd BIGINT NOT NULL CHECK (reserved_cost_microusd >= 0),
    actual_input BIGINT CHECK (actual_input >= 0),
    actual_output BIGINT CHECK (actual_output >= 0),
    actual_cost_microusd BIGINT CHECK (actual_cost_microusd >= 0),
    status TEXT NOT NULL CHECK (status IN ('reserved', 'completed', 'uncertain')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_investigation_usage_job ON investigation_usage_reservations (job_id);

CREATE TABLE investigation_usage_limits (
    id INT PRIMARY KEY,
    input_limit BIGINT NOT NULL CHECK (input_limit >= 0),
    output_limit BIGINT NOT NULL CHECK (output_limit >= 0),
    cost_limit_microusd BIGINT NOT NULL CHECK (cost_limit_microusd >= 0)
);`
