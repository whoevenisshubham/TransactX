package http

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresActivityFeedAggregatesDurableSourcesInStableOrder(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	when := time.Date(2026, 9, 29, 3, 0, 0, 0, time.UTC)
	participant := "ACTIVITY-" + uuid.NewString()
	reconID, integrityID := uuid.New(), uuid.New()
	var circuitID, chaosID int64
	if err := pool.QueryRow(ctx, `INSERT INTO circuit_transition_events
		(execution_target_id,previous_state,new_state,reason,transitioned_at,event_type)
		VALUES ($1,'CLOSED','OPEN','TEST_THRESHOLD',$2,'CIRCUIT_STATE_TRANSITION') RETURNING id`, participant, when).Scan(&circuitID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO chaos_events
		(scenario_id,event_type,target_id,fault_type,actor_id,actor_role,occurred_at)
		VALUES ($1,'CHAOS_STARTED',$2,'LATENCY','test-actor','OPS_ADMIN',$3) RETURNING id`, "scenario-"+participant, participant, when).Scan(&chaosID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO recon_runs
		(id,participant_id,scope_from,scope_to,status,discrepancy_count,divergent_buckets,divergent_records,started_at,completed_at)
		VALUES ($1,$2,$3,$4,'COMPLETED',1,1,1,$5,$5)`, reconID, participant, when.Add(-time.Hour), when, when); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO recon_discrepancies
		(run_id,participant_id,bucket_key,bucket_partition,bucket_start,bucket_width_ns,mismatch_category,evidence,detected_at)
		VALUES ($1,$2,'bucket',$2,$3,$4,'RECORD_MISMATCH','{}'::jsonb,$5)`, reconID, participant, when.Add(-time.Hour), int64(time.Hour), when); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO integrity_runs
		(id,participant_id,status,total_checks,passed_checks,failed_checks,error_checks,started_at,completed_at)
		VALUES ($1,$2,'COMPLETED',1,0,1,0,$3,$3)`, integrityID, participant, when); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM circuit_transition_events WHERE id=$1`, circuitID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM chaos_events WHERE id=$1`, chaosID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM recon_runs WHERE id=$1`, reconID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM integrity_runs WHERE id=$1`, integrityID)
	})

	events, err := newPostgresActivityStore(pool).List(ctx, 100, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	categories := make(map[string]bool)
	equalTimestampEvents := 0
	for index, event := range events {
		categories[event.Category] = true
		if event.OccurredAt.Equal(when) {
			equalTimestampEvents++
		}
		if index > 0 {
			previous := events[index-1]
			if previous.OccurredAt.Before(event.OccurredAt) || (previous.OccurredAt.Equal(event.OccurredAt) && previous.ID < event.ID) {
				t.Fatalf("events out of occurredAt DESC, id DESC order at %q then %q", previous.ID, event.ID)
			}
		}
	}
	if equalTimestampEvents < 6 {
		t.Fatalf("equal-timestamp fixture returned %d events, want at least 6", equalTimestampEvents)
	}
	for _, category := range []string{"CIRCUIT", "CHAOS", "RECONCILIATION", "INTEGRITY"} {
		if !categories[category] {
			t.Errorf("missing durable %s activity", category)
		}
	}
	cursor, err := newPostgresOperationalCursorStore(pool).Current(ctx)
	if err != nil {
		t.Fatalf("operational cursor: %v", err)
	}
	for _, topic := range []string{"health", "routing", "circuit", "chaos", "reconciliation", "integrity", "activity"} {
		if _, exists := cursor[topic]; !exists {
			t.Errorf("operational cursor missing %q", topic)
		}
	}
}
