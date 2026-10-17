package payments

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/accounts"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/bankservice"
	"github.com/transactx/backend/internal/ledger"
	"github.com/transactx/backend/internal/recipients"
)

func TestResume_TerminalNoOp(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set")
	}
	data := newRepositoryTestData(t)
	defer data.close(t)

	svc := NewService(accounts.NewRepository(data.pool), recipients.NewRepository(data.pool), data.repository, nil)

	terminalStates := []string{StateCompleted, StateFailed, StateReversed}
	for _, st := range terminalStates {
		paymentID := uuid.New()
		_, err := data.pool.Exec(context.Background(), `
			INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state)
			VALUES ($1, $2, $3, $4, 100, 'INR', 'ONLINE', $5)`,
			paymentID, data.userID, data.sourceID, data.receiverID, st)
		if err != nil {
			t.Fatalf("failed inserting payment in %s: %v", st, err)
		}

		res, err := svc.Resume(context.Background(), paymentID)
		if err != nil {
			t.Fatalf("Resume(%s) failed: %v", st, err)
		}
		if res.Status != ResumeStatusNoOp {
			t.Fatalf("expected ResumeStatusNoOp for state %s, got %s", st, res.Status)
		}
		if !res.Terminal {
			t.Fatalf("expected Terminal=true for state %s", st)
		}
		if res.ActionTaken != "TERMINAL_NOOP" {
			t.Fatalf("expected ActionTaken=TERMINAL_NOOP, got %s", res.ActionTaken)
		}
	}

	snap := svc.RecoveryMetrics().Snapshot()
	if snap["resume_noop"] != int64(len(terminalStates)) {
		t.Fatalf("expected resume_noop=%d, got %d", len(terminalStates), snap["resume_noop"])
	}
}

func TestResume_RouteRemoved(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set")
	}
	data := newRepositoryTestData(t)
	defer data.close(t)

	secondBankID := uuid.New()
	_, err := data.pool.Exec(context.Background(), `INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Dest Bank', 'ACTIVE')`, secondBankID, "DEST-"+secondBankID.String()[:20])
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM banks WHERE id = $1`, secondBankID)
	}()

	paymentID := uuid.New()
	_, err = data.pool.Exec(context.Background(), `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state, source_bank_id, destination_bank_id)
		VALUES ($1, $2, $3, $4, 100, 'INR', 'ONLINE', $5, $6, $7)`,
		paymentID, data.userID, data.sourceID, data.receiverID, StateProcessing, data.bankID, secondBankID)
	if err != nil {
		t.Fatal(err)
	}

	// Service without configured execution target or adapter for secondBankID
	svc := NewService(accounts.NewRepository(data.pool), recipients.NewRepository(data.pool), data.repository, nil)

	res, err := svc.Resume(context.Background(), paymentID)
	if err != nil {
		t.Fatalf("Resume returned unexpected error: %v", err)
	}
	if res.Status != ResumeStatusAttention {
		t.Fatalf("expected ResumeStatusAttention when route removed, got %s", res.Status)
	}
	if res.ActionTaken != "ROUTE_UNAVAILABLE_LEAVE_PENDING" {
		t.Fatalf("expected ActionTaken=ROUTE_UNAVAILABLE_LEAVE_PENDING, got %s", res.ActionTaken)
	}

	// Verify payment remains in PROCESSING without monetary changes
	refreshed, err := data.repository.Get(context.Background(), paymentID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.State != StateProcessing {
		t.Fatalf("payment state changed to %s, should remain PROCESSING", refreshed.State)
	}

	var ledgerCount int
	_ = data.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM ledger_transactions WHERE payment_id = $1`, paymentID).Scan(&ledgerCount)
	if ledgerCount != 0 {
		t.Fatalf("expected 0 ledger transactions, found %d", ledgerCount)
	}
}

