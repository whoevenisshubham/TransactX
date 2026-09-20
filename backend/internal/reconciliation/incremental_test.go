package reconciliation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBucketForRecordUsesUTCFloorAndExplicitBoundaries(t *testing.T) {
	width := time.Hour
	partition := "ledger"
	start := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)

	exact, err := BucketForRecord(incrementalRecord(1, start), partition, width)
	if err != nil {
		t.Fatal(err)
	}
	if !exact.Start.Equal(start) {
		t.Fatalf("exact boundary start = %v, want %v", exact.Start, start)
	}

	before, err := BucketForRecord(incrementalRecord(2, start.Add(-time.Nanosecond)), partition, width)
	if err != nil {
		t.Fatal(err)
	}
	if !before.Start.Equal(start.Add(-width)) {
		t.Fatalf("before-boundary start = %v, want %v", before.Start, start.Add(-width))
	}

	after, err := BucketForRecord(incrementalRecord(3, start.Add(time.Nanosecond)), partition, width)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Start.Equal(start) {
		t.Fatalf("after-boundary start = %v, want %v", after.Start, start)
	}

	local := incrementalRecord(4, time.Date(2026, 9, 20, 12, 30, 0, 0, time.FixedZone("+0530", 5*60*60+30*60)))
	utc := incrementalRecord(5, local.OccurredAt.UTC())
	localBucket, err := BucketForRecord(local, partition, width)
	if err != nil {
		t.Fatal(err)
	}
	utcBucket, err := BucketForRecord(utc, partition, width)
	if err != nil {
		t.Fatal(err)
	}
	if !localBucket.Equal(utcBucket) {
		t.Fatalf("equivalent instants mapped to different buckets: %v vs %v", localBucket, utcBucket)
	}

	negative, err := BucketForRecord(incrementalRecord(6, time.Unix(-1, 0).UTC()), partition, width)
	if err != nil {
		t.Fatal(err)
	}
	if !negative.Start.Equal(time.Unix(-3600, 0).UTC()) {
		t.Fatalf("negative instant start = %v, want %v", negative.Start, time.Unix(-3600, 0).UTC())
	}
}

func TestIncrementalScopeIsHalfOpen(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)
	ledger, err := NewIncrementalMerkleLedger("ledger", time.Hour, Scope{From: from, To: to})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.AppendRecord(context.Background(), incrementalRecord(1, from)); err != nil {
		t.Fatalf("scope start rejected: %v", err)
	}
	if _, err := ledger.AppendRecord(context.Background(), incrementalRecord(2, to)); !errors.Is(err, ErrRecordOutsideScope) {
		t.Fatalf("scope end error = %v, want ErrRecordOutsideScope", err)
	}
}

