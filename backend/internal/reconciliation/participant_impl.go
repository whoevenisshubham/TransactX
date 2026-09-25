package reconciliation

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/transactx/backend/internal/bank"
)

const (
	rootNodePath  = "root"
	emptyNodePath = "empty"
)

// LedgerSnapshotSource is the narrow repository/service-owned read boundary
// needed by a real reconciliation participant. bankservice.Service satisfies
// it through its existing GetLedgerSnapshot method.
type LedgerSnapshotSource interface {
	GetLedgerSnapshot(context.Context, bank.LedgerScope) (bank.LedgerSnapshot, error)
}

// MemoryParticipant is a deterministic research fixture. Its records are
// caller-provided logical ledger data; no random identifiers or metrics are
// generated. Every commitment uses the shared canonical/Merkle implementation.
type MemoryParticipant struct {
	mu             sync.RWMutex
	participantID  string
	partition      string
	bucketWidth    time.Duration
	capturedAt     time.Time
	records        []CanonicalRecord
	snapshots      map[string]participantSnapshot
	current        map[string]string
	nextGeneration uint64
}

type participantSnapshot struct {
	state      IncrementalCommitmentState
	capturedAt time.Time
}

func NewMemoryParticipant(participantID, partition string, bucketWidth time.Duration, records []CanonicalRecord) (*MemoryParticipant, error) {
	return NewMemoryParticipantWithCapture(participantID, partition, bucketWidth, time.Time{}, records)
}

func NewMemoryParticipantWithCapture(participantID, partition string, bucketWidth time.Duration, capturedAt time.Time, records []CanonicalRecord) (*MemoryParticipant, error) {
	if participantID == "" {
		return nil, fmt.Errorf("%w: participant identity is required", ErrParticipantMismatch)
	}
	if partition == "" || bucketWidth <= 0 {
		return nil, fmt.Errorf("%w: partition and positive bucket width are required", ErrInvalidIncrementalConfig)
	}
	return &MemoryParticipant{
		participantID: participantID,
		partition:     partition,
		bucketWidth:   bucketWidth,
		capturedAt:    capturedAt.UTC(),
		records:       cloneCanonicalRecords(records),
		snapshots:     make(map[string]participantSnapshot),
		current:       make(map[string]string),
	}, nil
}

// Refresh rebuilds one derived fixture commitment explicitly. It creates a
// new process-local generation and invalidates references to the old one.
func (participant *MemoryParticipant) Refresh(ctx context.Context, scope Scope) error {
	scope = scope.Normalize()
	if err := scope.Validate(); err != nil {
		return err
	}
	state, err := participant.buildState(ctx, scope)
	if err != nil {
		return err
	}
	participant.installSnapshot(scope, state, participant.capturedAt)
	return nil
}

func (participant *MemoryParticipant) GetRoot(ctx context.Context, scope Scope) (RootResult, error) {
	state, generation, normalized, err := participant.state(ctx, scope)
	if err != nil {
		return RootResult{}, err
	}
	return rootResult(participant.participantID, normalized, generation, state), nil
}

func (participant *MemoryParticipant) GetChildren(ctx context.Context, ref NodeRef) ([]NodeResult, error) {
	if err := participant.validateParticipant(ref.ParticipantID); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scope, err := parseScopeIdentity(ref.ScopeID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidNodeReference, err)
	}
	state, err := participant.referenceState(ref.Generation, scope, ErrInvalidNodeReference)
	if err != nil {
		return nil, err
	}
	return childrenFromState(participant.participantID, scope, ref.Generation, state, ref)
}

func (participant *MemoryParticipant) GetRecords(ctx context.Context, ref BucketRef) ([]CanonicalRecord, error) {
	if err := participant.validateParticipant(ref.ParticipantID); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scope, err := parseScopeIdentity(ref.ScopeID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidBucketReference, err)
	}
	state, err := participant.referenceState(ref.Generation, scope, ErrInvalidBucketReference)
	if err != nil {
		return nil, err
	}
	return recordsFromState(scope, ref.Generation, state, ref)
}

