package bankservice

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/bank"
)

func newTestBankPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	return pool
}

func setupTestAccount(t *testing.T, pool *pgxpool.Pool, schema string, initialBalance int64) uuid.UUID {
	t.Helper()
	accountID := uuid.New()
	_, err := pool.Exec(context.Background(), "INSERT INTO "+schema+".accounts (id, account_number, balance_paise, status) VALUES ($1, $2, $3, 'ACTIVE')",
		accountID, "ACC-"+accountID.String()[:12], initialBalance)
	if err != nil {
		t.Fatalf("failed to insert test account: %v", err)
	}
	return accountID
}

func TestPresence_AuthoritativeAbsentAndPresent_RealDB(t *testing.T) {
	pool := newTestBankPool(t)
	defer pool.Close()

	svc := NewParticipantService(pool, "BANK-A", "bank_a")
	server := httptest.NewServer(Handler(svc))
	defer server.Close()

	client, err := bank.NewHTTPClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewHTTPClient failed: %v", err)
	}

	ctx := context.Background()
	paymentID := uuid.New()
	operationID := uuid.New()

	// 1. Authoritative Absent: Operation was never recorded
	absentRes, err := client.GetOperationPresence(ctx, paymentID, operationID)
	if err != nil {
		t.Fatalf("unexpected error on absent presence check: %v", err)
	}
	if absentRes.Presence != bank.PresenceAbsent {
		t.Fatalf("expected presence absent, got %v", absentRes.Presence)
	}
	if absentRes.Status != nil {
		t.Fatalf("expected status nil for absent operation, got %v", absentRes.Status)
	}
	if absentRes.PaymentID != paymentID || absentRes.OperationID != operationID {
		t.Fatalf("mismatched IDs in presence result")
	}

	// 2. Authoritative Present: Execute a hold, then verify presence
	accountID := setupTestAccount(t, pool, "bank_a", 10000)
	holdRes, err := client.HoldFunds(ctx, bank.HoldFundsRequest{
		OperationRequest: bank.OperationRequest{
			PaymentID:      paymentID,
			OperationID:    operationID,
			IdempotencyKey: operationID.String(),
			AccountID:      accountID,
			AmountPaise:    2500,
			Currency:       "INR",
		},
	})
	if err != nil {
		t.Fatalf("HoldFunds failed: %v", err)
	}
	if holdRes.Status != bank.OperationSucceeded {
		t.Fatalf("expected hold success, got %v", holdRes.Status)
	}

	presentRes, err := client.GetOperationPresence(ctx, paymentID, operationID)
	if err != nil {
		t.Fatalf("unexpected error on present presence check: %v", err)
	}
	if presentRes.Presence != bank.PresencePresent {
		t.Fatalf("expected presence present, got %v", presentRes.Presence)
	}
	if presentRes.Status == nil || *presentRes.Status != bank.OperationSucceeded {
		t.Fatalf("expected status succeeded for present operation, got %v", presentRes.Status)
	}

	// 3. Mismatched Payment ID rejected
	diffPaymentID := uuid.New()
	_, err = client.GetOperationPresence(ctx, diffPaymentID, operationID)
	if err == nil {
		t.Fatal("expected error on mismatched payment ID, got nil")
	}
}

func TestPresence_ServiceTokenAuthentication(t *testing.T) {
	pool := newTestBankPool(t)
	defer pool.Close()

	svc := NewParticipantService(pool, "BANK-A", "bank_a")
	svc.SetServiceToken("secret-participant-token-xyz")
	server := httptest.NewServer(Handler(svc))
	defer server.Close()

	client, err := bank.NewHTTPClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewHTTPClient failed: %v", err)
	}

	ctx := context.Background()
	paymentID := uuid.New()
	operationID := uuid.New()

	// 1. Unauthenticated request must return 401 Unauthorized
	_, err = client.GetOperationPresence(ctx, paymentID, operationID)
	if err == nil {
		t.Fatal("expected unauthorized error when no token is sent, got nil")
	}

	// 2. Request with invalid token must return 401 Unauthorized
	client.SetServiceToken("wrong-token")
	_, err = client.GetOperationPresence(ctx, paymentID, operationID)
	if err == nil {
		t.Fatal("expected unauthorized error when wrong token is sent, got nil")
	}

	// 3. Request with valid token must succeed
	client.SetServiceToken("secret-participant-token-xyz")
	res, err := client.GetOperationPresence(ctx, paymentID, operationID)
	if err != nil {
		t.Fatalf("expected success with valid token, got: %v", err)
	}
	if res.Presence != bank.PresenceAbsent {
		t.Fatalf("expected absent, got %v", res.Presence)
	}
}

