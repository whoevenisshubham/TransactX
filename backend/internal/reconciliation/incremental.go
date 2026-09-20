package reconciliation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidIncrementalConfig  = errors.New("invalid incremental merkle configuration")
	ErrDuplicateRecord           = errors.New("record already exists in incremental ledger")
	ErrBucketOutOfOrder          = errors.New("new bucket is before the append frontier")
	ErrRecordBucketMove          = errors.New("upsert cannot move a record between buckets")
	ErrRecordOutsideScope        = errors.New("record is outside the incremental scope")
	ErrIncrementalConfigMismatch = errors.New("incremental commitment configuration mismatch")
)

// BucketForRecord deterministically maps a record timestamp to a UTC bucket.
// The start is floor(occurred_at_utc / width) * width, including for instants
// before the Unix epoch. Exact boundaries belong to the bucket starting there.
func BucketForRecord(record CanonicalRecord, partition string, width time.Duration) (BucketID, error) {
	if partition == "" || width <= 0 {
		return BucketID{}, fmt.Errorf("%w: partition and positive width are required", ErrInvalidIncrementalConfig)
	}
	widthNanos := int64(width)
	instantNanos := record.OccurredAt.UTC().UnixNano()
	quotient := instantNanos / widthNanos
	if instantNanos%widthNanos < 0 {
		quotient--
	}
	startNanos := quotient * widthNanos
	return NewBucketID(partition, time.Unix(0, startNanos).UTC(), width)
}

// IncrementalUpdate describes one normal append/upsert. Records considered is
// bucket-local; no unrelated ledger records are scanned by the update path.
type IncrementalUpdate struct {
	Bucket                  BucketID
	BucketsRecomputed       int
	AncestorNodesRecomputed int
	BucketsReused           int
	TotalRecordsConsidered  int
	ResultingRoot           []byte
}

// IncrementalRebuildResult describes an explicitly requested bootstrap or
// recovery rebuild. It is never returned by AppendRecord or UpsertRecord.
type IncrementalRebuildResult struct {
	BucketsRecomputed       int
	AncestorNodesRecomputed int
	TotalRecordsConsidered  int
	ResultingRoot           []byte
}

type recordIdentity struct {
	OperationID uuid.UUID
	PaymentID   uuid.UUID
	AccountID   uuid.UUID
	EntryType   string
}

func identityForRecord(record CanonicalRecord) recordIdentity {
	return recordIdentity{
		OperationID: record.OperationID,
		PaymentID:   record.PaymentID,
		AccountID:   record.AccountID,
		EntryType:   record.EntryType,
	}
}

type incrementalBucket struct {
	ID      BucketID
	Records []CanonicalRecord
	Root    []byte
}

// IncrementalMerkleLedger maintains derived commitments independently from an
// authoritative participant ledger. New buckets must arrive at the append
// frontier; existing buckets may receive same-bucket upserts.
type IncrementalMerkleLedger struct {
	mu           sync.RWMutex
	operationMu  sync.Mutex
	partition    string
	bucketWidth  time.Duration
	scope        Scope
	buckets      []incrementalBucket
	bucketIndex  map[string]int
	records      map[recordIdentity]string
	levels       [][][]byte
	totalRecords int
	fullRebuilds uint64
	// beforeBootstrapCommit is a package-local synchronization hook used by
	// deterministic concurrency tests; normal callers leave it nil.
	beforeBootstrapCommit func()
}

func NewIncrementalMerkleLedger(partition string, width time.Duration, scope Scope) (*IncrementalMerkleLedger, error) {
	if partition == "" || width <= 0 {
		return nil, fmt.Errorf("%w: partition and positive width are required", ErrInvalidIncrementalConfig)
	}
	scope = normalizeScope(scope)
	if err := validateScope(scope); err != nil {
		return nil, err
	}
	return &IncrementalMerkleLedger{
		partition:   partition,
		bucketWidth: width,
		scope:       scope,
		bucketIndex: make(map[string]int),
		records:     make(map[recordIdentity]string),
	}, nil
}

// AppendRecord adds a new logical record. Duplicate logical identities are
// rejected; use UpsertRecord for an in-place same-bucket update.
func (ledger *IncrementalMerkleLedger) AppendRecord(ctx context.Context, record CanonicalRecord) (IncrementalUpdate, error) {
	return ledger.applyRecord(ctx, record, false)
}

