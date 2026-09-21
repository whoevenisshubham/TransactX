package chaos_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/chaos"
)

func TestPostgresChaosPersistence(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL is not set: skipping live PostgreSQL chaos persistence tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to connect to PostgreSQL: %v", err)
	}
	defer pool.Close()

	repo := chaos.NewRepository(pool)
	ctrl := chaos.NewController(repo)

	scenarioID := "test-live-pg-" + time.Now().Format("20060102150405")
	req := chaos.StartRequest{
		ScenarioID: scenarioID,
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "RAIL-A",
		Parameters: chaos.ScenarioParameters{DurationMs: 5000},
	}

	sc, err := ctrl.Start(ctx, req, "ops-admin-user", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("failed to start live PG scenario: %v", err)
	}
	if !sc.Active {
		t.Fatalf("expected scenario to be active in PG")
	}

	// Fetch directly from repo
	fetched, err := repo.GetScenario(ctx, scenarioID)
	if err != nil {
		t.Fatalf("failed to get scenario from PG: %v", err)
	}
	if fetched.ScenarioID != scenarioID || !fetched.Active {
		t.Fatalf("unexpected fetched scenario: %+v", fetched)
	}

	// Stop scenario
	stopped, err := ctrl.Stop(ctx, scenarioID, "ops-admin-user", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("failed to stop scenario in PG: %v", err)
	}
	if stopped.Active {
		t.Fatalf("expected scenario to be inactive in PG")
	}

	// Reset
	if err := ctrl.Reset(ctx, "RAIL-A", "ops-admin-user", "OPS_ADMIN"); err != nil {
		t.Fatalf("failed to reset in PG: %v", err)
	}
}
