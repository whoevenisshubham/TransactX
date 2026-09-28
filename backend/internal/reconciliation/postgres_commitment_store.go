package reconciliation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresIncrementalCommitmentStore persists derived incremental Merkle commitments
// to PostgreSQL independently from participant ledger tables.
type PostgresIncrementalCommitmentStore struct {
	pool *pgxpool.Pool
}

// NewPostgresIncrementalCommitmentStore constructs a PostgreSQL-backed commitment store.
func NewPostgresIncrementalCommitmentStore(pool *pgxpool.Pool) *PostgresIncrementalCommitmentStore {
	return &PostgresIncrementalCommitmentStore{pool: pool}
}

// SaveState saves an incremental commitment state to PostgreSQL, replacing any existing
// commitment for the same partition, bucket width, and scope.
func (s *PostgresIncrementalCommitmentStore) SaveState(ctx context.Context, state IncrementalCommitmentState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateIncrementalState(state); err != nil {
		return err
	}
	state.Scope = normalizeScope(state.Scope)

	bucketsData, err := json.Marshal(state.Buckets)
	if err != nil {
		return fmt.Errorf("failed to marshal buckets: %w", err)
	}
	recordsData, err := json.Marshal(state.BucketRecords)
	if err != nil {
		return fmt.Errorf("failed to marshal bucket records: %w", err)
	}
	levelsData, err := json.Marshal(state.Levels)
	if err != nil {
		return fmt.Errorf("failed to marshal levels: %w", err)
	}

	query := `
		INSERT INTO merkle_commitments (
			partition, bucket_width_ns, scope_from, scope_to,
			generation, canonical_version, algorithm_version,
			root, record_count, rebuild_count, captured_at,
			buckets_data, records_data, levels_data
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7,
			$8, $9, $10, $11,
			$12, $13, $14
		)
		ON CONFLICT (partition, bucket_width_ns, scope_from, scope_to) DO UPDATE SET
			generation = EXCLUDED.generation,
			canonical_version = EXCLUDED.canonical_version,
			algorithm_version = EXCLUDED.algorithm_version,
			root = EXCLUDED.root,
			record_count = EXCLUDED.record_count,
			rebuild_count = EXCLUDED.rebuild_count,
			captured_at = EXCLUDED.captured_at,
			buckets_data = EXCLUDED.buckets_data,
			records_data = EXCLUDED.records_data,
			levels_data = EXCLUDED.levels_data,
			created_at = NOW();
	`
	_, err = s.pool.Exec(
		ctx, query,
		state.Partition,
		int64(state.BucketWidth),
		state.Scope.From.UTC(),
		state.Scope.To.UTC(),
		state.Generation,
		state.CanonicalVersion,
		state.AlgorithmVersion,
		state.Root,
		int64(state.RecordCount),
		int64(state.RebuildCount),
		state.CapturedAt.UTC(),
		bucketsData,
		recordsData,
		levelsData,
	)
	return err
}

