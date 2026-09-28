package reconciliation

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// maxRunsPage is the hard ceiling for a single list-runs query.
	maxRunsPage = 100
	// defaultRunsPage is the default page size for list-runs.
	defaultRunsPage = 20
	// maxDiscrepanciesPage is the hard ceiling for a single discrepancy query.
	maxDiscrepanciesPage = 200
	// defaultDiscrepanciesPage is the default page size for discrepancy listing.
	defaultDiscrepanciesPage = 50
)

// RunRepository persists and retrieves reconciliation run records and
// discrepancy evidence. It does not store authoritative financial state.
type RunRepository struct {
	db *pgxpool.Pool
}

// NewRunRepository creates a RunRepository backed by the provided pool.
func NewRunRepository(db *pgxpool.Pool) *RunRepository {
	return &RunRepository{db: db}
}

// CreateRun inserts a new RUNNING reconciliation record and returns it with
// the database-assigned ID and started_at.
func (repo *RunRepository) CreateRun(ctx context.Context, participantID string, scope Scope) (Run, error) {
	scope = scope.Normalize()
	if err := validateRepositoryScope(scope); err != nil {
		return Run{}, fmt.Errorf("%w: %v", ErrInvalidRunScope, err)
	}
	run := Run{}
	row := repo.db.QueryRow(ctx, `
		INSERT INTO recon_runs (participant_id, scope_from, scope_to, status, started_at)
		VALUES ($1, $2, $3, 'RUNNING', NOW())
		RETURNING id, participant_id, scope_from, scope_to, status,
		          canonical_root, participant_root, canonical_version, algorithm_version,
		          record_count, discrepancy_count, error_message, started_at, completed_at
	`, participantID, scope.From, scope.To)
	if scanErr := scanRun(row, &run); scanErr != nil {
		return Run{}, fmt.Errorf("create reconciliation run: %w", scanErr)
	}
	return run, nil
}

// CompleteRun marks a run COMPLETED and records the commitment comparison
// summary. This is the normal terminal state regardless of discrepancy count.
func (repo *RunRepository) CompleteRun(ctx context.Context, runID uuid.UUID, canonicalRoot, participantRoot []byte, canonicalVersion, algorithmVersion string, recordCount, discrepancyCount int64) (Run, error) {
	run := Run{}
	row := repo.db.QueryRow(ctx, `
		UPDATE recon_runs SET
			status            = 'COMPLETED',
			canonical_root    = $2,
			participant_root  = $3,
			canonical_version = $4,
			algorithm_version = $5,
			record_count      = $6,
			discrepancy_count = $7,
			completed_at      = NOW()
		WHERE id = $1 AND status = 'RUNNING'
		RETURNING id, participant_id, scope_from, scope_to, status,
		          canonical_root, participant_root, canonical_version, algorithm_version,
		          record_count, discrepancy_count, error_message, started_at, completed_at
	`, runID, canonicalRoot, participantRoot, canonicalVersion, algorithmVersion, recordCount, discrepancyCount)
	if scanErr := scanRun(row, &run); scanErr != nil {
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return Run{}, ErrRunNotFound
		}
		return Run{}, fmt.Errorf("complete reconciliation run: %w", scanErr)
	}
	return run, nil
}

// FailRun marks a run FAILED with an operational error message. Financial
// mismatches must not use this path; use CompleteRun + SaveDiscrepancy instead.
func (repo *RunRepository) FailRun(ctx context.Context, runID uuid.UUID, errMsg string) (Run, error) {
	run := Run{}
	row := repo.db.QueryRow(ctx, `
		UPDATE recon_runs SET
			status        = 'FAILED',
			error_message = $2,
			completed_at  = NOW()
		WHERE id = $1 AND status = 'RUNNING'
		RETURNING id, participant_id, scope_from, scope_to, status,
		          canonical_root, participant_root, canonical_version, algorithm_version,
		          record_count, discrepancy_count, error_message, started_at, completed_at
	`, runID, errMsg)
	if scanErr := scanRun(row, &run); scanErr != nil {
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return Run{}, ErrRunNotFound
		}
		return Run{}, fmt.Errorf("fail reconciliation run: %w", scanErr)
	}
	return run, nil
}

// GetRun returns one run by ID.
func (repo *RunRepository) GetRun(ctx context.Context, runID uuid.UUID) (Run, error) {
	run := Run{}
	row := repo.db.QueryRow(ctx, `
		SELECT id, participant_id, scope_from, scope_to, status,
		       canonical_root, participant_root, canonical_version, algorithm_version,
		       record_count, discrepancy_count, error_message, started_at, completed_at
		FROM recon_runs WHERE id = $1
	`, runID)
	if scanErr := scanRun(row, &run); scanErr != nil {
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return Run{}, ErrRunNotFound
		}
		return Run{}, fmt.Errorf("get reconciliation run: %w", scanErr)
	}
	return run, nil
}

// ListRunsRequest carries validated, bounded pagination parameters.
type ListRunsRequest struct {
	ParticipantID string // optional filter
	Limit         int
	Offset        int
}

