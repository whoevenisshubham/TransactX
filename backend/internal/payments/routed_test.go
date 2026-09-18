package payments

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/bankservice"
)

type successfulBankAdapter struct{}

type transientBankAdapter struct {
	successfulBankAdapter
	calls int
}

type pendingStatusBankAdapter struct{ transientBankAdapter }
type failedStatusBankAdapter struct{ transientBankAdapter }

func (adapter *transientBankAdapter) HoldFunds(context.Context, bank.HoldFundsRequest) (bank.HoldResult, error) {
	adapter.calls++
	return bank.HoldResult{}, &bank.AdapterError{Code: bank.ErrCodeTransientFailure, Message: "simulated unknown hold outcome"}
}

func (adapter *pendingStatusBankAdapter) GetOperationStatus(_ context.Context, request bank.OperationStatusRequest) (bank.OperationResult, error) {
	return bank.OperationResult{PaymentID: request.PaymentID, OperationID: request.OperationID, Status: bank.OperationPending}, nil
}

func (adapter *failedStatusBankAdapter) GetOperationStatus(_ context.Context, request bank.OperationStatusRequest) (bank.OperationResult, error) {
	return bank.OperationResult{PaymentID: request.PaymentID, OperationID: request.OperationID, Status: bank.OperationFailed}, nil
}

func (successfulBankAdapter) GetHealth(context.Context) (bank.HealthResult, error) {
	return bank.HealthResult{Available: true}, nil
}
func (successfulBankAdapter) ResolveAccount(context.Context, bank.ResolveAccountRequest) (bank.AccountResult, error) {
	return bank.AccountResult{Status: bank.AccountActive}, nil
}
func (successfulBankAdapter) HoldFunds(_ context.Context, request bank.HoldFundsRequest) (bank.HoldResult, error) {
	return bank.HoldResult{OperationResult: bank.OperationResult{PaymentID: request.PaymentID, OperationID: request.OperationID, Status: bank.OperationSucceeded}, HoldID: request.OperationID}, nil
}
func (successfulBankAdapter) ProvisionalCredit(_ context.Context, request bank.ProvisionalCreditRequest) (bank.OperationResult, error) {
	return bank.OperationResult{PaymentID: request.PaymentID, OperationID: request.OperationID, Status: bank.OperationSucceeded}, nil
}
func (successfulBankAdapter) ConfirmHold(_ context.Context, request bank.ConfirmHoldRequest) (bank.OperationResult, error) {
	return bank.OperationResult{PaymentID: request.PaymentID, OperationID: request.OperationID, Status: bank.OperationSucceeded}, nil
}
func (successfulBankAdapter) ReleaseHold(_ context.Context, request bank.ReleaseHoldRequest) (bank.OperationResult, error) {
	return bank.OperationResult{PaymentID: request.PaymentID, OperationID: request.OperationID, Status: bank.OperationSucceeded}, nil
}
func (successfulBankAdapter) ReverseProvisionalCredit(_ context.Context, request bank.ReverseCreditRequest) (bank.OperationResult, error) {
	return bank.OperationResult{PaymentID: request.PaymentID, OperationID: request.OperationID, Status: bank.OperationSucceeded}, nil
}
func (successfulBankAdapter) GetOperationStatus(_ context.Context, request bank.OperationStatusRequest) (bank.OperationResult, error) {
	return bank.OperationResult{PaymentID: request.PaymentID, OperationID: request.OperationID, Status: bank.OperationSucceeded}, nil
}
func (successfulBankAdapter) GetLedgerSnapshot(context.Context, bank.LedgerScope) (bank.LedgerSnapshot, error) {
	return bank.LedgerSnapshot{}, nil
}

func TestRoutedOperationIDsAreStablePerPaymentAndOperation(t *testing.T) {
	paymentID := uuid.New()
	holdID := operationID(paymentID, "hold")
	if holdID == uuid.Nil || holdID != operationID(paymentID, "hold") {
		t.Fatal("hold operation ID was not stable")
	}
	if holdID == operationID(paymentID, "provisional-credit") {
		t.Fatal("different bank operation types shared an operation ID")
	}
}