func TestResume_LockDeadline(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set")
	}
	data := newRepositoryTestData(t)
	defer data.close(t)

	paymentID := uuid.New()
	_, err := data.pool.Exec(context.Background(), `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state)
		VALUES ($1, $2, $3, $4, 100, 'INR', 'ONLINE', $5)`,
		paymentID, data.userID, data.sourceID, data.receiverID, StateCompleted)
	if err != nil {
		t.Fatal(err)
	}

	lockKey := int64(uuidToLockKey(paymentID))

	// Acquire advisory lock manually on dedicated connection
	conn, err := data.pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()

	var locked bool
	if err := conn.QueryRow(context.Background(), `SELECT pg_try_advisory_lock($1)`, lockKey).Scan(&locked); err != nil || !locked {
		t.Fatalf("failed acquiring manual advisory lock: err=%v, locked=%v", err, locked)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, lockKey)
	}()

	svc := NewService(accounts.NewRepository(data.pool), recipients.NewRepository(data.pool), data.repository, nil)

	// Call Resume with short deadline
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, resumeErr := svc.Resume(ctx, paymentID)
	elapsed := time.Since(start)

	if resumeErr == nil {
		t.Fatal("expected error due to lock deadline, got nil")
	}
	if !errors.Is(resumeErr, context.DeadlineExceeded) && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded error, got %v", resumeErr)
	}
	if elapsed < 40*time.Millisecond {
		t.Fatalf("Resume returned too early: %v", elapsed)
	}
}

func TestResume_StateSpecificContinuation_Committed(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set")
	}
	data := newRepositoryTestData(t)
	defer data.close(t)

	svc := NewService(accounts.NewRepository(data.pool), recipients.NewRepository(data.pool), data.repository, nil)

	// 1. COMMITTED payment WITHOUT ledger transaction must fail to complete
	invalidPaymentID := uuid.New()
	_, err := data.pool.Exec(context.Background(), `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state)
		VALUES ($1, $2, $3, $4, 100, 'INR', 'ONLINE', $5)`,
		invalidPaymentID, data.userID, data.sourceID, data.receiverID, StateCommitted)
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.Resume(context.Background(), invalidPaymentID)
	if err == nil {
		t.Fatal("expected error completing COMMITTED payment without ledger transaction, got nil")
	}

	// 2. COMMITTED payment WITH valid central ledger transaction completes to COMPLETED without adding another ledger transaction
	validPaymentID := uuid.New()
	_, err = data.pool.Exec(context.Background(), `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state)
		VALUES ($1, $2, $3, $4, 500, 'INR', 'ONLINE', $5)`,
		validPaymentID, data.userID, data.sourceID, data.receiverID, StateCommitted)
	if err != nil {
		t.Fatal(err)
	}

	ledgerRepo := ledger.NewRepository(data.pool)
	tx, err := data.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ledgerTxID, err := ledgerRepo.CreateTransaction(context.Background(), tx, validPaymentID)
	if err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatal(err)
	}
	if err := ledgerRepo.CreateEntry(context.Background(), tx, ledger.Entry{ID: uuid.New(), LedgerTransactionID: ledgerTxID, AccountID: data.sourceID, EntryType: ledger.EntryDebit, AmountPaise: 500}); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatal(err)
	}
	if err := ledgerRepo.CreateEntry(context.Background(), tx, ledger.Entry{ID: uuid.New(), LedgerTransactionID: ledgerTxID, AccountID: data.receiverID, EntryType: ledger.EntryCredit, AmountPaise: 500}); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}

	res, err := svc.Resume(context.Background(), validPaymentID)
	if err != nil {
		t.Fatalf("Resume(COMMITTED) failed: %v", err)
	}
	if res.Status != ResumeStatusCompleted {
		t.Fatalf("expected ResumeStatusCompleted, got %s", res.Status)
	}
	if res.Payment.State != StateCompleted {
		t.Fatalf("expected state COMPLETED, got %s", res.Payment.State)
	}

	// Verify ledger transaction count is STILL EXACTLY 1 (no duplicate transaction!)
	var txCount int
	_ = data.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM ledger_transactions WHERE payment_id = $1`, validPaymentID).Scan(&txCount)
	if txCount != 1 {
		t.Fatalf("expected exactly 1 ledger transaction, found %d", txCount)
	}
}

func TestResume_StateSpecificContinuation_BankSettledCentralPending(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set")
	}
	data := newRepositoryTestData(t)
	defer data.close(t)

	setBalances(t, data, 1000, 0)
	svc := NewService(accounts.NewRepository(data.pool), recipients.NewRepository(data.pool), data.repository, nil)

	paymentID := uuid.New()
	_, err := data.pool.Exec(context.Background(), `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state)
		VALUES ($1, $2, $3, $4, 400, 'INR', 'ONLINE', $5)`,
		paymentID, data.userID, data.sourceID, data.receiverID, StateBankSettledCentralPending)
	if err != nil {
		t.Fatal(err)
	}

	res, err := svc.Resume(context.Background(), paymentID)
	if err != nil {
		t.Fatalf("Resume(BANK_SETTLED_CENTRAL_PENDING) failed: %v", err)
	}
	if res.Status != ResumeStatusCompleted {
		t.Fatalf("expected ResumeStatusCompleted, got %s", res.Status)
	}
	if res.Payment.State != StateCompleted {
		t.Fatalf("expected StateCompleted, got %s", res.Payment.State)
	}

	// Verify double entry and balances
	var senderBal, receiverBal int64
	_ = data.pool.QueryRow(context.Background(), `SELECT balance_paise FROM accounts WHERE id = $1`, data.sourceID).Scan(&senderBal)
	_ = data.pool.QueryRow(context.Background(), `SELECT balance_paise FROM accounts WHERE id = $1`, data.receiverID).Scan(&receiverBal)
	if senderBal != 600 || receiverBal != 400 {
		t.Fatalf("balances unexpected: sender=%d (want 600), receiver=%d (want 400)", senderBal, receiverBal)
	}

	var txCount int
	_ = data.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM ledger_transactions WHERE payment_id = $1`, paymentID).Scan(&txCount)
	if txCount != 1 {
		t.Fatalf("expected exactly 1 ledger transaction, found %d", txCount)
	}
}