// normalizeListRunsRequest applies safe defaults and hard ceiling.
func normalizeListRunsRequest(req ListRunsRequest) ListRunsRequest {
	if req.Limit <= 0 {
		req.Limit = defaultRunsPage
	}
	if req.Limit > maxRunsPage {
		req.Limit = maxRunsPage
	}
	if req.Offset < 0 {
		req.Offset = 0
	}
	return req
}

// ListRuns returns a bounded, deterministically ordered page of runs.
func (repo *RunRepository) ListRuns(ctx context.Context, req ListRunsRequest) (RunListPage, error) {
	req = normalizeListRunsRequest(req)

	var totalCount int
	if req.ParticipantID != "" {
		row := repo.db.QueryRow(ctx, `SELECT COUNT(*) FROM recon_runs WHERE participant_id = $1`, req.ParticipantID)
		if err := row.Scan(&totalCount); err != nil {
			return RunListPage{}, fmt.Errorf("count reconciliation runs: %w", err)
		}
	} else {
		row := repo.db.QueryRow(ctx, `SELECT COUNT(*) FROM recon_runs`)
		if err := row.Scan(&totalCount); err != nil {
			return RunListPage{}, fmt.Errorf("count reconciliation runs: %w", err)
		}
	}

	var rows pgx.Rows
	var err error
	if req.ParticipantID != "" {
		rows, err = repo.db.Query(ctx, `
			SELECT id, participant_id, scope_from, scope_to, status,
			       canonical_root, participant_root, canonical_version, algorithm_version,
			       record_count, discrepancy_count, error_message, started_at, completed_at
			FROM recon_runs
			WHERE participant_id = $1
			ORDER BY started_at DESC
			LIMIT $2 OFFSET $3
		`, req.ParticipantID, req.Limit+1, req.Offset)
	} else {
		rows, err = repo.db.Query(ctx, `
			SELECT id, participant_id, scope_from, scope_to, status,
			       canonical_root, participant_root, canonical_version, algorithm_version,
			       record_count, discrepancy_count, error_message, started_at, completed_at
			FROM recon_runs
			ORDER BY started_at DESC
			LIMIT $1 OFFSET $2
		`, req.Limit+1, req.Offset)
	}
	if err != nil {
		return RunListPage{}, fmt.Errorf("list reconciliation runs: %w", err)
	}
	defer rows.Close()

	items := make([]Run, 0, req.Limit)
	for rows.Next() {
		var run Run
		if scanErr := scanRunRow(rows, &run); scanErr != nil {
			return RunListPage{}, fmt.Errorf("scan reconciliation run: %w", scanErr)
		}
		items = append(items, run)
	}
	if err := rows.Err(); err != nil {
		return RunListPage{}, fmt.Errorf("iterate reconciliation runs: %w", err)
	}

	page := RunListPage{Limit: req.Limit, Total: totalCount}
	if len(items) > req.Limit {
		items = items[:req.Limit]
		nextOff := req.Offset + req.Limit
		page.NextOffset = &nextOff
	}
	page.Items = items
	return page, nil
}

// SaveDiscrepancy inserts one discrepancy evidence record.
func (repo *RunRepository) SaveDiscrepancy(ctx context.Context, disc Discrepancy) (Discrepancy, error) {
	evidenceJSON, err := json.Marshal(disc.Evidence)
	if err != nil {
		return Discrepancy{}, fmt.Errorf("marshal discrepancy evidence: %w", err)
	}
	out := Discrepancy{}
	row := repo.db.QueryRow(ctx, `
		INSERT INTO recon_discrepancies (
			run_id, participant_id, bucket_key, bucket_partition,
			bucket_start, bucket_width_ns,
			expected_root, observed_root, mismatch_category,
			resolved, evidence, detected_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,false,$10,NOW())
		RETURNING id, run_id, participant_id, bucket_key, bucket_partition,
		          bucket_start, bucket_width_ns, expected_root, observed_root,
		          mismatch_category, resolved, evidence, detected_at
	`, disc.RunID, disc.ParticipantID, disc.BucketKey, disc.BucketPartition,
		disc.BucketStart, disc.BucketWidthNs,
		disc.ExpectedRoot, disc.ObservedRoot, disc.MismatchCategory,
		evidenceJSON)
	if scanErr := scanDiscrepancy(row, &out); scanErr != nil {
		return Discrepancy{}, fmt.Errorf("save discrepancy: %w", scanErr)
	}
	return out, nil
}

// ListDiscrepanciesRequest carries pagination for discrepancy listing.
type ListDiscrepanciesRequest struct {
	RunID  uuid.UUID
	Limit  int
	Offset int
}

func normalizeListDiscrepanciesRequest(req ListDiscrepanciesRequest) ListDiscrepanciesRequest {
	if req.Limit <= 0 {
		req.Limit = defaultDiscrepanciesPage
	}
	if req.Limit > maxDiscrepanciesPage {
		req.Limit = maxDiscrepanciesPage
	}
	if req.Offset < 0 {
		req.Offset = 0
	}
	return req
}

