package reconciliation

import (
	"bytes"
	"encoding/hex"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/bank"
)

var goldenRecord = CanonicalRecord{
	OperationID: uuid.MustParse("11111111-1111-4111-8111-111111111111"),
	PaymentID:   uuid.MustParse("22222222-2222-4222-8222-222222222222"),
	AccountID:   uuid.MustParse("33333333-3333-4333-8333-333333333333"),
	EntryType:   "FINAL_CREDIT",
	AmountPaise: 12550,
	Currency:    "INR",
	OccurredAt:  time.Date(2026, 9, 20, 12, 34, 56, 123456789, time.FixedZone("IST", 5*60*60)),
}

func TestCanonicalBytesGoldenVector(t *testing.T) {
	got, err := CanonicalBytes(goldenRecord)
	if err != nil {
		t.Fatal(err)
	}
	want := "545843414e4f4e7c76310000002431313131313131312d313131312d343131312d383131312d3131313131313131313131310000002432323232323232322d323232322d343232322d383232322d3232323232323232323232320000002433333333333333332d333333332d343333332d383333332d3333333333333333333333330000000c46494e414c5f43524544495400000005313235353000000003494e520000001e323032362d30392d32305430373a33343a35362e3132333435363738395a"
	if hex.EncodeToString(got) != want {
		t.Fatalf("canonical bytes = %s, want %s", hex.EncodeToString(got), want)
	}
	hash, err := LeafHash(goldenRecord)
	if err != nil {
		t.Fatal(err)
	}
	if gotHash := hex.EncodeToString(hash); gotHash != "dc94b95dd45fb7d01ee7c047005688382cc9aed3a902d71b5ea3891b8b1a2037" {
		t.Fatalf("leaf hash = %s", gotHash)
	}
}

func TestCanonicalBytesAndLeafHashAreDeterministic(t *testing.T) {
	first, err := CanonicalBytes(goldenRecord)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CanonicalBytes(goldenRecord)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("same logical record produced different canonical bytes")
	}
	hashA, err := LeafHash(goldenRecord)
	if err != nil {
		t.Fatal(err)
	}
	hashB, err := LeafHash(goldenRecord)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hashA, hashB) {
		t.Fatal("same logical record produced different leaf hashes")
	}
}

func TestSnapshotMetadataDoesNotAffectCanonicalRecord(t *testing.T) {
	entry := bank.LedgerEntry{
		OperationID: goldenRecord.OperationID,
		PaymentID:   goldenRecord.PaymentID,
		AccountID:   goldenRecord.AccountID,
		EntryType:   goldenRecord.EntryType,
		AmountPaise: goldenRecord.AmountPaise,
		Currency:    goldenRecord.Currency,
		OccurredAt:  goldenRecord.OccurredAt,
	}
	a := bank.LedgerSnapshot{BankID: "BANK-A", SnapshotID: uuid.New(), CapturedAt: time.Now(), Entries: []bank.LedgerEntry{entry}}
	b := bank.LedgerSnapshot{BankID: "BANK-B", SnapshotID: uuid.New(), CapturedAt: time.Now().Add(time.Hour), Entries: []bank.LedgerEntry{entry}}
	left, err := LeafHash(FromLedgerEntry(a.Entries[0]))
	if err != nil {
		t.Fatal(err)
	}
	right, err := LeafHash(FromLedgerEntry(b.Entries[0]))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(left, right) {
		t.Fatal("snapshot metadata changed canonical hash input")
	}
}

