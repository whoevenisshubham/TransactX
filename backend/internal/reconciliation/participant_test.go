package reconciliation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/bank"
)

func participantFixtureRecord(index int, occurredAt time.Time) CanonicalRecord {
	name := fmt.Sprintf("participant-fixture-%d", index)
	return CanonicalRecord{
		OperationID: uuid.NewMD5(uuid.NameSpaceOID, []byte(name+"-operation")),
		PaymentID:   uuid.NewMD5(uuid.NameSpaceOID, []byte(name+"-payment")),
		AccountID:   uuid.NewMD5(uuid.NameSpaceOID, []byte(name+"-account")),
		EntryType:   []string{"HOLD", "FINAL_CREDIT", "RELEASE"}[index%3],
		AmountPaise: int64(100 + index),
		Currency:    "INR",
		OccurredAt:  occurredAt,
	}
}

func participantScope() Scope {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return Scope{From: from, To: from.Add(4 * time.Hour)}
}

func participantRecords() []CanonicalRecord {
	base := participantScope().From
	return []CanonicalRecord{
		participantFixtureRecord(3, base.Add(3*time.Hour+2*time.Minute)),
		participantFixtureRecord(1, base.Add(1*time.Hour+2*time.Minute)),
		participantFixtureRecord(0, base.Add(30*time.Minute)),
		participantFixtureRecord(2, base.Add(2*time.Hour+2*time.Minute)),
	}
}

func TestMemoryParticipantsHaveBilateralDeterminismAndIdentityIndependence(t *testing.T) {
	records := participantRecords()
	left, err := NewMemoryParticipantWithCapture("BANK-A", "ledger", time.Hour, time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), records)
	if err != nil {
		t.Fatal(err)
	}
	right, err := NewMemoryParticipantWithCapture("BANK-B", "ledger", time.Hour, time.Date(2026, 1, 2, 1, 0, 0, 0, time.UTC), append([]CanonicalRecord(nil), records...))
	if err != nil {
		t.Fatal(err)
	}
	scope := participantScope()
	leftRoot, err := left.GetRoot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	rightRoot, err := right.GetRoot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(leftRoot.Root, rightRoot.Root) {
		t.Fatal("logically identical participant ledgers produced different roots")
	}
	if leftRoot.Ref.ParticipantID == rightRoot.Ref.ParticipantID {
		t.Fatal("participant identity was not kept distinct in references")
	}
	if leftRoot.Ref.ScopeID != rightRoot.Ref.ScopeID || leftRoot.Version != CanonicalVersion || leftRoot.Algorithm != MerkleAlgorithmVersion {
		t.Fatalf("root metadata = %+v / %+v", leftRoot, rightRoot)
	}

	leftRecords, err := left.GetRecords(context.Background(), BucketRef{ParticipantID: "BANK-A", ScopeID: leftRoot.Ref.ScopeID, Generation: leftRoot.Ref.Generation, Key: bucketKeyForRecord(t, records[0])})
	if err != nil {
		t.Fatal(err)
	}
	rightRecords, err := right.GetRecords(context.Background(), BucketRef{ParticipantID: "BANK-B", ScopeID: rightRoot.Ref.ScopeID, Generation: rightRoot.Ref.Generation, Key: bucketKeyForRecord(t, records[0])})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(leftRecords, rightRecords) {
		t.Fatalf("bilateral bucket records differ: %#v vs %#v", leftRecords, rightRecords)
	}
}

func TestParticipantScopeIsolationAndMetadata(t *testing.T) {
	participant, err := NewMemoryParticipant("BANK-A", "ledger", time.Hour, participantRecords())
	if err != nil {
		t.Fatal(err)
	}
	scope := participantScope()
	root, err := participant.GetRoot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	narrow := Scope{From: scope.From, To: scope.To.Add(-time.Hour)}
	narrowRoot, err := participant.GetRoot(context.Background(), narrow)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(root.Root, narrowRoot.Root) {
		t.Fatal("scope change did not change commitment")
	}
	metadata, err := participant.GetMetadata(context.Background(), narrow)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.RecordCount != 3 || metadata.ParticipantID != "BANK-A" {
		t.Fatalf("narrow metadata = %+v", metadata)
	}
}

