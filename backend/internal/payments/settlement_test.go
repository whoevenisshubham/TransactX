package payments

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/transactx/backend/internal/accounts"
	"github.com/transactx/backend/internal/ledger"
)

func TestCreateAndSettleIdempotentCommitsBalancedTransfer(t *testing.T) {
	data := newRepositoryTestData(t)
	defer data.close(t)
	setBalances(t, data, 1000, 500)

	ledgerRepository := ledger.NewRepository(data.pool)
	payment, duplicate, err := data.repository.CreateAndSettleIdempotent(context.Background(), settlementPayment(data, 300), "settle-key", "settle-hash", accounts.NewRepository(data.pool), ledgerRepository)
	if err != nil || duplicate {
		t.Fatalf("settlement = %s, duplicate %v, error %v", payment.State, duplicate, err)
	}
	if payment.State != StateCompleted {
		t.Fatalf("state = %q, want %q", payment.State, StateCompleted)
	}
	assertBalances(t, data, 700, 800)
	assertLedger(t, data, payment.ID, data.sourceID, data.receiverID, 300)

	for _, accountID := range []uuid.UUID{data.sourceID, data.receiverID} {
		reconstructed, err := ledgerRepository.ReconstructBalance(context.Background(), accountID)
		if err != nil {
			t.Fatal(err)
		}
		var materialized int64
		if err := data.pool.QueryRow(context.Background(), `SELECT balance_paise FROM accounts WHERE id = $1`, accountID).Scan(&materialized); err != nil {
			t.Fatal(err)
		}
		if reconstructed != materialized {
			t.Fatalf("account %s reconstructed balance = %d, materialized = %d", accountID, reconstructed, materialized)
		}
	}
}

func TestCreateAndSettleRejectsInsufficientFundsAtomically(t *testing.T) {
	data := newRepositoryTestData(t)
	defer data.close(t)
	setBalances(t, data, 100, 500)

	_, _, err := data.repository.CreateAndSettleIdempotent(context.Background(), settlementPayment(data, 300), "insufficient-key", "insufficient-hash", accounts.NewRepository(data.pool), ledger.NewRepository(data.pool))
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("error = %v, want ErrInsufficientFunds", err)
	}
	assertBalances(t, data, 100, 500)
	assertSettlementCounts(t, data, 0, 0, 0)
}

func TestCreateAndSettleRollsBackWhenReceiverCannotBeCredited(t *testing.T) {
	data := newRepositoryTestData(t)
	defer data.close(t)
	setBalances(t, data, 1000, 500)
	if _, err := data.pool.Exec(context.Background(), `UPDATE accounts SET status = 'BLOCKED' WHERE id = $1`, data.receiverID); err != nil {
		t.Fatal(err)
	}

	_, _, err := data.repository.CreateAndSettleIdempotent(context.Background(), settlementPayment(data, 300), "rollback-key", "rollback-hash", accounts.NewRepository(data.pool), ledger.NewRepository(data.pool))
	if err == nil {
		t.Fatal("settlement succeeded with blocked receiver")
	}
	assertBalances(t, data, 1000, 500)
	assertSettlementCounts(t, data, 0, 0, 0)
}

func TestCreateAndSettleExactRetryDoesNotRepeatSettlement(t *testing.T) {
	data := newRepositoryTestData(t)
	defer data.close(t)
	setBalances(t, data, 1000, 500)
	accountRepository := accounts.NewRepository(data.pool)
	ledgerRepository := ledger.NewRepository(data.pool)
	paymentInput := settlementPayment(data, 300)

	first, _, err := data.repository.CreateAndSettleIdempotent(context.Background(), paymentInput, "retry-key", "retry-hash", accountRepository, ledgerRepository)
	if err != nil {
		t.Fatal(err)
	}
	second, duplicate, err := data.repository.CreateAndSettleIdempotent(context.Background(), paymentInput, "retry-key", "retry-hash", accountRepository, ledgerRepository)
	if err != nil || !duplicate {
		t.Fatalf("retry = %s, duplicate %v, error %v", second.State, duplicate, err)
	}
	if second.ID != first.ID {
		t.Fatalf("retry payment = %s, want %s", second.ID, first.ID)
	}
	assertBalances(t, data, 700, 800)
	assertSettlementCounts(t, data, 1, 1, 2)
}

