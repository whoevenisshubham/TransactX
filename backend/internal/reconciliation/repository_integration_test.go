package reconciliation_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/transactx/backend/internal/reconciliation"
)

func TestRunRepositoryPersistsMetricsAndRecordDiscrepancies(t *testing.T) {
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
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	repo := reconciliation.NewRunRepository(pool)
	from := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	run, err := repo.CreateRun(ctx, "TEST-METRICS-"+uuid.NewString(), reconciliation.Scope{From: from, To: from.Add(time.Hour)})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM recon_runs WHERE id = $1`, run.ID) })

	disc, err := repo.SaveDiscrepancy(ctx, reconciliation.Discrepancy{
		RunID:            run.ID,
		ParticipantID:    run.ParticipantID,
		BucketKey:        "test-bucket",
		BucketPartition:  run.ParticipantID,
		BucketStart:      from,
		BucketWidthNs:    int64(time.Hour),
		ExpectedRoot:     []byte{1},
		ObservedRoot:     []byte{2},
		MismatchCategory: reconciliation.MismatchRecordDifference,
		Evidence:         map[string]string{"operation_id": uuid.NewString()},
	})
	if err != nil {
		t.Fatalf("SaveDiscrepancy record-level category: %v", err)
	}
	if disc.ID == uuid.Nil {
		t.Fatal("saved discrepancy has no ID")
	}

	metrics := reconciliation.RunMetrics{
		ElapsedNs:        1234567,
		NodesVisited:     14,
		RecordsInspected: 2,
		BytesExamined:    456,
		DivergentBuckets: 1,
		DivergentRecords: 1,
	}
	completed, err := repo.CompleteRun(ctx, run.ID, []byte{1}, []byte{2}, reconciliation.CanonicalVersion, reconciliation.MerkleAlgorithmVersion, 10, 1, metrics)
	if err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}
	if completed.ElapsedNs != metrics.ElapsedNs || completed.NodesVisited != metrics.NodesVisited ||
		completed.RecordsInspected != metrics.RecordsInspected || completed.BytesExamined != metrics.BytesExamined ||
		completed.DivergentBuckets != metrics.DivergentBuckets || completed.DivergentRecords != metrics.DivergentRecords {
		t.Fatalf("completed metrics = %+v, want %+v", completed, metrics)
	}

	loaded, err := repo.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if loaded.ElapsedNs != metrics.ElapsedNs || loaded.BytesExamined != metrics.BytesExamined || loaded.DivergentRecords != 1 {
		t.Fatalf("loaded metrics were not durable: %+v", loaded)
	}
	page, err := repo.ListRuns(ctx, reconciliation.ListRunsRequest{ParticipantID: run.ParticipantID, Limit: 10})
	if err != nil || len(page.Items) != 1 || page.Items[0].NodesVisited != metrics.NodesVisited {
		t.Fatalf("ListRuns metrics: page=%+v err=%v", page, err)
	}
	discrepancies, err := repo.ListDiscrepancies(ctx, reconciliation.ListDiscrepanciesRequest{RunID: run.ID, Limit: 10})
	if err != nil || len(discrepancies.Items) != 1 || discrepancies.Items[0].MismatchCategory != reconciliation.MismatchRecordDifference {
		t.Fatalf("ListDiscrepancies: page=%+v err=%v", discrepancies, err)
	}
}
