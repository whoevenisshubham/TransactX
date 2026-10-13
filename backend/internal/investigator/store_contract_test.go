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
	storetest.Run(t, func(t *testing.T, inputLimit, outputLimit int64) investigator.InvestigationStore {
		t.Helper()
		store := investigator.NewMemoryInvestigationStore(time.Now)
		store.SetUsageLimits(inputLimit, outputLimit)
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

	storetest.Run(t, func(t *testing.T, inputLimit, outputLimit int64) investigator.InvestigationStore {
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

		store, err := investigator.NewPostgresInvestigationStore(pool, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.EnsureSchema(ctx); err != nil {
			t.Fatal(err)
		}
		store.SetUsageLimits(inputLimit, outputLimit)
		return store
	})
}