func (participant *MemoryParticipant) GetBucketID(ctx context.Context, ref NodeRef) (BucketID, error) {
	if err := participant.validateParticipant(ref.ParticipantID); err != nil {
		return BucketID{}, err
	}
	if err := ctx.Err(); err != nil {
		return BucketID{}, err
	}
	scope, err := parseScopeIdentity(ref.ScopeID)
	if err != nil {
		return BucketID{}, fmt.Errorf("%w: %v", ErrInvalidNodeReference, err)
	}
	state, err := participant.referenceState(ref.Generation, scope, ErrInvalidNodeReference)
	if err != nil {
		return BucketID{}, err
	}
	return bucketIDFromState(scope, ref.Generation, state, ref)
}

func (participant *MemoryParticipant) GetMetadata(ctx context.Context, scope Scope) (ParticipantMetadata, error) {
	state, _, _, err := participant.state(ctx, scope)
	if err != nil {
		return ParticipantMetadata{}, err
	}
	return ParticipantMetadata{ParticipantID: participant.participantID, CapturedAt: participant.capturedAt, RecordCount: int64(state.RecordCount)}, nil
}

func (participant *MemoryParticipant) state(ctx context.Context, scope Scope) (IncrementalCommitmentState, string, Scope, error) {
	if err := ctx.Err(); err != nil {
		return IncrementalCommitmentState{}, "", Scope{}, err
	}
	scope = scope.Normalize()
	if err := scope.Validate(); err != nil {
		return IncrementalCommitmentState{}, "", Scope{}, err
	}
	key := ScopeIdentity(scope)
	participant.mu.RLock()
	generation, ok := participant.current[key]
	cached := participant.snapshots[generation]
	participant.mu.RUnlock()
	if ok {
		return cloneIncrementalState(cached.state), generation, scope, nil
	}
	state, err := participant.buildState(ctx, scope)
	if err != nil {
		return IncrementalCommitmentState{}, "", Scope{}, err
	}
	generation = participant.installSnapshot(scope, state, participant.capturedAt)
	return state, generation, scope, nil
}

func (participant *MemoryParticipant) buildState(ctx context.Context, scope Scope) (IncrementalCommitmentState, error) {
	if err := ctx.Err(); err != nil {
		return IncrementalCommitmentState{}, err
	}
	scope = scope.Normalize()
	if err := scope.Validate(); err != nil {
		return IncrementalCommitmentState{}, err
	}
	ledger, err := NewIncrementalMerkleLedger(participant.partition, participant.bucketWidth, scope)
	if err != nil {
		return IncrementalCommitmentState{}, err
	}
	filtered := make([]CanonicalRecord, 0, len(participant.records))
	for _, record := range participant.records {
		if scopeContains(scope, record.OccurredAt) {
			filtered = append(filtered, record.Normalize())
		}
	}
	if _, err := ledger.Bootstrap(ctx, filtered); err != nil {
		return IncrementalCommitmentState{}, err
	}
	state := ledger.Snapshot()
	state.CapturedAt = participant.capturedAt
	return state, nil
}

func (participant *MemoryParticipant) installSnapshot(scope Scope, state IncrementalCommitmentState, capturedAt time.Time) string {
	key := ScopeIdentity(scope)
	participant.mu.Lock()
	defer participant.mu.Unlock()
	if old, ok := participant.current[key]; ok {
		delete(participant.snapshots, old)
	}
	participant.nextGeneration++
	generation := fmt.Sprintf("g%d", participant.nextGeneration)
	participant.snapshots[generation] = participantSnapshot{state: cloneIncrementalState(state), capturedAt: capturedAt.UTC()}
	participant.current[key] = generation
	return generation
}

func (participant *MemoryParticipant) referenceState(generation string, scope Scope, invalid error) (IncrementalCommitmentState, error) {
	if generation == "" {
		return IncrementalCommitmentState{}, fmt.Errorf("%w: generation is required", invalid)
	}
	key := ScopeIdentity(scope)
	participant.mu.RLock()
	snapshot, ok := participant.snapshots[generation]
	current := participant.current[key]
	participant.mu.RUnlock()
	if !ok || current != generation {
		return IncrementalCommitmentState{}, fmt.Errorf("%w: generation %q is no longer current", ErrStaleReference, generation)
	}
	return cloneIncrementalState(snapshot.state), nil
}