// LoadState loads an exact commitment by partition, bucket width, and scope.
func (s *PostgresIncrementalCommitmentStore) LoadState(ctx context.Context, partition string, width time.Duration, scope Scope) (IncrementalCommitmentState, bool, error) {
	if err := ctx.Err(); err != nil {
		return IncrementalCommitmentState{}, false, err
	}
	if partition == "" || width <= 0 {
		return IncrementalCommitmentState{}, false, fmt.Errorf("%w: partition and positive width are required", ErrInvalidIncrementalConfig)
	}
	scope = normalizeScope(scope)
	if err := validateScope(scope); err != nil {
		return IncrementalCommitmentState{}, false, err
	}

	query := `
		SELECT partition, bucket_width_ns, scope_from, scope_to,
		       generation, canonical_version, algorithm_version,
		       root, record_count, rebuild_count, captured_at,
		       buckets_data, records_data, levels_data
		FROM merkle_commitments
		WHERE partition = $1 AND bucket_width_ns = $2 AND scope_from = $3 AND scope_to = $4
	`
	var (
		st           IncrementalCommitmentState
		widthNs      int64
		recordCount  int64
		rebuildCount int64
		bucketsRaw   []byte
		recordsRaw   []byte
		levelsRaw    []byte
	)
	err := s.pool.QueryRow(ctx, query, partition, int64(width), scope.From.UTC(), scope.To.UTC()).Scan(
		&st.Partition,
		&widthNs,
		&st.Scope.From,
		&st.Scope.To,
		&st.Generation,
		&st.CanonicalVersion,
		&st.AlgorithmVersion,
		&st.Root,
		&recordCount,
		&rebuildCount,
		&st.CapturedAt,
		&bucketsRaw,
		&recordsRaw,
		&levelsRaw,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IncrementalCommitmentState{}, false, nil
		}
		return IncrementalCommitmentState{}, false, err
	}

	st.BucketWidth = time.Duration(widthNs)
	st.RecordCount = int(recordCount)
	st.RebuildCount = uint64(rebuildCount)
	st.Scope.From = st.Scope.From.UTC()
	st.Scope.To = st.Scope.To.UTC()
	st.CapturedAt = st.CapturedAt.UTC()

	if err := json.Unmarshal(bucketsRaw, &st.Buckets); err != nil {
		return IncrementalCommitmentState{}, false, fmt.Errorf("failed to unmarshal buckets: %w", err)
	}
	if err := json.Unmarshal(recordsRaw, &st.BucketRecords); err != nil {
		return IncrementalCommitmentState{}, false, fmt.Errorf("failed to unmarshal bucket records: %w", err)
	}
	if err := json.Unmarshal(levelsRaw, &st.Levels); err != nil {
		return IncrementalCommitmentState{}, false, fmt.Errorf("failed to unmarshal levels: %w", err)
	}

	if err := validateIncrementalState(st); err != nil {
		return IncrementalCommitmentState{}, false, err
	}
	return st, true, nil
}

// FindState searches for the latest commitment for the partition and scope, regardless of bucket width.
func (s *PostgresIncrementalCommitmentStore) FindState(ctx context.Context, partition string, scope Scope) (IncrementalCommitmentState, bool, error) {
	if err := ctx.Err(); err != nil {
		return IncrementalCommitmentState{}, false, err
	}
	if partition == "" {
		return IncrementalCommitmentState{}, false, fmt.Errorf("%w: partition is required", ErrInvalidIncrementalConfig)
	}
	scope = normalizeScope(scope)
	if err := validateScope(scope); err != nil {
		return IncrementalCommitmentState{}, false, err
	}

	query := `
		SELECT partition, bucket_width_ns, scope_from, scope_to,
		       generation, canonical_version, algorithm_version,
		       root, record_count, rebuild_count, captured_at,
		       buckets_data, records_data, levels_data
		FROM merkle_commitments
		WHERE partition = $1 AND scope_from = $2 AND scope_to = $3
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`
	var (
		st           IncrementalCommitmentState
		widthNs      int64
		recordCount  int64
		rebuildCount int64
		bucketsRaw   []byte
		recordsRaw   []byte
		levelsRaw    []byte
	)
	err := s.pool.QueryRow(ctx, query, partition, scope.From.UTC(), scope.To.UTC()).Scan(
		&st.Partition,
		&widthNs,
		&st.Scope.From,
		&st.Scope.To,
		&st.Generation,
		&st.CanonicalVersion,
		&st.AlgorithmVersion,
		&st.Root,
		&recordCount,
		&rebuildCount,
		&st.CapturedAt,
		&bucketsRaw,
		&recordsRaw,
		&levelsRaw,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IncrementalCommitmentState{}, false, nil
		}
		return IncrementalCommitmentState{}, false, err
	}

	st.BucketWidth = time.Duration(widthNs)
	st.RecordCount = int(recordCount)
	st.RebuildCount = uint64(rebuildCount)
	st.Scope.From = st.Scope.From.UTC()
	st.Scope.To = st.Scope.To.UTC()
	st.CapturedAt = st.CapturedAt.UTC()

	if err := json.Unmarshal(bucketsRaw, &st.Buckets); err != nil {
		return IncrementalCommitmentState{}, false, fmt.Errorf("failed to unmarshal buckets: %w", err)
	}
	if err := json.Unmarshal(recordsRaw, &st.BucketRecords); err != nil {
		return IncrementalCommitmentState{}, false, fmt.Errorf("failed to unmarshal bucket records: %w", err)
	}
	if err := json.Unmarshal(levelsRaw, &st.Levels); err != nil {
		return IncrementalCommitmentState{}, false, fmt.Errorf("failed to unmarshal levels: %w", err)
	}

	if err := validateIncrementalState(st); err != nil {
		return IncrementalCommitmentState{}, false, err
	}
	return st, true, nil
}