func TestConcurrentInsufficientFundsSettlementsAreAtomic(t *testing.T) {
	data := newRepositoryTestData(t)
	defer data.close(t)
	setBalances(t, data, 100, 0)

	results := runConcurrentSettlements(t, data, 10, func(index int) Payment {
		return settlementPayment(data, 20)
	})
	var successful, insufficient int
	for result := range results {
		if result.err == nil {
			successful++
		} else if errors.Is(result.err, ErrInsufficientFunds) {
			insufficient++
		} else {
			t.Fatalf("settlement failed with unexpected error: %v", result.err)
		}
	}
	if successful != 5 || insufficient != 5 {
		t.Fatalf("settlements = successful %d, insufficient %d; want 5, 5", successful, insufficient)
	}
	assertBalances(t, data, 0, 100)
	assertNoNegativeBalances(t, data)
	assertSettlementCounts(t, data, 5, 5, 10)
	assertIdempotencyRecordCount(t, data, 5)
	assertLedgerInvariants(t, data, 5, 100)
	assertReconstructedBalances(t, data)
}

func TestConcurrentSettlementStressPreservesBalanceAndLedgerInvariants(t *testing.T) {
	data := newRepositoryTestData(t)
	defer data.close(t)
	const requests = 75
	const amount = int64(7)
	const startingBalance = int64(100)
	setBalances(t, data, startingBalance, 0)

	results := runConcurrentSettlements(t, data, requests, func(index int) Payment {
		return settlementPayment(data, amount)
	})
	var successfulAmount int64
	var successful int
	for result := range results {
		if result.err == nil {
			successful++
			successfulAmount += result.payment.AmountPaise
		} else if !errors.Is(result.err, ErrInsufficientFunds) {
			t.Fatalf("settlement failed with unexpected error: %v", result.err)
		}
	}
	if successfulAmount > startingBalance {
		t.Fatalf("successful amount = %d, exceeds starting balance %d", successfulAmount, startingBalance)
	}
	if successful == 0 {
		t.Fatal("stress test produced no successful settlements")
	}
	assertNoNegativeBalances(t, data)
	assertSettlementCounts(t, data, successful, successful, successful*2)
	assertLedgerInvariants(t, data, successful, successfulAmount)
	assertReconstructedBalances(t, data)
}

func TestConcurrentSettlementsWithSameIdempotencyKeyCreateOneLogicalPayment(t *testing.T) {
	data := newRepositoryTestData(t)
	defer data.close(t)
	setBalances(t, data, 1000, 0)

	const requests = 20
	results := make(chan settlementResult, requests)
	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	for range requests {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			payment, duplicate, err := data.repository.CreateAndSettleIdempotent(context.Background(), settlementPayment(data, 100), "same-settlement-key", "same-settlement-hash", accounts.NewRepository(data.pool), ledger.NewRepository(data.pool))
			results <- settlementResult{payment: payment, duplicate: duplicate, err: err}
		}()
	}
	close(start)
	waitGroup.Wait()
	close(results)

	var paymentID uuid.UUID
	var duplicates int
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.payment.State != StateCompleted {
			t.Fatalf("payment state = %q, want %q", result.payment.State, StateCompleted)
		}
		if paymentID == uuid.Nil {
			paymentID = result.payment.ID
		} else if result.payment.ID != paymentID {
			t.Fatalf("payment = %s, want shared payment %s", result.payment.ID, paymentID)
		}
		if result.duplicate {
			duplicates++
		}
	}
	if duplicates != requests-1 {
		t.Fatalf("replay responses = %d, want %d", duplicates, requests-1)
	}
	assertSettlementCounts(t, data, 1, 1, 2)
	var idempotencyCount int
	if err := data.pool.QueryRow(context.Background(), `SELECT count(*) FROM idempotency_records WHERE user_id = $1 AND key = 'same-settlement-key'`, data.userID).Scan(&idempotencyCount); err != nil {
		t.Fatal(err)
	}
	if idempotencyCount != 1 {
		t.Fatalf("idempotency records = %d, want 1", idempotencyCount)
	}
	assertBalances(t, data, 900, 100)
	assertLedgerInvariants(t, data, 1, 100)
}

type settlementResult struct {
	payment   Payment
	duplicate bool
	err       error
}

func runConcurrentSettlements(t *testing.T, data repositoryTestData, requests int, payment func(index int) Payment) chan settlementResult {
	t.Helper()
	results := make(chan settlementResult, requests)
	ready := make(chan struct{})
	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	for index := range requests {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			ready <- struct{}{}
			<-start
			created, duplicate, err := data.repository.CreateAndSettleIdempotent(context.Background(), payment(index), "settlement-key-"+uuid.NewString(), uuid.NewString(), accounts.NewRepository(data.pool), ledger.NewRepository(data.pool))
			results <- settlementResult{payment: created, duplicate: duplicate, err: err}
		}(index)
	}
	for range requests {
		<-ready
	}
	close(start)
	waitGroup.Wait()
	close(results)
	return results
}