func TestBankSettledCentralPendingTransitionsCanRecover(t *testing.T) {
	payment := Payment{State: StateProcessing}
	if err := Transition(&payment, StateBankSettledCentralPending); err != nil {
		t.Fatal(err)
	}
	if err := Transition(&payment, StateCommitted); err != nil {
		t.Fatal(err)
	}
	if err := Transition(&payment, StateCompleted); err != nil {
		t.Fatal(err)
	}
}

func TestRoutedPaymentPersistsBothBanksAndOperationTracking(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set")
	}
	data := newRepositoryTestData(t)
	secondBankID := uuid.New()
	setBalances(t, data, 1000, 0)
	defer func() {
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM payment_bank_operations WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, data.userID)
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM ledger_entries WHERE ledger_transaction_id IN (SELECT lt.id FROM ledger_transactions lt JOIN payments p ON p.id = lt.payment_id WHERE p.initiated_by_user_id = $1)`, data.userID)
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM ledger_transactions WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, data.userID)
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM idempotency_records WHERE user_id = $1`, data.userID)
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM payments WHERE initiated_by_user_id = $1`, data.userID)
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM accounts WHERE id IN ($1, $2)`, data.sourceID, data.receiverID)
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM users WHERE id IN ($1, $2)`, data.userID, data.receiverUserID)
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM banks WHERE id IN ($1, $2)`, data.bankID, secondBankID)
		data.pool.Close()
	}()
	if _, err := data.pool.Exec(context.Background(), `INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Destination Bank', 'ACTIVE')`, secondBankID, "DEST-"+secondBankID.String()[:20]); err != nil {
		t.Fatal(err)
	}
	payment, duplicate, err := data.repository.CreateRoutedIdempotent(context.Background(), settlementPayment(data, 100), "routed-key", "routed-hash", data.bankID, secondBankID, successfulBankAdapter{}, successfulBankAdapter{})
	if err != nil || duplicate || payment.State != StateCompleted {
		t.Fatalf("routed payment = %+v, duplicate=%v, err=%v", payment, duplicate, err)
	}
	var sourceBankID, destinationBankID uuid.UUID
	if err := data.pool.QueryRow(context.Background(), `SELECT source_bank_id, destination_bank_id FROM payments WHERE id = $1`, payment.ID).Scan(&sourceBankID, &destinationBankID); err != nil {
		t.Fatal(err)
	}
	if sourceBankID != data.bankID || destinationBankID != secondBankID {
		t.Fatalf("persisted banks = %s -> %s", sourceBankID, destinationBankID)
	}
	var operationCount int
	if err := data.pool.QueryRow(context.Background(), `SELECT count(*) FROM payment_bank_operations WHERE payment_id = $1`, payment.ID).Scan(&operationCount); err != nil {
		t.Fatal(err)
	}
	if operationCount != 4 {
		t.Fatalf("tracked bank operations = %d, want 4", operationCount)
	}
}

