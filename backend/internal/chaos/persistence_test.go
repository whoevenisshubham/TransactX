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

func TestPostgresExactlyOnceExpiry(t *testing.T) {
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
	now := time.Now().UTC()
	scenarioID := "test-live-atomic-exp-" + now.Format("20060102150405")

	// Insert scenario directly into PostgreSQL
	sc := chaos.ChaosScenario{
		ScenarioID: scenarioID,
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "RAIL-A",
		Parameters: chaos.ScenarioParameters{DurationMs: 1000},
		StartedAt:  now.Add(-10 * time.Second),
		ExpiresAt:  now.Add(-5 * time.Second),
		Active:     true,
		Mode:       chaos.ExecutionModeSimulation,
		CreatedBy:  "ops-admin-user",
		CreatedAt:  now.Add(-10 * time.Second),
		UpdatedAt:  now.Add(-10 * time.Second),
	}
	startEvent := chaos.ChaosEvent{
		ScenarioID: scenarioID,
		EventType:  chaos.EventTypeChaosStarted,
		TargetID:   "RAIL-A",
		FaultType:  string(sc.Type),
		ActorID:    "ops-admin-user",
		ActorRole:  "OPS_ADMIN",
		OccurredAt: sc.StartedAt,
	}
	if err := repo.CreateScenarioWithEvent(ctx, sc, startEvent); err != nil {
		t.Fatalf("failed to insert test scenario: %v", err)
	}

	expEvent := chaos.ChaosEvent{
		ScenarioID: scenarioID,
		EventType:  chaos.EventTypeChaosExpired,
		TargetID:   "RAIL-A",
		FaultType:  string(sc.Type),
		ActorID:    "SYSTEM",
		ActorRole:  "SYSTEM",
		OccurredAt: sc.ExpiresAt,
	}

	// First expiry: should return true
	first, err := repo.ExpireScenario(ctx, scenarioID, now, expEvent)
	if err != nil || !first {
		t.Fatalf("expected first expiry to return true: %v, err=%v", first, err)
	}

	// Second expiry: should return false
	second, err := repo.ExpireScenario(ctx, scenarioID, now, expEvent)
	if err != nil || second {
		t.Fatalf("expected second expiry to return false: %v, err=%v", second, err)
	}
}

// TestPostgresResetVsExpiryAtomic verifies atomic classification between RESET and EXPIRY in PostgreSQL.
func TestPostgresResetVsExpiryAtomic(t *testing.T) {
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
	now := time.Now().UTC()
	futureID := "test-live-future-" + now.Format("20060102150405")
	expiredID := "test-live-expired-" + now.Format("20060102150405")

	// 1. Future scenario (expires 60s in future)
	futureSc := chaos.ChaosScenario{
		ScenarioID: futureID,
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   "RAIL-A",
		StartedAt:  now.Add(-10 * time.Second),
		ExpiresAt:  now.Add(60 * time.Second),
		Active:     true,
		Mode:       chaos.ExecutionModeSimulation,
		CreatedBy:  "ops-admin-user",
	}
	if err := repo.CreateScenarioWithEvent(ctx, futureSc, chaos.ChaosEvent{
		ScenarioID: futureID,
		EventType:  chaos.EventTypeChaosStarted,
		TargetID:   "RAIL-A",
		FaultType:  string(futureSc.Type),
		ActorID:    "ops-admin-user",
		ActorRole:  "OPS_ADMIN",
		OccurredAt: futureSc.StartedAt,
	}); err != nil {
		t.Fatalf("failed to insert future scenario: %v", err)
	}

	// 2. Expired scenario (expired 10s ago)
	expiredSc := chaos.ChaosScenario{
		ScenarioID: expiredID,
		Type:       chaos.ScenarioTypeLatency,
		TargetID:   "RAIL-B",
		StartedAt:  now.Add(-20 * time.Second),
		ExpiresAt:  now.Add(-10 * time.Second),
		Active:     true,
		Mode:       chaos.ExecutionModeSimulation,
		CreatedBy:  "ops-admin-user",
	}
	if err := repo.CreateScenarioWithEvent(ctx, expiredSc, chaos.ChaosEvent{
		ScenarioID: expiredID,
		EventType:  chaos.EventTypeChaosStarted,
		TargetID:   "RAIL-B",
		FaultType:  string(expiredSc.Type),
		ActorID:    "ops-admin-user",
		ActorRole:  "OPS_ADMIN",
		OccurredAt: expiredSc.StartedAt,
	}); err != nil {
		t.Fatalf("failed to insert expired scenario: %v", err)
	}

	// 3. Reset all active scenarios
	resetList, err := repo.ResetScenarios(ctx, "", now, "ops-admin-user", "ops-admin-user", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("ResetScenarios failed: %v", err)
	}
	if len(resetList) < 2 {
		t.Fatalf("expected at least 2 scenarios reset, got %d", len(resetList))
	}

	// 4. Verify future scenario got CHAOS_RESET and stopped_by = ops-admin-user
	fut, err := repo.GetScenario(ctx, futureID)
	if err != nil || fut.Active {
		t.Fatalf("future scenario must be inactive, got active=%v, err=%v", fut.Active, err)
	}
	if fut.StoppedBy == nil || *fut.StoppedBy != "ops-admin-user" {
		t.Fatalf("expected stoppedBy = ops-admin-user, got %v", fut.StoppedBy)
	}

	// 5. Verify expired scenario got CHAOS_EXPIRED and stopped_by = SYSTEM_AUTO_EXPIRY
	exp, err := repo.GetScenario(ctx, expiredID)
	if err != nil || exp.Active {
		t.Fatalf("expired scenario must be inactive, got active=%v, err=%v", exp.Active, err)
	}
	if exp.StoppedBy == nil || *exp.StoppedBy != "SYSTEM_AUTO_EXPIRY" {
		t.Fatalf("expected stoppedBy = SYSTEM_AUTO_EXPIRY, got %v", exp.StoppedBy)
	}

	// 6. Verify events
	events, err := repo.ListEvents(ctx, futureID, 10)
	if err != nil {
		t.Fatalf("ListEvents for future failed: %v", err)
	}
	hasReset := false
	hasExpired := false
	for _, e := range events {
		if e.EventType == chaos.EventTypeChaosReset {
			hasReset = true
		}
		if e.EventType == chaos.EventTypeChaosExpired {
			hasExpired = true
		}
	}
	if !hasReset || hasExpired {
		t.Fatalf("future scenario: expected CHAOS_RESET=true, CHAOS_EXPIRED=false, got reset=%v, exp=%v", hasReset, hasExpired)
	}

	eventsExp, err := repo.ListEvents(ctx, expiredID, 10)
	if err != nil {
		t.Fatalf("ListEvents for expired failed: %v", err)
	}
	hasResetExp := false
	hasExpiredExp := false
	for _, e := range eventsExp {
		if e.EventType == chaos.EventTypeChaosReset {
			hasResetExp = true
		}
		if e.EventType == chaos.EventTypeChaosExpired {
			hasExpiredExp = true
		}
	}
	if hasResetExp || !hasExpiredExp {
		t.Fatalf("expired scenario: expected CHAOS_RESET=false, CHAOS_EXPIRED=true, got reset=%v, exp=%v", hasResetExp, hasExpiredExp)
	}
}