func TestParticipantChildrenAndRecordsAreDeterministicallyOrdered(t *testing.T) {
	participant, err := NewMemoryParticipant("BANK-A", "ledger", time.Hour, participantRecords())
	if err != nil {
		t.Fatal(err)
	}
	scope := participantScope()
	root, err := participant.GetRoot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	children, err := participant.GetChildren(context.Background(), root.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 2 || children[0].Ref.Path != "L1/0" || children[1].Ref.Path != "L1/1" {
		t.Fatalf("root children = %+v", children)
	}
	childrenAgain, err := participant.GetChildren(context.Background(), root.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(children, childrenAgain) {
		t.Fatal("repeated child reads were not deterministic")
	}
	grandchildren, err := participant.GetChildren(context.Background(), children[0].Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(grandchildren) != 2 || grandchildren[0].Ref.Path != "L0/0" || grandchildren[1].Ref.Path != "L0/1" {
		t.Fatalf("first internal children = %+v", grandchildren)
	}

	bucketKey := bucketKeyForRecord(t, participantRecords()[0])
	records, err := participant.GetRecords(context.Background(), BucketRef{ParticipantID: "BANK-A", ScopeID: root.Ref.ScopeID, Generation: root.Ref.Generation, Key: bucketKey})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(records, SortRecords(records)) {
		t.Fatal("records were not returned in canonical order")
	}
}

func TestParticipantEmptyAndInvalidReferences(t *testing.T) {
	participant, err := NewMemoryParticipant("BANK-A", "ledger", time.Hour, participantRecords())
	if err != nil {
		t.Fatal(err)
	}
	emptyScope := Scope{From: participantScope().To.Add(time.Hour), To: participantScope().To.Add(2 * time.Hour)}
	root, err := participant.GetRoot(context.Background(), emptyScope)
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Root) != 32 || root.Ref.Path != emptyNodePath {
		t.Fatalf("empty root = %+v", root)
	}
	children, err := participant.GetChildren(context.Background(), root.Ref)
	if err != nil || children == nil || len(children) != 0 {
		t.Fatalf("empty children = %#v, err=%v", children, err)
	}
	if _, err := participant.GetChildren(context.Background(), NodeRef{ParticipantID: "BANK-A", ScopeID: root.Ref.ScopeID, Generation: root.Ref.Generation, Path: "L0/99"}); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("missing node error = %v", err)
	}
	if _, err := participant.GetChildren(context.Background(), NodeRef{ParticipantID: "BANK-A", ScopeID: "bad", Path: rootNodePath}); !errors.Is(err, ErrInvalidNodeReference) {
		t.Fatalf("malformed node error = %v", err)
	}
	if _, err := participant.GetRecords(context.Background(), BucketRef{ParticipantID: "BANK-A", ScopeID: root.Ref.ScopeID, Generation: root.Ref.Generation, Key: "not-a-bucket"}); !errors.Is(err, ErrInvalidBucketReference) {
		t.Fatalf("malformed bucket error = %v", err)
	}
	if _, err := participant.GetRecords(context.Background(), BucketRef{ParticipantID: "BANK-A", ScopeID: root.Ref.ScopeID, Generation: root.Ref.Generation, Key: bucketHeader + "missing"}); !errors.Is(err, ErrBucketNotFound) {
		t.Fatalf("missing bucket error = %v", err)
	}
	if _, err := participant.GetRoot(context.Background(), Scope{From: emptyScope.To, To: emptyScope.From}); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("invalid scope error = %v", err)
	}
}