func TestResume_PoolConstrained(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set")
	}
	// Small pool with 4 max connections
	data := newRepositoryTestDataWithMaxConns(t, 4)
	defer data.close(t)

	svc := NewService(accounts.NewRepository(data.pool), recipients.NewRepository(data.pool), data.repository, nil)
	// Reserve 2 slots for queries, 2 for concurrency
	svc.SetPaymentConcurrency(2)

	paymentIDs := make([]uuid.UUID, 5)
	for i := range paymentIDs {
		paymentIDs[i] = uuid.New()
		_, err := data.pool.Exec(context.Background(), `
			INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state)
			VALUES ($1, $2, $3, $4, 100, 'INR', 'ONLINE', $5)`,
			paymentIDs[i], data.userID, data.sourceID, data.receiverID, StateCompleted)
		if err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 20)

	// Launch concurrent resumes and concurrent non-lock queries
	for i := 0; i < 10; i++ {
		wg.Add(1)
		pid := paymentIDs[i%len(paymentIDs)]
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, err := svc.Resume(ctx, pid)
			if err != nil {
				errCh <- err
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, err := data.repository.Get(ctx, pid)
			if err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("pool constrained test encountered error: %v", err)
	}
}

func TestResume_TwoResumesAndDuplicateCreate(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set")
	}
	data := newRepositoryTestData(t)
	defer data.close(t)

	setBalances(t, data, 5000, 0)
	insertParticipantAccount(t, data.pool, "bank_a", data.sourceID, "src-"+data.sourceID.String(), 5000)
	insertParticipantAccount(t, data.pool, "bank_a", data.receiverID, "dst-"+data.receiverID.String(), 0)
	defer func() {
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM bank_a.ledger_entries WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, data.userID)
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM bank_a.operations WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, data.userID)
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM bank_a.accounts WHERE id IN ($1, $2)`, data.sourceID, data.receiverID)
		_, _ = data.pool.Exec(context.Background(), `DELETE FROM payment_bank_operations WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, data.userID)
	}()
	bankAServer := httptest.NewServer(bankservice.Handler(bankservice.NewService(data.pool)))
	defer bankAServer.Close()

	bankAClient, err := bank.NewHTTPClient(bankAServer.URL, bankAServer.Client())
	if err != nil {
		t.Fatal(err)
	}

	adapters := map[uuid.UUID]bank.BankAdapter{
		data.bankID: bankAClient,
	}

	executionTargets := map[RouteKey][]ExecutionTarget{
		{SourceBankID: data.bankID, DestinationBankID: data.bankID}: {
			{
				CandidateID:        "direct",
				ExecutionTargetID:  "RAIL-A",
				SourceAdapter:      bankAClient,
				DestinationAdapter: bankAClient,
			},
		},
	}

	svc := NewServiceWithExecutionTargets(
		accounts.NewRepository(data.pool),
		recipients.NewRepository(data.pool),
		data.repository,
		adapters,
		executionTargets,
		nil,
		SelectionModeAdaptive,
		"",
	)
	svc.SetPaymentConcurrency(3)

	createInput := CreateInput{
		UserID:          data.userID,
		SourceAccountID: data.sourceID,
		Recipient:       "receiver-" + data.receiverUserID.String(),
		AmountPaise:     1000,
		Currency:        "INR",
		Note:            "Serialization test",
		IdempotencyKey:  "idemp-" + uuid.NewString(),
	}

	// 1. Initial creation
	payment, duplicate, err := svc.CreateWithResult(context.Background(), createInput)
	if err != nil || duplicate {
		t.Fatalf("initial CreateWithResult failed: err=%v, duplicate=%v", err, duplicate)
	}
	if payment.State != StateCompleted {
		t.Fatalf("expected initial payment COMPLETED, got %s", payment.State)
	}

	// 2. Now run 2 concurrent Resumes and 1 concurrent duplicate CreateWithResult
	var wg sync.WaitGroup
	errCh := make(chan error, 3)

	// Resume 1
	wg.Add(1)
	go func() {
		defer wg.Done()
		res, rErr := svc.Resume(context.Background(), payment.ID)
		if rErr != nil {
			errCh <- rErr
			return
		}
		if res.Payment.State != StateCompleted {
			errCh <- errors.New("resume 1 did not observe COMPLETED state")
		}
	}()

	// Resume 2
	wg.Add(1)
	go func() {
		defer wg.Done()
		res, rErr := svc.Resume(context.Background(), payment.ID)
		if rErr != nil {
			errCh <- rErr
			return
		}
		if res.Payment.State != StateCompleted {
			errCh <- errors.New("resume 2 did not observe COMPLETED state")
		}
	}()

	// Duplicate CreateWithResult
	wg.Add(1)
	go func() {
		defer wg.Done()
		dupPayment, isDup, cErr := svc.CreateWithResult(context.Background(), createInput)
		if cErr != nil {
			errCh <- cErr
			return
		}
		if !isDup {
			errCh <- errors.New("expected duplicate=true for CreateWithResult")
			return
		}
		if dupPayment.State != StateCompleted {
			errCh <- errors.New("duplicate create did not observe COMPLETED state")
		}
	}()

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent execution encountered error: %v", err)
	}

	// 3. Financial Invariants Verification
	// Assert exactly ONE ledger transaction exists for payment.ID
	var ledgerTxCount int
	if err := data.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM ledger_transactions WHERE payment_id = $1`, payment.ID).Scan(&ledgerTxCount); err != nil {
		t.Fatal(err)
	}
	if ledgerTxCount != 1 {
		t.Fatalf("expected exactly 1 ledger transaction, found %d", ledgerTxCount)
	}

	// Assert exactly ONE debit and ONE credit entry
	var debitCount, creditCount int
	rows, err := data.pool.Query(context.Background(), `
		SELECT entry_type, COUNT(*)
		FROM ledger_entries le
		JOIN ledger_transactions lt ON lt.id = le.ledger_transaction_id
		WHERE lt.payment_id = $1
		GROUP BY entry_type`, payment.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var entryType string
		var count int
		if err := rows.Scan(&entryType, &count); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if entryType == ledger.EntryDebit {
			debitCount = count
		} else if entryType == ledger.EntryCredit {
			creditCount = count
		}
	}
	rows.Close()
	if debitCount != 1 || creditCount != 1 {
		t.Fatalf("expected 1 debit and 1 credit entry, got debit=%d credit=%d", debitCount, creditCount)
	}

	// Assert balances: exactly 1000 paise deducted from sender and credited to receiver
	var senderBal, receiverBal int64
	_ = data.pool.QueryRow(context.Background(), `SELECT balance_paise FROM accounts WHERE id = $1`, data.sourceID).Scan(&senderBal)
	_ = data.pool.QueryRow(context.Background(), `SELECT balance_paise FROM accounts WHERE id = $1`, data.receiverID).Scan(&receiverBal)
	if senderBal != 4000 || receiverBal != 1000 {
		t.Fatalf("balances incorrect after concurrent resumes and duplicate create: sender=%d, receiver=%d", senderBal, receiverBal)
	}

	// Assert operation rows in payment_bank_operations: exactly 4 operations (hold, provisional-credit, confirm-hold, credit)
	// and no duplicate operations from concurrent resumes or duplicate create!
	var opCount int
	_ = data.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM payment_bank_operations WHERE payment_id = $1`, payment.ID).Scan(&opCount)
	if opCount != 4 {
		t.Fatalf("expected exactly 4 bank operations (hold + provisional credit + confirm hold + credit), found %d", opCount)
	}
}

func uuidToLockKey(id uuid.UUID) uint64 {
	var k1, k2 uint64
	for i := 0; i < 8; i++ {
		k1 = (k1 << 8) | uint64(id[i])
		k2 = (k2 << 8) | uint64(id[8+i])
	}
	return k1 ^ k2
}