// UpsertRecord replaces an existing logical record in the same bucket or
// appends it as a new record. Moving an existing record to another bucket is
// rejected because it would require a non-local ordered-bucket insertion.
func (ledger *IncrementalMerkleLedger) UpsertRecord(ctx context.Context, record CanonicalRecord) (IncrementalUpdate, error) {
	return ledger.applyRecord(ctx, record, true)
}

func (ledger *IncrementalMerkleLedger) applyRecord(ctx context.Context, record CanonicalRecord, upsert bool) (IncrementalUpdate, error) {
	ledger.operationMu.Lock()
	defer ledger.operationMu.Unlock()
	if err := ctx.Err(); err != nil {
		return IncrementalUpdate{}, err
	}
	record = record.Normalize()
	if !scopeContains(ledger.scope, record.OccurredAt) {
		return IncrementalUpdate{}, fmt.Errorf("%w: %s", ErrRecordOutsideScope, record.OccurredAt.Format(time.RFC3339Nano))
	}
	bucketID, err := BucketForRecord(record, ledger.partition, ledger.bucketWidth)
	if err != nil {
		return IncrementalUpdate{}, err
	}
	identity := identityForRecord(record)

	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return IncrementalUpdate{}, err
	}

	oldBucketKey, exists := ledger.records[identity]
	if exists && !upsert {
		return IncrementalUpdate{}, ErrDuplicateRecord
	}
	if exists && oldBucketKey != bucketID.String() {
		return IncrementalUpdate{}, ErrRecordBucketMove
	}

	bucketIndex, bucketExists := ledger.bucketIndex[bucketID.String()]
	if !bucketExists {
		if len(ledger.buckets) > 0 && bucketID.Compare(ledger.buckets[len(ledger.buckets)-1].ID) <= 0 {
			return IncrementalUpdate{}, fmt.Errorf("%w: %s", ErrBucketOutOfOrder, bucketID)
		}
		bucketIndex = len(ledger.buckets)
	}

	var candidate []CanonicalRecord
	if bucketExists {
		candidate = append([]CanonicalRecord(nil), ledger.buckets[bucketIndex].Records...)
		if exists {
			found := false
			for i := range candidate {
				if identityForRecord(candidate[i]) == identity {
					candidate[i] = record
					found = true
					break
				}
			}
			if !found {
				return IncrementalUpdate{}, errors.New("incremental record index is inconsistent")
			}
		} else {
			candidate = append(candidate, record)
		}
	} else {
		candidate = []CanonicalRecord{record}
	}
	ordered, root, err := orderedBucketCommitment(candidate)
	if err != nil {
		return IncrementalUpdate{}, err
	}

	if !bucketExists {
		ledger.buckets = append(ledger.buckets, incrementalBucket{ID: bucketID, Records: ordered, Root: root})
		ledger.bucketIndex[bucketID.String()] = bucketIndex
		ledger.levels = appendLevelLeaf(ledger.levels, root)
		ledger.totalRecords++
	} else {
		ledger.buckets[bucketIndex].Records = ordered
		ledger.buckets[bucketIndex].Root = root
		ledger.levels[0][bucketIndex] = hashCopy(root)
		if !exists {
			ledger.totalRecords++
		}
	}
	ledger.records[identity] = bucketID.String()
	ancestors := ledger.updateAncestorPath(bucketIndex)
	return IncrementalUpdate{
		Bucket:                  bucketID,
		BucketsRecomputed:       1,
		AncestorNodesRecomputed: ancestors,
		BucketsReused:           len(ledger.buckets) - 1,
		TotalRecordsConsidered:  len(ordered),
		ResultingRoot:           ledger.rootLocked(),
	}, nil
}

