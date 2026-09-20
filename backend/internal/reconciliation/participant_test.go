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

	leftRecords, err := left.GetRecords(context.Background(), BucketRef{ParticipantID: "BANK-A", ScopeID: leftRoot.Ref.ScopeID, Key: bucketKeyForRecord(t, records[0])})
	if err != nil {
		t.Fatal(err)
	}
	rightRecords, err := right.GetRecords(context.Background(), BucketRef{ParticipantID: "BANK-B", ScopeID: rightRoot.Ref.ScopeID, Key: bucketKeyForRecord(t, records[0])})
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
	records, err := participant.GetRecords(context.Background(), BucketRef{ParticipantID: "BANK-A", ScopeID: root.Ref.ScopeID, Key: bucketKey})
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
	if _, err := participant.GetChildren(context.Background(), NodeRef{ParticipantID: "BANK-A", ScopeID: root.Ref.ScopeID, Path: "L0/99"}); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("missing node error = %v", err)
	}
	if _, err := participant.GetChildren(context.Background(), NodeRef{ParticipantID: "BANK-A", ScopeID: "bad", Path: rootNodePath}); !errors.Is(err, ErrInvalidNodeReference) {
		t.Fatalf("malformed node error = %v", err)
	}
	if _, err := participant.GetRecords(context.Background(), BucketRef{ParticipantID: "BANK-A", ScopeID: root.Ref.ScopeID, Key: "not-a-bucket"}); !errors.Is(err, ErrInvalidBucketReference) {
		t.Fatalf("malformed bucket error = %v", err)
	}
	if _, err := participant.GetRecords(context.Background(), BucketRef{ParticipantID: "BANK-A", ScopeID: root.Ref.ScopeID, Key: bucketHeader + "missing"}); !errors.Is(err, ErrBucketNotFound) {
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
	if _, err := repository.GetRoot(context.Background(), participantScope()); !errors.Is(err, sentinel) {
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
	if _, err := repository.GetRecords(context.Background(), BucketRef{ParticipantID: "BANK-A", ScopeID: repositoryRoot.Ref.ScopeID, Key: bucketKeyForRecord(t, records[0])}); err != nil {
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