func (participant *MemoryParticipant) validateParticipant(requested string) error {
	if requested != "" && requested != participant.participantID {
		return fmt.Errorf("%w: requested=%q actual=%q", ErrParticipantMismatch, requested, participant.participantID)
	}
	return nil
}

// RepositoryParticipant reads the existing participant ledger through a
// repository/service snapshot source. It does not duplicate money movement or
// write participant state. The source remains authoritative for initialization
// and explicit refresh; normal reads consume maintained derived state only.
type RepositoryParticipant struct {
	mu             sync.RWMutex
	source         LedgerSnapshotSource
	commitments    IncrementalCommitmentStore
	participantID  string
	partition      string
	bucketWidth    time.Duration
	snapshots      map[string]participantSnapshot
	current        map[string]string
	nextGeneration uint64
}

func NewRepositoryParticipant(source LedgerSnapshotSource, participantID, partition string, bucketWidth time.Duration) (*RepositoryParticipant, error) {
	return NewRepositoryParticipantWithCommitmentStore(source, participantID, partition, bucketWidth, NewMemoryIncrementalCommitmentStore())
}

// NewRepositoryParticipantWithCommitmentStore injects the derived commitment
// persistence boundary. The default constructor uses a process-local store;
// callers that need restart persistence provide a durable implementation of
// IncrementalCommitmentStore without changing participant-ledger authority.
func NewRepositoryParticipantWithCommitmentStore(source LedgerSnapshotSource, participantID, partition string, bucketWidth time.Duration, commitments IncrementalCommitmentStore) (*RepositoryParticipant, error) {
	if source == nil {
		return nil, fmt.Errorf("%w: snapshot source is required", ErrInvalidIncrementalConfig)
	}
	if commitments == nil {
		return nil, fmt.Errorf("%w: commitment store is required", ErrInvalidIncrementalConfig)
	}
	if participantID == "" {
		return nil, fmt.Errorf("%w: participant identity is required", ErrParticipantMismatch)
	}
	if partition == "" || bucketWidth <= 0 {
		return nil, fmt.Errorf("%w: partition and positive bucket width are required", ErrInvalidIncrementalConfig)
	}
	return &RepositoryParticipant{source: source, commitments: commitments, participantID: participantID, partition: partition, bucketWidth: bucketWidth, snapshots: make(map[string]participantSnapshot), current: make(map[string]string)}, nil
}

// BucketWidth returns the configured bucket width for the participant.
func (participant *RepositoryParticipant) BucketWidth() time.Duration {
	participant.mu.RLock()
	defer participant.mu.RUnlock()
	return participant.bucketWidth
}

// Partition returns the configured partition for the participant.
func (participant *RepositoryParticipant) Partition() string {
	participant.mu.RLock()
	defer participant.mu.RUnlock()
	return participant.partition
}

// Initialize explicitly materializes a commitment from the authoritative
// participant ledger. This is the only normal API path that may call
// Bootstrap; it is intended for initial population or recovery.
func (participant *RepositoryParticipant) Initialize(ctx context.Context, scope Scope) error {
	state, capturedAt, normalized, err := participant.materialize(ctx, scope)
	if err != nil {
		return err
	}
	if err := participant.commitments.SaveState(ctx, state); err != nil {
		return err
	}
	participant.installSnapshot(normalized, state, capturedAt)
	return nil
}

// Refresh is an explicit recovery/reinitialization operation. It always
// creates a new commitment generation and invalidates references to the prior
// generation for the same scope.
func (participant *RepositoryParticipant) Refresh(ctx context.Context, scope Scope) error {
	return participant.Initialize(ctx, scope)
}

func (participant *RepositoryParticipant) GetRoot(ctx context.Context, scope Scope) (RootResult, error) {
	state, generation, normalized, err := participant.state(ctx, scope)
	if err != nil {
		return RootResult{}, err
	}
	return rootResult(participant.participantID, normalized, generation, state), nil
}

func (participant *RepositoryParticipant) GetChildren(ctx context.Context, ref NodeRef) ([]NodeResult, error) {
	if ref.ParticipantID != "" && ref.ParticipantID != participant.participantID {
		return nil, fmt.Errorf("%w: requested=%q actual=%q", ErrParticipantMismatch, ref.ParticipantID, participant.participantID)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scope, err := parseScopeIdentity(ref.ScopeID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidNodeReference, err)
	}
	state, err := participant.referenceState(ref.Generation, scope, ErrInvalidNodeReference)
	if err != nil {
		return nil, err
	}
	return childrenFromState(participant.participantID, scope, ref.Generation, state, ref)
}