// ListDiscrepancies returns a bounded page of discrepancy evidence for a run.
func (repo *RunRepository) ListDiscrepancies(ctx context.Context, req ListDiscrepanciesRequest) (DiscrepancyListPage, error) {
	req = normalizeListDiscrepanciesRequest(req)

	var totalCount int
	if err := repo.db.QueryRow(ctx, `SELECT COUNT(*) FROM recon_discrepancies WHERE run_id = $1`, req.RunID).Scan(&totalCount); err != nil {
		return DiscrepancyListPage{}, fmt.Errorf("count discrepancies: %w", err)
	}

	rows, err := repo.db.Query(ctx, `
		SELECT id, run_id, participant_id, bucket_key, bucket_partition,
		       bucket_start, bucket_width_ns, expected_root, observed_root,
		       mismatch_category, resolved, evidence, detected_at
		FROM recon_discrepancies
		WHERE run_id = $1
		ORDER BY detected_at ASC
		LIMIT $2 OFFSET $3
	`, req.RunID, req.Limit+1, req.Offset)
	if err != nil {
		return DiscrepancyListPage{}, fmt.Errorf("list discrepancies: %w", err)
	}
	defer rows.Close()

	items := make([]Discrepancy, 0, req.Limit)
	for rows.Next() {
		var disc Discrepancy
		if scanErr := scanDiscrepancyRow(rows, &disc); scanErr != nil {
			return DiscrepancyListPage{}, fmt.Errorf("scan discrepancy: %w", scanErr)
		}
		items = append(items, disc)
	}
	if err := rows.Err(); err != nil {
		return DiscrepancyListPage{}, fmt.Errorf("iterate discrepancies: %w", err)
	}

	page := DiscrepancyListPage{Limit: req.Limit, Total: totalCount}
	if len(items) > req.Limit {
		items = items[:req.Limit]
		nextOff := req.Offset + req.Limit
		page.NextOffset = &nextOff
	}
	page.Items = items
	return page, nil
}

// --- scan helpers ---

type runScanner interface {
	Scan(dest ...any) error
}

func scanRun(scanner runScanner, run *Run) error {
	return scanRunFields(scanner, run)
}

func scanRunRow(row pgx.Row, run *Run) error {
	return scanRunFields(row, run)
}

func scanRunFields(scanner runScanner, run *Run) error {
	var canonRoot, partRoot []byte
	var canonVer, algoVer, errMsg *string
	var completedAt *time.Time
	if err := scanner.Scan(
		&run.ID, &run.ParticipantID, &run.ScopeFrom, &run.ScopeTo, &run.Status,
		&canonRoot, &partRoot, &canonVer, &algoVer,
		&run.RecordCount, &run.DiscrepancyCount, &errMsg,
		&run.StartedAt, &completedAt,
	); err != nil {
		return err
	}
	run.ScopeFrom = run.ScopeFrom.UTC()
	run.ScopeTo = run.ScopeTo.UTC()
	run.StartedAt = run.StartedAt.UTC()
	if completedAt != nil {
		t := completedAt.UTC()
		run.CompletedAt = &t
	}
	if canonRoot != nil {
		run.CanonicalRoot = canonRoot
	}
	if partRoot != nil {
		run.ParticipantRoot = partRoot
	}
	if canonVer != nil {
		run.CanonicalVersion = *canonVer
	}
	if algoVer != nil {
		run.AlgorithmVersion = *algoVer
	}
	if errMsg != nil {
		run.ErrorMessage = *errMsg
	}
	return nil
}

type discScanner interface {
	Scan(dest ...any) error
}

func scanDiscrepancy(scanner discScanner, disc *Discrepancy) error {
	return scanDiscrepancyFields(scanner, disc)
}

func scanDiscrepancyRow(row pgx.Row, disc *Discrepancy) error {
	return scanDiscrepancyFields(row, disc)
}

func scanDiscrepancyFields(scanner discScanner, disc *Discrepancy) error {
	var expRoot, obsRoot []byte
	var evidenceJSON []byte
	if err := scanner.Scan(
		&disc.ID, &disc.RunID, &disc.ParticipantID, &disc.BucketKey, &disc.BucketPartition,
		&disc.BucketStart, &disc.BucketWidthNs,
		&expRoot, &obsRoot, &disc.MismatchCategory,
		&disc.Resolved, &evidenceJSON, &disc.DetectedAt,
	); err != nil {
		return err
	}
	disc.BucketStart = disc.BucketStart.UTC()
	disc.DetectedAt = disc.DetectedAt.UTC()
	if expRoot != nil {
		disc.ExpectedRoot = expRoot
	}
	if obsRoot != nil {
		disc.ObservedRoot = obsRoot
	}
	if len(evidenceJSON) > 0 {
		evidence := make(map[string]string)
		if err := json.Unmarshal(evidenceJSON, &evidence); err == nil {
			disc.Evidence = evidence
		}
	}
	return nil
}

// hexOrEmpty returns a hex string for a hash, or empty string for nil/empty.
func hexOrEmpty(hash []byte) string {
	if len(hash) == 0 {
		return ""
	}
	return hex.EncodeToString(hash)
}