// Bootstrap rebuilds all derived state from a complete logical record set.
// It is explicitly separated from normal incremental operations for initial
// population and recovery only.
func (ledger *IncrementalMerkleLedger) Bootstrap(ctx context.Context, records []CanonicalRecord) (IncrementalRebuildResult, error) {
	ledger.operationMu.Lock()
	defer ledger.operationMu.Unlock()
	if err := ctx.Err(); err != nil {
		return IncrementalRebuildResult{}, err
	}
	type groupedBucket struct {
		id      BucketID
		records []CanonicalRecord
	}
	grouped := make(map[string]*groupedBucket)
	identities := make(map[recordIdentity]string)
	for _, raw := range records {
		if err := ctx.Err(); err != nil {
			return IncrementalRebuildResult{}, err
		}
		record := raw.Normalize()
		if !scopeContains(ledger.scope, record.OccurredAt) {
			return IncrementalRebuildResult{}, fmt.Errorf("%w: %s", ErrRecordOutsideScope, record.OccurredAt.Format(time.RFC3339Nano))
		}
		bucketID, err := BucketForRecord(record, ledger.partition, ledger.bucketWidth)
		if err != nil {
			return IncrementalRebuildResult{}, err
		}
		identity := identityForRecord(record)
		if previous, exists := identities[identity]; exists {
			return IncrementalRebuildResult{}, fmt.Errorf("%w: %s and %s", ErrDuplicateRecord, previous, bucketID)
		}
		identities[identity] = bucketID.String()
		key := bucketID.String()
		if grouped[key] == nil {
			grouped[key] = &groupedBucket{id: bucketID}
		}
		grouped[key].records = append(grouped[key].records, record)
	}

	orderedGroups := make([]*groupedBucket, 0, len(grouped))
	for _, group := range grouped {
		orderedGroups = append(orderedGroups, group)
	}
	sort.Slice(orderedGroups, func(i, j int) bool { return orderedGroups[i].id.Compare(orderedGroups[j].id) < 0 })

	newBuckets := make([]incrementalBucket, 0, len(orderedGroups))
	newIndex := make(map[string]int, len(orderedGroups))
	roots := make([][]byte, 0, len(orderedGroups))
	for i, group := range orderedGroups {
		orderedRecords, root, err := orderedBucketCommitment(group.records)
		if err != nil {
			return IncrementalRebuildResult{}, err
		}
		newBuckets = append(newBuckets, incrementalBucket{ID: group.id, Records: orderedRecords, Root: root})
		newIndex[group.id.String()] = i
		roots = append(roots, root)
	}
	newLevels := buildLevels(roots)

	if ledger.beforeBootstrapCommit != nil {
		ledger.beforeBootstrapCommit()
	}
	ledger.mu.Lock()
	ledger.buckets = newBuckets
	ledger.bucketIndex = newIndex
	ledger.records = identities
	ledger.levels = newLevels
	ledger.totalRecords = len(records)
	ledger.fullRebuilds++
	result := IncrementalRebuildResult{
		BucketsRecomputed:       len(newBuckets),
		AncestorNodesRecomputed: countInternalHashes(newLevels),
		TotalRecordsConsidered:  len(records),
		ResultingRoot:           ledger.rootLocked(),
	}
	ledger.mu.Unlock()
	return result, nil
}

// NewIncrementalMerkleLedgerFromState creates a ledger configured from and
// restored from persisted derived state. It does not call Bootstrap.
func NewIncrementalMerkleLedgerFromState(partition string, width time.Duration, state IncrementalCommitmentState) (*IncrementalMerkleLedger, error) {
	ledger, err := NewIncrementalMerkleLedger(partition, width, state.Scope)
	if err != nil {
		return nil, err
	}
	if err := ledger.Restore(state); err != nil {
		return nil, err
	}
	return ledger, nil
}

// Restore imports validated derived state and reconstructs only commitment
// indexes. It never reconstructs or mutates authoritative participant state.
func (ledger *IncrementalMerkleLedger) Restore(state IncrementalCommitmentState) error {
	ledger.operationMu.Lock()
	defer ledger.operationMu.Unlock()
	if err := validateIncrementalState(state); err != nil {
		return err
	}
	if state.Partition != ledger.partition || state.BucketWidth != ledger.bucketWidth || !scopeEqual(state.Scope, ledger.scope) {
		return fmt.Errorf("%w: ledger partition=%q width=%s scope=%s..%s, state partition=%q width=%s scope=%s..%s", ErrIncrementalConfigMismatch, ledger.partition, ledger.bucketWidth, ledger.scope.From, ledger.scope.To, state.Partition, state.BucketWidth, state.Scope.From, state.Scope.To)
	}

	buckets := make([]incrementalBucket, len(state.Buckets))
	bucketIndex := make(map[string]int, len(state.Buckets))
	records := make(map[recordIdentity]string, state.RecordCount)
	for i, commitment := range state.Buckets {
		bucketRecords := append([]CanonicalRecord(nil), state.BucketRecords[i]...)
		buckets[i] = incrementalBucket{ID: commitment.ID, Records: bucketRecords, Root: hashCopy(commitment.Root)}
		bucketIndex[commitment.ID.String()] = i
		for _, record := range bucketRecords {
			identity := identityForRecord(record)
			if _, exists := records[identity]; exists {
				return fmt.Errorf("%w: duplicate restored record identity", ErrDuplicateRecord)
			}
			records[identity] = commitment.ID.String()
		}
	}

	ledger.mu.Lock()
	ledger.buckets = buckets
	ledger.bucketIndex = bucketIndex
	ledger.records = records
	ledger.levels = cloneLevels(state.Levels)
	ledger.totalRecords = state.RecordCount
	ledger.fullRebuilds = state.RebuildCount
	ledger.mu.Unlock()
	return nil
}