func TestPresence_ConflictingPayloadRejected_RealDB(t *testing.T) {
	pool := newTestBankPool(t)
	defer pool.Close()

	svc := NewParticipantService(pool, "BANK-A", "bank_a")
	ctx := context.Background()
	accountID := setupTestAccount(t, pool, "bank_a", 10000)
	paymentID := uuid.New()
	operationID := uuid.New()

	// Execute original hold for 3000 paise
	_, err := svc.HoldFunds(ctx, bank.HoldFundsRequest{
		OperationRequest: bank.OperationRequest{
			PaymentID:      paymentID,
			OperationID:    operationID,
			IdempotencyKey: operationID.String(),
			AccountID:      accountID,
			AmountPaise:    3000,
			Currency:       "INR",
		},
	})
	if err != nil {
		t.Fatalf("HoldFunds failed: %v", err)
	}

	// Replaying with identical payload succeeds idempotently
	exactRetry, err := svc.HoldFunds(ctx, bank.HoldFundsRequest{
		OperationRequest: bank.OperationRequest{
			PaymentID:      paymentID,
			OperationID:    operationID,
			IdempotencyKey: operationID.String(),
			AccountID:      accountID,
			AmountPaise:    3000,
			Currency:       "INR",
		},
	})
	if err != nil {
		t.Fatalf("expected idempotent success on exact retry, got: %v", err)
	}
	if exactRetry.Status != bank.OperationSucceeded {
		t.Fatalf("expected succeeded status, got %v", exactRetry.Status)
	}

	// Replaying with same OperationID but conflicting amount must be rejected permanently
	_, conflictErr := svc.HoldFunds(ctx, bank.HoldFundsRequest{
		OperationRequest: bank.OperationRequest{
			PaymentID:      paymentID,
			OperationID:    operationID,
			IdempotencyKey: operationID.String(),
			AccountID:      accountID,
			AmountPaise:    5000, // Conflict!
			Currency:       "INR",
		},
	})
	if conflictErr == nil {
		t.Fatal("expected conflict error when reusing OperationID with different payload, got nil")
	}
	adapterErr, ok := conflictErr.(*bank.AdapterError)
	if !ok || adapterErr.Code != bank.ErrCodePermanentFailure {
		t.Fatalf("expected permanent failure code, got %v", conflictErr)
	}
}

func TestPresence_DelayedOriginalRacingReplay_RealDB(t *testing.T) {
	pool := newTestBankPool(t)
	defer pool.Close()

	svc := NewParticipantService(pool, "BANK-A", "bank_a")
	ctx := context.Background()
	accountID := setupTestAccount(t, pool, "bank_a", 10000)
	paymentID := uuid.New()
	operationID := uuid.New()

	// Simulate two concurrent requests with identical payload racing to the bank
	// (e.g. replay arriving concurrently with delayed in-flight original)
	req := bank.HoldFundsRequest{
		OperationRequest: bank.OperationRequest{
			PaymentID:      paymentID,
			OperationID:    operationID,
			IdempotencyKey: operationID.String(),
			AccountID:      accountID,
			AmountPaise:    4000,
			Currency:       "INR",
		},
	}

	done := make(chan error, 2)
	go func() {
		_, err := svc.HoldFunds(ctx, req)
		done <- err
	}()
	go func() {
		_, err := svc.HoldFunds(ctx, req)
		done <- err
	}()

	for i := 0; i < 2; i++ {
		err := <-done
		if err != nil {
			t.Fatalf("racing hold execution failed: %v", err)
		}
	}

	// Verify balance debited exactly once: 10000 - 4000 = 6000
	var balance int64
	err := pool.QueryRow(ctx, "SELECT balance_paise FROM bank_a.accounts WHERE id = $1", accountID).Scan(&balance)
	if err != nil {
		t.Fatalf("failed to query balance: %v", err)
	}
	if balance != 6000 {
		t.Fatalf("expected balance 6000, got %d (double debit occurred!)", balance)
	}

	// Verify exactly one ledger entry
	var ledgerCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM bank_a.ledger_entries WHERE operation_id = $1", operationID).Scan(&ledgerCount)
	if err != nil {
		t.Fatalf("failed to query ledger count: %v", err)
	}
	if ledgerCount != 1 {
		t.Fatalf("expected 1 ledger entry, got %d", ledgerCount)
	}
}
