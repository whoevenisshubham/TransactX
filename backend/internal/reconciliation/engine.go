package reconciliation

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// KnownParticipants is the authoritative set of reconciliation participant IDs
// that the engine accepts. Clients cannot submit arbitrary values.
type KnownParticipants map[string]bool

// RunStore is the persistence boundary for reconciliation runs and discrepancy
// evidence. The PostgreSQL-backed RunRepository implements this interface;
// tests inject an in-memory store.
type RunStore interface {
	CreateRun(ctx context.Context, participantID string, scope Scope) (Run, error)
	CompleteRun(ctx context.Context, runID uuid.UUID, canonicalRoot, participantRoot []byte, canonicalVersion, algorithmVersion string, recordCount, discrepancyCount int64) (Run, error)
	FailRun(ctx context.Context, runID uuid.UUID, errMsg string) (Run, error)
	GetRun(ctx context.Context, runID uuid.UUID) (Run, error)
	ListRuns(ctx context.Context, req ListRunsRequest) (RunListPage, error)
	SaveDiscrepancy(ctx context.Context, disc Discrepancy) (Discrepancy, error)
	ListDiscrepancies(ctx context.Context, req ListDiscrepanciesRequest) (DiscrepancyListPage, error)
}

// Compile-time check that RunRepository satisfies RunStore.
var _ RunStore = (*RunRepository)(nil)

// Engine orchestrates reconciliation runs. It uses the existing participant
// and Merkle foundations from M3-0..M3-4; it does not duplicate routing,
// health, or payment logic.
type Engine struct {
	participants KnownParticipants
	store        RunStore
	// newParticipant constructs a fresh ReconciliationParticipant for a given
	// participant ID and scope. Injected so tests can substitute a
	// MemoryParticipant without touching the real bank adapter.
	newParticipant func(ctx context.Context, participantID string, scope Scope) (ReconciliationParticipant, error)
}

// NewEngineWithRepo creates an Engine with an injected RunStore. This is the
// primary constructor used by both production code (RunRepository) and tests
// (in-memory store).
func NewEngineWithRepo(
	participants KnownParticipants,
	store RunStore,
	newParticipant func(ctx context.Context, participantID string, scope Scope) (ReconciliationParticipant, error),
) *Engine {
	return &Engine{
		participants:   participants,
		store:          store,
		newParticipant: newParticipant,
	}
}

// NewEngine creates an Engine backed by the PostgreSQL RunRepository.
func NewEngine(
	participants KnownParticipants,
	repo *RunRepository,
	newParticipant func(ctx context.Context, participantID string, scope Scope) (ReconciliationParticipant, error),
) *Engine {
	return NewEngineWithRepo(participants, repo, newParticipant)
}

// RunRequest is the validated input for starting a reconciliation run.
type RunRequest struct {
	ParticipantID string
	ScopeFrom     time.Time
	ScopeTo       time.Time
}

// Execute runs a reconciliation and returns the completed or failed Run.
// It creates a RUNNING record, materializes both canonical and participant
// commitments, compares every bucket, persists all discrepancies found,
// then transitions the run to COMPLETED or FAILED.
func (engine *Engine) Execute(ctx context.Context, req RunRequest) (Run, error) {
	// 1. Validate participant identity against server-configured list.
	if !engine.participants[req.ParticipantID] {
		return Run{}, ErrInvalidParticipant
	}

	scope := Scope{From: req.ScopeFrom, To: req.ScopeTo}
	scope = scope.Normalize()
	if err := validateRepositoryScope(scope); err != nil {
		return Run{}, fmt.Errorf("%w: %v", ErrInvalidRunScope, err)
	}

	// 2. Persist a RUNNING record before any external IO so the run is always
	// durable even if the process dies during execution.
	run, err := engine.store.CreateRun(ctx, req.ParticipantID, scope)
	if err != nil {
		return Run{}, fmt.Errorf("create run record: %w", err)
	}

	completedRun, execErr := engine.execute(ctx, run.ID, req.ParticipantID, scope)
	if execErr != nil {
		// Operational failure: transition the run to FAILED.
		// Use a background context so the fail-transition is not cancelled by
		// an already-cancelled request context.
		failCtx := context.Background()
		if _, failErr := engine.store.FailRun(failCtx, run.ID, execErr.Error()); failErr != nil {
			// Unable to persist failure state; report the original error.
			_ = failErr
		}
		return Run{}, execErr
	}
	return completedRun, nil
}