// FullRebuildCount is a testable proof that normal updates do not call
// Bootstrap internally.
func (ledger *IncrementalMerkleLedger) FullRebuildCount() uint64 {
	ledger.mu.RLock()
	defer ledger.mu.RUnlock()
	return ledger.fullRebuilds
}

// Root returns the current derived global root.
func (ledger *IncrementalMerkleLedger) Root() []byte {
	ledger.mu.RLock()
	defer ledger.mu.RUnlock()
	return ledger.rootLocked()
}

// Snapshot returns persistence-ready derived state, including ordered bucket
// identities, bucket roots, global root, scope, versions, and record count.
func (ledger *IncrementalMerkleLedger) Snapshot() IncrementalCommitmentState {
	ledger.mu.RLock()
	defer ledger.mu.RUnlock()
	return ledger.snapshotLocked()
}

// Persist writes only derived commitment state to the supplied store. It does
// not mutate or claim authority over participant ledger records.
func (ledger *IncrementalMerkleLedger) Persist(ctx context.Context, store IncrementalCommitmentStore) error {
	if store == nil {
		return errors.New("incremental commitment store is required")
	}
	return store.SaveState(ctx, ledger.Snapshot())
}

func (ledger *IncrementalMerkleLedger) rootLocked() []byte {
	if len(ledger.levels) == 0 {
		return EmptyHash()
	}
	return hashCopy(ledger.levels[len(ledger.levels)-1][0])
}

func (ledger *IncrementalMerkleLedger) snapshotLocked() IncrementalCommitmentState {
	buckets := make([]BucketCommitment, 0, len(ledger.buckets))
	for _, bucket := range ledger.buckets {
		root := hashCopy(bucket.Root)
		buckets = append(buckets, BucketCommitment{
			ID:      bucket.ID,
			Root:    root,
			Records: len(bucket.Records),
			Metadata: CommitmentMetadata{
				CanonicalVersion: CanonicalVersion,
				AlgorithmVersion: MerkleAlgorithmVersion,
				Bucket:           bucket.ID,
				Scope:            ledger.scope,
				Root:             hashCopy(root),
				RecordCount:      len(bucket.Records),
			},
		})
	}
	return IncrementalCommitmentState{
		Partition:        ledger.partition,
		BucketWidth:      ledger.bucketWidth,
		CanonicalVersion: CanonicalVersion,
		AlgorithmVersion: MerkleAlgorithmVersion,
		Scope:            ledger.scope,
		Buckets:          buckets,
		BucketRecords:    cloneBucketRecords(ledger.buckets),
		Levels:           cloneLevels(ledger.levels),
		Root:             ledger.rootLocked(),
		RecordCount:      ledger.totalRecords,
		RebuildCount:     ledger.fullRebuilds,
	}
}

func cloneBucketRecords(buckets []incrementalBucket) [][]CanonicalRecord {
	clone := make([][]CanonicalRecord, len(buckets))
	for i, bucket := range buckets {
		clone[i] = append([]CanonicalRecord(nil), bucket.Records...)
	}
	return clone
}

func cloneLevels(levels [][][]byte) [][][]byte {
	clone := make([][][]byte, len(levels))
	for level, nodes := range levels {
		clone[level] = make([][]byte, len(nodes))
		for i, node := range nodes {
			clone[level][i] = hashCopy(node)
		}
	}
	return clone
}

func orderedBucketCommitment(records []CanonicalRecord) ([]CanonicalRecord, []byte, error) {
	ordered := SortRecords(records)
	leaves, err := LeafHashes(ordered)
	if err != nil {
		return nil, nil, err
	}
	root, err := BucketRoot(leaves)
	if err != nil {
		return nil, nil, err
	}
	return ordered, root, nil
}