func (participant *RepositoryParticipant) GetRecords(ctx context.Context, ref BucketRef) ([]CanonicalRecord, error) {
	if ref.ParticipantID != "" && ref.ParticipantID != participant.participantID {
		return nil, fmt.Errorf("%w: requested=%q actual=%q", ErrParticipantMismatch, ref.ParticipantID, participant.participantID)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scope, err := parseScopeIdentity(ref.ScopeID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidBucketReference, err)
	}
	state, err := participant.referenceState(ref.Generation, scope, ErrInvalidBucketReference)
	if err != nil {
		return nil, err
	}
	return recordsFromState(scope, ref.Generation, state, ref)
}

func (participant *RepositoryParticipant) GetBucketID(ctx context.Context, ref NodeRef) (BucketID, error) {
	if ref.ParticipantID != "" && ref.ParticipantID != participant.participantID {
		return BucketID{}, fmt.Errorf("%w: requested=%q actual=%q", ErrParticipantMismatch, ref.ParticipantID, participant.participantID)
	}
	if err := ctx.Err(); err != nil {
		return BucketID{}, err
	}
	scope, err := parseScopeIdentity(ref.ScopeID)
	if err != nil {
		return BucketID{}, fmt.Errorf("%w: %v", ErrInvalidNodeReference, err)
	}
	state, err := participant.referenceState(ref.Generation, scope, ErrInvalidNodeReference)
	if err != nil {
		return BucketID{}, err
	}
	return bucketIDFromState(scope, ref.Generation, state, ref)
}

func (participant *RepositoryParticipant) GetMetadata(ctx context.Context, scope Scope) (ParticipantMetadata, error) {
	state, _, _, err := participant.state(ctx, scope)
	if err != nil {
		return ParticipantMetadata{}, err
	}
	return ParticipantMetadata{ParticipantID: participant.participantID, CapturedAt: state.CapturedAt.UTC(), RecordCount: int64(state.RecordCount)}, nil
}

func (participant *RepositoryParticipant) state(ctx context.Context, scope Scope) (IncrementalCommitmentState, string, Scope, error) {
	if err := ctx.Err(); err != nil {
		return IncrementalCommitmentState{}, "", Scope{}, err
	}
	scope = scope.Normalize()
	if err := validateRepositoryScope(scope); err != nil {
		return IncrementalCommitmentState{}, "", Scope{}, err
	}
	key := ScopeIdentity(scope)
	participant.mu.RLock()
	generation, ok := participant.current[key]
	cached := participant.snapshots[generation]
	participant.mu.RUnlock()
	if ok {
		return cloneIncrementalState(cached.state), generation, scope, nil
	}
	state, found, err := participant.commitments.LoadState(ctx, participant.partition, participant.bucketWidth, scope)
	if err != nil {
		return IncrementalCommitmentState{}, "", Scope{}, err
	}
	if !found {
		return IncrementalCommitmentState{}, "", Scope{}, fmt.Errorf("%w: call Initialize or Refresh for scope %s", ErrCommitmentUnavailable, ScopeIdentity(scope))
	}
	generation = participant.installSnapshot(scope, state, state.CapturedAt)
	return state, generation, scope, nil
}