func TestBilateralParticipantsProduceIdenticalCanonicalBytesAndHash(t *testing.T) {
	operationID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	paymentID := uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	accountID := uuid.MustParse("cccccccc-cccc-4ccc-8ccc-cccccccccccc")
	bankAEntry := bank.LedgerEntry{
		OperationID: operationID,
		PaymentID:   paymentID,
		AccountID:   accountID,
		EntryType:   "FINAL_CREDIT",
		AmountPaise: 9001,
		Currency:    "INR",
		OccurredAt:  time.Date(2026, 9, 20, 12, 0, 0, 123456789, time.FixedZone("IST", 5*60*60)),
	}
	bankBEntry := bank.LedgerEntry{
		OperationID: operationID,
		PaymentID:   paymentID,
		AccountID:   accountID,
		EntryType:   "FINAL_CREDIT",
		AmountPaise: 9001,
		Currency:    "INR",
		OccurredAt:  time.Date(2026, 9, 20, 6, 30, 0, 123456789, time.FixedZone("UTC_MINUS_30", -30*60)),
	}
	bankASnapshot := bank.LedgerSnapshot{BankID: "BANK-A", SnapshotID: uuid.New(), CapturedAt: time.Now(), Entries: []bank.LedgerEntry{bankAEntry}}
	bankBSnapshot := bank.LedgerSnapshot{BankID: "BANK-B", SnapshotID: uuid.New(), CapturedAt: time.Now().Add(24 * time.Hour), Entries: []bank.LedgerEntry{bankBEntry}}
	bankARecord := FromLedgerEntry(bankASnapshot.Entries[0])
	bankBRecord := CanonicalRecord{
		OperationID: bankBSnapshot.Entries[0].OperationID,
		PaymentID:   bankBSnapshot.Entries[0].PaymentID,
		AccountID:   bankBSnapshot.Entries[0].AccountID,
		EntryType:   bankBSnapshot.Entries[0].EntryType,
		AmountPaise: bankBSnapshot.Entries[0].AmountPaise,
		Currency:    bankBSnapshot.Entries[0].Currency,
		OccurredAt:  bankBSnapshot.Entries[0].OccurredAt,
	}
	bytesA, err := CanonicalBytes(bankARecord)
	if err != nil {
		t.Fatal(err)
	}
	bytesB, err := CanonicalBytes(bankBRecord)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytesA, bytesB) {
		t.Fatalf("bilateral canonical bytes differ: A=%x B=%x", bytesA, bytesB)
	}
	hashA, err := LeafHash(bankARecord)
	if err != nil {
		t.Fatal(err)
	}
	hashB, err := LeafHash(bankBRecord)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hashA, hashB) {
		t.Fatalf("bilateral leaf hashes differ: A=%x B=%x", hashA, hashB)
	}
}

