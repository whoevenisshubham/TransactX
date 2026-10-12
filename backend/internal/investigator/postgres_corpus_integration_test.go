package investigator

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresCorpusLifecycle uses disposable test-only DDL because migration
// 000027 is M2-owned. It verifies the repository contract without introducing
// a second canonical migration.
func TestPostgresCorpusLifecycle(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set: skipping disposable PostgreSQL corpus test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "tx_corpus_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupContext, "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Errorf("drop disposable corpus schema: %v", err)
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
	defer pool.Close()
	if _, err := pool.Exec(ctx, disposableCorpusDDL); err != nil {
		t.Fatal(err)
	}
	repository, err := NewPostgresCorpusRepository(pool)
	if err != nil {
		t.Fatal(err)
	}

	build := approvedCorpus(t)
	if err := repository.Stage(ctx, build); err != nil {
		t.Fatal(err)
	}
	if err := repository.Stage(ctx, build); err != nil {
		t.Fatalf("duplicate stage: %v", err)
	}
	if err := repository.Activate(ctx, build.Version, build.ManifestSHA256); err != nil {
		t.Fatal(err)
	}

	scope := TrustedScope{ActorID: "operator", Role: "OPS_VIEWER", SelectedPaymentID: "selected"}
	chunks, err := repository.Retrieve(ctx, scope, "lost participant reply unknown outcome", []string{"recovery", "unknown_outcome"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !hasChunk(chunks, "docs/runbooks/routed-payment-recovery.md", "Unknown participant outcome") {
		t.Fatalf("expected recovery chunk, got %v", summarizeChunks(chunks))
	}
	saved := chunks[0]

	injected := `pending'); DROP TABLE runbook_chunks; --`
	if _, err := repository.Retrieve(ctx, scope, injected, []string{"recovery"}, 5); err != nil {
		t.Fatalf("parameterized injection-shaped query failed: %v", err)
	}
	var chunkCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM runbook_chunks`).Scan(&chunkCount); err != nil || chunkCount != len(build.Chunks) {
		t.Fatalf("chunks after injection-shaped query = %d, %v", chunkCount, err)
	}

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
	resolved, err := repository.Resolve(ctx, build.Version, saved.ID, scope.Role)
	if err != nil || resolved.SHA256 != saved.SHA256 {
		t.Fatalf("old citation after switch = %+v, %v", resolved, err)
	}
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

const disposableCorpusDDL = `
CREATE TABLE corpus_versions (
    version TEXT PRIMARY KEY,
    manifest_sha256 TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('staging', 'active', 'retired')),
    chunk_count INTEGER NOT NULL CHECK (chunk_count >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    activated_at TIMESTAMPTZ,
    deactivated_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX corpus_versions_one_active_idx ON corpus_versions ((status)) WHERE status = 'active';
CREATE TABLE runbook_chunks (
    corpus_version TEXT NOT NULL REFERENCES corpus_versions(version),
    chunk_id TEXT NOT NULL,
    document_path TEXT NOT NULL,
    heading TEXT NOT NULL,
    start_line INTEGER NOT NULL CHECK (start_line > 0),
    end_line INTEGER NOT NULL CHECK (end_line >= start_line),
    source_sha256 TEXT NOT NULL,
    content_sha256 TEXT NOT NULL,
    implementation_status TEXT NOT NULL CHECK (implementation_status IN ('existing', 'planned')),
    tags TEXT[] NOT NULL,
    allowed_roles TEXT[] NOT NULL,
    content TEXT NOT NULL,
    search_vector TSVECTOR GENERATED ALWAYS AS (
        to_tsvector('simple', coalesce(heading, '') || ' ' || coalesce(content, ''))
    ) STORED,
    PRIMARY KEY (corpus_version, chunk_id)
);
CREATE INDEX runbook_chunks_search_idx ON runbook_chunks USING GIN (search_vector);
CREATE INDEX runbook_chunks_tags_idx ON runbook_chunks USING GIN (tags);`
