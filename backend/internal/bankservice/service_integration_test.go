package bankservice

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/transactx/backend/internal/bank"
)

func TestBankAHTTPIntegrationPreservesDurableOperationFlow(t *testing.T) {
	pool := newBankServiceTestPool(t)
	defer pool.Close()
	if !bankSchemaAvailable(t, pool) {
		t.Skip("M1-6 bank_a schema is not applied")
	}
	accountID, receiverID, paymentID := uuid.New(), uuid.New(), uuid.New()
	cleanupBankFixtures(t, pool, accountID, receiverID, paymentID)
	defer cleanupBankFixtures(t, pool, accountID, receiverID, paymentID)
	insertBankAccount(t, pool, accountID, "http-source", 500)
	insertBankAccount(t, pool, receiverID, "http-receiver", 0)

	server := httptest.NewServer(Handler(NewService(pool)))
	defer server.Close()
	client, err := bank.NewHTTPClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	holdID := uuid.New()
	hold, err := client.HoldFunds(context.Background(), bank.HoldFundsRequest{OperationRequest: bank.OperationRequest{PaymentID: paymentID, OperationID: holdID, IdempotencyKey: "http-hold", AccountID: accountID, AmountPaise: 200, Currency: "INR"}})
	if err != nil {
		t.Fatal(err)
	}
	creditID := uuid.New()
	if _, err := client.ProvisionalCredit(context.Background(), bank.ProvisionalCreditRequest{OperationRequest: bank.OperationRequest{PaymentID: paymentID, OperationID: creditID, IdempotencyKey: "http-credit", AccountID: receiverID, AmountPaise: 200, Currency: "INR"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ConfirmHold(context.Background(), bank.ConfirmHoldRequest{PaymentID: paymentID, OperationID: uuid.New(), IdempotencyKey: "http-confirm-hold", HoldID: hold.HoldID}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ConfirmHold(context.Background(), bank.ConfirmHoldRequest{PaymentID: paymentID, OperationID: uuid.New(), IdempotencyKey: "http-finalize-credit", HoldID: creditID}); err != nil {
		t.Fatal(err)
	}
	status, err := client.GetOperationStatus(context.Background(), bank.OperationStatusRequest{PaymentID: paymentID, OperationID: creditID})
	if err != nil || status.Status != bank.OperationSucceeded {
		t.Fatalf("HTTP operation status = %+v, err = %v", status, err)
	}
	assertBankBalance(t, pool, receiverID, 200)
}

func TestBankBIndependentDurableParticipantFlow(t *testing.T) {
	pool := newBankServiceTestPool(t)
	defer pool.Close()
	if !bankBSchemaAvailable(t, pool) {
		t.Skip("Phase 4 bank_b schema is not applied")
	}
	accountID, receiverID, paymentID := uuid.New(), uuid.New(), uuid.New()
	_, _ = pool.Exec(context.Background(), `DELETE FROM bank_b.ledger_entries WHERE payment_id = $1`, paymentID)
	_, _ = pool.Exec(context.Background(), `DELETE FROM bank_b.operations WHERE payment_id = $1`, paymentID)
	_, _ = pool.Exec(context.Background(), `DELETE FROM bank_b.accounts WHERE id IN ($1, $2)`, accountID, receiverID)
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM bank_b.ledger_entries WHERE payment_id = $1`, paymentID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM bank_b.operations WHERE payment_id = $1`, paymentID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM bank_b.accounts WHERE id IN ($1, $2)`, accountID, receiverID)
	}()
	if _, err := pool.Exec(context.Background(), `INSERT INTO bank_b.accounts (id, account_number, balance_paise, status) VALUES ($1, $2, 500, 'ACTIVE'), ($3, $4, 0, 'ACTIVE')`, accountID, "b-source-"+accountID.String(), receiverID, "b-receiver-"+receiverID.String()); err != nil {
		t.Fatal(err)
	}
	service := NewParticipantService(pool, "BANK-B", "bank_b")
	health, err := service.GetHealth(context.Background())
	if err != nil || !health.Available {
		t.Fatalf("Bank B health = %+v, err = %v", health, err)
	}
	account, err := service.ResolveAccount(context.Background(), bank.ResolveAccountRequest{AccountID: accountID})
	if err != nil || account.Status != bank.AccountActive {
		t.Fatalf("Bank B account = %+v, err = %v", account, err)
	}
	service.SetAvailable(false)
	health, err = service.GetHealth(context.Background())
	if err != nil || health.Available {
		t.Fatalf("Bank B unavailable health = %+v, err = %v", health, err)
	}
	service.SetAvailable(true)
	service.SetLatency(20 * time.Millisecond)
	latencyContext, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	_, err = service.ResolveAccount(latencyContext, bank.ResolveAccountRequest{AccountID: accountID})
	cancel()
	var latencyError *bank.AdapterError
	if !errors.As(err, &latencyError) || latencyError.Code != bank.ErrCodeTransientFailure {
		t.Fatalf("Bank B latency error = %v", err)
	}
	service.SetLatency(0)
	holdID := uuid.New()
	if _, err := service.HoldFunds(context.Background(), bank.HoldFundsRequest{OperationRequest: bank.OperationRequest{PaymentID: paymentID, OperationID: holdID, IdempotencyKey: "b-hold", AccountID: accountID, AmountPaise: 200, Currency: "INR"}}); err != nil {
		t.Fatal(err)
	}
	creditID := uuid.New()
	if _, err := service.ProvisionalCredit(context.Background(), bank.ProvisionalCreditRequest{OperationRequest: bank.OperationRequest{PaymentID: paymentID, OperationID: creditID, IdempotencyKey: "b-credit", AccountID: receiverID, AmountPaise: 200, Currency: "INR"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfirmHold(context.Background(), bank.ConfirmHoldRequest{PaymentID: paymentID, OperationID: uuid.New(), IdempotencyKey: "b-confirm", HoldID: holdID}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfirmHold(context.Background(), bank.ConfirmHoldRequest{PaymentID: paymentID, OperationID: uuid.New(), IdempotencyKey: "b-finalize", HoldID: creditID}); err != nil {
		t.Fatal(err)
	}
	status, err := NewParticipantService(pool, "BANK-B", "bank_b").GetOperationStatus(context.Background(), bank.OperationStatusRequest{PaymentID: paymentID, OperationID: creditID})
	if err != nil || status.Status != bank.OperationSucceeded {
		t.Fatalf("Bank B status after restart = %+v, err = %v", status, err)
	}
	assertParticipantBalance(t, pool, "bank_b", accountID, 300)
	assertParticipantBalance(t, pool, "bank_b", receiverID, 200)
}

func TestBankAHTTPLostResponseIsResolvedByOriginalOperationStatus(t *testing.T) {
	pool := newBankServiceTestPool(t)
	defer pool.Close()
	if !bankSchemaAvailable(t, pool) {
		t.Skip("M1-6 bank_a schema is not applied")
	}
	accountID, paymentID := uuid.New(), uuid.New()
	cleanupBankFixtures(t, pool, accountID, uuid.Nil, paymentID)
	defer cleanupBankFixtures(t, pool, accountID, uuid.Nil, paymentID)
	insertBankAccount(t, pool, accountID, "lost-response", 500)
	server := httptest.NewServer(Handler(NewService(pool)))
	defer server.Close()

	baseClient := server.Client()
	lostResponseClient := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		response, err := baseClient.Transport.RoundTrip(request)
		if err != nil {
			return nil, err
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		return nil, errors.New("simulated lost response")
	})}
	lostAdapter, err := bank.NewHTTPClient(server.URL, lostResponseClient)
	if err != nil {
		t.Fatal(err)
	}
	normalAdapter, err := bank.NewHTTPClient(server.URL, baseClient)
	if err != nil {
		t.Fatal(err)
	}
	holdID := uuid.New()
	_, err = lostAdapter.HoldFunds(context.Background(), bank.HoldFundsRequest{OperationRequest: bank.OperationRequest{PaymentID: paymentID, OperationID: holdID, IdempotencyKey: "lost-response-hold", AccountID: accountID, AmountPaise: 200, Currency: "INR"}})
	var adapterErr *bank.AdapterError
	if !errors.As(err, &adapterErr) || adapterErr.Code != bank.ErrCodeTransientFailure {
		t.Fatalf("lost response error = %v, want transient failure", err)
	}
	status, err := normalAdapter.GetOperationStatus(context.Background(), bank.OperationStatusRequest{PaymentID: paymentID, OperationID: holdID})
	if err != nil || status.Status != bank.OperationSucceeded {
		t.Fatalf("resolved operation status = %+v, err = %v", status, err)
	}
	assertBankBalance(t, pool, accountID, 300)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestBankAOperationsAreDurableIdempotentAndRestartSafe(t *testing.T) {
	pool := newBankServiceTestPool(t)
	defer pool.Close()
	if !bankSchemaAvailable(t, pool) {
		t.Skip("M1-6 bank_a schema is not applied")
	}
	accountID, receiverID := uuid.New(), uuid.New()
	paymentID := uuid.New()
	cleanupBankFixtures(t, pool, accountID, receiverID, paymentID)
	defer cleanupBankFixtures(t, pool, accountID, receiverID, paymentID)
	insertBankAccount(t, pool, accountID, "source", 1000)
	insertBankAccount(t, pool, receiverID, "receiver", 0)

	service := NewService(pool)
	holdID := uuid.New()
	holdRequest := bank.HoldFundsRequest{OperationRequest: bank.OperationRequest{PaymentID: paymentID, OperationID: holdID, IdempotencyKey: "hold-" + holdID.String(), AccountID: accountID, AmountPaise: 300, Currency: "INR"}}
	firstHold, err := service.HoldFunds(context.Background(), holdRequest)
	if err != nil {
		t.Fatal(err)
	}
	secondHold, err := service.HoldFunds(context.Background(), holdRequest)
	if err != nil || secondHold.OperationID != firstHold.OperationID {
		t.Fatalf("idempotent hold = %+v, err = %v", secondHold, err)
	}
	if _, err := service.HoldFunds(context.Background(), bank.HoldFundsRequest{OperationRequest: bank.OperationRequest{PaymentID: paymentID, OperationID: holdID, IdempotencyKey: holdRequest.IdempotencyKey, AccountID: accountID, AmountPaise: 301, Currency: "INR"}}); err == nil {
		t.Fatal("operation ID reuse with a different amount was accepted")
	}
	assertBankBalance(t, pool, accountID, 700)

	creditID := uuid.New()
	creditRequest := bank.ProvisionalCreditRequest{OperationRequest: bank.OperationRequest{PaymentID: paymentID, OperationID: creditID, IdempotencyKey: "credit-" + creditID.String(), AccountID: receiverID, AmountPaise: 300, Currency: "INR"}}
	if _, err := service.ProvisionalCredit(context.Background(), creditRequest); err != nil {
		t.Fatal(err)
	}
	assertBankBalance(t, pool, receiverID, 0)

	restartedService := NewService(pool)
	status, err := restartedService.GetOperationStatus(context.Background(), bank.OperationStatusRequest{PaymentID: paymentID, OperationID: holdID})
	if err != nil || status.Status != bank.OperationSucceeded {
		t.Fatalf("restarted status = %+v, err = %v", status, err)
	}
	if _, err := restartedService.ConfirmHold(context.Background(), bank.ConfirmHoldRequest{PaymentID: paymentID, OperationID: uuid.New(), IdempotencyKey: "confirm-" + paymentID.String(), HoldID: holdID}); err != nil {
		t.Fatal(err)
	}
	if _, err := restartedService.ConfirmHold(context.Background(), bank.ConfirmHoldRequest{PaymentID: paymentID, OperationID: uuid.New(), IdempotencyKey: "finalize-" + paymentID.String(), HoldID: creditID}); err != nil {
		t.Fatal(err)
	}
	assertBankBalance(t, pool, accountID, 700)
	assertBankBalance(t, pool, receiverID, 300)

	rows, err := pool.Query(context.Background(), `SELECT entry_type FROM bank_a.ledger_entries WHERE payment_id = $1 ORDER BY occurred_at, id`, paymentID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var entries []string
	for rows.Next() {
		var entry string
		if err := rows.Scan(&entry); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	if len(entries) != 3 || entries[0] != "HOLD" || entries[1] != "PROVISIONAL_CREDIT" || entries[2] != "FINAL_CREDIT" {
		t.Fatalf("bank ledger entries = %v", entries)
	}
}

func TestBankAConcurrentSameOperationHasOneMonetaryEffect(t *testing.T) {
	pool := newBankServiceTestPool(t)
	defer pool.Close()
	if !bankSchemaAvailable(t, pool) {
		t.Skip("M1-6 bank_a schema is not applied")
	}
	accountID, paymentID := uuid.New(), uuid.New()
	cleanupBankFixtures(t, pool, accountID, uuid.Nil, paymentID)
	defer cleanupBankFixtures(t, pool, accountID, uuid.Nil, paymentID)
	insertBankAccount(t, pool, accountID, "concurrent-operation", 1000)
	request := bank.HoldFundsRequest{OperationRequest: bank.OperationRequest{PaymentID: paymentID, OperationID: uuid.New(), IdempotencyKey: "concurrent-hold", AccountID: accountID, AmountPaise: 300, Currency: "INR"}}
	service := NewService(pool)
	results := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for range 2 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			_, err := service.HoldFunds(context.Background(), request)
			results <- err
		}()
	}
	waitGroup.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertBankBalance(t, pool, accountID, 700)
	var operationCount, ledgerCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM bank_a.operations WHERE payment_id = $1`, paymentID).Scan(&operationCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM bank_a.ledger_entries WHERE payment_id = $1`, paymentID).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if operationCount != 1 || ledgerCount != 1 {
		t.Fatalf("operation/ledger counts = %d/%d, want 1/1", operationCount, ledgerCount)
	}
}

func TestBankAReleaseAndReverseAreIdempotentCompensations(t *testing.T) {
	pool := newBankServiceTestPool(t)
	defer pool.Close()
	if !bankSchemaAvailable(t, pool) {
		t.Skip("M1-6 bank_a schema is not applied")
	}
	accountID, receiverID, paymentID := uuid.New(), uuid.New(), uuid.New()
	cleanupBankFixtures(t, pool, accountID, receiverID, paymentID)
	defer cleanupBankFixtures(t, pool, accountID, receiverID, paymentID)
	insertBankAccount(t, pool, accountID, "source-release", 1000)
	insertBankAccount(t, pool, receiverID, "receiver-release", 0)
	service := NewService(pool)

	holdID := uuid.New()
	_, err := service.HoldFunds(context.Background(), bank.HoldFundsRequest{OperationRequest: bank.OperationRequest{PaymentID: paymentID, OperationID: holdID, IdempotencyKey: "release-hold", AccountID: accountID, AmountPaise: 200, Currency: "INR"}})
	if err != nil {
		t.Fatal(err)
	}
	releaseRequest := bank.ReleaseHoldRequest{PaymentID: paymentID, OperationID: uuid.New(), IdempotencyKey: "release-operation", HoldID: holdID}
	if _, err := service.ReleaseHold(context.Background(), releaseRequest); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReleaseHold(context.Background(), releaseRequest); err != nil {
		t.Fatal(err)
	}
	assertBankBalance(t, pool, accountID, 1000)

	creditID := uuid.New()
	_, err = service.ProvisionalCredit(context.Background(), bank.ProvisionalCreditRequest{OperationRequest: bank.OperationRequest{PaymentID: paymentID, OperationID: creditID, IdempotencyKey: "reverse-credit", AccountID: receiverID, AmountPaise: 150, Currency: "INR"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReleaseHold(context.Background(), bank.ReleaseHoldRequest{PaymentID: paymentID, OperationID: uuid.New(), IdempotencyKey: "invalid-release", HoldID: creditID}); err == nil {
		t.Fatal("release unexpectedly accepted a provisional credit operation")
	}
	reverseRequest := bank.ReverseCreditRequest{PaymentID: paymentID, OperationID: uuid.New(), IdempotencyKey: "reverse-operation", OriginalOperationID: creditID}
	if _, err := service.ReverseProvisionalCredit(context.Background(), reverseRequest); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReverseProvisionalCredit(context.Background(), reverseRequest); err != nil {
		t.Fatal(err)
	}
	assertBankBalance(t, pool, receiverID, 0)
}

func newBankServiceTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

func bankSchemaAvailable(t *testing.T, pool *pgxpool.Pool) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass('bank_a.accounts') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func bankBSchemaAvailable(t *testing.T, pool *pgxpool.Pool) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass('bank_b.accounts') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func assertParticipantBalance(t *testing.T, pool *pgxpool.Pool, schema string, accountID uuid.UUID, want int64) {
	t.Helper()
	var got int64
	if err := pool.QueryRow(context.Background(), `SELECT balance_paise FROM `+schema+`.accounts WHERE id = $1`, accountID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("participant balance = %d, want %d", got, want)
	}
}

func insertBankAccount(t *testing.T, pool *pgxpool.Pool, id uuid.UUID, number string, balance int64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `INSERT INTO bank_a.accounts (id, account_number, balance_paise, status) VALUES ($1, $2, $3, 'ACTIVE')`, id, number+"-"+id.String(), balance); err != nil {
		t.Fatal(err)
	}
}

func assertBankBalance(t *testing.T, pool *pgxpool.Pool, accountID uuid.UUID, want int64) {
	t.Helper()
	var got int64
	if err := pool.QueryRow(context.Background(), `SELECT balance_paise FROM bank_a.accounts WHERE id = $1`, accountID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("bank balance = %d, want %d", got, want)
	}
}

func cleanupBankFixtures(t *testing.T, pool *pgxpool.Pool, accountID, receiverID, paymentID uuid.UUID) {
	t.Helper()
	_, _ = pool.Exec(context.Background(), `DELETE FROM bank_a.ledger_entries WHERE payment_id = $1`, paymentID)
	_, _ = pool.Exec(context.Background(), `DELETE FROM bank_a.operations WHERE payment_id = $1`, paymentID)
	_, _ = pool.Exec(context.Background(), `DELETE FROM bank_a.accounts WHERE id IN ($1, $2)`, accountID, receiverID)
}
