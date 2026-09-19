package payments

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCreateIdempotentConcurrentRequestsCreateOnePayment(t *testing.T) {
	payload := newRepositoryTestData(t)
	defer payload.close(t)

	const requests = 8
	results := make(chan struct {
		payment Payment
		dup     bool
		err     error
	}, requests)
	var waitGroup sync.WaitGroup
	for range requests {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			payment, duplicate, err := payload.repository.CreateIdempotent(context.Background(), Payment{
				ID: uuid.New(), InitiatedByUserID: payload.userID, SenderAccountID: payload.sourceID,
				ReceiverAccountID: payload.receiverID, AmountPaise: 100, Currency: "INR", State: StateCreated,
			}, "concurrent-key", "same-hash")
			results <- struct {
				payment Payment
				dup     bool
				err     error
			}{payment, duplicate, err}
		}()
	}
	waitGroup.Wait()
	close(results)

	var paymentID uuid.UUID
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if paymentID == uuid.Nil {
			paymentID = result.payment.ID
		} else if result.payment.ID != paymentID {
			t.Fatalf("duplicate returned payment %s, want %s", result.payment.ID, paymentID)
		}
	}
	var paymentCount, recordCount int
	if err := payload.pool.QueryRow(context.Background(), `SELECT count(*) FROM payments WHERE initiated_by_user_id = $1`, payload.userID).Scan(&paymentCount); err != nil {
		t.Fatal(err)
	}
	if err := payload.pool.QueryRow(context.Background(), `SELECT count(*) FROM idempotency_records WHERE user_id = $1 AND key = 'concurrent-key'`, payload.userID).Scan(&recordCount); err != nil {
		t.Fatal(err)
	}
	if paymentCount != 1 || recordCount != 1 {
		t.Fatalf("counts = payments %d, idempotency records %d; want 1, 1", paymentCount, recordCount)
	}
	var linkedPaymentID uuid.UUID
	if err := payload.pool.QueryRow(context.Background(), `SELECT payment_id FROM idempotency_records WHERE user_id = $1 AND key = 'concurrent-key'`, payload.userID).Scan(&linkedPaymentID); err != nil {
		t.Fatal(err)
	}
	if linkedPaymentID != paymentID {
		t.Fatalf("idempotency record points to %s, want %s", linkedPaymentID, paymentID)
	}
}

func TestCreateIdempotentConflictRollsBackPayment(t *testing.T) {
	payload := newRepositoryTestData(t)
	defer payload.close(t)

	original, _, err := payload.repository.CreateIdempotent(context.Background(), Payment{
		ID: uuid.New(), InitiatedByUserID: payload.userID, SenderAccountID: payload.sourceID,
		ReceiverAccountID: payload.receiverID, AmountPaise: 100, Currency: "INR", State: StateCreated,
	}, "rollback-key", "original-hash")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = payload.repository.CreateIdempotent(context.Background(), Payment{
		ID: uuid.New(), InitiatedByUserID: payload.userID, SenderAccountID: payload.sourceID,
		ReceiverAccountID: payload.receiverID, AmountPaise: 200, Currency: "INR", State: StateCreated,
	}, "rollback-key", "different-hash")
	if err != ErrIdempotencyConflict {
		t.Fatalf("error = %v, want ErrIdempotencyConflict", err)
	}
	var count int
	if err := payload.pool.QueryRow(context.Background(), `SELECT count(*) FROM payments WHERE initiated_by_user_id = $1`, payload.userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("payment count = %d, want 1", count)
	}
	var linkedPaymentID uuid.UUID
	if err := payload.pool.QueryRow(context.Background(), `SELECT payment_id FROM idempotency_records WHERE user_id = $1 AND key = 'rollback-key'`, payload.userID).Scan(&linkedPaymentID); err != nil {
		t.Fatal(err)
	}
	if linkedPaymentID != original.ID {
		t.Fatalf("idempotency record points to %s, want %s", linkedPaymentID, original.ID)
	}
}

type repositoryTestData struct {
	pool           *pgxpool.Pool
	repository     *Repository
	userID         uuid.UUID
	receiverUserID uuid.UUID
	bankID         uuid.UUID
	sourceID       uuid.UUID
	receiverID     uuid.UUID
}

func newRepositoryTestData(t *testing.T) repositoryTestData {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	data := repositoryTestData{pool: pool, repository: NewRepository(pool), userID: uuid.New(), receiverUserID: uuid.New(), bankID: uuid.New(), sourceID: uuid.New(), receiverID: uuid.New()}
	_, err = pool.Exec(context.Background(), `
		INSERT INTO users (id, name, phone, upi_id, password_hash, role) VALUES
		($1, 'Payer', $2, $3, 'hash', 'CUSTOMER'), ($4, 'Receiver', $5, $6, 'hash', 'CUSTOMER')`,
		data.userID, data.userID.String()[:20], "payer-"+data.userID.String(), data.receiverUserID, data.receiverUserID.String()[:20], "receiver-"+data.receiverUserID.String())
	if err != nil {
		pool.Close()
		t.Fatal(err)
	}
	_, err = pool.Exec(context.Background(), `INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Test Bank', 'ACTIVE')`, data.bankID, "TEST-"+data.bankID.String()[:20])
	if err == nil {
		_, err = pool.Exec(context.Background(), `INSERT INTO accounts (id, user_id, bank_id, account_number, status) VALUES ($1, $2, $3, $4, 'ACTIVE'), ($5, $6, $3, $7, 'ACTIVE')`, data.sourceID, data.userID, data.bankID, "source-"+data.sourceID.String(), data.receiverID, data.receiverUserID, "receiver-"+data.receiverID.String())
	}
	if err != nil {
		data.close(t)
		t.Fatal(err)
	}
	return data
}

func (data repositoryTestData) close(t *testing.T) {
	t.Helper()
	if data.pool == nil {
		return
	}
	_, _ = data.pool.Exec(context.Background(), `DELETE FROM idempotency_records WHERE user_id = $1`, data.userID)
	_, _ = data.pool.Exec(context.Background(), `DELETE FROM payment_bank_operations WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, data.userID)
	_, _ = data.pool.Exec(context.Background(), `DELETE FROM ledger_entries WHERE ledger_transaction_id IN (SELECT lt.id FROM ledger_transactions lt JOIN payments p ON p.id = lt.payment_id WHERE p.initiated_by_user_id = $1)`, data.userID)
	_, _ = data.pool.Exec(context.Background(), `DELETE FROM ledger_transactions WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, data.userID)
	_, _ = data.pool.Exec(context.Background(), `DELETE FROM payments WHERE initiated_by_user_id = $1`, data.userID)
	_, _ = data.pool.Exec(context.Background(), `DELETE FROM accounts WHERE id IN ($1, $2)`, data.sourceID, data.receiverID)
	_, _ = data.pool.Exec(context.Background(), `DELETE FROM users WHERE id IN ($1, $2)`, data.userID, data.receiverUserID)
	_, _ = data.pool.Exec(context.Background(), `DELETE FROM banks WHERE id = $1`, data.bankID)
	data.pool.Close()
}
