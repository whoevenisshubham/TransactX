package reconciliation

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/bankservice"
)

func TestProjectedCentralLedgerMatchesParticipantRecords(t *testing.T) {
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

	userID, bankID := uuid.New(), uuid.New()
	sourceAccount, destinationAccount := uuid.New(), uuid.New()
	paymentID := uuid.New()
	holdID, confirmID, creditID, finalizeID, failedReleaseID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	suffix := uuid.NewString()
	base := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	scope := bank.LedgerScope{From: base.Add(-time.Hour), To: base.Add(time.Hour)}

	if _, err := pool.Exec(ctx, `INSERT INTO users (id, name, phone, upi_id, password_hash, role) VALUES ($1, 'Projection User', $2, $3, 'hash', 'CUSTOMER')`, userID, suffix[:20], suffix+"@projection"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Projection Bank', 'ACTIVE')`, bankID, "PROJ-"+suffix[:20]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO accounts (id, user_id, bank_id, bank_account_id, account_number, balance_paise, status) VALUES ($1,$2,$3,$1,$4,1000,'ACTIVE'),($5,$2,$3,$5,$6,0,'ACTIVE')`, sourceAccount, userID, bankID, "central-source-"+suffix, destinationAccount, "central-dest-"+suffix); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state, source_bank_id, destination_bank_id, source_bank_account_id, destination_bank_account_id, created_at, completed_at) VALUES ($1,$2,$3,$4,100,'INR','COMPLETED',$5,$5,$3,$4,$6,$6)`, paymentID, userID, sourceAccount, destinationAccount, bankID, base); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO bank_a.accounts (id, account_number, balance_paise, status) VALUES ($1,$2,900,'ACTIVE'),($3,$4,100,'ACTIVE')`, sourceAccount, "participant-source-"+suffix, destinationAccount, "participant-dest-"+suffix); err != nil {
		t.Fatal(err)
	}
	participantOps := []struct {
		id, operation uuid.UUID
		kind, status  string
		account       uuid.UUID
		occurred      time.Time
		entry         string
	}{
		{uuid.New(), holdID, "HOLD", "CONFIRMED", sourceAccount, base.Add(10 * time.Minute), "HOLD"},
		{uuid.New(), creditID, "PROVISIONAL_CREDIT", "FINAL", destinationAccount, base.Add(20 * time.Minute), "PROVISIONAL_CREDIT"},
		{uuid.New(), finalizeID, "CONFIRM_HOLD", "FINAL", destinationAccount, base.Add(30 * time.Minute), "FINAL_CREDIT"},
	}
	for _, operation := range participantOps {
		if _, err := pool.Exec(ctx, `INSERT INTO bank_a.operations (id, bank_id, payment_id, operation_id, idempotency_key, operation_type, account_id, amount_paise, currency, status) VALUES ($1,'BANK-A',$2,$3,$4,$5,$6,100,'INR',$7)`, operation.id, paymentID, operation.operation, operation.operation.String(), operation.kind, operation.account, operation.status); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO bank_a.ledger_entries (id, operation_id, payment_id, account_id, entry_type, amount_paise, currency, occurred_at) VALUES ($1,$2,$3,$4,$5,100,'INR',$6)`, uuid.New(), operation.operation, paymentID, operation.account, operation.entry, operation.occurred); err != nil {
			t.Fatal(err)
		}
	}
	centralOps := []struct {
		operation uuid.UUID
		kind, status string
		account uuid.UUID
		created time.Time
	}{
		{holdID, "HOLD", "SUCCEEDED", sourceAccount, base.Add(time.Minute)},
		{creditID, "PROVISIONAL_CREDIT", "SUCCEEDED", destinationAccount, base.Add(2*time.Minute)},
		{confirmID, "CONFIRM_SOURCE_HOLD", "SUCCEEDED", sourceAccount, base.Add(3*time.Minute)},
		{finalizeID, "FINALIZE_CREDIT", "SUCCEEDED", destinationAccount, base.Add(4*time.Minute)},
		{failedReleaseID, "RELEASE_HOLD", "FAILED", sourceAccount, base.Add(5*time.Minute)},
	}
	for _, operation := range centralOps {
		if _, err := pool.Exec(ctx, `INSERT INTO payment_bank_operations (id,payment_id,bank_id,operation_id,operation_type,status,account_id,idempotency_key,amount_paise,currency,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,100,'INR',$9)`, uuid.New(), paymentID, bankID, operation.operation, operation.kind, operation.status, operation.account, operation.operation.String(), operation.created); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM bank_a.ledger_entries WHERE payment_id = $1`, paymentID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM bank_a.operations WHERE payment_id = $1`, paymentID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM bank_a.accounts WHERE id IN ($1,$2)`, sourceAccount, destinationAccount)
		_, _ = pool.Exec(context.Background(), `DELETE FROM payment_bank_operations WHERE payment_id = $1`, paymentID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM payments WHERE id = $1`, paymentID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE id IN ($1,$2)`, sourceAccount, destinationAccount)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM banks WHERE id = $1`, bankID)
	})

	participant := bankservice.NewParticipantService(pool, "BANK-A", "bank_a")
	participantSnapshot, err := participant.GetLedgerSnapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := NewProjectedCentralLedgerSnapshotSource(pool, "PROJ-"+suffix[:20], participant).GetLedgerSnapshot(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(projected.Entries) != 3 || len(participantSnapshot.Entries) != 3 {
		t.Fatalf("projection cardinality central=%d participant=%d", len(projected.Entries), len(participantSnapshot.Entries))
	}
	participantByID := make(map[uuid.UUID]bank.LedgerEntry, len(participantSnapshot.Entries))
	for _, entry := range participantSnapshot.Entries {
		participantByID[entry.OperationID] = entry
	}
	for _, entry := range projected.Entries {
		observed, ok := participantByID[entry.OperationID]
		if !ok || entry.EntryType != observed.EntryType || entry.AccountID != observed.AccountID || entry.AmountPaise != observed.AmountPaise || !entry.OccurredAt.Equal(observed.OccurredAt) {
			t.Fatalf("projected record does not match participant: %+v / %+v", entry, observed)
		}
	}
}
