package investigator

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const postgresRetrieveRunbooksSQL = `
WITH requested AS (
    SELECT websearch_to_tsquery('simple', $1) AS query
)
SELECT c.chunk_id,
       c.document_path,
       c.heading,
       c.content_sha256,
       c.corpus_version,
       c.content,
       c.start_line,
       c.end_line,
       c.allowed_roles,
       c.source_sha256,
       c.implementation_status,
       c.tags
FROM runbook_chunks c
JOIN corpus_versions v ON v.version = c.corpus_version
CROSS JOIN requested r
WHERE v.status = 'active'
  AND c.allowed_roles @> ARRAY[$2]::text[]
  AND (c.search_vector @@ r.query OR c.tags && $3::text[])
ORDER BY (
    ts_rank_cd(c.search_vector, r.query)
    + CASE WHEN c.tags && $3::text[] THEN 0.10 ELSE 0 END
  ) DESC,
  c.chunk_id ASC
LIMIT $4`

// PostgresCorpusRepository expects the M2-owned migration described in
// INTEGRATION.md. Constructing it does not create tables or modify shared
// configuration.
type PostgresCorpusRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresCorpusRepository(pool *pgxpool.Pool) (*PostgresCorpusRepository, error) {
	if pool == nil {
		return nil, errors.New("corpus PostgreSQL pool is required")
	}
	return &PostgresCorpusRepository{pool: pool}, nil
}