func TestRoutedPaymentExecutesAcrossIndependentBankAAndBankBHTTPParticipants(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set")
	}
	data := newRepositoryTestData(t)
	secondBankID := uuid.New()
	setBalances(t, data, 1000, 0)
	defer func() {
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM bank_a.ledger_entries WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, data.userID)
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM bank_a.operations WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, data.userID)
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM bank_b.ledger_entries WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, data.userID)
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM bank_b.operations WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, data.userID)
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM bank_a.accounts WHERE id IN ($1, $2)`, data.sourceID, data.receiverID)
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM bank_b.accounts WHERE id IN ($1, $2)`, data.sourceID, data.receiverID)
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM banks WHERE id = $1`, secondBankID)
		_, _ = data.pool.Exec(context.Background(), `UPDATE accounts SET bank_id = $2, bank_account_id = $1 WHERE id = $1`, data.receiverID, data.bankID)
		data.close(t)
	}()
	if _, err := data.pool.Exec(context.Background(), `INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Bank B', 'ACTIVE')`, secondBankID, "BANK-B-"+secondBankID.String()[:12]); err != nil {
		t.Fatal(err)
	}
	if _, err := data.pool.Exec(context.Background(), `UPDATE accounts SET bank_id = $2, bank_account_id = id WHERE id = $1`, data.sourceID, data.bankID); err != nil {
		t.Fatal(err)
	}
	if _, err := data.pool.Exec(context.Background(), `UPDATE accounts SET bank_id = $2, bank_account_id = id WHERE id = $1`, data.receiverID, secondBankID); err != nil {
		t.Fatal(err)
	}
	if _, err := data.pool.Exec(context.Background(), `INSERT INTO bank_a.accounts (id, account_number, balance_paise, status) VALUES ($1, $2, 1000, 'ACTIVE')`, data.sourceID, "a-"+data.sourceID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := data.pool.Exec(context.Background(), `INSERT INTO bank_b.accounts (id, account_number, balance_paise, status) VALUES ($1, $2, 0, 'ACTIVE')`, data.receiverID, "b-"+data.receiverID.String()); err != nil {
		t.Fatal(err)
	}
	bankAServer := httptest.NewServer(bankservice.Handler(bankservice.NewService(data.pool)))
	defer bankAServer.Close()
	bankBServer := httptest.NewServer(bankservice.Handler(bankservice.NewParticipantService(data.pool, "BANK-B", "bank_b")))
	defer bankBServer.Close()
	adapterA, err := bank.NewHTTPClient(bankAServer.URL, bankAServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	adapterB, err := bank.NewHTTPClient(bankBServer.URL, bankBServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	payment, duplicate, err := data.repository.CreateRoutedIdempotent(context.Background(), settlementPayment(data, 250), "real-a-b", "real-a-b-hash", data.bankID, secondBankID, adapterA, adapterB)
	if err != nil || duplicate || payment.State != StateCompleted {
		t.Fatalf("A -> B payment = %+v, duplicate=%v, err=%v", payment, duplicate, err)
	}
	assertRoutedParticipantBalance(t, data.pool, "bank_a", data.sourceID, 750)
	assertRoutedParticipantBalance(t, data.pool, "bank_b", data.receiverID, 250)
}

func assertRoutedParticipantBalance(t *testing.T, pool *pgxpool.Pool, schema string, accountID uuid.UUID, want int64) {
	t.Helper()
	var got int64
	if err := pool.QueryRow(context.Background(), `SELECT balance_paise FROM `+schema+`.accounts WHERE id = $1`, accountID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s balance = %d, want %d", schema, got, want)
	}
}

func TestBankSettledCentralPendingRecoveryOnlyWritesCentralState(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set")
	}
	data := newRepositoryTestData(t)
	setBalances(t, data, 1000, 0)
	defer data.close(t)
	payment := settlementPayment(data, 250)
	if _, err := data.pool.Exec(context.Background(), `INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state, source_bank_id, destination_bank_id) VALUES ($1, $2, $3, $4, $5, 'INR', 'BANK_SETTLED_CENTRAL_PENDING', $6, $6)`, payment.ID, payment.InitiatedByUserID, payment.SenderAccountID, payment.ReceiverAccountID, payment.AmountPaise, data.bankID); err != nil {
		t.Fatal(err)
	}
	recovered, err := data.repository.RecoverBankSettledCentralPending(context.Background(), payment.ID)
	if err != nil || recovered.State != StateCompleted {
		t.Fatalf("recovered payment = %+v, err = %v", recovered, err)
	}
	assertBalances(t, data, 750, 250)
	assertSettlementCounts(t, data, 1, 1, 2)
}

func TestRoutedTransientOutcomeBecomesPendingWithoutRetry(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set")
	}
	data := newRepositoryTestData(t)
	setBalances(t, data, 1000, 0)
	defer func() {
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM payment_bank_operations WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, data.userID)
		data.close(t)
	}()
	source := &transientBankAdapter{}
	payment, _, err := data.repository.CreateRoutedIdempotent(context.Background(), settlementPayment(data, 100), "pending-key", "pending-hash", data.bankID, data.bankID, source, successfulBankAdapter{})
	if err != nil || payment.State != StatePendingReconciliation {
		t.Fatalf("transient routed payment = %+v, err = %v", payment, err)
	}
	if source.calls != 1 {
		t.Fatalf("hold calls = %d, want 1", source.calls)
	}
}