func (participant *RepositoryParticipant) materialize(ctx context.Context, scope Scope) (IncrementalCommitmentState, time.Time, Scope, error) {
	if err := ctx.Err(); err != nil {
		return IncrementalCommitmentState{}, time.Time{}, Scope{}, err
	}
	scope = scope.Normalize()
	if err := validateRepositoryScope(scope); err != nil {
		return IncrementalCommitmentState{}, time.Time{}, Scope{}, err
	}
	snapshot, err := participant.source.GetLedgerSnapshot(ctx, bank.LedgerScope{From: scope.From, To: scope.To})
	if err != nil {
		return IncrementalCommitmentState{}, time.Time{}, Scope{}, err
	}
	if snapshot.BankID != "" && snapshot.BankID != participant.participantID {
		return IncrementalCommitmentState{}, time.Time{}, Scope{}, fmt.Errorf("%w: snapshot=%q participant=%q", ErrParticipantMismatch, snapshot.BankID, participant.participantID)
	}
	records := make([]CanonicalRecord, 0, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		if scopeContains(scope, entry.OccurredAt) {
			records = append(records, FromLedgerEntry(entry))
		}
	}
	ledger, err := NewIncrementalMerkleLedger(participant.partition, participant.bucketWidth, scope)
	if err != nil {
		return IncrementalCommitmentState{}, time.Time{}, Scope{}, err
	}
	if _, err := ledger.Bootstrap(ctx, records); err != nil {
		return IncrementalCommitmentState{}, time.Time{}, Scope{}, err
	}
	state := ledger.Snapshot()
	state.CapturedAt = snapshot.CapturedAt.UTC()
	return state, state.CapturedAt, scope, nil
}

func (participant *RepositoryParticipant) installSnapshot(scope Scope, state IncrementalCommitmentState, capturedAt time.Time) string {
	key := ScopeIdentity(scope)
	participant.mu.Lock()
	defer participant.mu.Unlock()
	if old, ok := participant.current[key]; ok {
		delete(participant.snapshots, old)
	}
	participant.nextGeneration++
	generation := fmt.Sprintf("g%d", participant.nextGeneration)
	participant.snapshots[generation] = participantSnapshot{state: cloneIncrementalState(state), capturedAt: capturedAt.UTC()}
	participant.current[key] = generation
	return generation
}

func (participant *RepositoryParticipant) referenceState(generation string, scope Scope, invalid error) (IncrementalCommitmentState, error) {
	if generation == "" {
		return IncrementalCommitmentState{}, fmt.Errorf("%w: generation is required", invalid)
	}
	key := ScopeIdentity(scope)
	participant.mu.RLock()
	snapshot, ok := participant.snapshots[generation]
	current := participant.current[key]
	participant.mu.RUnlock()
	if !ok || current != generation {
		return IncrementalCommitmentState{}, fmt.Errorf("%w: generation %q is no longer current", ErrStaleReference, generation)
	}
	return cloneIncrementalState(snapshot.state), nil
}

func validateRepositoryScope(scope Scope) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if scope.From.IsZero() || scope.To.IsZero() {
		return fmt.Errorf("%w: repository reads require explicit scope endpoints", ErrInvalidScope)
	}
	return nil
}

func nodeRegionFromState(level, index int, state IncrementalCommitmentState) LogicalRegion {
	if len(state.Buckets) == 0 {
		return LogicalRegion{}
	}
	firstLeaf := index << level
	lastLeaf := ((index + 1) << level) - 1
	if firstLeaf >= len(state.Buckets) {
		firstLeaf = len(state.Buckets) - 1
	}
	if lastLeaf >= len(state.Buckets) {
		lastLeaf = len(state.Buckets) - 1
	}
	return LogicalRegion{
		Start: state.Buckets[firstLeaf].ID.Start,
		End:   state.Buckets[lastLeaf].ID.Start.Add(state.Buckets[lastLeaf].ID.Width),
	}
}

func rootResult(participantID string, scope Scope, generation string, state IncrementalCommitmentState) RootResult {
	path := emptyNodePath
	var region LogicalRegion
	if len(state.Buckets) > 0 {
		region = LogicalRegion{
			Start: state.Buckets[0].ID.Start,
			End:   state.Buckets[len(state.Buckets)-1].ID.Start.Add(state.Buckets[len(state.Buckets)-1].ID.Width),
		}
	}
	if len(state.Levels) > 0 {
		path = fmt.Sprintf("L%d/0", len(state.Levels)-1)
	}
	ref := NodeRef{
		ParticipantID: participantID,
		ScopeID:       ScopeIdentity(scope),
		Generation:    generation,
		Path:          path,
		Region:        region,
	}
	return RootResult{
		Root:      hashCopy(state.Root),
		Algorithm: state.AlgorithmVersion,
		Version:   state.CanonicalVersion,
		Ref:       ref,
		Region:    region,
	}
}

