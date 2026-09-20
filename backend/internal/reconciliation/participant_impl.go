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
	mu            sync.RWMutex
	participantID string
	partition     string
	bucketWidth   time.Duration
	capturedAt    time.Time
	records       []CanonicalRecord
	states        map[string]IncrementalCommitmentState
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
		states:        make(map[string]IncrementalCommitmentState),
	}, nil
}

// Refresh rebuilds one derived commitment from the fixture's logical records.
// Normal reads reuse the last commitment for the scope; callers that replace a
// fixture must refresh explicitly so a stale commitment is never mistaken for
// a newly captured ledger.
func (participant *MemoryParticipant) Refresh(ctx context.Context, scope Scope) error {
	_, err := participant.buildState(ctx, scope)
	return err
}

func (participant *MemoryParticipant) GetRoot(ctx context.Context, scope Scope) (RootResult, error) {
	state, err := participant.state(ctx, scope)
	if err != nil {
		return RootResult{}, err
	}
	return rootResult(participant.participantID, scope, state), nil
}

func (participant *MemoryParticipant) GetChildren(ctx context.Context, ref NodeRef) ([]NodeResult, error) {
	if err := participant.validateParticipant(ref.ParticipantID); err != nil {
		return nil, err
	}
	scope, err := parseScopeIdentity(ref.ScopeID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidNodeReference, err)
	}
	state, err := participant.state(ctx, scope)
	if err != nil {
		return nil, err
	}
	return childrenFromState(participant.participantID, scope, state, ref)
}

func (participant *MemoryParticipant) GetRecords(ctx context.Context, ref BucketRef) ([]CanonicalRecord, error) {
	if err := participant.validateParticipant(ref.ParticipantID); err != nil {
		return nil, err
	}
	scope, err := parseScopeIdentity(ref.ScopeID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidBucketReference, err)
	}
	state, err := participant.state(ctx, scope)
	if err != nil {
		return nil, err
	}
	return recordsFromState(scope, state, ref)
}

func (participant *MemoryParticipant) GetMetadata(ctx context.Context, scope Scope) (ParticipantMetadata, error) {
	state, err := participant.state(ctx, scope)
	if err != nil {
		return ParticipantMetadata{}, err
	}
	return ParticipantMetadata{ParticipantID: participant.participantID, CapturedAt: participant.capturedAt, RecordCount: int64(state.RecordCount)}, nil
}

func (participant *MemoryParticipant) state(ctx context.Context, scope Scope) (IncrementalCommitmentState, error) {
	if err := ctx.Err(); err != nil {
		return IncrementalCommitmentState{}, err
	}
	scope = scope.Normalize()
	if err := scope.Validate(); err != nil {
		return IncrementalCommitmentState{}, err
	}
	key := ScopeIdentity(scope)
	participant.mu.RLock()
	cached, ok := participant.states[key]
	participant.mu.RUnlock()
	if ok {
		return cloneIncrementalState(cached), nil
	}
	return participant.buildState(ctx, scope)
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
	participant.mu.Lock()
	participant.states[ScopeIdentity(scope)] = cloneIncrementalState(state)
	participant.mu.Unlock()
	return state, nil
}

func (participant *MemoryParticipant) validateParticipant(requested string) error {
	if requested != "" && requested != participant.participantID {
		return fmt.Errorf("%w: requested=%q actual=%q", ErrParticipantMismatch, requested, participant.participantID)
	}
	return nil
}

// RepositoryParticipant reads the existing participant ledger through a
// repository/service snapshot source. It does not duplicate money movement or
// write participant state. The source remains authoritative for each read.
type RepositoryParticipant struct {
	mu            sync.RWMutex
	source        LedgerSnapshotSource
	participantID string
	partition     string
	bucketWidth   time.Duration
	snapshots     map[string]repositorySnapshot
}

type repositorySnapshot struct {
	state      IncrementalCommitmentState
	capturedAt time.Time
}

func NewRepositoryParticipant(source LedgerSnapshotSource, participantID, partition string, bucketWidth time.Duration) (*RepositoryParticipant, error) {
	if source == nil {
		return nil, fmt.Errorf("%w: snapshot source is required", ErrInvalidIncrementalConfig)
	}
	if participantID == "" {
		return nil, fmt.Errorf("%w: participant identity is required", ErrParticipantMismatch)
	}
	if partition == "" || bucketWidth <= 0 {
		return nil, fmt.Errorf("%w: partition and positive bucket width are required", ErrInvalidIncrementalConfig)
	}
	return &RepositoryParticipant{source: source, participantID: participantID, partition: partition, bucketWidth: bucketWidth, snapshots: make(map[string]repositorySnapshot)}, nil
}

// Refresh captures the authoritative participant ledger again and replaces
// the derived commitment for the scope. Reads otherwise reuse the captured
// commitment so child and bucket calls observe one coherent snapshot.
func (participant *RepositoryParticipant) Refresh(ctx context.Context, scope Scope) error {
	_, _, _, err := participant.buildState(ctx, scope)
	return err
}

func (participant *RepositoryParticipant) GetRoot(ctx context.Context, scope Scope) (RootResult, error) {
	state, _, normalized, err := participant.state(ctx, scope)
	if err != nil {
		return RootResult{}, err
	}
	return rootResult(participant.participantID, normalized, state), nil
}

