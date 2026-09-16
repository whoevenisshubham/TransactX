package payments

import (
	"context"
	"errors"
	"os"
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

func TestSettlementTestsRequireDatabaseURL(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set")
	}
}