func settlementPayment(data repositoryTestData, amount int64) Payment {
	return Payment{ID: uuid.New(), InitiatedByUserID: data.userID, SenderAccountID: data.sourceID, ReceiverAccountID: data.receiverID, AmountPaise: amount, Currency: "INR", State: StateCreated}
}

func setBalances(t *testing.T, data repositoryTestData, sender, receiver int64) {
	t.Helper()
	if _, err := data.pool.Exec(context.Background(), `UPDATE accounts SET balance_paise = $2, opening_balance_paise = $2 WHERE id = $1`, data.sourceID, sender); err != nil {
		t.Fatal(err)
	}
	if _, err := data.pool.Exec(context.Background(), `UPDATE accounts SET balance_paise = $2, opening_balance_paise = $2 WHERE id = $1`, data.receiverID, receiver); err != nil {
		t.Fatal(err)
	}
}

func assertBalances(t *testing.T, data repositoryTestData, sender, receiver int64) {
	t.Helper()
	var actualSender, actualReceiver int64
	if err := data.pool.QueryRow(context.Background(), `SELECT balance_paise FROM accounts WHERE id = $1`, data.sourceID).Scan(&actualSender); err != nil {
		t.Fatal(err)
	}
	if err := data.pool.QueryRow(context.Background(), `SELECT balance_paise FROM accounts WHERE id = $1`, data.receiverID).Scan(&actualReceiver); err != nil {
		t.Fatal(err)
	}
	if actualSender != sender || actualReceiver != receiver {
		t.Fatalf("balances = sender %d, receiver %d; want sender %d, receiver %d", actualSender, actualReceiver, sender, receiver)
	}
}

func assertLedger(t *testing.T, data repositoryTestData, paymentID, senderID, receiverID uuid.UUID, amount int64) {
	t.Helper()
	assertSettlementCounts(t, data, 1, 1, 2)
	var debit, credit int64
	if err := data.pool.QueryRow(context.Background(), `SELECT count(*) FROM ledger_entries le JOIN ledger_transactions lt ON lt.id = le.ledger_transaction_id WHERE lt.payment_id = $1 AND le.account_id = $2 AND le.entry_type = 'DEBIT' AND le.amount_paise = $3`, paymentID, senderID, amount).Scan(&debit); err != nil {
		t.Fatal(err)
	}
	if err := data.pool.QueryRow(context.Background(), `SELECT count(*) FROM ledger_entries le JOIN ledger_transactions lt ON lt.id = le.ledger_transaction_id WHERE lt.payment_id = $1 AND le.account_id = $2 AND le.entry_type = 'CREDIT' AND le.amount_paise = $3`, paymentID, receiverID, amount).Scan(&credit); err != nil {
		t.Fatal(err)
	}
	if debit != 1 || credit != 1 {
		t.Fatalf("ledger debit count = %d, credit count = %d", debit, credit)
	}
}

func assertSettlementCounts(t *testing.T, data repositoryTestData, transactions, payments, entries int) {
	t.Helper()
	var actualTransactions, actualPayments, actualEntries int
	if err := data.pool.QueryRow(context.Background(), `SELECT count(*) FROM ledger_transactions lt JOIN payments p ON p.id = lt.payment_id WHERE p.initiated_by_user_id = $1`, data.userID).Scan(&actualTransactions); err != nil {
		t.Fatal(err)
	}
	if err := data.pool.QueryRow(context.Background(), `SELECT count(*) FROM payments WHERE initiated_by_user_id = $1`, data.userID).Scan(&actualPayments); err != nil {
		t.Fatal(err)
	}
	if err := data.pool.QueryRow(context.Background(), `SELECT count(*) FROM ledger_entries le JOIN ledger_transactions lt ON lt.id = le.ledger_transaction_id JOIN payments p ON p.id = lt.payment_id WHERE p.initiated_by_user_id = $1`, data.userID).Scan(&actualEntries); err != nil {
		t.Fatal(err)
	}
	if actualTransactions != transactions || actualPayments != payments || actualEntries != entries {
		t.Fatalf("settlement counts = transactions %d, payments %d, entries %d; want %d, %d, %d", actualTransactions, actualPayments, actualEntries, transactions, payments, entries)
	}
}