func TestIncrementalSingleAppendRecomputesOneBucketAndAncestors(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger := newIncrementalTestLedger(t, 0, 8)
	initial := make([]CanonicalRecord, 4)
	for i := range initial {
		initial[i] = incrementalRecord(i, base.Add(time.Duration(i)*time.Hour))
	}
	if _, err := ledger.Bootstrap(context.Background(), initial); err != nil {
		t.Fatal(err)
	}
	before := ledger.Snapshot()
	fullRebuilds := ledger.FullRebuildCount()
	update, err := ledger.AppendRecord(context.Background(), incrementalRecord(100, base.Add(30*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if update.BucketsRecomputed != 1 || update.BucketsReused != 3 || update.AncestorNodesRecomputed != 2 || update.TotalRecordsConsidered != 2 {
		t.Fatalf("incremental counters = %+v", update)
	}
	if ledger.FullRebuildCount() != fullRebuilds {
		t.Fatal("normal append invoked the full rebuild path")
	}
	if bytes.Equal(before.Root, update.ResultingRoot) {
		t.Fatal("append did not change global root")
	}
	after := ledger.Snapshot()
	if !bytes.Equal(before.Buckets[1].Root, after.Buckets[1].Root) || !bytes.Equal(before.Buckets[2].Root, after.Buckets[2].Root) || !bytes.Equal(before.Buckets[3].Root, after.Buckets[3].Root) {
		t.Fatal("unrelated bucket root changed")
	}
	if bytes.Equal(before.Buckets[0].Root, after.Buckets[0].Root) {
		t.Fatal("affected bucket root did not change")
	}
	assertGlobalRootMatchesSnapshot(t, after)
}

func TestIncrementalDifferentBucketAppendReusesPreviousRoots(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger := newIncrementalTestLedger(t, 0, 8)
	initial := []CanonicalRecord{
		incrementalRecord(1, base),
		incrementalRecord(2, base.Add(time.Hour)),
	}
	if _, err := ledger.Bootstrap(context.Background(), initial); err != nil {
		t.Fatal(err)
	}
	before := ledger.Snapshot()
	update, err := ledger.AppendRecord(context.Background(), incrementalRecord(3, base.Add(2*time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	if update.BucketsRecomputed != 1 || update.BucketsReused != 2 || update.AncestorNodesRecomputed != 1 || update.TotalRecordsConsidered != 1 {
		t.Fatalf("different-bucket counters = %+v", update)
	}
	after := ledger.Snapshot()
	for i := range before.Buckets {
		if !bytes.Equal(before.Buckets[i].Root, after.Buckets[i].Root) {
			t.Fatalf("previous bucket %d changed", i)
		}
	}
	assertGlobalRootMatchesSnapshot(t, after)
}

func TestIncrementalSameBucketUpsertOnlyTouchesAncestorPath(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger := newIncrementalTestLedger(t, 0, 8)
	initial := make([]CanonicalRecord, 8)
	for i := range initial {
		initial[i] = incrementalRecord(i, base.Add(time.Duration(i)*time.Hour))
	}
	if _, err := ledger.Bootstrap(context.Background(), initial); err != nil {
		t.Fatal(err)
	}
	before := ledger.Snapshot()
	changed := initial[3]
	changed.AmountPaise++
	update, err := ledger.UpsertRecord(context.Background(), changed)
	if err != nil {
		t.Fatal(err)
	}
	if update.BucketsRecomputed != 1 || update.BucketsReused != 7 || update.AncestorNodesRecomputed != 3 || update.TotalRecordsConsidered != 1 {
		t.Fatalf("same-bucket counters = %+v", update)
	}
	after := ledger.Snapshot()
	for i := range before.Buckets {
		if i == 3 {
			if bytes.Equal(before.Buckets[i].Root, after.Buckets[i].Root) {
				t.Fatal("updated bucket root did not change")
			}
			continue
		}
		if !bytes.Equal(before.Buckets[i].Root, after.Buckets[i].Root) {
			t.Fatalf("unrelated bucket %d changed", i)
		}
	}
	assertGlobalRootMatchesSnapshot(t, after)
}

func TestIncrementalAppendIsDeterministicAndMatchesBootstrap(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	records := []CanonicalRecord{
		incrementalRecord(4, base.Add(3*time.Hour+5*time.Minute)),
		incrementalRecord(1, base.Add(30*time.Minute)),
		incrementalRecord(3, base.Add(2*time.Hour)),
		incrementalRecord(2, base.Add(time.Hour)),
	}
	bootstrapped := newIncrementalTestLedger(t, 0, 8)
	if _, err := bootstrapped.Bootstrap(context.Background(), records); err != nil {
		t.Fatal(err)
	}
	left := newIncrementalTestLedger(t, 0, 8)
	right := newIncrementalTestLedger(t, 0, 8)
	ordered := SortRecords(records)
	for _, record := range ordered {
		if _, err := left.AppendRecord(context.Background(), record); err != nil {
			t.Fatal(err)
		}
		if _, err := right.AppendRecord(context.Background(), record); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(bootstrapped.Root(), left.Root()) || !bytes.Equal(left.Root(), right.Root()) {
		t.Fatalf("bootstrap/incremental roots differ: bootstrap=%x left=%x right=%x", bootstrapped.Root(), left.Root(), right.Root())
	}
	if left.FullRebuildCount() != 0 || right.FullRebuildCount() != 0 {
		t.Fatal("incremental append path performed a full rebuild")
	}
}

func TestIncrementalAppendAcrossBucketCountsMatchesGlobalRoot(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger := newIncrementalTestLedger(t, 0, 40)
	for i := 0; i < 32; i++ {
		if _, err := ledger.AppendRecord(context.Background(), incrementalRecord(i, base.Add(time.Duration(i)*time.Hour))); err != nil {
			t.Fatalf("append bucket %d: %v", i, err)
		}
		state := ledger.Snapshot()
		assertGlobalRootMatchesSnapshot(t, state)
		if len(state.Buckets) != i+1 || len(state.Levels[0]) != i+1 {
			t.Fatalf("bucket count %d state = %+v", i, state)
		}
	}
}

func TestIncrementalPersistLoadRestoreAndContinue(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	original := newIncrementalTestLedger(t, 0, 8)
	initial := []CanonicalRecord{
		incrementalRecord(1, base),
		incrementalRecord(2, base.Add(time.Hour)),
		incrementalRecord(3, base.Add(2*time.Hour)),
	}
	if _, err := original.Bootstrap(context.Background(), initial); err != nil {
		t.Fatal(err)
	}
	store := NewMemoryIncrementalCommitmentStore()
	if err := original.Persist(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	persisted, ok, err := store.LoadState(context.Background(), "ledger", time.Hour, original.Snapshot().Scope)
	if err != nil || !ok {
		t.Fatalf("persisted load ok=%v err=%v", ok, err)
	}
	restored, err := NewIncrementalMerkleLedgerFromState("ledger", time.Hour, persisted)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original.Root(), restored.Root()) {
		t.Fatalf("restored root = %x, original = %x", restored.Root(), original.Root())
	}
	if restored.FullRebuildCount() != original.FullRebuildCount() {
		t.Fatalf("restored rebuild count = %d, want %d", restored.FullRebuildCount(), original.FullRebuildCount())
	}

	if _, err := restored.AppendRecord(context.Background(), incrementalRecord(4, base.Add(3*time.Hour))); err != nil {
		t.Fatalf("restored append: %v", err)
	}
	updated := initial[1]
	updated.AmountPaise++
	if _, err := restored.UpsertRecord(context.Background(), updated); err != nil {
		t.Fatalf("restored same-bucket upsert: %v", err)
	}
	if restored.FullRebuildCount() != original.FullRebuildCount() {
		t.Fatal("restored incremental operations changed rebuild count")
	}
	assertGlobalRootMatchesSnapshot(t, restored.Snapshot())
}

func TestIncrementalRestoreRejectsCorruptedState(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	source := newIncrementalTestLedger(t, 0, 8)
	if _, err := source.AppendRecord(context.Background(), incrementalRecord(1, base)); err != nil {
		t.Fatal(err)
	}
	state := source.Snapshot()
	tests := []struct {
		name string
		edit func(*IncrementalCommitmentState)
		want error
	}{
		{name: "canonical version", edit: func(s *IncrementalCommitmentState) { s.CanonicalVersion = "v0" }, want: ErrIncompatibleCommitmentVersion},
		{name: "algorithm version", edit: func(s *IncrementalCommitmentState) { s.AlgorithmVersion = "merkle-v0" }, want: ErrIncompatibleCommitmentVersion},
		{name: "scope", edit: func(s *IncrementalCommitmentState) { s.Scope.To = s.Scope.From.Add(-time.Hour) }},
		{name: "tree width", edit: func(s *IncrementalCommitmentState) { s.Levels = s.Levels[:len(s.Levels)-1] }},
		{name: "bucket order", edit: func(s *IncrementalCommitmentState) { s.Buckets[0].ID.Partition = "other" }},
		{name: "metadata", edit: func(s *IncrementalCommitmentState) { s.Buckets[0].Metadata.RecordCount++ }},
		{name: "final root", edit: func(s *IncrementalCommitmentState) { s.Root[0] ^= 0xff }},
		{name: "bucket root", edit: func(s *IncrementalCommitmentState) { s.Buckets[0].Root[0] ^= 0xff }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			corrupt := cloneIncrementalState(state)
			test.edit(&corrupt)
			ledger, err := NewIncrementalMerkleLedgerFromState("ledger", time.Hour, corrupt)
			if test.want != nil {
				if !errors.Is(err, test.want) {
					t.Fatalf("restore error = %v, want %v", err, test.want)
				}
			} else if err == nil {
				t.Fatal("corrupted state was accepted")
			}
			if ledger != nil {
				t.Fatal("corrupted state returned a ledger")
			}
		})
	}
}

func TestIncrementalBootstrapSerializesAgainstAppend(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger := newIncrementalTestLedger(t, 0, 8)
	started := make(chan struct{})
	release := make(chan struct{})
	ledger.beforeBootstrapCommit = func() {
		close(started)
		<-release
	}

	bootstrapDone := make(chan error, 1)
	go func() {
		_, err := ledger.Bootstrap(context.Background(), []CanonicalRecord{incrementalRecord(1, base)})
		bootstrapDone <- err
	}()
	<-started
	appendDone := make(chan error, 1)
	go func() {
		_, err := ledger.AppendRecord(context.Background(), incrementalRecord(2, base.Add(10*time.Minute)))
		appendDone <- err
	}()
	select {
	case err := <-appendDone:
		t.Fatalf("append completed while bootstrap held operation lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-bootstrapDone; err != nil {
		t.Fatal(err)
	}
	if err := <-appendDone; err != nil {
		t.Fatal(err)
	}
	if got := ledger.Snapshot().RecordCount; got != 2 {
		t.Fatalf("serialized bootstrap lost append: record count = %d", got)
	}
}

func TestIncrementalCommitmentStoreScopeAndVersionIsolation(t *testing.T) {
	ledger := newIncrementalTestLedger(t, 0, 8)
	if _, err := ledger.AppendRecord(context.Background(), incrementalRecord(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	state := ledger.Snapshot()
	store := NewMemoryIncrementalCommitmentStore()
	if err := store.SaveState(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := store.LoadState(context.Background(), state.Partition, state.BucketWidth, state.Scope)
	if err != nil || !ok || !bytes.Equal(loaded.Root, state.Root) {
		t.Fatalf("same-scope load ok=%v err=%v", ok, err)
	}
	if len(loaded.Levels) != len(state.Levels) || len(loaded.Buckets) != len(state.Buckets) {
		t.Fatalf("loaded ancestor state shape = levels %d buckets %d", len(loaded.Levels), len(loaded.Buckets))
	}
	otherScope := state.Scope
	otherScope.From = otherScope.From.Add(24 * time.Hour)
	otherScope.To = otherScope.To.Add(24 * time.Hour)
	if _, ok, err := store.LoadState(context.Background(), state.Partition, state.BucketWidth, otherScope); err != nil || ok {
		t.Fatalf("different scope reused state ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.LoadState(context.Background(), "other-ledger", state.BucketWidth, state.Scope); err != nil || ok {
		t.Fatalf("different partition reused state ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.LoadState(context.Background(), state.Partition, 2*time.Hour, state.Scope); err != nil || ok {
		t.Fatalf("different width reused state ok=%v err=%v", ok, err)
	}
	if _, err := NewIncrementalMerkleLedgerFromState("other-ledger", state.BucketWidth, state); !errors.Is(err, ErrIncrementalConfigMismatch) {
		t.Fatalf("different partition restore error = %v", err)
	}
	if _, err := NewIncrementalMerkleLedgerFromState(state.Partition, 2*time.Hour, state); !errors.Is(err, ErrIncrementalConfigMismatch) {
		t.Fatalf("different width restore error = %v", err)
	}
	wrongCanonical := state
	wrongCanonical.CanonicalVersion = "v0"
	if err := store.SaveState(context.Background(), wrongCanonical); !errors.Is(err, ErrIncompatibleCommitmentVersion) {
		t.Fatalf("wrong canonical save error = %v", err)
	}
	wrongAlgorithm := state
	wrongAlgorithm.AlgorithmVersion = "merkle-v0"
	if err := store.SaveState(context.Background(), wrongAlgorithm); !errors.Is(err, ErrIncompatibleCommitmentVersion) {
		t.Fatalf("wrong algorithm save error = %v", err)
	}
	corrupt := cloneIncrementalState(state)
	corrupt.Root[0] ^= 0xff
	store.entries[commitmentStateKey(state.Partition, state.BucketWidth, state.Scope)] = corrupt
	if _, ok, err := store.LoadState(context.Background(), state.Partition, state.BucketWidth, state.Scope); ok || err == nil {
		t.Fatalf("corrupt persisted state load ok=%v err=%v", ok, err)
	}
}

func TestIncrementalBootstrapAndAdversarialReuseCounters(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger := newIncrementalTestLedger(t, 0, 32)
	records := make([]CanonicalRecord, 16)
	for i := range records {
		records[i] = incrementalRecord(i, base.Add(time.Duration(i)*time.Hour))
	}
	bootstrap, err := ledger.Bootstrap(context.Background(), records)
	if err != nil {
		t.Fatal(err)
	}
	if bootstrap.BucketsRecomputed != 16 || bootstrap.TotalRecordsConsidered != 16 || ledger.FullRebuildCount() != 1 {
		t.Fatalf("bootstrap result = %+v rebuilds=%d", bootstrap, ledger.FullRebuildCount())
	}
	before := ledger.Snapshot()
	update, err := ledger.AppendRecord(context.Background(), incrementalRecord(100, base.Add(5*time.Hour+time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if update.BucketsRecomputed != 1 || update.BucketsReused != 15 || update.TotalRecordsConsidered != 2 || ledger.FullRebuildCount() != 1 {
		t.Fatalf("adversarial incremental result = %+v rebuilds=%d", update, ledger.FullRebuildCount())
	}
	after := ledger.Snapshot()
	for i := range before.Buckets {
		if i != 5 && !bytes.Equal(before.Buckets[i].Root, after.Buckets[i].Root) {
			t.Fatalf("adversarial unrelated bucket %d changed", i)
		}
	}
}

func TestIncrementalStoreConcurrentAccess(t *testing.T) {
	ledger := newIncrementalTestLedger(t, 0, 8)
	store := NewMemoryIncrementalCommitmentStore()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := ledger.AppendRecord(context.Background(), incrementalRecord(0, base)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 1; i <= 24; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.SaveState(context.Background(), ledger.Snapshot()); err != nil {
				t.Errorf("save: %v", err)
			}
			current := ledger.Snapshot()
			_, _, _ = store.LoadState(context.Background(), current.Partition, current.BucketWidth, current.Scope)
			if _, err := ledger.AppendRecord(context.Background(), incrementalRecord(i, base.Add(10*time.Minute))); err != nil {
				t.Errorf("append: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := ledger.Snapshot().RecordCount; got != 25 {
		t.Fatalf("concurrent record count = %d, want 25", got)
	}
}

func TestIncrementalRejectsOutOfOrderNewBucketAndDuplicate(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ledger := newIncrementalTestLedger(t, 0, 8)
	if _, err := ledger.AppendRecord(context.Background(), incrementalRecord(1, base.Add(2*time.Hour))); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.AppendRecord(context.Background(), incrementalRecord(1, base.Add(2*time.Hour))); !errors.Is(err, ErrDuplicateRecord) {
		t.Fatalf("duplicate error = %v", err)
	}
	if _, err := ledger.AppendRecord(context.Background(), incrementalRecord(2, base.Add(time.Hour))); !errors.Is(err, ErrBucketOutOfOrder) {
		t.Fatalf("out-of-order error = %v", err)
	}
	changedBucket := incrementalRecord(1, base.Add(3*time.Hour))
	changedBucket.AmountPaise++
	if _, err := ledger.UpsertRecord(context.Background(), changedBucket); !errors.Is(err, ErrRecordBucketMove) {
		t.Fatalf("bucket move error = %v", err)
	}
}

func assertGlobalRootMatchesSnapshot(t *testing.T, state IncrementalCommitmentState) {
	t.Helper()
	root, err := GlobalRoot(state.Buckets)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(root, state.Root) {
		t.Fatalf("incremental root = %x, global root = %x", state.Root, root)
	}
}

func newIncrementalTestLedger(t *testing.T, fromOffset, hours int) *IncrementalMerkleLedger {
	t.Helper()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(fromOffset) * time.Hour)
	ledger, err := NewIncrementalMerkleLedger("ledger", time.Hour, Scope{From: base, To: base.Add(time.Duration(hours) * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	return ledger
}

func incrementalRecord(index int, occurredAt time.Time) CanonicalRecord {
	return CanonicalRecord{
		OperationID: uuid.NewMD5(uuid.NameSpaceOID, []byte(fmt.Sprintf("incremental-operation-%d", index))),
		PaymentID:   uuid.NewMD5(uuid.NameSpaceOID, []byte(fmt.Sprintf("incremental-payment-%d", index))),
		AccountID:   uuid.NewMD5(uuid.NameSpaceOID, []byte(fmt.Sprintf("incremental-account-%d", index))),
		EntryType:   "ENTRY",
		AmountPaise: int64(100 + index),
		Currency:    "INR",
		OccurredAt:  occurredAt,
	}
}