func appendLevelLeaf(levels [][][]byte, root []byte) [][][]byte {
	if len(levels) == 0 {
		return [][][]byte{{hashCopy(root)}}
	}
	levels[0] = append(levels[0], hashCopy(root))
	return levels
}

// updateAncestorPath updates only parents reachable from bucketIndex. Odd
// final children are promoted without hashing, exactly as M3-2 defines.
func (ledger *IncrementalMerkleLedger) updateAncestorPath(bucketIndex int) int {
	ancestors := 0
	currentIndex := bucketIndex
	for level := 0; level < len(ledger.levels); level++ {
		current := ledger.levels[level]
		if len(current) <= 1 {
			break
		}
		parentIndex := currentIndex / 2
		if level+1 >= len(ledger.levels) {
			ledger.levels = append(ledger.levels, make([][]byte, 0, parentIndex+1))
		}
		next := ledger.levels[level+1]
		for len(next) <= parentIndex {
			next = append(next, nil)
		}
		if currentIndex == len(current)-1 && len(current)%2 == 1 {
			next[parentIndex] = hashCopy(current[currentIndex])
		} else {
			leftIndex := parentIndex * 2
			next[parentIndex] = InternalNodeHash(current[leftIndex], current[leftIndex+1])
			ancestors++
		}
		ledger.levels[level+1] = next
		currentIndex = parentIndex
	}
	return ancestors
}

func buildLevels(roots [][]byte) [][][]byte {
	if len(roots) == 0 {
		return nil
	}
	levels := [][][]byte{make([][]byte, len(roots))}
	for i, root := range roots {
		levels[0][i] = hashCopy(root)
	}
	for len(levels[len(levels)-1]) > 1 {
		current := levels[len(levels)-1]
		next := make([][]byte, 0, (len(current)+1)/2)
		for i := 0; i < len(current); i += 2 {
			if i+1 == len(current) {
				next = append(next, hashCopy(current[i]))
			} else {
				next = append(next, InternalNodeHash(current[i], current[i+1]))
			}
		}
		levels = append(levels, next)
	}
	return levels
}

func countInternalHashes(levels [][][]byte) int {
	count := 0
	for level := 0; level+1 < len(levels); level++ {
		count += len(levels[level]) / 2
	}
	return count
}

func normalizeScope(scope Scope) Scope {
	scope.From = scope.From.UTC()
	scope.To = scope.To.UTC()
	return scope
}

func validateScope(scope Scope) error {
	if !scope.From.IsZero() && !scope.To.IsZero() && !scope.To.After(scope.From) {
		return fmt.Errorf("%w: scope must be half-open with To after From", ErrInvalidIncrementalConfig)
	}
	return nil
}

func scopeContains(scope Scope, instant time.Time) bool {
	instant = instant.UTC()
	if !scope.From.IsZero() && instant.Before(scope.From) {
		return false
	}
	if !scope.To.IsZero() && !instant.Before(scope.To) {
		return false
	}
	return true
}

// IncrementalCommitmentState is the persistence-facing derived state. It
// contains no authoritative participant-ledger mutations.
type IncrementalCommitmentState struct {
	Partition        string
	BucketWidth      time.Duration
	CanonicalVersion string
	AlgorithmVersion string
	Scope            Scope
	// CapturedAt is derived snapshot metadata. It is never part of canonical
	// record bytes or any Merkle hash input.
	CapturedAt    time.Time
	Buckets       []BucketCommitment
	BucketRecords [][]CanonicalRecord
	Levels        [][][]byte
	Root          []byte
	RecordCount   int
	RebuildCount  uint64
}

// IncrementalCommitmentStore persists the global and bucket-level derived
// state independently from participant ledger storage.
type IncrementalCommitmentStore interface {
	SaveState(context.Context, IncrementalCommitmentState) error
	LoadState(context.Context, string, time.Duration, Scope) (IncrementalCommitmentState, bool, error)
}

// MemoryIncrementalCommitmentStore is a concurrency-safe development store.
type MemoryIncrementalCommitmentStore struct {
	mu      sync.RWMutex
	entries map[string]IncrementalCommitmentState
}

func NewMemoryIncrementalCommitmentStore() *MemoryIncrementalCommitmentStore {
	return &MemoryIncrementalCommitmentStore{entries: make(map[string]IncrementalCommitmentState)}
}

