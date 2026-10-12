package audit

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/transactx/backend/internal/database"
)

func TestPostgresStore_RecordAndList(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := database.NewPool(context.Background(), dbURL)
	if err != nil {
		t.Skip("PostgreSQL not available for test")
	}
	defer pool.Close()

	store := NewPostgresStore(pool)
	actorID := uuid.New()

	entry := Entry{
		ActorID:    &actorID,
		ActorRole:  "OPS_ADMIN",
		Operation:  "CHAOS_START",
		EntityType: "SCENARIO",
		EntityID:   "bank-a-latency",
		ReasonCode: "TEST_DRILL",
		Details:    map[string]any{"duration": "30s", "latencyMs": 500},
		RequestID:  "req-test-123",
		TraceID:    "trace-test-456",
		Result:     "SUCCESS",
	}

	if err := store.Record(context.Background(), entry); err != nil {
		t.Fatalf("failed to record audit entry: %v", err)
	}

	entries, err := store.ListRecent(context.Background(), 10)
	if err != nil {
		t.Fatalf("failed to list audit entries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected at least 1 entry, got 0")
	}

	found := false
	for _, e := range entries {
		if e.RequestID == "req-test-123" {
			found = true
			if e.Operation != "CHAOS_START" || e.ActorRole != "OPS_ADMIN" || e.Result != "SUCCESS" {
				t.Errorf("mismatched audit fields: %+v", e)
			}
			break
		}
	}
	if !found {
		t.Errorf("recorded entry not found in recent entries")
	}
}
