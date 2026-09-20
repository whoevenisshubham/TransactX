package health

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPersistenceRoundTripAndConcurrentRecording(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	p, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	repository := NewRepository(p)
	target := "health-test-" + time.Now().UTC().Format("20060102150405.000000000")
	now := time.Now().UTC().Truncate(time.Millisecond)
	var group sync.WaitGroup
	for index := 0; index < 8; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			if err := repository.Record(context.Background(), HealthSample{TargetID: target, SampledAt: now.Add(time.Duration(index) * time.Millisecond), Available: true, Latency: 20 * time.Millisecond, Outcome: OutcomeSuccess}); err != nil {
				t.Errorf("record sample: %v", err)
			}
		}(index)
	}
	group.Wait()
	samples, err := repository.ListRecent(context.Background(), target, now.Add(-time.Second), now.Add(time.Second), 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 8 {
		t.Fatalf("sample count = %d, want 8", len(samples))
	}
	if Snapshot(target, samples, now.Add(time.Second), DefaultConfig()).SuccessScore != 1 {
		t.Fatal("round-trip snapshot is not successful")
	}
}