func (store *MemoryIncrementalCommitmentStore) SaveState(ctx context.Context, state IncrementalCommitmentState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateIncrementalState(state); err != nil {
		return err
	}
	state = cloneIncrementalState(state)
	store.mu.Lock()
	if store.entries == nil {
		store.entries = make(map[string]IncrementalCommitmentState)
	}
	store.entries[commitmentStateKey(state.Partition, state.BucketWidth, state.Scope)] = state
	store.mu.Unlock()
	return nil
}

func (store *MemoryIncrementalCommitmentStore) LoadState(ctx context.Context, partition string, width time.Duration, scope Scope) (IncrementalCommitmentState, bool, error) {
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
	store.mu.RLock()
	state, ok := store.entries[commitmentStateKey(partition, width, scope)]
	store.mu.RUnlock()
	if !ok {
		return IncrementalCommitmentState{}, false, nil
	}
	if err := validateIncrementalState(state); err != nil {
		return IncrementalCommitmentState{}, false, err
	}
	if state.Partition != partition || state.BucketWidth != width || !scopeEqual(state.Scope, scope) {
		return IncrementalCommitmentState{}, false, fmt.Errorf("%w: requested partition=%q width=%s, stored partition=%q width=%s", ErrIncrementalConfigMismatch, partition, width, state.Partition, state.BucketWidth)
	}
	return cloneIncrementalState(state), true, nil
}

func validateIncrementalState(state IncrementalCommitmentState) error {
	if state.Partition == "" || state.BucketWidth <= 0 {
		return fmt.Errorf("%w: persisted partition and positive bucket width are required", ErrInvalidIncrementalConfig)
	}
	if err := ValidateCommitmentVersions(state.CanonicalVersion, state.AlgorithmVersion); err != nil {
		return err
	}
	state.Scope = normalizeScope(state.Scope)
	if err := validateScope(state.Scope); err != nil {
		return err
	}
	if len(state.Root) != sha256.Size {
		return errors.New("incremental commitment root must be a SHA-256 digest")
	}
	if len(state.BucketRecords) != len(state.Buckets) {
		return errors.New("incremental bucket records do not match buckets")
	}
	if len(state.Buckets) == 0 {
		if len(state.Levels) != 0 {
			return errors.New("empty incremental commitment must not contain tree levels")
		}
		if !EqualBytes(state.Root, EmptyHash()) {
			return errors.New("empty incremental commitment must use the empty root")
		}
	} else {
		if len(state.Levels) == 0 || len(state.Levels[0]) != len(state.Buckets) {
			return errors.New("incremental commitment levels do not match buckets")
		}
		for level, nodes := range state.Levels {
			if len(nodes) == 0 {
				return errors.New("incremental commitment contains an empty tree level")
			}
			for _, node := range nodes {
				if len(node) != sha256.Size {
					return errors.New("incremental commitment tree node must be a SHA-256 digest")
				}
			}
			if level+1 < len(state.Levels) && len(state.Levels[level+1]) != (len(nodes)+1)/2 {
				return errors.New("incremental commitment levels have invalid widths")
			}
		}
		for i, bucket := range state.Buckets {
			if !EqualBytes(state.Levels[0][i], bucket.Root) {
				return fmt.Errorf("incremental leaf level does not match bucket %s", bucket.ID)
			}
		}
		for level := 0; level+1 < len(state.Levels); level++ {
			current := state.Levels[level]
			next := state.Levels[level+1]
			for i := 0; i < len(current); i += 2 {
				parent := i / 2
				var want []byte
				if i+1 == len(current) {
					want = current[i]
				} else {
					want = InternalNodeHash(current[i], current[i+1])
				}
				if !EqualBytes(next[parent], want) {
					return fmt.Errorf("incremental ancestor level %d is inconsistent at node %d", level+1, parent)
				}
			}
		}
		if !EqualBytes(state.Levels[len(state.Levels)-1][0], state.Root) {
			return errors.New("incremental root does not match final tree level")
		}
	}
	previousCount := 0
	identities := make(map[recordIdentity]struct{}, state.RecordCount)
	for i, bucket := range state.Buckets {
		if err := bucket.ID.Validate(); err != nil {
			return err
		}
		if bucket.ID.Partition != state.Partition || bucket.ID.Width != state.BucketWidth {
			return fmt.Errorf("bucket %s does not match persisted partition/width", bucket.ID)
		}
		if len(bucket.Root) != sha256.Size || bucket.Records < 0 {
			return fmt.Errorf("invalid commitment bucket %s", bucket.ID)
		}
		if i > 0 && state.Buckets[i-1].ID.Compare(bucket.ID) >= 0 {
			return errors.New("incremental commitment buckets are not strictly ordered")
		}
		if err := ValidateCommitmentMetadata(bucket.Metadata); err != nil {
			return err
		}
		if !bucket.Metadata.Bucket.Equal(bucket.ID) || !scopeEqual(bucket.Metadata.Scope, state.Scope) || !EqualBytes(bucket.Metadata.Root, bucket.Root) || bucket.Metadata.RecordCount != bucket.Records {
			return fmt.Errorf("bucket metadata does not match commitment %s", bucket.ID)
		}
		bucketRecords := state.BucketRecords[i]
		if len(bucketRecords) != bucket.Records {
			return fmt.Errorf("bucket %s record count does not match persisted records", bucket.ID)
		}
		for recordIndex, record := range bucketRecords {
			identity := identityForRecord(record)
			if _, exists := identities[identity]; exists {
				return fmt.Errorf("duplicate persisted record identity in bucket %s", bucket.ID)
			}
			identities[identity] = struct{}{}
			if !scopeContains(state.Scope, record.OccurredAt) {
				return fmt.Errorf("bucket %s contains a record outside scope", bucket.ID)
			}
			mapped, err := BucketForRecord(record, state.Partition, state.BucketWidth)
			if err != nil || !mapped.Equal(bucket.ID) {
				return fmt.Errorf("bucket %s record %d maps to a different bucket", bucket.ID, recordIndex)
			}
			if recordIndex > 0 {
				orderedPair := SortRecords([]CanonicalRecord{bucketRecords[recordIndex-1], record})
				if !bytesEqualCanonical(orderedPair[0], bucketRecords[recordIndex-1]) {
					return fmt.Errorf("bucket %s records are not canonically ordered", bucket.ID)
				}
			}
		}
		_, expectedBucketRoot, err := orderedBucketCommitment(bucketRecords)
		if err != nil || !EqualBytes(expectedBucketRoot, bucket.Root) {
			return fmt.Errorf("bucket %s root does not match persisted records", bucket.ID)
		}
		previousCount += bucket.Records
	}
	if previousCount != state.RecordCount {
		return fmt.Errorf("record count %d does not match buckets %d", state.RecordCount, previousCount)
	}
	return nil
}