func assertNoNegativeBalances(t *testing.T, data repositoryTestData) {
	t.Helper()
	var negative int
	if err := data.pool.QueryRow(context.Background(), `SELECT count(*) FROM accounts WHERE id IN ($1, $2) AND balance_paise < 0`, data.sourceID, data.receiverID).Scan(&negative); err != nil {
		t.Fatal(err)
	}
	if negative != 0 {
		t.Fatalf("negative affected accounts = %d, want 0", negative)
	}
}

func assertLedgerInvariants(t *testing.T, data repositoryTestData, successfulPayments int, expectedAmount int64) {
	t.Helper()
	var invalidPayments int
	if err := data.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM (
			SELECT p.id
			FROM payments p
			LEFT JOIN ledger_transactions lt ON lt.payment_id = p.id
			LEFT JOIN ledger_entries le ON le.ledger_transaction_id = lt.id
			WHERE p.initiated_by_user_id = $1
			GROUP BY p.id
			HAVING count(DISTINCT lt.id) <> 1
				OR count(*) <> 2
				OR count(*) FILTER (WHERE le.entry_type = 'DEBIT') <> 1
				OR count(*) FILTER (WHERE le.entry_type = 'CREDIT') <> 1
				OR count(*) FILTER (WHERE le.entry_type = 'DEBIT' AND le.account_id = $2 AND le.amount_paise = p.amount_paise) <> 1
				OR count(*) FILTER (WHERE le.entry_type = 'CREDIT' AND le.account_id = $3 AND le.amount_paise = p.amount_paise) <> 1
				OR min(le.amount_paise) <> max(le.amount_paise)
		) invalid`, data.userID, data.sourceID, data.receiverID).Scan(&invalidPayments); err != nil {
		t.Fatal(err)
	}
	if invalidPayments != 0 {
		t.Fatalf("payments with invalid ledger shape = %d, want 0", invalidPayments)
	}
	var debitTotal, creditTotal int64
	if err := data.pool.QueryRow(context.Background(), `
		SELECT COALESCE(SUM(CASE WHEN le.entry_type = 'DEBIT' THEN le.amount_paise ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN le.entry_type = 'CREDIT' THEN le.amount_paise ELSE 0 END), 0)
		FROM ledger_entries le
		JOIN ledger_transactions lt ON lt.id = le.ledger_transaction_id
		JOIN payments p ON p.id = lt.payment_id
		WHERE p.initiated_by_user_id = $1`, data.userID).Scan(&debitTotal, &creditTotal); err != nil {
		t.Fatal(err)
	}
	if debitTotal != creditTotal || debitTotal != expectedAmount {
		t.Fatalf("ledger totals = debit %d, credit %d; want equal %d", debitTotal, creditTotal, expectedAmount)
	}
	var completed int
	if err := data.pool.QueryRow(context.Background(), `SELECT count(*) FROM payments WHERE initiated_by_user_id = $1 AND state = 'COMPLETED'`, data.userID).Scan(&completed); err != nil {
		t.Fatal(err)
	}
	if completed != successfulPayments {
		t.Fatalf("completed payments = %d, want %d", completed, successfulPayments)
	}
}

func assertIdempotencyRecordCount(t *testing.T, data repositoryTestData, expected int) {
	t.Helper()
	var count int
	if err := data.pool.QueryRow(context.Background(), `SELECT count(*) FROM idempotency_records WHERE user_id = $1`, data.userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != expected {
		t.Fatalf("idempotency records = %d, want %d", count, expected)
	}
}

func assertReconstructedBalances(t *testing.T, data repositoryTestData) {
	t.Helper()
	for _, accountID := range []uuid.UUID{data.sourceID, data.receiverID} {
		var expected, actual int64
		if err := data.pool.QueryRow(context.Background(), `
			SELECT a.opening_balance_paise + COALESCE(SUM(CASE WHEN le.entry_type = 'CREDIT' THEN le.amount_paise ELSE -le.amount_paise END), 0)
			FROM accounts a
			LEFT JOIN ledger_entries le ON le.account_id = a.id
			WHERE a.id = $1
			GROUP BY a.opening_balance_paise`, accountID).Scan(&expected); err != nil {
			t.Fatal(err)
		}
		if err := data.pool.QueryRow(context.Background(), `SELECT balance_paise FROM accounts WHERE id = $1`, accountID).Scan(&actual); err != nil {
			t.Fatal(err)
		}
		if expected != actual {
			t.Fatalf("account %s reconstructed balance = %d, materialized = %d", accountID, expected, actual)
		}
	}
}

func TestSettlementTestsRequireDatabaseURL(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set")
	}
}
