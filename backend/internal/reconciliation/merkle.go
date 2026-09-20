package reconciliation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
)

const (
	MerkleAlgorithmVersion = "merkle-v1"
	nodeDomain             = "TXNODE|v1|"
	emptyDomain            = "TXEMPTY|v1"
)

// LeafHashes normalizes and orders records with the frozen M3-1 ordering,
// then hashes each record with the frozen canonical v1 serializer.
func LeafHashes(records []CanonicalRecord) ([][]byte, error) {
	ordered := SortRecords(records)
	hashes := make([][]byte, 0, len(ordered))
	for _, record := range ordered {
		hash, err := LeafHash(record)
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, hashCopy(hash))
	}
	return hashes, nil
}

// InternalNodeHash computes SHA-256("TXNODE|v1|" || left || right).
func InternalNodeHash(left, right []byte) []byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte(nodeDomain))
	_, _ = hash.Write(left)
	_, _ = hash.Write(right)
	return hash.Sum(nil)
}

// EmptyHash computes the explicit empty commitment SHA-256("TXEMPTY|v1").
func EmptyHash() []byte {
	hash := sha256.Sum256([]byte(emptyDomain))
	return hashCopy(hash[:])
}

// BucketRoot computes a root over ordered leaf hashes. An empty bucket uses
// EmptyHash. A one-leaf bucket is that leaf. At each higher level, an odd final
// child is promoted unchanged; leaves are never duplicated or padded.
func BucketRoot(leaves [][]byte) ([]byte, error) {
	if len(leaves) == 0 {
		return EmptyHash(), nil
	}
	level := make([][]byte, len(leaves))
	for i, leaf := range leaves {
		if len(leaf) != sha256.Size {
			return nil, fmt.Errorf("leaf %d has length %d, want %d", i, len(leaf), sha256.Size)
		}
		level[i] = hashCopy(leaf)
	}
	for len(level) > 1 {
		next := make([][]byte, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			if i+1 == len(level) {
				next = append(next, hashCopy(level[i]))
				continue
			}
			next = append(next, InternalNodeHash(level[i], level[i+1]))
		}
		level = next
	}
	return level[0], nil
}

// BucketCommitment is an ordered bucket root plus the metadata required to
// reproduce the commitment. It contains no database or snapshot identifiers.
type BucketCommitment struct {
	ID       BucketID
	Root     []byte
	Records  int
	Metadata CommitmentMetadata
}

// GlobalRoot sorts bucket commitments by BucketID and computes the same
// domain-separated parent tree over their roots. Duplicate bucket identities
// are rejected rather than silently making ordering input-dependent.
func GlobalRoot(buckets []BucketCommitment) ([]byte, error) {
	if len(buckets) == 0 {
		return EmptyHash(), nil
	}
	ordered := append([]BucketCommitment(nil), buckets...)
	sortBucketCommitments(ordered)
	roots := make([][]byte, 0, len(ordered))
	for i, bucket := range ordered {
		if i > 0 && ordered[i-1].ID.Equal(bucket.ID) {
			return nil, fmt.Errorf("duplicate bucket identity %s", bucket.ID)
		}
		if len(bucket.Root) != sha256.Size {
			return nil, fmt.Errorf("bucket %s root has length %d, want %d", bucket.ID, len(bucket.Root), sha256.Size)
		}
		roots = append(roots, bucket.Root)
	}
	return BucketRoot(roots)
}

func hashCopy(value []byte) []byte {
	return append([]byte(nil), value...)
}

// CommitmentMetadata records the algorithm/version and scope needed to
// reproduce a bucket commitment. It is intentionally storage-agnostic.
type CommitmentMetadata struct {
	CanonicalVersion string
	AlgorithmVersion string
	Bucket           BucketID
	Scope            Scope
	Root             []byte
	RecordCount      int
}

// CommitmentStore is the smallest persistence-facing abstraction for M3-2.
// It stores derived commitment metadata; it does not mutate participant ledgers
// or implement incremental maintenance.
type CommitmentStore interface {
	Save(context.Context, CommitmentMetadata) error
	Load(context.Context, BucketID) (CommitmentMetadata, bool, error)
}

// MemoryCommitmentStore is a deterministic in-memory implementation for tests
// and standalone engine development. M3-3 may replace it with incremental
// participant-local persistence without changing the Merkle calculations.
type MemoryCommitmentStore struct {
	mu      sync.RWMutex
	entries map[string]CommitmentMetadata
}

func NewMemoryCommitmentStore() *MemoryCommitmentStore {
	return &MemoryCommitmentStore{entries: make(map[string]CommitmentMetadata)}
}

func (store *MemoryCommitmentStore) Save(ctx context.Context, metadata CommitmentMetadata) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := metadata.Bucket.Validate(); err != nil {
		return err
	}
	if len(metadata.Root) != sha256.Size {
		return errors.New("commitment root must be a SHA-256 digest")
	}
	metadata.Root = hashCopy(metadata.Root)
	store.mu.Lock()
	defer store.mu.Unlock()
	store.entries[metadata.Bucket.String()] = metadata
	return nil
}

func (store *MemoryCommitmentStore) Load(ctx context.Context, bucket BucketID) (CommitmentMetadata, bool, error) {
	if err := ctx.Err(); err != nil {
		return CommitmentMetadata{}, false, err
	}
	if err := bucket.Validate(); err != nil {
		return CommitmentMetadata{}, false, err
	}
	store.mu.RLock()
	metadata, ok := store.entries[bucket.String()]
	store.mu.RUnlock()
	if !ok {
		return CommitmentMetadata{}, false, nil
	}
	metadata.Root = hashCopy(metadata.Root)
	return metadata, true, nil
}

func sortBucketCommitments(buckets []BucketCommitment) {
	for i := range buckets {
		buckets[i].Root = hashCopy(buckets[i].Root)
	}
	sortBucketIDs(buckets)
}

func sortBucketIDs(buckets []BucketCommitment) {
	for i := 1; i < len(buckets); i++ {
		for j := i; j > 0 && buckets[j].ID.Compare(buckets[j-1].ID) < 0; j-- {
			buckets[j], buckets[j-1] = buckets[j-1], buckets[j]
		}
	}
}

// EqualBytes is kept small for callers comparing independently returned roots.
func EqualBytes(left, right []byte) bool { return bytes.Equal(left, right) }