func cloneIncrementalState(state IncrementalCommitmentState) IncrementalCommitmentState {
	clone := state
	clone.Scope = normalizeScope(state.Scope)
	clone.BucketRecords = make([][]CanonicalRecord, len(state.BucketRecords))
	for i, records := range state.BucketRecords {
		clone.BucketRecords[i] = append([]CanonicalRecord(nil), records...)
	}
	clone.Root = hashCopy(state.Root)
	clone.Levels = make([][][]byte, len(state.Levels))
	for level, nodes := range state.Levels {
		clone.Levels[level] = make([][]byte, len(nodes))
		for i, node := range nodes {
			clone.Levels[level][i] = hashCopy(node)
		}
	}
	clone.Buckets = make([]BucketCommitment, len(state.Buckets))
	for i, bucket := range state.Buckets {
		clone.Buckets[i] = bucket
		clone.Buckets[i].Root = hashCopy(bucket.Root)
		clone.Buckets[i].Metadata.Root = hashCopy(bucket.Metadata.Root)
	}
	return clone
}

func scopeEqual(left, right Scope) bool {
	return left.From.UTC().Equal(right.From.UTC()) && left.To.UTC().Equal(right.To.UTC())
}

func bytesEqualCanonical(left, right CanonicalRecord) bool {
	leftBytes, leftErr := CanonicalBytes(left)
	rightBytes, rightErr := CanonicalBytes(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftBytes, rightBytes)
}

func scopeKey(scope Scope) string {
	scope = normalizeScope(scope)
	return scope.From.Format(time.RFC3339Nano) + "|" + scope.To.Format(time.RFC3339Nano)
}

func commitmentStateKey(partition string, width time.Duration, scope Scope) string {
	return strconv.Itoa(len(partition)) + ":" + partition + "|" + strconv.FormatInt(int64(width), 10) + "|" + scopeKey(scope)
}
