package reconciliation

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

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
	_, err = pool.Exec(ctx, `INSERT INTO bank_a.operations (id, payment_id, operation_id, idempotency_key, operation_type, account_id, amount_paise, currency, status) VALUES ($1, $2, $3, $4, 'HOLD', $5, 275, 'INR', 'ACTIVE')`, operationRowID, paymentID, operationID, "m3-4-"+operationID.String(), accountID)
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