func TestTimestampNormalization(t *testing.T) {
	local := goldenRecord
	local.OccurredAt = time.Date(2026, 9, 20, 12, 34, 56, 123456789, time.FixedZone("OTHER", 5*60*60))
	utc := goldenRecord
	utc.OccurredAt = time.Date(2026, 9, 20, 7, 34, 56, 123456789, time.UTC)
	left, err := CanonicalBytes(local)
	if err != nil {
		t.Fatal(err)
	}
	right, err := CanonicalBytes(utc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(left, right) {
		t.Fatal("equivalent instants in different zones serialized differently")
	}
}

func TestLogicalChangesChangeCanonicalBytesAndHash(t *testing.T) {
	base, err := CanonicalBytes(goldenRecord)
	if err != nil {
		t.Fatal(err)
	}
	baseHash, err := LeafHash(goldenRecord)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []CanonicalRecord{
		func() CanonicalRecord { v := goldenRecord; v.OperationID = uuid.New(); return v }(),
		func() CanonicalRecord { v := goldenRecord; v.PaymentID = uuid.New(); return v }(),
		func() CanonicalRecord { v := goldenRecord; v.AccountID = uuid.New(); return v }(),
		func() CanonicalRecord { v := goldenRecord; v.AmountPaise++; return v }(),
		func() CanonicalRecord { v := goldenRecord; v.EntryType = "RELEASE"; return v }(),
		func() CanonicalRecord { v := goldenRecord; v.Currency = "USD"; return v }(),
		func() CanonicalRecord { v := goldenRecord; v.OccurredAt = v.OccurredAt.Add(time.Nanosecond); return v }(),
	}
	for i, mutation := range mutations {
		encoded, err := CanonicalBytes(mutation)
		if err != nil {
			t.Fatalf("mutation %d: %v", i, err)
		}
		if bytes.Equal(base, encoded) {
			t.Fatalf("mutation %d did not change canonical bytes", i)
		}
		hash, err := LeafHash(mutation)
		if err != nil {
			t.Fatalf("mutation %d hash: %v", i, err)
		}
		if bytes.Equal(baseHash, hash) {
			t.Fatalf("mutation %d did not change leaf hash", i)
		}
	}
}

func TestSortRecordsUsesAllLogicalTieBreakers(t *testing.T) {
	when := time.Date(2026, 9, 20, 7, 0, 0, 0, time.UTC)
	operationID := uuid.MustParse("55555555-5555-4555-8555-555555555555")
	accountLow := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	accountHigh := uuid.MustParse("ffffffff-ffff-4fff-8fff-ffffffffffff")
	paymentLow := uuid.MustParse("00000000-0000-4000-8000-000000000002")
	paymentHigh := uuid.MustParse("ffffffff-ffff-4fff-8fff-fffffffffff0")
	records := []CanonicalRecord{
		{OperationID: operationID, AccountID: accountHigh, PaymentID: paymentHigh, EntryType: "ENTRY", AmountPaise: 20, Currency: "ZAR", OccurredAt: when},
		{OperationID: operationID, AccountID: accountLow, PaymentID: paymentHigh, EntryType: "ENTRY", AmountPaise: 20, Currency: "ZAR", OccurredAt: when},
		{OperationID: operationID, AccountID: accountLow, PaymentID: paymentLow, EntryType: "ENTRY", AmountPaise: 20, Currency: "ZAR", OccurredAt: when},
		{OperationID: operationID, AccountID: accountLow, PaymentID: paymentLow, EntryType: "ENTRY", AmountPaise: 10, Currency: "ZAR", OccurredAt: when},
		{OperationID: operationID, AccountID: accountLow, PaymentID: paymentLow, EntryType: "ENTRY", AmountPaise: 10, Currency: "INR", OccurredAt: when},
	}
	ordered := SortRecords([]CanonicalRecord{records[0], records[1], records[2], records[3], records[4]})
	want := []CanonicalRecord{records[4], records[3], records[2], records[1], records[0]}
	for i := range want {
		if !bytes.Equal(mustCanonicalBytes(t, ordered[i]), mustCanonicalBytes(t, want[i])) {
			t.Fatalf("tie-break order at index %d = %+v, want %+v", i, ordered[i], want[i])
		}
	}
	reversed := SortRecords([]CanonicalRecord{records[4], records[3], records[2], records[1], records[0]})
	for i := range ordered {
		if !bytes.Equal(mustCanonicalBytes(t, ordered[i]), mustCanonicalBytes(t, reversed[i])) {
			t.Fatalf("input order changed full tie-break ordering at index %d", i)
		}
	}
}

func TestSortRecordsIsDeterministicAndIndependentOfInputOrder(t *testing.T) {
	first := goldenRecord
	second := goldenRecord
	second.OperationID = uuid.MustParse("00000000-0000-4000-8000-000000000001")
	second.OccurredAt = first.OccurredAt.Add(-time.Nanosecond)
	third := goldenRecord
	third.OperationID = uuid.MustParse("ffffffff-ffff-4fff-8fff-ffffffffffff")
	third.OccurredAt = first.OccurredAt
	third.EntryType = "HOLD"
	ordered := SortRecords([]CanonicalRecord{third, first, second})
	if len(ordered) != 3 || ordered[0].OperationID != second.OperationID || ordered[1].EntryType != "FINAL_CREDIT" || ordered[2].OperationID != third.OperationID {
		t.Fatalf("unexpected canonical order: %+v", ordered)
	}
	reordered := SortRecords([]CanonicalRecord{second, third, first})
	for i := range ordered {
		left, err := CanonicalBytes(ordered[i])
		if err != nil {
			t.Fatal(err)
		}
		right, err := CanonicalBytes(reordered[i])
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(left, right) {
			t.Fatalf("input order changed canonical order at index %d", i)
		}
	}
}

func TestSortRecordsEmptyAndSmallSets(t *testing.T) {
	empty := SortRecords(nil)
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty canonical set = %#v, want non-nil empty slice", empty)
	}
	one := SortRecords([]CanonicalRecord{goldenRecord})
	if len(one) != 1 || !bytes.Equal(mustCanonicalBytes(t, one[0]), mustCanonicalBytes(t, goldenRecord)) {
		t.Fatalf("one-record canonical set changed: %+v", one)
	}
}

func mustCanonicalBytes(t *testing.T, record CanonicalRecord) []byte {
	t.Helper()
	encoded, err := CanonicalBytes(record)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