func TestParticipantContextAndSnapshotErrorsPropagate(t *testing.T) {
	participant, err := NewMemoryParticipant("BANK-A", "ledger", time.Hour, participantRecords())
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := participant.GetRoot(canceled, participantScope()); !errors.Is(err, context.Canceled) {
		t.Fatalf("memory cancellation error = %v", err)
	}

	sentinel := errors.New("snapshot source failed")
	source := fakeSnapshotSource{err: sentinel}
	repository, err := NewRepositoryParticipant(source, "BANK-A", "ledger", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Initialize(context.Background(), participantScope()); !errors.Is(err, sentinel) {
		t.Fatalf("repository source error = %v", err)
	}
}

func TestRepositoryParticipantMatchesMemoryFixture(t *testing.T) {
	records := participantRecords()
	entries := make([]bank.LedgerEntry, 0, len(records))
	for _, record := range records {
		entries = append(entries, bank.LedgerEntry{OperationID: record.OperationID, PaymentID: record.PaymentID, AccountID: record.AccountID, EntryType: record.EntryType, AmountPaise: record.AmountPaise, Currency: record.Currency, OccurredAt: record.OccurredAt})
	}
	snapshot := bank.LedgerSnapshot{BankID: "BANK-A", SnapshotID: uuid.New(), CapturedAt: time.Date(2026, 1, 2, 2, 0, 0, 0, time.UTC), Entries: entries}
	sourceCalls := 0
	repository, err := NewRepositoryParticipant(fakeSnapshotSource{snapshot: snapshot, calls: &sourceCalls}, "BANK-A", "ledger", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	memory, err := NewMemoryParticipant("fixture", "ledger", time.Hour, records)
	if err != nil {
		t.Fatal(err)
	}
	scope := participantScope()
	if err := repository.Initialize(context.Background(), scope); err != nil {
		t.Fatal(err)
	}
	repositoryRoot, err := repository.GetRoot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	memoryRoot, err := memory.GetRoot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(repositoryRoot.Root, memoryRoot.Root) {
		t.Fatal("repository participant diverged from equivalent memory fixture")
	}
	if _, err := repository.GetChildren(context.Background(), repositoryRoot.Ref); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.GetRecords(context.Background(), BucketRef{ParticipantID: "BANK-A", ScopeID: repositoryRoot.Ref.ScopeID, Generation: repositoryRoot.Ref.Generation, Key: bucketKeyForRecord(t, records[0])}); err != nil {
		t.Fatal(err)
	}
	metadata, err := repository.GetMetadata(context.Background(), scope)
	if err != nil || metadata.RecordCount != int64(len(records)) || !metadata.CapturedAt.Equal(snapshot.CapturedAt) {
		t.Fatalf("repository metadata = %+v, err=%v", metadata, err)
	}
	if sourceCalls != 1 {
		t.Fatalf("participant rebuilt the authoritative snapshot %d times for one read snapshot", sourceCalls)
	}
}

func TestRepositoryParticipantUsesMaintainedStateAndPinsGeneration(t *testing.T) {
	records := participantRecords()
	entries := make([]bank.LedgerEntry, 0, len(records))
	for _, record := range records {
		entries = append(entries, bank.LedgerEntry{OperationID: record.OperationID, PaymentID: record.PaymentID, AccountID: record.AccountID, EntryType: record.EntryType, AmountPaise: record.AmountPaise, Currency: record.Currency, OccurredAt: record.OccurredAt})
	}
	snapshot := bank.LedgerSnapshot{BankID: "BANK-A", CapturedAt: time.Date(2026, 1, 2, 2, 0, 0, 0, time.UTC), Entries: entries}
	sourceCalls := 0
	source := &fakeSnapshotSource{snapshot: snapshot, calls: &sourceCalls}
	store := NewMemoryIncrementalCommitmentStore()
	participant, err := NewRepositoryParticipantWithCommitmentStore(source, "BANK-A", "ledger", time.Hour, store)
	if err != nil {
		t.Fatal(err)
	}
	scope := participantScope()
	if err := participant.Initialize(context.Background(), scope); err != nil {
		t.Fatal(err)
	}
	rootA, err := participant.GetRoot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	childrenA, err := participant.GetChildren(context.Background(), rootA.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(childrenA) == 0 {
		t.Fatal("initialized commitment returned no children")
	}
	if _, err := participant.GetRecords(context.Background(), BucketRef{ParticipantID: "BANK-A", ScopeID: childrenA[0].Ref.ScopeID, Generation: childrenA[0].Ref.Generation, Key: bucketKeyForRecord(t, records[0])}); err != nil {
		t.Fatal(err)
	}

	// Normal reads consume the maintained state and do not call the ledger
	// source again, so they cannot silently invoke Bootstrap.
	source.err = errors.New("source must not be consulted by normal reads")
	rootAgain, err := participant.GetRoot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rootA.Root, rootAgain.Root) || rootA.Ref.Generation != rootAgain.Ref.Generation || sourceCalls != 1 {
		t.Fatalf("maintained read changed commitment or consulted source: rootA=%+v rootAgain=%+v calls=%d", rootA, rootAgain, sourceCalls)
	}
	reloaded, err := NewRepositoryParticipantWithCommitmentStore(source, "BANK-A", "ledger", time.Hour, store)
	if err != nil {
		t.Fatal(err)
	}
	persistedRoot, err := reloaded.GetRoot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rootA.Root, persistedRoot.Root) || sourceCalls != 1 {
		t.Fatalf("persisted commitment was not reused: root=%+v persisted=%+v calls=%d", rootA, persistedRoot, sourceCalls)
	}

	// Refresh is explicit recovery. It installs a new generation and makes all
	// references from the prior generation stale.
	source.err = nil
	source.snapshot.CapturedAt = snapshot.CapturedAt.Add(time.Minute)
	if err := participant.Refresh(context.Background(), scope); err != nil {
		t.Fatal(err)
	}
	rootB, err := participant.GetRoot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if rootA.Ref.Generation == rootB.Ref.Generation {
		t.Fatal("refresh reused the prior commitment generation")
	}
	if _, err := participant.GetChildren(context.Background(), rootA.Ref); !errors.Is(err, ErrStaleReference) {
		t.Fatalf("old node reference error = %v", err)
	}
	if _, err := participant.GetRecords(context.Background(), BucketRef{ParticipantID: "BANK-A", ScopeID: rootA.Ref.ScopeID, Generation: rootA.Ref.Generation, Key: bucketKeyForRecord(t, records[0])}); !errors.Is(err, ErrStaleReference) {
		t.Fatalf("old bucket reference error = %v", err)
	}
	if _, err := participant.GetChildren(context.Background(), rootB.Ref); err != nil {
		t.Fatalf("new root reference rejected: %v", err)
	}
	if _, err := participant.GetRecords(context.Background(), BucketRef{ParticipantID: "BANK-A", ScopeID: rootB.Ref.ScopeID, Generation: rootB.Ref.Generation, Key: bucketKeyForRecord(t, records[0])}); err != nil {
		t.Fatalf("new bucket reference rejected: %v", err)
	}
}

func bucketKeyForRecord(t *testing.T, record CanonicalRecord) string {
	t.Helper()
	bucket, err := BucketForRecord(record, "ledger", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return bucket.String()
}

type fakeSnapshotSource struct {
	snapshot bank.LedgerSnapshot
	err      error
	calls    *int
}

func (source fakeSnapshotSource) GetLedgerSnapshot(ctx context.Context, _ bank.LedgerScope) (bank.LedgerSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return bank.LedgerSnapshot{}, err
	}
	if source.err != nil {
		return bank.LedgerSnapshot{}, source.err
	}
	if source.calls != nil {
		*source.calls = *source.calls + 1
	}
	return source.snapshot, nil
}

// TestParticipantBoundaryRejectsForeignParticipantRef directly verifies that
// both MemoryParticipant and RepositoryParticipant reject any NodeRef or BucketRef
// targeting a foreign participant identity with ErrParticipantMismatch.
func TestParticipantBoundaryRejectsForeignParticipantRef(t *testing.T) {
	scope := participantScope()
	records := participantRecords()

	// 1. Test MemoryParticipant boundary rejection
	memP, err := NewMemoryParticipant("BANK-A", "ledger", time.Hour, records)
	if err != nil {
		t.Fatal(err)
	}
	memRoot, err := memP.GetRoot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}

	foreignNodeRef := NodeRef{
		ParticipantID: "BANK-B",
		ScopeID:       memRoot.Ref.ScopeID,
		Generation:    memRoot.Ref.Generation,
		Path:          memRoot.Ref.Path,
	}
	foreignBucketRef := BucketRef{
		ParticipantID: "BANK-B",
		ScopeID:       memRoot.Ref.ScopeID,
		Generation:    memRoot.Ref.Generation,
		Key:           bucketKeyForRecord(t, records[0]),
	}

	if _, err := memP.GetChildren(context.Background(), foreignNodeRef); !errors.Is(err, ErrParticipantMismatch) {
		t.Fatalf("MemoryParticipant.GetChildren expected ErrParticipantMismatch, got: %v", err)
	}
	if _, err := memP.GetRecords(context.Background(), foreignBucketRef); !errors.Is(err, ErrParticipantMismatch) {
		t.Fatalf("MemoryParticipant.GetRecords expected ErrParticipantMismatch, got: %v", err)
	}
	if _, err := memP.GetBucketID(context.Background(), foreignNodeRef); !errors.Is(err, ErrParticipantMismatch) {
		t.Fatalf("MemoryParticipant.GetBucketID expected ErrParticipantMismatch, got: %v", err)
	}

	// 2. Test RepositoryParticipant boundary rejection
	entries := make([]bank.LedgerEntry, 0, len(records))
	for _, record := range records {
		entries = append(entries, bank.LedgerEntry{
			OperationID: record.OperationID,
			PaymentID:   record.PaymentID,
			AccountID:   record.AccountID,
			EntryType:   record.EntryType,
			AmountPaise: record.AmountPaise,
			Currency:    record.Currency,
			OccurredAt:  record.OccurredAt,
		})
	}
	snapshot := bank.LedgerSnapshot{
		BankID:     "BANK-A",
		SnapshotID: uuid.New(),
		CapturedAt: time.Date(2026, 1, 2, 2, 0, 0, 0, time.UTC),
		Entries:    entries,
	}
	repoP, err := NewRepositoryParticipant(fakeSnapshotSource{snapshot: snapshot}, "BANK-A", "ledger", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := repoP.Initialize(context.Background(), scope); err != nil {
		t.Fatal(err)
	}
	repoRoot, err := repoP.GetRoot(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}

	repoForeignNode := NodeRef{
		ParticipantID: "BANK-B",
		ScopeID:       repoRoot.Ref.ScopeID,
		Generation:    repoRoot.Ref.Generation,
		Path:          repoRoot.Ref.Path,
	}
	repoForeignBucket := BucketRef{
		ParticipantID: "BANK-B",
		ScopeID:       repoRoot.Ref.ScopeID,
		Generation:    repoRoot.Ref.Generation,
		Key:           bucketKeyForRecord(t, records[0]),
	}

	if _, err := repoP.GetChildren(context.Background(), repoForeignNode); !errors.Is(err, ErrParticipantMismatch) {
		t.Fatalf("RepositoryParticipant.GetChildren expected ErrParticipantMismatch, got: %v", err)
	}
	if _, err := repoP.GetRecords(context.Background(), repoForeignBucket); !errors.Is(err, ErrParticipantMismatch) {
		t.Fatalf("RepositoryParticipant.GetRecords expected ErrParticipantMismatch, got: %v", err)
	}
	if _, err := repoP.GetBucketID(context.Background(), repoForeignNode); !errors.Is(err, ErrParticipantMismatch) {
		t.Fatalf("RepositoryParticipant.GetBucketID expected ErrParticipantMismatch, got: %v", err)
	}
}
