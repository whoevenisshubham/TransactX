package reconciliation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLeafHashesEmptyOneAndMultiple(t *testing.T) {
	empty, err := LeafHashes(nil)
	if err != nil {
		t.Fatal(err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty leaves = %#v", empty)
	}
	records := testRecords(3)
	one, err := LeafHashes(records[:1])
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 || len(one[0]) != sha256.Size {
		t.Fatalf("one leaf result = %#v", one)
	}
	multiple, err := LeafHashes([]CanonicalRecord{records[2], records[0], records[1]})
	if err != nil {
		t.Fatal(err)
	}
	if len(multiple) != 3 || !EqualBytes(multiple[0], one[0]) {
		// records[0] is the earliest record and therefore first after sorting.
		t.Fatalf("multiple leaves were not sorted canonically: %x", multiple)
	}
}

func TestLeafMutationAndRepeatability(t *testing.T) {
	base, err := LeafHashes(testRecords(2))
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := LeafHashes(testRecords(2))
	if err != nil {
		t.Fatal(err)
	}
	if len(base) != len(repeat) || !EqualBytes(base[0], repeat[0]) || !EqualBytes(base[1], repeat[1]) {
		t.Fatal("identical logical inputs produced different leaves")
	}
	mutated := testRecords(2)
	mutated[0].AmountPaise++
	changed, err := LeafHashes(mutated)
	if err != nil {
		t.Fatal(err)
	}
	if EqualBytes(base[0], changed[0]) {
		t.Fatal("logical mutation did not change leaf")
	}
}

func TestBucketRootEmptyOneTwoOddAndSeveral(t *testing.T) {
	leaves := testLeaves(5)
	empty, err := BucketRoot(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !EqualBytes(empty, EmptyHash()) {
		t.Fatal("empty bucket does not use empty commitment")
	}
	for count := 1; count <= 5; count++ {
		root, err := BucketRoot(leaves[:count])
		if err != nil {
			t.Fatalf("count %d: %v", count, err)
		}
		if len(root) != sha256.Size {
			t.Fatalf("count %d root length = %d", count, len(root))
		}
	}
	three, err := BucketRoot(leaves[:3])
	if err != nil {
		t.Fatal(err)
	}
	wantOdd := InternalNodeHash(InternalNodeHash(leaves[0], leaves[1]), leaves[2])
	if !EqualBytes(three, wantOdd) {
		t.Fatalf("odd promotion root = %x, want %x", three, wantOdd)
	}
}

func TestBucketRootOrderSensitivityAndDeterminism(t *testing.T) {
	leaves := testLeaves(4)
	forward, err := BucketRoot(leaves)
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := BucketRoot(leaves)
	if err != nil {
		t.Fatal(err)
	}
	if !EqualBytes(forward, repeat) {
		t.Fatal("same ordered leaves produced different bucket roots")
	}
	reversed := append([][]byte(nil), leaves...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	backward, err := BucketRoot(reversed)
	if err != nil {
		t.Fatal(err)
	}
	if EqualBytes(forward, backward) {
		t.Fatal("reordering leaves did not change bucket root")
	}
}

func TestGlobalRootEmptyOneTwoOddAndMultipleBuckets(t *testing.T) {
	empty, err := GlobalRoot(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !EqualBytes(empty, EmptyHash()) {
		t.Fatal("empty bucket set does not use empty commitment")
	}
	buckets := testBuckets(5)
	for count := 1; count <= 5; count++ {
		root, err := GlobalRoot(buckets[:count])
		if err != nil {
			t.Fatalf("bucket count %d: %v", count, err)
		}
		if len(root) != sha256.Size {
			t.Fatalf("bucket count %d root length = %d", count, len(root))
		}
	}
}

func TestGlobalRootSortsBucketsAndChangedBucketAffectsAncestors(t *testing.T) {
	buckets := testBuckets(3)
	forward, err := GlobalRoot(buckets)
	if err != nil {
		t.Fatal(err)
	}
	shuffled := []BucketCommitment{buckets[2], buckets[0], buckets[1]}
	shuffledRoot, err := GlobalRoot(shuffled)
	if err != nil {
		t.Fatal(err)
	}
	if !EqualBytes(forward, shuffledRoot) {
		t.Fatal("bucket input order changed global root")
	}
	mutated := append([]BucketCommitment(nil), buckets...)
	mutated[1].Root = EmptyHash()
	mutatedRoot, err := GlobalRoot(mutated)
	if err != nil {
		t.Fatal(err)
	}
	if EqualBytes(forward, mutatedRoot) {
		t.Fatal("changed bucket did not change global ancestor root")
	}
	if !EqualBytes(buckets[0].Root, mutated[0].Root) || !EqualBytes(buckets[2].Root, mutated[2].Root) {
		t.Fatal("unrelated bucket roots changed")
	}
}

func TestDomainSeparatedLeafNodeAndEmptyCommitments(t *testing.T) {
	record := testRecords(1)[0]
	leaf, err := LeafHash(record)
	if err != nil {
		t.Fatal(err)
	}
	left, right := testLeaves(2)[0], testLeaves(2)[1]
	node := InternalNodeHash(left, right)
	empty := EmptyHash()
	if EqualBytes(leaf, node) || EqualBytes(leaf, empty) || EqualBytes(node, empty) {
		t.Fatalf("domain-separated commitments collided: leaf=%x node=%x empty=%x", leaf, node, empty)
	}
}

func TestEmptyHashGoldenVector(t *testing.T) {
	const want = "8f8a3f35a5a6d7bab4457c0662f408255cb33929f5fcd48cda835f26d98328e7"
	if got := hex.EncodeToString(EmptyHash()); got != want {
		t.Fatalf("empty hash = %s, want %s", got, want)
	}
}

func TestInternalNodeHashGoldenVector(t *testing.T) {
	left := make([]byte, sha256.Size)
	right := make([]byte, sha256.Size)
	for i := range left {
		left[i] = byte(i)
		right[i] = byte(i + sha256.Size)
	}
	const want = "eb29568f3be5dc67c859055f8fcd76b89c38ecf912c9ad75980e10222b18c0e2"
	if got := hex.EncodeToString(InternalNodeHash(left, right)); got != want {
		t.Fatalf("internal node hash = %s, want %s", got, want)
	}
}

func TestSmallDeterministicDatasetCoverage(t *testing.T) {
	for count := 0; count <= 10; count++ {
		records := testRecords(count)
		first, err := LeafHashes(records)
		if err != nil {
			t.Fatalf("leaves %d: %v", count, err)
		}
		second, err := LeafHashes(records)
		if err != nil {
			t.Fatalf("repeat leaves %d: %v", count, err)
		}
		if len(first) != len(second) {
			t.Fatalf("leaf count %d changed on repeat", count)
		}
		for i := range first {
			if !EqualBytes(first[i], second[i]) {
				t.Fatalf("leaf %d changed on repeat for count %d", i, count)
			}
		}
		root, err := BucketRoot(first)
		if err != nil {
			t.Fatalf("bucket %d: %v", count, err)
		}
		if len(root) != sha256.Size {
			t.Fatalf("bucket %d root length = %d", count, len(root))
		}
	}
	for count := 0; count <= 10; count++ {
		buckets := testBuckets(count)
		first, err := GlobalRoot(buckets)
		if err != nil {
			t.Fatalf("global %d: %v", count, err)
		}
		second, err := GlobalRoot(buckets)
		if err != nil || !EqualBytes(first, second) {
			t.Fatalf("global root %d is not repeatable: %v", count, err)
		}
	}
}

func TestCommitmentMetadataStoreRoundTrip(t *testing.T) {
	bucket := testBuckets(1)[0]
	store := NewMemoryCommitmentStore()
	metadata := CommitmentMetadata{
		CanonicalVersion: CanonicalVersion,
		AlgorithmVersion: MerkleAlgorithmVersion,
		Bucket:           bucket.ID,
		Scope:            Scope{From: bucket.ID.Start, To: bucket.ID.Start.Add(bucket.ID.Width)},
		Root:             bucket.Root,
		RecordCount:      bucket.Records,
	}
	if err := store.Save(context.Background(), metadata); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := store.Load(context.Background(), bucket.ID)
	if err != nil || !ok {
		t.Fatalf("load metadata ok=%v err=%v", ok, err)
	}
	if loaded.CanonicalVersion != CanonicalVersion || loaded.AlgorithmVersion != MerkleAlgorithmVersion || loaded.RecordCount != metadata.RecordCount || !EqualBytes(loaded.Root, metadata.Root) {
		t.Fatalf("metadata round trip = %+v, want %+v", loaded, metadata)
	}
	loaded.Root[0] ^= 0xff
	again, _, err := store.Load(context.Background(), bucket.ID)
	if err != nil || EqualBytes(loaded.Root, again.Root) {
		t.Fatal("metadata store exposed mutable root storage")
	}
}

func TestCommitmentMetadataVersionsAcceptCurrentValues(t *testing.T) {
	metadata := validCommitmentMetadata(t)
	if err := ValidateCommitmentMetadata(metadata); err != nil {
		t.Fatalf("current versions rejected: %v", err)
	}
	store := NewMemoryCommitmentStore()
	if err := store.Save(context.Background(), metadata); err != nil {
		t.Fatalf("save current versions: %v", err)
	}
	loaded, ok, err := store.Load(context.Background(), metadata.Bucket)
	if err != nil || !ok {
		t.Fatalf("load current versions ok=%v err=%v", ok, err)
	}
	if loaded.CanonicalVersion != CanonicalVersion || loaded.AlgorithmVersion != MerkleAlgorithmVersion {
		t.Fatalf("loaded versions = %q/%q", loaded.CanonicalVersion, loaded.AlgorithmVersion)
	}
}

func TestCommitmentMetadataVersionsRejectWrongCanonical(t *testing.T) {
	metadata := validCommitmentMetadata(t)
	metadata.CanonicalVersion = "v0"
	assertVersionRejected(t, metadata, "canonical", CanonicalVersion, "v0")
}

func TestCommitmentMetadataVersionsRejectWrongAlgorithm(t *testing.T) {
	metadata := validCommitmentMetadata(t)
	metadata.AlgorithmVersion = "merkle-v0"
	assertVersionRejected(t, metadata, "algorithm", MerkleAlgorithmVersion, "merkle-v0")
}

func TestCommitmentMetadataVersionsRejectBothAndLoad(t *testing.T) {
	metadata := validCommitmentMetadata(t)
	metadata.CanonicalVersion = "v0"
	metadata.AlgorithmVersion = "merkle-v0"
	store := NewMemoryCommitmentStore()
	err := store.Save(context.Background(), metadata)
	if err == nil || !errors.Is(err, ErrIncompatibleCommitmentVersion) {
		t.Fatalf("save error = %v, want incompatible version", err)
	}
	var saveVersionErr *IncompatibleCommitmentVersionError
	if !errors.As(err, &saveVersionErr) || len(saveVersionErr.Mismatches) != 2 {
		t.Fatalf("save error = %T %v, want two version mismatches", err, err)
	}
	store.entries[metadata.Bucket.String()] = metadata

	_, ok, err := store.Load(context.Background(), metadata.Bucket)
	if ok || err == nil || !errors.Is(err, ErrIncompatibleCommitmentVersion) {
		t.Fatalf("load incompatible metadata ok=%v err=%v", ok, err)
	}
	var versionErr *IncompatibleCommitmentVersionError
	if !errors.As(err, &versionErr) || len(versionErr.Mismatches) != 2 {
		t.Fatalf("load error = %T %v, want two version mismatches", err, err)
	}
	if !hasVersionMismatch(versionErr, "canonical", CanonicalVersion, "v0") || !hasVersionMismatch(versionErr, "algorithm", MerkleAlgorithmVersion, "merkle-v0") {
		t.Fatalf("load mismatches = %+v", versionErr.Mismatches)
	}
}

func assertVersionRejected(t *testing.T, metadata CommitmentMetadata, kind, expected, actual string) {
	t.Helper()
	store := NewMemoryCommitmentStore()
	err := store.Save(context.Background(), metadata)
	if err == nil || !errors.Is(err, ErrIncompatibleCommitmentVersion) {
		t.Fatalf("save error = %v, want incompatible version", err)
	}
	var versionErr *IncompatibleCommitmentVersionError
	if !errors.As(err, &versionErr) || !hasVersionMismatch(versionErr, kind, expected, actual) {
		t.Fatalf("save error = %T %v, want %s %q (want %q)", err, err, kind, actual, expected)
	}
}

func hasVersionMismatch(err *IncompatibleCommitmentVersionError, kind, expected, actual string) bool {
	for _, mismatch := range err.Mismatches {
		if mismatch.Kind == kind && mismatch.Expected == expected && mismatch.Actual == actual {
			return true
		}
	}
	return false
}

func validCommitmentMetadata(t *testing.T) CommitmentMetadata {
	t.Helper()
	bucket := testBuckets(1)[0]
	return CommitmentMetadata{
		CanonicalVersion: CanonicalVersion,
		AlgorithmVersion: MerkleAlgorithmVersion,
		Bucket:           bucket.ID,
		Scope:            Scope{From: bucket.ID.Start, To: bucket.ID.Start.Add(bucket.ID.Width)},
		Root:             bucket.Root,
		RecordCount:      bucket.Records,
	}
}

func testRecords(count int) []CanonicalRecord {
	records := make([]CanonicalRecord, count)
	for i := range records {
		records[i] = CanonicalRecord{
			OperationID: uuid.NewMD5(uuid.NameSpaceOID, []byte("operation"+string(rune(i)))),
			PaymentID:   uuid.NewMD5(uuid.NameSpaceOID, []byte("payment"+string(rune(i)))),
			AccountID:   uuid.NewMD5(uuid.NameSpaceOID, []byte("account"+string(rune(i)))),
			EntryType:   "ENTRY",
			AmountPaise: int64(100 + i),
			Currency:    "INR",
			OccurredAt:  time.Date(2026, 1, 1, 0, i, 0, 0, time.UTC),
		}
	}
	return records
}

func testLeaves(count int) [][]byte {
	hashes, err := LeafHashes(testRecords(count))
	if err != nil {
		panic(err)
	}
	return hashes
}

func testBuckets(count int) []BucketCommitment {
	buckets := make([]BucketCommitment, count)
	for i := range buckets {
		id, err := NewBucketID("ledger", time.Date(2026, 1, 1, i, 0, 0, 0, time.UTC), time.Hour)
		if err != nil {
			panic(err)
		}
		leaves := testLeaves(i % 4)
		root, err := BucketRoot(leaves)
		if err != nil {
			panic(err)
		}
		buckets[i] = BucketCommitment{ID: id, Root: root, Records: len(leaves)}
	}
	return buckets
}