// TestPostgresStaleExpiryRestartFinalization tests that an expired active scenario in PostgreSQL
// is automatically finalized during a restart when Start or Hydrate runs against the target.
func TestPostgresStaleExpiryRestartFinalization(t *testing.T) {
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
	now := time.Now().UTC()
	staleID := "test-live-stale-" + now.Format("20060102150405")
	newID := "test-live-new-" + now.Format("20060102150405")
	targetID := "RAIL-LIVE-STALE"

	// 1. Insert an expired scenario directly in PostgreSQL with active = true
	staleSc := chaos.ChaosScenario{
		ScenarioID: staleID,
		Type:       chaos.ScenarioTypeBankOutage,
		TargetID:   targetID,
		StartedAt:  now.Add(-20 * time.Second),
		ExpiresAt:  now.Add(-5 * time.Second), // expired 5 seconds ago
		Active:     true,
		Mode:       chaos.ExecutionModeSimulation,
		CreatedBy:  "ops-admin-user",
	}
	if err := repo.CreateScenarioWithEvent(ctx, staleSc, chaos.ChaosEvent{
		ScenarioID: staleID,
		EventType:  chaos.EventTypeChaosStarted,
		TargetID:   targetID,
		FaultType:  string(staleSc.Type),
		ActorID:    "ops-admin-user",
		ActorRole:  "OPS_ADMIN",
		OccurredAt: staleSc.StartedAt,
	}); err != nil {
		t.Fatalf("failed to insert stale scenario in PG: %v", err)
	}

	// 2. Create a fresh Controller simulating a service restart
	ctrl := chaos.NewController(repo)
	ctrl.SetTargetValidator(func(tID string) bool { return true })

	// 3. Start a new scenario on the same target
	req := chaos.StartRequest{
		ScenarioID: newID,
		Type:       chaos.ScenarioTypeLatency,
		TargetID:   targetID,
		Parameters: chaos.ScenarioParameters{DurationMs: 10000, LatencyMs: 50},
	}
	sc2, err := ctrl.Start(ctx, req, "ops-admin-user", "OPS_ADMIN")
	if err != nil {
		t.Fatalf("ctrl.Start failed on target with stale scenario in PG: %v", err)
	}
	if !sc2.Active {
		t.Fatalf("new scenario must be active")
	}

	// 4. Verify old scenario is inactive in PostgreSQL with stopped_by = SYSTEM_AUTO_EXPIRY
	oldSc, err := repo.GetScenario(ctx, staleID)
	if err != nil || oldSc.Active {
		t.Fatalf("stale scenario must be inactive in PG: active=%v, err=%v", oldSc.Active, err)
	}
	if oldSc.StoppedBy == nil || *oldSc.StoppedBy != "SYSTEM_AUTO_EXPIRY" {
		t.Fatalf("expected stale scenario stoppedBy = SYSTEM_AUTO_EXPIRY, got %v", oldSc.StoppedBy)
	}

	// 5. Verify exactly one CHAOS_EXPIRED event for stale scenario
	events, err := repo.ListEvents(ctx, staleID, 10)
	if err != nil {
		t.Fatalf("ListEvents failed: %v", err)
	}
	expiredCount := 0
	for _, e := range events {
		if e.EventType == chaos.EventTypeChaosExpired {
			expiredCount++
		}
	}
	if expiredCount != 1 {
		t.Fatalf("expected exactly 1 CHAOS_EXPIRED event for stale scenario in PG, got %d", expiredCount)
	}

	// Cleanup: stop new scenario
	_, _ = ctrl.Stop(ctx, newID, "ops-admin-user", "OPS_ADMIN")
}