// Stage is idempotent for the exact same version and manifest hash. An
// interrupted transaction cannot expose a partial version because the version
// row and all immutable chunks commit together.
func (repository *PostgresCorpusRepository) Stage(ctx context.Context, build CorpusBuild) error {
	if err := validateCorpusBuild(build); err != nil {
		return err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin corpus stage: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	if _, err := tx.Exec(ctx, `
        INSERT INTO corpus_versions (version, manifest_sha256, status, chunk_count)
        VALUES ($1, $2, 'staging', $3)
        ON CONFLICT (version) DO NOTHING`, build.Version, build.ManifestSHA256, len(build.Chunks)); err != nil {
		return fmt.Errorf("insert corpus version: %w", err)
	}
	var storedManifestHash string
	if err := tx.QueryRow(ctx, `SELECT manifest_sha256 FROM corpus_versions WHERE version = $1 FOR UPDATE`, build.Version).Scan(&storedManifestHash); err != nil {
		return fmt.Errorf("read corpus version: %w", err)
	}
	if storedManifestHash != build.ManifestSHA256 {
		return ErrCorpusVersionConflict
	}

	for _, chunk := range build.Chunks {
		if _, err := tx.Exec(ctx, `
            INSERT INTO runbook_chunks (
                corpus_version, chunk_id, document_path, heading, start_line, end_line,
                source_sha256, content_sha256, implementation_status, tags,
                allowed_roles, content
            ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
            ON CONFLICT (corpus_version, chunk_id) DO NOTHING`,
			build.Version, chunk.ID, chunk.Path, chunk.Heading, chunk.StartLine, chunk.EndLine,
			chunk.SourceSHA256, chunk.SHA256, chunk.ImplementationStatus, chunk.Tags,
			chunk.AllowedRoles, chunk.Content); err != nil {
			return fmt.Errorf("insert corpus chunk %s: %w", chunk.ID, err)
		}
	}
	var storedCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM runbook_chunks WHERE corpus_version = $1`, build.Version).Scan(&storedCount); err != nil {
		return fmt.Errorf("count staged corpus chunks: %w", err)
	}
	if storedCount != len(build.Chunks) {
		return fmt.Errorf("staged corpus chunk count %d does not match build %d", storedCount, len(build.Chunks))
	}
	if _, err := tx.Exec(ctx, `UPDATE corpus_versions SET chunk_count = $2 WHERE version = $1`, build.Version, storedCount); err != nil {
		return fmt.Errorf("record corpus chunk count: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit corpus stage: %w", err)
	}
	return nil
}

// Activate atomically retires the prior active corpus and activates the exact
// staged version/hash. Retired chunks remain addressable for saved reports.
func (repository *PostgresCorpusRepository) Activate(ctx context.Context, version, manifestSHA string) error {
	if !corpusVersionPattern.MatchString(version) || !corpusHashPattern.MatchString(manifestSHA) {
		return errors.New("invalid corpus activation identity")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin corpus activation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var storedHash, status string
	if err := tx.QueryRow(ctx, `
        SELECT manifest_sha256, status
        FROM corpus_versions
        WHERE version = $1
        FOR UPDATE`, version).Scan(&storedHash, &status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("staged corpus version not found")
		}
		return fmt.Errorf("read corpus activation target: %w", err)
	}
	if storedHash != manifestSHA {
		return ErrCorpusVersionConflict
	}
	if status != "staging" && status != "active" && status != "retired" {
		return fmt.Errorf("corpus version has invalid activation status %q", status)
	}
	if _, err := tx.Exec(ctx, `
        UPDATE corpus_versions
        SET status = 'retired', deactivated_at = CURRENT_TIMESTAMP
        WHERE status = 'active' AND version <> $1`, version); err != nil {
		return fmt.Errorf("retire prior corpus: %w", err)
	}
	if _, err := tx.Exec(ctx, `
        UPDATE corpus_versions
        SET status = 'active', activated_at = COALESCE(activated_at, CURRENT_TIMESTAMP), deactivated_at = NULL
        WHERE version = $1`, version); err != nil {
		return fmt.Errorf("activate corpus: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit corpus activation: %w", err)
	}
	return nil
}

func (repository *PostgresCorpusRepository) Retrieve(ctx context.Context, scope TrustedScope, query string, tags []string, topK int) ([]RunbookChunk, error) {
	if err := validateRetrieval(scope, query, tags, topK); err != nil {
		return nil, err
	}
	statement, arguments := postgresRetrievalQuery(scope, query, tags, topK)
	rows, err := repository.pool.Query(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("retrieve runbooks: %w", err)
	}
	defer rows.Close()

	var chunks []RunbookChunk
	for rows.Next() {
		var chunk RunbookChunk
		if err := rows.Scan(
			&chunk.ID, &chunk.Path, &chunk.Heading, &chunk.SHA256, &chunk.Version,
			&chunk.Content, &chunk.StartLine, &chunk.EndLine, &chunk.AllowedRoles,
			&chunk.SourceSHA256, &chunk.ImplementationStatus, &chunk.Tags,
		); err != nil {
			return nil, fmt.Errorf("scan runbook chunk: %w", err)
		}
		chunks = append(chunks, chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runbook chunks: %w", err)
	}
	return chunks, nil
}

// Resolve retrieves an immutable citation from any retained corpus version;
// it is deliberately independent of the current active version.
func (repository *PostgresCorpusRepository) Resolve(ctx context.Context, version, chunkID, role string) (RunbookChunk, error) {
	if !corpusVersionPattern.MatchString(version) || chunkID == "" || (role != "OPS_ADMIN" && role != "OPS_VIEWER") {
		return RunbookChunk{}, errors.New("invalid citation identity or role")
	}
	var chunk RunbookChunk
	err := repository.pool.QueryRow(ctx, `
        SELECT chunk_id, document_path, heading, content_sha256, corpus_version,
               content, start_line, end_line, allowed_roles, source_sha256,
               implementation_status, tags
        FROM runbook_chunks
        WHERE corpus_version = $1
          AND chunk_id = $2
          AND allowed_roles @> ARRAY[$3]::text[]`, version, chunkID, role).Scan(
		&chunk.ID, &chunk.Path, &chunk.Heading, &chunk.SHA256, &chunk.Version,
		&chunk.Content, &chunk.StartLine, &chunk.EndLine, &chunk.AllowedRoles,
		&chunk.SourceSHA256, &chunk.ImplementationStatus, &chunk.Tags,
	)
	if err != nil {
		return RunbookChunk{}, fmt.Errorf("resolve runbook citation: %w", err)
	}
	return chunk, nil
}

func postgresRetrievalQuery(scope TrustedScope, query string, tags []string, topK int) (string, []any) {
	return postgresRetrieveRunbooksSQL, []any{query, scope.Role, append([]string(nil), tags...), topK}
}

var _ RunbookRetriever = (*PostgresCorpusRepository)(nil)