func TestRoutedPendingOperationResolvesByStatusWithoutRepeatingHold(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set")
	}
	data := newRepositoryTestData(t)
	setBalances(t, data, 1000, 0)
	defer func() {
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM payment_bank_operations WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, data.userID)
		data.close(t)
	}()
	source := &transientBankAdapter{}
	payment, _, err := data.repository.CreateRoutedIdempotent(context.Background(), settlementPayment(data, 100), "pending-recovery-key", "pending-recovery-hash", data.bankID, data.bankID, source, successfulBankAdapter{})
	if err != nil || payment.State != StatePendingReconciliation {
		t.Fatalf("initial pending payment = %+v, err = %v", payment, err)
	}
	recovered, duplicate, err := data.repository.CreateRoutedIdempotent(context.Background(), settlementPayment(data, 100), "pending-recovery-key", "pending-recovery-hash", data.bankID, data.bankID, source, successfulBankAdapter{})
	if err != nil || !duplicate || recovered.State != StateCompleted {
		t.Fatalf("recovered payment = %+v, duplicate=%v, err = %v", recovered, duplicate, err)
	}
	if source.calls != 1 {
		t.Fatalf("hold calls = %d, want exactly one original monetary call", source.calls)
	}
}

func TestRoutedPendingOperationStaysPendingWhenStatusIsUnknown(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set")
	}
	data := newRepositoryTestData(t)
	setBalances(t, data, 1000, 0)
	defer func() {
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM payment_bank_operations WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, data.userID)
		data.close(t)
	}()
	source := &pendingStatusBankAdapter{}
	payment, _, err := data.repository.CreateRoutedIdempotent(context.Background(), settlementPayment(data, 100), "still-pending-key", "still-pending-hash", data.bankID, data.bankID, source, successfulBankAdapter{})
	if err != nil || payment.State != StatePendingReconciliation {
		t.Fatalf("initial pending payment = %+v, err = %v", payment, err)
	}
	if err := data.repository.RecoverRoutedPayment(context.Background(), payment, source, successfulBankAdapter{}); err != nil {
		t.Fatal(err)
	}
	recovered, err := data.repository.Get(context.Background(), payment.ID)
	if err != nil || recovered.State != StatePendingReconciliation {
		t.Fatalf("unresolved payment = %+v, err = %v", recovered, err)
	}
	if source.calls != 1 {
		t.Fatalf("hold calls = %d, want exactly one original monetary call", source.calls)
	}
}

func TestRoutedPendingOperationWithDefiniteFailureDoesNotRepeatOperation(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set")
	}
	data := newRepositoryTestData(t)
	setBalances(t, data, 1000, 0)
	defer func() {
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM payment_bank_operations WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, data.userID)
		data.close(t)
	}()
	source := &failedStatusBankAdapter{}
	payment, _, err := data.repository.CreateRoutedIdempotent(context.Background(), settlementPayment(data, 100), "failed-status-key", "failed-status-hash", data.bankID, data.bankID, source, successfulBankAdapter{})
	if err != nil || payment.State != StatePendingReconciliation {
		t.Fatalf("initial pending payment = %+v, err = %v", payment, err)
	}
	if err := data.repository.RecoverRoutedPayment(context.Background(), payment, source, successfulBankAdapter{}); err != nil {
		t.Fatal(err)
	}
	recovered, err := data.repository.Get(context.Background(), payment.ID)
	if err != nil || recovered.State != StateFailed {
		t.Fatalf("failed payment = %+v, err = %v", recovered, err)
	}
	if source.calls != 1 {
		t.Fatalf("hold calls = %d, want exactly one original monetary call", source.calls)
	}
}