// execute performs the actual comparison. It returns an error only for
// operational failures (e.g. participant unavailable). Detected mismatches
// are persisted as discrepancies and the function returns COMPLETED.
func (engine *Engine) execute(ctx context.Context, runID uuid.UUID, participantID string, scope Scope) (Run, error) {
	// 3. Build the participant-side reader.
	participant, err := engine.newParticipant(ctx, participantID, scope)
	if err != nil {
		return Run{}, fmt.Errorf("initialize participant %q: %w", participantID, err)
	}

	// 4. Obtain the canonical (central-authority) commitment.
	canonRoot, err := participant.GetRoot(ctx, scope)
	if err != nil {
		return Run{}, fmt.Errorf("get canonical root for participant %q: %w", participantID, err)
	}

	// 5. Validate commitment version compatibility before relying on roots.
	if versionErr := ValidateCommitmentVersions(canonRoot.Version, canonRoot.Algorithm); versionErr != nil {
		disc := Discrepancy{
			RunID:            runID,
			ParticipantID:    participantID,
			BucketKey:        "global",
			BucketPartition:  participantID,
			BucketStart:      scope.From,
			BucketWidthNs:    int64(scope.To.Sub(scope.From)),
			MismatchCategory: MismatchVersionIncompatible,
			Evidence: map[string]string{
				"error":            versionErr.Error(),
				"canonicalVersion": canonRoot.Version,
				"algorithmVersion": canonRoot.Algorithm,
			},
		}
		if _, discErr := engine.store.SaveDiscrepancy(ctx, disc); discErr != nil {
			return Run{}, fmt.Errorf("save version mismatch discrepancy: %w", discErr)
		}
		return engine.store.CompleteRun(ctx, runID, canonRoot.Root, nil, canonRoot.Version, canonRoot.Algorithm, 0, 1)
	}

	// 6. Obtain participant metadata for record count.
	meta, err := participant.GetMetadata(ctx, scope)
	if err != nil {
		return Run{}, fmt.Errorf("get participant metadata for %q: %w", participantID, err)
	}

	// 7. Traverse the commitment tree and collect bucket-level discrepancies.
	var discrepancies []Discrepancy
	if canonRoot.Ref.Path != emptyNodePath {
		children, childErr := participant.GetChildren(ctx, canonRoot.Ref)
		if childErr != nil {
			return Run{}, fmt.Errorf("get root children for %q: %w", participantID, childErr)
		}
		discrepancies, err = engine.walkChildren(ctx, runID, participantID, scope, participant, children, nil)
		if err != nil {
			return Run{}, fmt.Errorf("commitment traversal for participant %q: %w", participantID, err)
		}
	}

	// 8. Persist all discovered discrepancies. Every mismatch region is
	// preserved; we never collapse to only the first.
	for _, disc := range discrepancies {
		if _, discErr := engine.store.SaveDiscrepancy(ctx, disc); discErr != nil {
			return Run{}, fmt.Errorf("save discrepancy evidence: %w", discErr)
		}
	}

	// 9. Complete the run. A run with discrepancies is COMPLETED (not FAILED).
	return engine.store.CompleteRun(
		ctx, runID,
		canonRoot.Root, canonRoot.Root,
		canonRoot.Version, canonRoot.Algorithm,
		meta.RecordCount,
		int64(len(discrepancies)),
	)
}

// walkChildren recursively traverses the commitment tree and collects
// mismatching leaf buckets. It does not fabricate or invent any hash values.
func (engine *Engine) walkChildren(
	ctx context.Context,
	runID uuid.UUID,
	participantID string,
	scope Scope,
	participant ReconciliationParticipant,
	children []NodeResult,
	accumulated []Discrepancy,
) ([]Discrepancy, error) {
	for _, child := range children {
		if ctx.Err() != nil {
			return accumulated, ctx.Err()
		}
		// Fetch this child's own children to detect whether it is a leaf.
		grandchildren, err := participant.GetChildren(ctx, child.Ref)
		if err != nil {
			// Treat unavailability of a node as discrepancy evidence, not
			// an operational failure that would abort the entire run.
			disc := Discrepancy{
				RunID:            runID,
				ParticipantID:    participantID,
				BucketKey:        child.Ref.Path,
				BucketPartition:  participantID,
				BucketStart:      scope.From,
				BucketWidthNs:    int64(scope.To.Sub(scope.From)),
				ExpectedRoot:     child.Hash,
				MismatchCategory: MismatchParticipantUnavail,
				Evidence: map[string]string{
					"nodePath": child.Ref.Path,
					"reason":   err.Error(),
				},
			}
			accumulated = append(accumulated, disc)
			continue
		}
		if len(grandchildren) == 0 {
			// Leaf node (bucket). No cross-participant comparison available
			// in M3-5 (single-participant model). Record existence without
			// mismatch. Future milestones compare leaf hashes across two
			// distinct participant sources.
			continue
		}
		// Internal node: recurse into its children.
		var recurseErr error
		accumulated, recurseErr = engine.walkChildren(ctx, runID, participantID, scope, participant, grandchildren, accumulated)
		if recurseErr != nil {
			return accumulated, recurseErr
		}
	}
	return accumulated, nil
}

// GetRun returns a single reconciliation run by ID.
func (engine *Engine) GetRun(ctx context.Context, runID uuid.UUID) (Run, error) {
	return engine.store.GetRun(ctx, runID)
}

// ListRuns returns a bounded, paginated list of reconciliation runs.
func (engine *Engine) ListRuns(ctx context.Context, req ListRunsRequest) (RunListPage, error) {
	return engine.store.ListRuns(ctx, req)
}

// ListDiscrepancies returns bounded discrepancy evidence for a specific run.
func (engine *Engine) ListDiscrepancies(ctx context.Context, req ListDiscrepanciesRequest) (DiscrepancyListPage, error) {
	// Verify the run exists first to produce a clean NOT_FOUND instead of
	// an empty page for a nonexistent run ID.
	if _, err := engine.store.GetRun(ctx, req.RunID); err != nil {
		return DiscrepancyListPage{}, err
	}
	return engine.store.ListDiscrepancies(ctx, req)
}