func childrenFromState(participantID string, scope Scope, generation string, state IncrementalCommitmentState, ref NodeRef) ([]NodeResult, error) {
	if ref.ScopeID != ScopeIdentity(scope) || ref.Generation != generation {
		return nil, fmt.Errorf("%w: reference commitment does not match requested generation", ErrStaleReference)
	}
	if ref.Path == emptyNodePath {
		if len(state.Levels) != 0 {
			return nil, ErrInvalidNodeReference
		}
		return []NodeResult{}, nil
	}
	level, index, err := parseNodePath(ref.Path, len(state.Levels))
	if err != nil {
		return nil, err
	}
	if index >= len(state.Levels[level]) {
		return nil, ErrNodeNotFound
	}
	if level == 0 {
		return []NodeResult{}, nil
	}
	childLevel := level - 1
	first := index * 2
	children := make([]NodeResult, 0, 2)
	for childIndex := first; childIndex < first+2 && childIndex < len(state.Levels[childLevel]); childIndex++ {
		region := nodeRegionFromState(childLevel, childIndex, state)
		childRef := NodeRef{
			ParticipantID: participantID,
			ScopeID:       ScopeIdentity(scope),
			Generation:    generation,
			Path:          fmt.Sprintf("L%d/%d", childLevel, childIndex),
			Region:        region,
		}
		children = append(children, NodeResult{
			Ref:    childRef,
			Hash:   hashCopy(state.Levels[childLevel][childIndex]),
			Region: region,
		})
	}
	return children, nil
}

func recordsFromState(scope Scope, generation string, state IncrementalCommitmentState, ref BucketRef) ([]CanonicalRecord, error) {
	if ref.ScopeID != ScopeIdentity(scope) || ref.Generation != generation {
		return nil, fmt.Errorf("%w: reference commitment does not match requested generation", ErrStaleReference)
	}
	if strings.HasPrefix(ref.Key, "L0/") || ref.Key == rootNodePath {
		level, index, err := parseNodePath(ref.Key, len(state.Levels))
		if err == nil && level == 0 && index < len(state.BucketRecords) {
			return cloneCanonicalRecords(state.BucketRecords[index]), nil
		}
	}
	if !strings.HasPrefix(ref.Key, bucketHeader) {
		return nil, ErrInvalidBucketReference
	}
	for i, bucket := range state.Buckets {
		if bucket.ID.String() == ref.Key {
			return cloneCanonicalRecords(state.BucketRecords[i]), nil
		}
	}
	return nil, ErrBucketNotFound
}

func bucketIDFromState(scope Scope, generation string, state IncrementalCommitmentState, ref NodeRef) (BucketID, error) {
	if ref.ScopeID != ScopeIdentity(scope) || ref.Generation != generation {
		return BucketID{}, fmt.Errorf("%w: reference commitment does not match requested generation", ErrStaleReference)
	}
	level, index, err := parseNodePath(ref.Path, len(state.Levels))
	if err != nil {
		return BucketID{}, err
	}
	if level != 0 || index >= len(state.Buckets) {
		return BucketID{}, ErrBucketNotFound
	}
	return state.Buckets[index].ID, nil
}

func parseNodePath(path string, levelCount int) (int, int, error) {
	if path == rootNodePath {
		if levelCount == 0 {
			return 0, 0, ErrNodeNotFound
		}
		return levelCount - 1, 0, nil
	}
	if !strings.HasPrefix(path, "L") {
		return 0, 0, ErrInvalidNodeReference
	}
	parts := strings.Split(strings.TrimPrefix(path, "L"), "/")
	if len(parts) != 2 {
		return 0, 0, ErrInvalidNodeReference
	}
	level, err := strconv.Atoi(parts[0])
	if err != nil || level < 0 || level >= levelCount {
		return 0, 0, ErrNodeNotFound
	}
	index, err := strconv.Atoi(parts[1])
	if err != nil || index < 0 {
		return 0, 0, ErrInvalidNodeReference
	}
	return level, index, nil
}

func cloneCanonicalRecords(records []CanonicalRecord) []CanonicalRecord {
	return append([]CanonicalRecord(nil), records...)
}

var _ ReconciliationParticipant = (*MemoryParticipant)(nil)
var _ ReconciliationParticipant = (*RepositoryParticipant)(nil)
