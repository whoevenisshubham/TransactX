package investigator

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/database"
)

// TestPostgresCorpusLifecycle verifies the repository contract, versioning semantics,
// unique chunks, content hashes, and audience filtering against the real canonical migrations.
func TestPostgresCorpusLifecycle(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set: skipping disposable PostgreSQL corpus test")
	}

	testDBURL, cleanupDB := setupDisposableDB(t, databaseURL)
	defer cleanupDB()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, testDBURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	// Apply all canonical migrations (000001 through 000029) via authoritative Migrator
	migrationsDir := findRepoMigrationsDir(t)
	migrator, err := database.NewMigrator(pool, migrationsDir)
	if err != nil {
		t.Fatalf("new migrator: %v", err)
	}
	applied, err := migrator.Up(ctx)
	if err != nil {
		t.Fatalf("apply canonical migrations: %v", err)
	}
	if applied < 30 {
		t.Fatalf("expected at least 30 migrations applied, got %d", applied)
	}

	repository, err := NewPostgresCorpusRepository(pool)
	if err != nil {
		t.Fatal(err)
	}

	build := approvedCorpus(t)
	// 1. Stage corpus
	if err := repository.Stage(ctx, build); err != nil {
		t.Fatal(err)
	}
	// Verify staged version semantics via SQL
	var stageStatus string
	var stageChunks int
	if err := pool.QueryRow(ctx, `SELECT status, chunk_count FROM corpus_versions WHERE version = $1`, build.Version).Scan(&stageStatus, &stageChunks); err != nil {
		t.Fatalf("query staged version: %v", err)
	}
	if stageStatus != "staging" || stageChunks != len(build.Chunks) {
		t.Fatalf("unexpected staged version record: status=%s, chunks=%d", stageStatus, stageChunks)
	}

	// Verify unique chunks and non-empty content hashes via SQL
	var distinctChunkCount, totalChunkCount, emptyHashCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT chunk_id), COUNT(*), COUNT(*) FILTER (WHERE content_sha256 = '' OR source_sha256 = '')
		FROM runbook_chunks
		WHERE corpus_version = $1`, build.Version).Scan(&distinctChunkCount, &totalChunkCount, &emptyHashCount); err != nil {
		t.Fatalf("query chunk metrics: %v", err)
	}
	if distinctChunkCount != totalChunkCount || totalChunkCount != len(build.Chunks) {
		t.Fatalf("chunk uniqueness failure: distinct=%d, total=%d, expected=%d", distinctChunkCount, totalChunkCount, len(build.Chunks))
	}
	if emptyHashCount != 0 {
		t.Fatalf("detected %d chunks with missing content or source hashes", emptyHashCount)
	}

	// Idempotent re-stage
	if err := repository.Stage(ctx, build); err != nil {
		t.Fatalf("duplicate stage: %v", err)
	}

	// 2. Activate corpus
	if err := repository.Activate(ctx, build.Version, build.ManifestSHA256); err != nil {
		t.Fatal(err)
	}

	// Verify active version semantics and one-active constraint via SQL
	var activeStatus string
	var activatedAt *time.Time
	var activeCount int
	if err := pool.QueryRow(ctx, `SELECT status, activated_at FROM corpus_versions WHERE version = $1`, build.Version).Scan(&activeStatus, &activatedAt); err != nil {
		t.Fatalf("query active version: %v", err)
	}
	if activeStatus != "active" || activatedAt == nil {
		t.Fatalf("version was not marked active: status=%s, activatedAt=%v", activeStatus, activatedAt)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM corpus_versions WHERE status = 'active'`).Scan(&activeCount); err != nil {
		t.Fatalf("count active versions: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("expected exactly 1 active version, got %d", activeCount)
	}

	// 3. Retrieve operational runbooks
	scope := TrustedScope{ActorID: "operator", Role: "OPS_VIEWER", SelectedPaymentID: "selected"}
	chunks, err := repository.Retrieve(ctx, scope, "lost participant reply unknown outcome", []string{"recovery", "unknown_outcome"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !hasChunk(chunks, "docs/runbooks/routed-payment-recovery.md", "Unknown participant outcome") {
		t.Fatalf("expected recovery chunk, got %v", summarizeChunks(chunks))
	}
	saved := chunks[0]

	// 4. Verify audience filtering: CUSTOMER role must be denied access to operational runbooks
	customerScope := TrustedScope{ActorID: "customer", Role: "CUSTOMER", SelectedPaymentID: "selected"}
	customerChunks, err := repository.Retrieve(ctx, customerScope, "lost participant reply unknown outcome", []string{"recovery"}, 5)
	if err == nil && len(customerChunks) > 0 {
		t.Fatalf("customer role retrieved operational chunks: %v", summarizeChunks(customerChunks))
	}

	// SQL assertion for audience filtering: runbooks must NOT allow CUSTOMER role
	var customerAccessibleCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM runbook_chunks
		WHERE corpus_version = $1 AND 'CUSTOMER' = ANY(allowed_roles)`, build.Version).Scan(&customerAccessibleCount); err != nil {
		t.Fatalf("query customer accessible chunks: %v", err)
	}
	if customerAccessibleCount != 0 {
		t.Fatalf("expected 0 customer accessible chunks in operational runbooks, found %d", customerAccessibleCount)
	}

	// 5. Test injection defense
	injected := `pending'); DROP TABLE runbook_chunks; --`
	if _, err := repository.Retrieve(ctx, scope, injected, []string{"recovery"}, 5); err != nil {
		t.Fatalf("parameterized injection-shaped query failed: %v", err)
	}
	var chunkCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM runbook_chunks WHERE corpus_version = $1`, build.Version).Scan(&chunkCount); err != nil || chunkCount != len(build.Chunks) {
		t.Fatalf("chunks after injection-shaped query = %d, %v", chunkCount, err)
	}

	// 6. Stage and activate new version (v2) and verify previous version is retired
	next := CorpusBuild{Version: "tx.corpus.c1.pg2", ManifestSHA256: strings.Repeat("b", 64)}
	for index, chunk := range build.Chunks {
		if chunk.Path == "docs/runbooks/projections-and-anomaly-analytics.md" {
			continue
		}
		chunk.Version = next.Version
		chunk.ID = fmt.Sprintf("runbook:pg2:%03d", index)
		next.Chunks = append(next.Chunks, chunk)
	}
	if err := repository.Stage(ctx, next); err != nil {
		t.Fatal(err)
	}
	if err := repository.Activate(ctx, next.Version, next.ManifestSHA256); err != nil {
		t.Fatal(err)
	}

	// Verify retired version semantics via SQL
	var prevStatus string
	var prevDeactivatedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT status, deactivated_at FROM corpus_versions WHERE version = $1`, build.Version).Scan(&prevStatus, &prevDeactivatedAt); err != nil {
		t.Fatalf("query previous version status: %v", err)
	}
	if prevStatus != "retired" || prevDeactivatedAt == nil {
		t.Fatalf("previous version was not retired: status=%s, deactivatedAt=%v", prevStatus, prevDeactivatedAt)
	}

	// Citation resolution persists across active version transition
	resolved, err := repository.Resolve(ctx, build.Version, saved.ID, scope.Role)
	if err != nil || resolved.SHA256 != saved.SHA256 {
		t.Fatalf("old citation after switch = %+v, %v", resolved, err)
	}

	// Active retrieval reflects new version
	removed, err := repository.Retrieve(ctx, scope, "IsolationForest held-out baseline", []string{"baseline", "model"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, chunk := range removed {
		if chunk.Path == "docs/runbooks/projections-and-anomaly-analytics.md" {
			t.Fatal("removed document was returned from new active corpus")
		}
	}
}

func hasChunk(chunks []RunbookChunk, path, heading string) bool {
	for _, chunk := range chunks {
		if chunk.Path == path && chunk.Heading == heading {
			return true
		}
	}
	return false
}