func (participant *RepositoryParticipant) GetChildren(ctx context.Context, ref NodeRef) ([]NodeResult, error) {
	if ref.ParticipantID != "" && ref.ParticipantID != participant.participantID {
		return nil, fmt.Errorf("%w: requested=%q actual=%q", ErrParticipantMismatch, ref.ParticipantID, participant.participantID)
	}
	scope, err := parseScopeIdentity(ref.ScopeID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidNodeReference, err)
	}
	state, _, normalized, err := participant.state(ctx, scope)
	if err != nil {
		return nil, err
	}
	return childrenFromState(participant.participantID, normalized, state, ref)
}

func (participant *RepositoryParticipant) GetRecords(ctx context.Context, ref BucketRef) ([]CanonicalRecord, error) {
	if ref.ParticipantID != "" && ref.ParticipantID != participant.participantID {
		return nil, fmt.Errorf("%w: requested=%q actual=%q", ErrParticipantMismatch, ref.ParticipantID, participant.participantID)
	}
	scope, err := parseScopeIdentity(ref.ScopeID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidBucketReference, err)
	}
	state, _, normalized, err := participant.state(ctx, scope)
	if err != nil {
		return nil, err
	}
	return recordsFromState(normalized, state, ref)
}

func (participant *RepositoryParticipant) GetMetadata(ctx context.Context, scope Scope) (ParticipantMetadata, error) {
	state, capturedAt, _, err := participant.state(ctx, scope)
	if err != nil {
		return ParticipantMetadata{}, err
	}
	return ParticipantMetadata{ParticipantID: participant.participantID, CapturedAt: capturedAt.UTC(), RecordCount: int64(state.RecordCount)}, nil
}

func (participant *RepositoryParticipant) state(ctx context.Context, scope Scope) (IncrementalCommitmentState, time.Time, Scope, error) {
	if err := ctx.Err(); err != nil {
		return IncrementalCommitmentState{}, time.Time{}, Scope{}, err
	}
	scope = scope.Normalize()
	if err := scope.Validate(); err != nil {
		return IncrementalCommitmentState{}, time.Time{}, Scope{}, err
	}
	if scope.From.IsZero() || scope.To.IsZero() {
		return IncrementalCommitmentState{}, time.Time{}, Scope{}, fmt.Errorf("%w: repository reads require explicit scope endpoints", ErrInvalidScope)
	}
	key := ScopeIdentity(scope)
	participant.mu.RLock()
	cached, ok := participant.snapshots[key]
	participant.mu.RUnlock()
	if ok {
		return cloneIncrementalState(cached.state), cached.capturedAt, scope, nil
	}
	return participant.buildState(ctx, scope)
}

func (participant *RepositoryParticipant) buildState(ctx context.Context, scope Scope) (IncrementalCommitmentState, time.Time, Scope, error) {
	if err := ctx.Err(); err != nil {
		return IncrementalCommitmentState{}, time.Time{}, Scope{}, err
	}
	scope = scope.Normalize()
	if err := scope.Validate(); err != nil {
		return IncrementalCommitmentState{}, time.Time{}, Scope{}, err
	}
	if scope.From.IsZero() || scope.To.IsZero() {
		return IncrementalCommitmentState{}, time.Time{}, Scope{}, fmt.Errorf("%w: repository reads require explicit scope endpoints", ErrInvalidScope)
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
	participant.mu.Lock()
	participant.snapshots[ScopeIdentity(scope)] = repositorySnapshot{state: cloneIncrementalState(state), capturedAt: snapshot.CapturedAt.UTC()}
	participant.mu.Unlock()
	return state, snapshot.CapturedAt.UTC(), scope, nil
}

func rootResult(participantID string, scope Scope, state IncrementalCommitmentState) RootResult {
	path := emptyNodePath
	if len(state.Levels) > 0 {
		path = fmt.Sprintf("L%d/0", len(state.Levels)-1)
	}
	return RootResult{Root: hashCopy(state.Root), Algorithm: state.AlgorithmVersion, Version: state.CanonicalVersion, Ref: NodeRef{ParticipantID: participantID, ScopeID: ScopeIdentity(scope), Path: path}}
}

func childrenFromState(participantID string, scope Scope, state IncrementalCommitmentState, ref NodeRef) ([]NodeResult, error) {
	if ref.ScopeID != ScopeIdentity(scope) {
		return nil, fmt.Errorf("%w: reference scope does not match requested scope", ErrParticipantMismatch)
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
		children = append(children, NodeResult{Ref: NodeRef{ParticipantID: participantID, ScopeID: ScopeIdentity(scope), Path: fmt.Sprintf("L%d/%d", childLevel, childIndex)}, Hash: hashCopy(state.Levels[childLevel][childIndex])})
	}
	return children, nil
}

func recordsFromState(scope Scope, state IncrementalCommitmentState, ref BucketRef) ([]CanonicalRecord, error) {
	if ref.ScopeID != ScopeIdentity(scope) {
		return nil, fmt.Errorf("%w: reference scope does not match requested scope", ErrParticipantMismatch)
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
