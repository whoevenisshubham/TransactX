package reconciliation

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/bankservice"
)

func TestRepositoryParticipantUsesPersistedParticipantLedger(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	var available bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('bank_a.ledger_entries') IS NOT NULL`).Scan(&available); err != nil {
		t.Fatal(err)
	}
	if !available {
		t.Skip("bank_a ledger schema is not available")
	}

	accountID := uuid.New()
	operationRowID := uuid.New()
	operationID := uuid.New()
	paymentID := uuid.New()
	occurredAt := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	accountNumber := "m3-4-" + accountID.String()
	_, err = pool.Exec(ctx, `INSERT INTO bank_a.accounts (id, account_number, balance_paise, status) VALUES ($1, $2, 10000, 'ACTIVE')`, accountID, accountNumber)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM bank_a.ledger_entries WHERE operation_id = $1`, operationID)
		_, _ = pool.Exec(ctx, `DELETE FROM bank_a.operations WHERE operation_id = $1`, operationID)
		_, _ = pool.Exec(ctx, `DELETE FROM bank_a.accounts WHERE id = $1`, accountID)
	}()
	_, err = pool.Exec(ctx, `INSERT INTO bank_a.operations (id, bank_id, payment_id, operation_id, idempotency_key, operation_type, account_id, amount_paise, currency, status) VALUES ($1, 'BANK-A', $2, $3, $4, 'HOLD', $5, 275, 'INR', 'ACTIVE')`, operationRowID, paymentID, operationID, "m3-4-"+operationID.String(), accountID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO bank_a.ledger_entries (id, operation_id, payment_id, account_id, entry_type, amount_paise, currency, occurred_at) VALUES ($1, $2, $3, $4, 'HOLD', 275, 'INR', $5)`, uuid.New(), operationID, paymentID, accountID, occurredAt)
	if err != nil {
		t.Fatal(err)
	}

	participant := bankservice.NewParticipantService(pool, "BANK-A", "bank_a")
	repository, err := NewRepositoryParticipant(participant, "BANK-A", "ledger", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{From: occurredAt.Add(-time.Hour), To: occurredAt.Add(time.Hour)}
	if err := repository.Initialize(ctx, scope); err != nil {
		t.Fatal(err)
	}
	root, err := repository.GetRoot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := repository.GetMetadata(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.RecordCount != 1 || metadata.CapturedAt.IsZero() {
		t.Fatalf("persisted participant metadata = %+v", metadata)
	}
	entry := participantFixtureRecord(0, occurredAt)
	entry.OperationID, entry.PaymentID, entry.AccountID = operationID, paymentID, accountID
	entry.AmountPaise = 275
	memory, err := NewMemoryParticipant("fixture", "ledger", time.Hour, []CanonicalRecord{entry})
	if err != nil {
		t.Fatal(err)
	}
	expected, err := memory.GetRoot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if string(root.Root) != string(expected.Root) {
		t.Fatalf("repository root differs from equivalent logical fixture: %x vs %x", root.Root, expected.Root)
	}
}

func TestRepositoryParticipantRestartRestoration(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	// Prepare records and initial participant
	records := participantRecords()
	entries := make([]bank.LedgerEntry, 0, len(records))
	for _, record := range records {
		entries = append(entries, bank.LedgerEntry{OperationID: record.OperationID, PaymentID: record.PaymentID, AccountID: record.AccountID, EntryType: record.EntryType, AmountPaise: record.AmountPaise, Currency: record.Currency, OccurredAt: record.OccurredAt})
	}
	snapshot := bank.LedgerSnapshot{BankID: "BANK-RESTART", CapturedAt: time.Date(2026, 1, 2, 2, 0, 0, 0, time.UTC), Entries: entries}
	sourceCalls := 0
	source := &fakeSnapshotSource{snapshot: snapshot, calls: &sourceCalls}

	// Create Postgres-backed commitment store
	store := NewPostgresIncrementalCommitmentStore(pool)

	// Ensure clean state (delete old states if they exist for this test)
	// We'll use a unique scope to avoid collisions
	scope := participantScope()

	// Run 1: Initialize
	participant1, err := NewRepositoryParticipantWithCommitmentStore(source, "BANK-RESTART", "ledger", time.Hour, store)
	if err != nil {
		t.Fatal(err)
	}
	if err := participant1.Initialize(ctx, scope); err != nil {
		t.Fatal(err)
	}

	root1, err := participant1.GetRoot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	generationG1 := root1.Ref.Generation
	if generationG1 == "" {
		t.Fatal("Generation G1 is empty")
	}

	// Ensure it persisted to PG via store.SaveState inside Initialize.
	// Now, create a fresh participant (simulating a restart)
	sourceCalls = 0 // Reset calls

	// Run 2: Restart using a fresh commitment store instance
	store2 := NewPostgresIncrementalCommitmentStore(pool)
	participant2, err := NewRepositoryParticipantWithCommitmentStore(source, "BANK-RESTART", "ledger", time.Hour, store2)
	if err != nil {
		t.Fatal(err)
	}

	// GetRoot should load from PostgreSQL without calling source
	root2, err := participant2.GetRoot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	metadata2, err := participant2.GetMetadata(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}

	// Assert equality between participant1's persisted state and participant2's restored state
	if root2.Ref.Generation != generationG1 {
		t.Fatalf("Generation mismatch on restart: got %q, want %q", root2.Ref.Generation, generationG1)
	}
	if string(root2.Root) != string(root1.Root) {
		t.Fatal("Root hash mismatch on restart")
	}
	if root2.Ref.ScopeID != root1.Ref.ScopeID {
		t.Fatalf("Scope mismatch on restart: got %q, want %q", root2.Ref.ScopeID, root1.Ref.ScopeID)
	}
	if root2.Version != root1.Version {
		t.Fatalf("CanonicalVersion mismatch on restart: got %q, want %q", root2.Version, root1.Version)
	}
	if root2.Algorithm != root1.Algorithm {
		t.Fatalf("AlgorithmVersion mismatch on restart: got %q, want %q", root2.Algorithm, root1.Algorithm)
	}
	if participant2.BucketWidth() != participant1.BucketWidth() {
		t.Fatalf("BucketWidth mismatch on restart: got %s, want %s", participant2.BucketWidth(), participant1.BucketWidth())
	}

	metadata1, err := participant1.GetMetadata(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if metadata2.RecordCount != metadata1.RecordCount {
		t.Fatalf("RecordCount mismatch on restart: got %d, want %d", metadata2.RecordCount, metadata1.RecordCount)
	}

	if sourceCalls != 0 {
		t.Fatalf("Expected 0 ledger rebuilds on restart, got %d", sourceCalls)
	}
}

func TestDurableOwnerNamespacesRestartAndStaleGeneration(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	participantID := "BANK-DURABLE-" + uuid.NewString()[:12]
	scope := participantScope()
	records := participantRecords()
	entries := make([]bank.LedgerEntry, len(records))
	for i, record := range records {
		entries[i] = bank.LedgerEntry{
			OperationID: record.OperationID, PaymentID: record.PaymentID, AccountID: record.AccountID,
			EntryType: record.EntryType, AmountPaise: record.AmountPaise, Currency: record.Currency, OccurredAt: record.OccurredAt,
		}
	}
	sourceCalls := 0
	source := fakeSnapshotSource{snapshot: bank.LedgerSnapshot{BankID: participantID, CapturedAt: time.Now().UTC(), Entries: entries}, calls: &sourceCalls}
	canonicalOwner := "canonical:" + participantID
	participantOwner := "participant:" + participantID
	canonicalMaintainer, err := NewDurableRepositoryParticipant(pool, source, participantID, canonicalOwner, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	participantMaintainer, err := NewDurableRepositoryParticipant(pool, source, participantID, participantOwner, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := canonicalMaintainer.Initialize(ctx, scope); err != nil {
		t.Fatal(err)
	}
	if err := participantMaintainer.Initialize(ctx, scope); err != nil {
		t.Fatal(err)
	}
	canonicalRoot, _ := canonicalMaintainer.GetRoot(ctx, scope)
	participantRoot, _ := participantMaintainer.GetRoot(ctx, scope)
	if !bytes.Equal(canonicalRoot.Root, participantRoot.Root) {
		t.Fatalf("healthy maintained roots differ: %x != %x", canonicalRoot.Root, participantRoot.Root)
	}

	sourceCalls = 0
	canonicalReader, _ := NewDurableRepositoryParticipant(pool, source, participantID, canonicalOwner, time.Hour)
	participantReader, _ := NewDurableRepositoryParticipant(pool, source, participantID, participantOwner, time.Hour)
	restartedRoot, err := canonicalReader.GetRoot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if restartedRoot.Ref.Generation != canonicalRoot.Ref.Generation || !bytes.Equal(restartedRoot.Root, canonicalRoot.Root) {
		t.Fatal("durable canonical state changed after participant recreation")
	}
	if _, err := participantReader.GetRoot(ctx, scope); err != nil {
		t.Fatal(err)
	}
	if sourceCalls != 0 {
		t.Fatalf("normal reads called authoritative sources %d times", sourceCalls)
	}

	engine := NewEngine(KnownParticipants{participantID: true}, NewRunRepository(pool),
		func(context.Context, string, Scope) (ReconciliationParticipant, error) { return canonicalReader, nil },
		func(context.Context, string, Scope) (ReconciliationParticipant, error) { return participantReader, nil },
	)
	run, err := engine.Execute(ctx, RunRequest{ParticipantID: participantID, ScopeFrom: scope.From, ScopeTo: scope.To})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM recon_discrepancies WHERE run_id = $1`, run.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM recon_runs WHERE id = $1`, run.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM merkle_commitments WHERE owner_id IN ($1, $2)`, canonicalOwner, participantOwner)
	})
	if run.DiscrepancyCount != 0 || sourceCalls != 0 {
		t.Fatalf("healthy reconciliation = discrepancies %d, source calls %d", run.DiscrepancyCount, sourceCalls)
	}

	updated := append([]bank.LedgerEntry(nil), entries...)
	extra := participantFixtureRecord(9, scope.From.Add(45*time.Minute))
	updated = append(updated, bank.LedgerEntry{OperationID: extra.OperationID, PaymentID: extra.PaymentID, AccountID: extra.AccountID, EntryType: extra.EntryType, AmountPaise: extra.AmountPaise, Currency: extra.Currency, OccurredAt: extra.OccurredAt})
	refreshSource := fakeSnapshotSource{snapshot: bank.LedgerSnapshot{BankID: participantID, CapturedAt: time.Now().UTC(), Entries: updated}}
	refresher, _ := NewDurableRepositoryParticipant(pool, refreshSource, participantID, canonicalOwner, time.Hour)
	if err := refresher.Initialize(ctx, scope); err != nil {
		t.Fatal(err)
	}
	_, err = canonicalReader.GetChildren(ctx, canonicalRoot.Ref)
	if !errors.Is(err, ErrStaleReference) {
		t.Fatalf("old generation error = %v, want ErrStaleReference", err)
	}
}
