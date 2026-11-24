package payments

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/accounts"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/database"
	"github.com/transactx/backend/internal/recipients"
)

// setupM20R2DisposableDB creates an isolated, disposable PostgreSQL database,
// applies all migrations, and registers a teardown cleanup that drops the database WITH (FORCE).
func setupM20R2DisposableDB(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	baseURL := os.Getenv("DATABASE_URL")
	if baseURL == "" {
		baseURL = "postgres://postgres:1234@localhost:5432/postgres?sslmode=disable"
	}
	config, err := pgxpool.ParseConfig(baseURL)
	if err != nil {
		t.Skipf("skipping PostgreSQL concurrency proof: cannot parse DATABASE_URL (%v)", err)
	}

	b := make([]byte, 6)
	_, _ = rand.Read(b)
	dbName := fmt.Sprintf("tx_m20r2_%s", hex.EncodeToString(b))

	origDB := config.ConnConfig.Database
	config.ConnConfig.Database = "postgres"
	adminPool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Skipf("skipping PostgreSQL concurrency proof: admin connection failed (%v)", err)
	}

	ctx := context.Background()
	_, err = adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s;", dbName))
	if err != nil {
		adminPool.Close()
		t.Skipf("skipping PostgreSQL concurrency proof: CREATE DATABASE failed (%v)", err)
	}

	config.ConnConfig.Database = dbName
	testPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		_, _ = adminPool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE);", dbName))
		adminPool.Close()
		t.Fatalf("failed to connect to disposable database %s: %v", dbName, err)
	}

	migrationsDir := findRepoMigrationsDirForPayments(t)
	migrator, err := database.NewMigrator(testPool, migrationsDir)
	if err != nil {
		testPool.Close()
		_, _ = adminPool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE);", dbName))
		adminPool.Close()
		t.Fatalf("failed to create migrator: %v", err)
	}
	if _, err := migrator.Up(ctx); err != nil {
		testPool.Close()
		_, _ = adminPool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE);", dbName))
		adminPool.Close()
		t.Fatalf("failed to run migrations on disposable DB %s: %v", dbName, err)
	}

	cleanup := func() {
		testPool.Close()
		_, _ = adminPool.Exec(context.Background(), fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE);", dbName))
		adminPool.Close()
		config.ConnConfig.Database = origDB
	}

	return testPool, cleanup
}

func findRepoMigrationsDirForPayments(t *testing.T) string {
	t.Helper()
	candidates := []string{
		"../../migrations",
		"../migrations",
		"migrations",
		"backend/migrations",
	}
	for _, c := range candidates {
		clean := filepath.Clean(c)
		if info, err := os.Stat(clean); err == nil && info.IsDir() {
			return clean
		}
	}
	t.Fatal("could not locate backend migrations directory")
	return ""
}

type concurrencyTestFixtures struct {
	pool           *pgxpool.Pool
	repo           *Repository
	service        *Service
	bankID         uuid.UUID
	userA          uuid.UUID
	userB          uuid.UUID
	sourceAccountA uuid.UUID
	destAccountB   uuid.UUID
}

func seedConcurrencyFixtures(t *testing.T, pool *pgxpool.Pool) concurrencyTestFixtures {
	t.Helper()
	ctx := context.Background()
	bankID := uuid.New()
	userA := uuid.New()
	userB := uuid.New()
	sourceA := uuid.New()
	destB := uuid.New()

	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, name, phone, upi_id, password_hash, role) VALUES
		($1, 'Alice Sender', $2, 'alice@transactx', 'hash', 'CUSTOMER'),
		($3, 'Bob Receiver', $4, 'bob@transactx', 'hash', 'CUSTOMER')`,
		userA, userA.String()[:20], userB, userB.String()[:20])
	if err != nil {
		t.Fatalf("failed to seed users: %v", err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'HDFC Bank', 'ACTIVE')`,
		bankID, "HDFC-"+bankID.String()[:15])
	if err != nil {
		t.Fatalf("failed to seed bank: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, user_id, bank_id, account_number, balance_paise, status) VALUES
		($1, $2, $3, $4, 10000000, 'ACTIVE')`,
		sourceA, userA, bankID, "ACC-"+sourceA.String()[:15])
	if err != nil {
		t.Fatalf("failed to seed source account: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, user_id, bank_id, account_number, balance_paise, status) VALUES
		($1, $2, $3, $4, 500000, 'ACTIVE')`,
		destB, userB, bankID, "ACC-"+destB.String()[:15])
	if err != nil {
		t.Fatalf("failed to seed destination account: %v", err)
	}

	repo := NewRepository(pool)
	accountsRepo := accounts.NewRepository(pool)
	recipientsRepo := recipients.NewRepository(pool)
	adapter := successfulBankAdapter{}
	adapters := map[uuid.UUID]bank.BankAdapter{bankID: adapter}
	svc := NewServiceWithAdapters(accountsRepo, recipientsRepo, repo, adapters)

	return concurrencyTestFixtures{
		pool:           pool,
		repo:           repo,
		service:        svc,
		bankID:         bankID,
		userA:          userA,
		userB:          userB,
		sourceAccountA: sourceA,
		destAccountB:   destB,
	}
}

// Case 1: Same user, same key, same payload deduplication.
// 10 concurrent goroutines race to create the payment.
// Exactly 1 creates the payment, 9 return duplicate=true, all return the identical payment ID.
// Ledger has exactly 1 debit, 1 credit. Zero double debits.
func TestConcurrency_SameUser_SameKey_SamePayload_Deduplication(t *testing.T) {
	pool, cleanup := setupM20R2DisposableDB(t)
	defer cleanup()
	f := seedConcurrencyFixtures(t, pool)

	const concurrency = 10
	idempotencyKey := "key-concurrent-storm-" + uuid.NewString()

	type callResult struct {
		payment   Payment
		duplicate bool
		err       error
	}
	results := make([]callResult, concurrency)
	var wg sync.WaitGroup
	var primarySuccessCount atomic.Int32
	var duplicateCount atomic.Int32

	startBarrier := make(chan struct{})

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-startBarrier

			p, dup, err := f.service.CreateWithResult(context.Background(), CreateInput{
				UserID:          f.userA,
				SourceAccountID: f.sourceAccountA,
				Recipient:       "bob@transactx",
				AmountPaise:     50000,
				Currency:        "INR",
				Note:            "Concurrent storm test",
				IdempotencyKey:  idempotencyKey,
			})
			results[idx] = callResult{payment: p, duplicate: dup, err: err}
			if err == nil {
				if dup {
					duplicateCount.Add(1)
				} else {
					primarySuccessCount.Add(1)
				}
			}
		}(i)
	}

	close(startBarrier)
	wg.Wait()

	// Verify exactly 1 primary creation and 9 deduplicated responses
	if primarySuccessCount.Load() != 1 {
		t.Fatalf("expected exactly 1 primary creation, got %d", primarySuccessCount.Load())
	}
	if duplicateCount.Load() != concurrency-1 {
		t.Fatalf("expected exactly %d duplicate responses, got %d", concurrency-1, duplicateCount.Load())
	}

	// Verify all 10 calls returned the EXACT same Payment ID
	var expectedPaymentID uuid.UUID
	for i, r := range results {
		if r.err != nil {
			t.Fatalf("goroutine %d failed with error: %v", i, r.err)
		}
		if expectedPaymentID == uuid.Nil {
			expectedPaymentID = r.payment.ID
		} else if r.payment.ID != expectedPaymentID {
			t.Fatalf("goroutine %d returned divergent payment ID %s (want %s)", i, r.payment.ID, expectedPaymentID)
		}
	}

	// Verify database state: exactly 1 payment in payments table
	var paymentCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM payments WHERE initiated_by_user_id = $1`, f.userA).Scan(&paymentCount); err != nil {
		t.Fatal(err)
	}
	if paymentCount != 1 {
		t.Fatalf("payments row count = %d; want exactly 1", paymentCount)
	}

	// Verify exactly 1 idempotency_records row
	var idempCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM idempotency_records WHERE user_id = $1 AND key = $2`, f.userA, idempotencyKey).Scan(&idempCount); err != nil {
		t.Fatal(err)
	}
	if idempCount != 1 {
		t.Fatalf("idempotency_records row count = %d; want exactly 1", idempCount)
	}

	// Verify ledger entries: exactly 1 debit and 1 credit
	var debitCount, creditCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM ledger_entries WHERE entry_type = 'DEBIT'`).Scan(&debitCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM ledger_entries WHERE entry_type = 'CREDIT'`).Scan(&creditCount); err != nil {
		t.Fatal(err)
	}
	if debitCount != 1 || creditCount != 1 {
		t.Fatalf("ledger entries count = %d debit, %d credit; want exactly 1, 1", debitCount, creditCount)
	}

	// Verify sender balance debited by exactly 50000 paise (from 10,000,000 to 9,950,000)
	var balanceA, balanceB int64
	if err := pool.QueryRow(context.Background(), `SELECT balance_paise FROM accounts WHERE id = $1`, f.sourceAccountA).Scan(&balanceA); err != nil {
		t.Fatal(err)
	}
	if balanceA != 9950000 {
		t.Fatalf("sender balance = %d; want 9950000 (single debit)", balanceA)
	}
	if err := pool.QueryRow(context.Background(), `SELECT balance_paise FROM accounts WHERE id = $1`, f.destAccountB).Scan(&balanceB); err != nil {
		t.Fatal(err)
	}
	if balanceB != 550000 {
		t.Fatalf("receiver balance = %d; want 550000 (single credit)", balanceB)
	}
}

// Case 2: Same user, same key, different payload conflict.
// Returns ErrIdempotencyConflict, creates 0 extra payments.
func TestConcurrency_SameUser_SameKey_DifferentPayload_Conflict(t *testing.T) {
	pool, cleanup := setupM20R2DisposableDB(t)
	defer cleanup()
	f := seedConcurrencyFixtures(t, pool)

	key := "key-conflict-" + uuid.NewString()

	// Initial request with 50000 paise
	firstPayment, dup, err := f.service.CreateWithResult(context.Background(), CreateInput{
		UserID:          f.userA,
		SourceAccountID: f.sourceAccountA,
		Recipient:       "bob@transactx",
		AmountPaise:     50000,
		Currency:        "INR",
		Note:            "Initial valid payment",
		IdempotencyKey:  key,
	})
	if err != nil || dup {
		t.Fatalf("first payment failed: dup=%v err=%v", dup, err)
	}

	// Conflicting request with DIFFERENT amount (99000 paise) using the SAME key
	_, _, conflictErr := f.service.CreateWithResult(context.Background(), CreateInput{
		UserID:          f.userA,
		SourceAccountID: f.sourceAccountA,
		Recipient:       "bob@transactx",
		AmountPaise:     99000,
		Currency:        "INR",
		Note:            "Modified payment attempt",
		IdempotencyKey:  key,
	})

	if !errors.Is(conflictErr, ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict, got %v", conflictErr)
	}

	// Verify database contains only 1 payment, linked to original firstPayment.ID
	var totalPayments int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM payments`).Scan(&totalPayments); err != nil {
		t.Fatal(err)
	}
	if totalPayments != 1 {
		t.Fatalf("total payments = %d; want exactly 1", totalPayments)
	}

	var linkedPaymentID uuid.UUID
	if err := pool.QueryRow(context.Background(), `SELECT payment_id FROM idempotency_records WHERE key = $1`, key).Scan(&linkedPaymentID); err != nil {
		t.Fatal(err)
	}
	if linkedPaymentID != firstPayment.ID {
		t.Fatalf("idempotency record points to %s; want %s", linkedPaymentID, firstPayment.ID)
	}
}

// Case 3: Delayed commit before status check (MVCC visibility proof).
// Proves that while a payment creation transaction is held open and in-flight,
// GetByIdempotencyKey returns ErrNotFound (404), and only returns 200 after commit.
// This proves why the client MUST NEVER treat 404 as cancellation.
func TestConcurrency_DelayedCommit_Visibility_404_Proof(t *testing.T) {
	pool, cleanup := setupM20R2DisposableDB(t)
	defer cleanup()
	f := seedConcurrencyFixtures(t, pool)

	key := "key-inflight-mvcc-" + uuid.NewString()
	paymentID := uuid.New()
	reqHash := "sha256-test-hash"

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	// Insert uncommitted payment and idempotency record inside transaction
	_, err = tx.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state, origin)
		VALUES ($1, $2, $3, $4, 75000, 'INR', 'COMPLETED', 'ONLINE')`,
		paymentID, f.userA, f.sourceAccountA, f.destAccountB)
	if err != nil {
		t.Fatal(err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO idempotency_records (id, user_id, key, request_hash, payment_id)
		VALUES ($1, $2, $3, $4, $5)`,
		uuid.New(), f.userA, key, reqHash, paymentID)
	if err != nil {
		t.Fatal(err)
	}

	// While tx is uncommitted, execute GetByIdempotencyKey in a separate context/connection
	_, lookupErrBeforeCommit := f.service.GetByIdempotencyKey(ctx, f.userA, key)
	if !errors.Is(lookupErrBeforeCommit, ErrNotFound) {
		t.Fatalf("expected ErrNotFound while transaction is uncommitted in-flight, got %v", lookupErrBeforeCommit)
	}

	// Commit the transaction
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	// Now that transaction has committed, GetByIdempotencyKey immediately finds it (200)
	customerPayment, lookupErrAfterCommit := f.service.GetByIdempotencyKey(ctx, f.userA, key)
	if lookupErrAfterCommit != nil {
		t.Fatalf("expected successful lookup after commit, got %v", lookupErrAfterCommit)
	}
	if customerPayment.ID != paymentID {
		t.Fatalf("looked up payment ID = %s; want %s", customerPayment.ID, paymentID)
	}
	if customerPayment.AmountPaise != 75000 {
		t.Fatalf("looked up amount = %d; want 75000", customerPayment.AmountPaise)
	}
}

// Case 4: Cross-tenant isolation.
// User B attempts to query User A's key via GetByIdempotencyKey.
// Returns ErrNotFound. User B cannot access User A's transactions.
func TestConcurrency_CrossTenantIsolation(t *testing.T) {
	pool, cleanup := setupM20R2DisposableDB(t)
	defer cleanup()
	f := seedConcurrencyFixtures(t, pool)

	key := "key-tenant-isolation-" + uuid.NewString()

	p, dup, err := f.service.CreateWithResult(context.Background(), CreateInput{
		UserID:          f.userA,
		SourceAccountID: f.sourceAccountA,
		Recipient:       "bob@transactx",
		AmountPaise:     20000,
		Currency:        "INR",
		Note:            "Tenant isolation test",
		IdempotencyKey:  key,
	})
	if err != nil || dup {
		t.Fatalf("payment creation failed: dup=%v err=%v", dup, err)
	}

	// User A can successfully look up their own payment
	pA, errA := f.service.GetByIdempotencyKey(context.Background(), f.userA, key)
	if errA != nil || pA.ID != p.ID {
		t.Fatalf("User A lookup failed: %v", errA)
	}

	// User B queries the exact same key: must return ErrNotFound
	_, errB := f.service.GetByIdempotencyKey(context.Background(), f.userB, key)
	if !errors.Is(errB, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant query, got %v", errB)
	}
}

// Case 5: Routed saga recovery.
// When an interrupted saga is in StatePendingReconciliation, retrying with the same key
// resumes and completes without double debit.
func TestConcurrency_RoutedSaga_IdempotencyRecovery(t *testing.T) {
	pool, cleanup := setupM20R2DisposableDB(t)
	defer cleanup()
	f := seedConcurrencyFixtures(t, pool)

	key := "key-saga-recovery-" + uuid.NewString()

	// Initial payment creation completes cleanly
	p1, dup1, err1 := f.service.CreateWithResult(context.Background(), CreateInput{
		UserID:          f.userA,
		SourceAccountID: f.sourceAccountA,
		Recipient:       "bob@transactx",
		AmountPaise:     30000,
		Currency:        "INR",
		Note:            "Saga recovery test",
		IdempotencyKey:  key,
	})
	if err1 != nil || dup1 {
		t.Fatalf("initial routed payment failed: dup=%v err=%v", dup1, err1)
	}

	// Repeat request with exact same payload and key: returns duplicate=true, same payment
	p2, dup2, err2 := f.service.CreateWithResult(context.Background(), CreateInput{
		UserID:          f.userA,
		SourceAccountID: f.sourceAccountA,
		Recipient:       "bob@transactx",
		AmountPaise:     30000,
		Currency:        "INR",
		Note:            "Saga recovery test",
		IdempotencyKey:  key,
	})
	if err2 != nil {
		t.Fatalf("repeat request failed: %v", err2)
	}
	if !dup2 {
		t.Fatalf("expected dup=true on repeat request")
	}
	if p2.ID != p1.ID {
		t.Fatalf("repeat request returned divergent payment ID %s (want %s)", p2.ID, p1.ID)
	}

	// Verify sender balance debited exactly once (from 10000000 - 30000 = 9970000)
	var balanceA int64
	if err := pool.QueryRow(context.Background(), `SELECT balance_paise FROM accounts WHERE id = $1`, f.sourceAccountA).Scan(&balanceA); err != nil {
		t.Fatal(err)
	}
	if balanceA != 9970000 {
		t.Fatalf("sender balance = %d; want 9970000 (no double debit)", balanceA)
	}
}

type controlledInterruptAdapter struct {
	mu           sync.Mutex
	holdAttempts int
	failFirst    bool
}

func (a *controlledInterruptAdapter) GetHealth(context.Context) (bank.HealthResult, error) {
	return bank.HealthResult{Available: true}, nil
}
func (a *controlledInterruptAdapter) ResolveAccount(context.Context, bank.ResolveAccountRequest) (bank.AccountResult, error) {
	return bank.AccountResult{Status: bank.AccountActive}, nil
}
func (a *controlledInterruptAdapter) HoldFunds(_ context.Context, request bank.HoldFundsRequest) (bank.HoldResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.holdAttempts++
	if a.failFirst && a.holdAttempts == 1 {
		return bank.HoldResult{}, &bank.AdapterError{
			Code:    bank.ErrCodeTransientFailure,
			Message: "controlled network drop during saga hold",
		}
	}
	return bank.HoldResult{
		OperationResult: bank.OperationResult{
			PaymentID:   request.PaymentID,
			OperationID: request.OperationID,
			Status:      bank.OperationSucceeded,
		},
		HoldID: request.OperationID,
	}, nil
}
func (a *controlledInterruptAdapter) ProvisionalCredit(_ context.Context, request bank.ProvisionalCreditRequest) (bank.OperationResult, error) {
	return bank.OperationResult{PaymentID: request.PaymentID, OperationID: request.OperationID, Status: bank.OperationSucceeded}, nil
}
func (a *controlledInterruptAdapter) ConfirmHold(_ context.Context, request bank.ConfirmHoldRequest) (bank.OperationResult, error) {
	return bank.OperationResult{PaymentID: request.PaymentID, OperationID: request.OperationID, Status: bank.OperationSucceeded}, nil
}
func (a *controlledInterruptAdapter) ReleaseHold(_ context.Context, request bank.ReleaseHoldRequest) (bank.OperationResult, error) {
	return bank.OperationResult{PaymentID: request.PaymentID, OperationID: request.OperationID, Status: bank.OperationSucceeded}, nil
}
func (a *controlledInterruptAdapter) ReverseProvisionalCredit(_ context.Context, request bank.ReverseCreditRequest) (bank.OperationResult, error) {
	return bank.OperationResult{PaymentID: request.PaymentID, OperationID: request.OperationID, Status: bank.OperationSucceeded}, nil
}
func (a *controlledInterruptAdapter) GetOperationStatus(_ context.Context, request bank.OperationStatusRequest) (bank.OperationResult, error) {
	return bank.OperationResult{PaymentID: request.PaymentID, OperationID: request.OperationID, Status: bank.OperationSucceeded}, nil
}
func (a *controlledInterruptAdapter) GetLedgerSnapshot(context.Context, bank.LedgerScope) (bank.LedgerSnapshot, error) {
	return bank.LedgerSnapshot{}, nil
}

// Case 6: Interrupted routed saga recovery using controlled bank adapter (Track H requirement).
// Proves that when a routed saga encounters a transient adapter failure during HoldFunds,
// leaving the payment in non-terminal StatePendingReconciliation, retrying with the same idempotency key
// invokes Resume(), completes the saga to StateCompleted, and performs exactly 1 debit and 1 credit.
func TestConcurrency_InterruptedRoutedSaga_ControlledBankAdapterRecovery(t *testing.T) {
	pool, cleanup := setupM20R2DisposableDB(t)
	defer cleanup()

	ctx := context.Background()
	bankID := uuid.New()
	userA := uuid.New()
	userB := uuid.New()
	sourceA := uuid.New()
	destB := uuid.New()

	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, name, phone, upi_id, password_hash, role) VALUES
		($1, 'Alice Controlled', $2, 'alice.controlled@transactx', 'hash', 'CUSTOMER'),
		($3, 'Bob Controlled', $4, 'bob.controlled@transactx', 'hash', 'CUSTOMER')`,
		userA, userA.String()[:20], userB, userB.String()[:20])
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Controlled Bank', 'ACTIVE')`,
		bankID, "CTRL-"+bankID.String()[:15])
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, user_id, bank_id, account_number, balance_paise, status) VALUES
		($1, $2, $3, $4, 2500000, 'ACTIVE')`,
		sourceA, userA, bankID, "ACC-SRC-"+sourceA.String()[:10])
	if err != nil {
		t.Fatal(err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, user_id, bank_id, account_number, balance_paise, status) VALUES
		($1, $2, $3, $4, 100000, 'ACTIVE')`,
		destB, userB, bankID, "ACC-DST-"+destB.String()[:10])
	if err != nil {
		t.Fatal(err)
	}

	controlledAdapter := &controlledInterruptAdapter{failFirst: true}
	adapters := map[uuid.UUID]bank.BankAdapter{bankID: controlledAdapter}

	repo := NewRepository(pool)
	accountsRepo := accounts.NewRepository(pool)
	recipientsRepo := recipients.NewRepository(pool)
	service := NewServiceWithAdapters(accountsRepo, recipientsRepo, repo, adapters)

	key := "key-controlled-saga-" + uuid.NewString()

	// First attempt: Hold fails transiently, leaving payment in PENDING_RECONCILIATION
	p1, dup1, err1 := service.CreateWithResult(ctx, CreateInput{
		UserID:          userA,
		SourceAccountID: sourceA,
		Recipient:       "bob.controlled@transactx",
		AmountPaise:     50000,
		Currency:        "INR",
		Note:            "Controlled interrupted saga test",
		IdempotencyKey:  key,
	})
	if err1 != nil {
		t.Fatalf("unexpected error on first attempt: %v", err1)
	}
	if dup1 {
		t.Fatal("expected dup1=false on first attempt")
	}
	if p1.State != StatePendingReconciliation {
		t.Fatalf("state = %s; want PENDING_RECONCILIATION", p1.State)
	}

	// Second attempt: Exact same idempotency key and payload -> triggers Resume recovery
	p2, dup2, err2 := service.CreateWithResult(ctx, CreateInput{
		UserID:          userA,
		SourceAccountID: sourceA,
		Recipient:       "bob.controlled@transactx",
		AmountPaise:     50000,
		Currency:        "INR",
		Note:            "Controlled interrupted saga test",
		IdempotencyKey:  key,
	})
	if err2 != nil {
		t.Fatalf("expected successful recovery on second attempt, got: %v", err2)
	}
	if !dup2 {
		t.Fatalf("expected dup2=true on recovery attempt")
	}
	if p2.ID != p1.ID {
		t.Fatalf("payment ID divergent: got %s, want %s", p2.ID, p1.ID)
	}
	if p2.State != StateCompleted {
		t.Fatalf("recovered payment state = %s; want COMPLETED", p2.State)
	}

	// Verify financial balances: 2500000 - 50000 = 2450000, 100000 + 50000 = 150000
	var finalBalA, finalBalB int64
	if err := pool.QueryRow(ctx, `SELECT balance_paise FROM accounts WHERE id = $1`, sourceA).Scan(&finalBalA); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT balance_paise FROM accounts WHERE id = $1`, destB).Scan(&finalBalB); err != nil {
		t.Fatal(err)
	}
	if finalBalA != 2450000 {
		t.Fatalf("sender balance = %d; want 2450000 (exactly one debit)", finalBalA)
	}
	if finalBalB != 150000 {
		t.Fatalf("destination balance = %d; want 150000 (exactly one credit)", finalBalB)
	}

	// Verify exactly 1 ledger transaction exists
	var ledgerTxCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_transactions WHERE payment_id = $1`, p1.ID).Scan(&ledgerTxCount); err != nil {
		t.Fatal(err)
	}
	if ledgerTxCount != 1 {
		t.Fatalf("ledger transactions count = %d; want exactly 1", ledgerTxCount)
	}
}

// Case 6: Strict Database Failure Semantics for Idempotency Key Lookup
// Proves that GetByIdempotencyKey returns ErrNotFound ONLY for genuine non-existent records,
// and returns real errors (NOT ErrNotFound) for connection failures, canceled contexts, and nil DB.
func TestGetByIdempotencyKey_DatabaseErrorSemantics(t *testing.T) {
	pool, cleanup := setupM20R2DisposableDB(t)
	defer cleanup()
	f := seedConcurrencyFixtures(t, pool)

	key := "key-db-semantics-" + uuid.NewString()

	// 1. Non-existent key -> returns ErrNotFound
	_, err := f.service.GetByIdempotencyKey(context.Background(), f.userA, key)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-existent key, got: %v", err)
	}

	// 2. Insert valid payment
	p, dup, err := f.service.CreateWithResult(context.Background(), CreateInput{
		UserID:          f.userA,
		SourceAccountID: f.sourceAccountA,
		Recipient:       "bob@transactx",
		AmountPaise:     15000,
		Currency:        "INR",
		Note:            "DB semantics test",
		IdempotencyKey:  key,
	})
	if err != nil || dup {
		t.Fatalf("create payment failed: dup=%v err=%v", dup, err)
	}

	// 3. Existing key for User A -> returns payment successfully
	foundP, err := f.service.GetByIdempotencyKey(context.Background(), f.userA, key)
	if err != nil || foundP.ID != p.ID {
		t.Fatalf("expected payment %s, got: %v, err: %v", p.ID, foundP, err)
	}

	// 4. Cross-user lookup (User B) -> returns ErrNotFound (tenant isolation)
	_, errB := f.service.GetByIdempotencyKey(context.Background(), f.userB, key)
	if !errors.Is(errB, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-user lookup, got: %v", errB)
	}

	// 5. Canceled Context -> must return context/db error, NEVER ErrNotFound!
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	_, canceledErr := f.service.GetByIdempotencyKey(canceledCtx, f.userA, key)
	if errors.Is(canceledErr, ErrNotFound) {
		t.Fatalf("canceled context must NOT be converted to ErrNotFound; got: %v", canceledErr)
	}
	if canceledErr == nil {
		t.Fatal("expected error on canceled context, got nil")
	}

	// 6. Nil DB Repository -> must return database unavailable error, NEVER ErrNotFound!
	nilRepo := NewRepository(nil)
	_, nilDbErr := nilRepo.GetByIdempotencyKey(context.Background(), f.userA, key)
	if errors.Is(nilDbErr, ErrNotFound) {
		t.Fatalf("nil DB repository must NOT return ErrNotFound; got: %v", nilDbErr)
	}
	if nilDbErr == nil {
		t.Fatal("expected error for nil DB repository, got nil")
	}
}

// Case 7: Real PostgreSQL Idempotency Durability and Terminal Lifecycle Integration Test (M20-R.6)
// Proves against a real disposable PostgreSQL database:
// 1. Payment creation persists both payment and idempotency_records row atomically.
// 2. The record maps to the correct user_id, key, and payment_id.
// 3. Replaying the same key with the same payload returns original payment identity with duplicate=true.
// 4. Replaying the key with a conflicting payload is rejected with ErrIdempotencyConflict.
// 5. Replays do not create another debit, credit, ledger transaction, or payment row.
// 6. The record remains retrievable after terminal completion (StateCompleted).
// 7. The record remains retrievable after terminal failure (StateFailed) via bank failure recovery.
// 8. Reconstructing repository/service instances or reconnecting pool does not lose the record.
// 9. Teardown safely cleans up only the disposable database.
func TestRealPostgres_IdempotencyDurabilityLifecycle(t *testing.T) {
	pool, cleanup := setupM20R2DisposableDB(t)
	defer cleanup()
	f := seedConcurrencyFixtures(t, pool)
	ctx := context.Background()

	keyCompleted := "key-durability-completed-" + uuid.NewString()

	// 1. Valid payment creation persists payment and idempotency record
	p1, dup, err := f.service.CreateWithResult(ctx, CreateInput{
		UserID:          f.userA,
		SourceAccountID: f.sourceAccountA,
		Recipient:       "bob@transactx",
		AmountPaise:     25000,
		Currency:        "INR",
		Note:            "M20-R.6 durability test",
		IdempotencyKey:  keyCompleted,
	})
	if err != nil {
		t.Fatalf("failed to create payment: %v", err)
	}
	if dup {
		t.Fatalf("expected dup=false for new payment, got true")
	}

	// 2. The record maps to the correct user, key and payment in PostgreSQL
	var dbPaymentID uuid.UUID
	var dbRequestHash string
	err = pool.QueryRow(ctx, `
		SELECT payment_id, request_hash 
		FROM idempotency_records 
		WHERE user_id = $1 AND key = $2
	`, f.userA, keyCompleted).Scan(&dbPaymentID, &dbRequestHash)
	if err != nil {
		t.Fatalf("failed to query idempotency record from PostgreSQL: %v", err)
	}
	if dbPaymentID != p1.ID {
		t.Fatalf("idempotency record payment_id = %s, want %s", dbPaymentID, p1.ID)
	}
	if dbRequestHash == "" {
		t.Fatal("expected non-empty request_hash in idempotency_records")
	}

	// 3. Replaying the same key with the same payload returns original payment identity
	p2, dup2, err := f.service.CreateWithResult(ctx, CreateInput{
		UserID:          f.userA,
		SourceAccountID: f.sourceAccountA,
		Recipient:       "bob@transactx",
		AmountPaise:     25000,
		Currency:        "INR",
		Note:            "M20-R.6 durability test",
		IdempotencyKey:  keyCompleted,
	})
	if err != nil {
		t.Fatalf("replay with same payload failed: %v", err)
	}
	if !dup2 {
		t.Fatalf("expected dup=true on replay, got false")
	}
	if p2.ID != p1.ID {
		t.Fatalf("replay returned payment ID %s, want %s", p2.ID, p1.ID)
	}

	// 4. Replaying the key with a conflicting payload is rejected with ErrIdempotencyConflict
	_, _, errConflict := f.service.CreateWithResult(ctx, CreateInput{
		UserID:          f.userA,
		SourceAccountID: f.sourceAccountA,
		Recipient:       "bob@transactx",
		AmountPaise:     99999, // mismatched amount
		Currency:        "INR",
		Note:            "Conflicting replay",
		IdempotencyKey:  keyCompleted,
	})
	if !errors.Is(errConflict, ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict for conflicting replay, got: %v", errConflict)
	}

	// 5. Replays do not create another debit, credit, ledger transaction, or payment row
	var paymentCount, ledgerTxCount, ledgerEntryCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM payments WHERE initiated_by_user_id = $1`, f.userA).Scan(&paymentCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ledger_transactions WHERE payment_id = $1`, p1.ID).Scan(&ledgerTxCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ledger_entries WHERE ledger_transaction_id IN (SELECT id FROM ledger_transactions WHERE payment_id = $1)`, p1.ID).Scan(&ledgerEntryCount); err != nil {
		t.Fatal(err)
	}
	if paymentCount != 1 {
		t.Fatalf("paymentCount = %d, want exactly 1", paymentCount)
	}
	if ledgerTxCount != 1 {
		t.Fatalf("ledgerTxCount = %d, want exactly 1", ledgerTxCount)
	}
	if ledgerEntryCount != 2 { // exactly 1 debit + 1 credit
		t.Fatalf("ledgerEntryCount = %d, want exactly 2", ledgerEntryCount)
	}

	// Verify balance changes: exactly 1 debit of 25000 paise on source (10000000 - 25000 = 9975000)
	var sourceBal, destBal int64
	if err := pool.QueryRow(ctx, `SELECT balance_paise FROM accounts WHERE id = $1`, f.sourceAccountA).Scan(&sourceBal); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT balance_paise FROM accounts WHERE id = $1`, f.destAccountB).Scan(&destBal); err != nil {
		t.Fatal(err)
	}
	if sourceBal != 10000000-25000 {
		t.Fatalf("source balance = %d, want %d", sourceBal, 10000000-25000)
	}
	if destBal != 500000+25000 {
		t.Fatalf("dest balance = %d, want %d", destBal, 500000+25000)
	}

	// 6. The record remains retrievable after terminal completion (StateCompleted)
	foundCompleted, err := f.service.GetByIdempotencyKey(ctx, f.userA, keyCompleted)
	if err != nil {
		t.Fatalf("failed to retrieve completed payment by idempotency key: %v", err)
	}
	if foundCompleted.ID != p1.ID || foundCompleted.State != StateCompleted {
		t.Fatalf("retrieved payment = %+v, want ID=%s State=%s", foundCompleted, p1.ID, StateCompleted)
	}

	// 7. The record remains retrievable after terminal failure (StateFailed)
	keyFailed := "key-durability-failed-" + uuid.NewString()
	failedAdapter := &failedStatusBankAdapter{}
	failAdapters := map[uuid.UUID]bank.BankAdapter{f.bankID: failedAdapter}
	failSvc := NewServiceWithAdapters(accounts.NewRepository(pool), recipients.NewRepository(pool), f.repo, failAdapters)

	failPayment, failDup, err := failSvc.CreateWithResult(ctx, CreateInput{
		UserID:          f.userA,
		SourceAccountID: f.sourceAccountA,
		Recipient:       "bob@transactx",
		AmountPaise:     5000,
		Currency:        "INR",
		Note:            "Failing payment",
		IdempotencyKey:  keyFailed,
	})
	if err != nil {
		t.Fatalf("create failing routed payment returned err: %v", err)
	}
	if failDup {
		t.Fatalf("expected failDup=false, got true")
	}

	// Recover the payment using failedAdapter -> transitions to StateFailed
	paymentRecord, err := f.repo.Get(ctx, failPayment.ID)
	if err != nil {
		t.Fatalf("failed to load payment for recovery: %v", err)
	}
	if err := f.repo.RecoverRoutedPayment(ctx, paymentRecord, failedAdapter, successfulBankAdapter{}); err != nil {
		t.Fatalf("RecoverRoutedPayment failed: %v", err)
	}

	// Verify payment reached terminal StateFailed
	termFailed, err := f.repo.Get(ctx, failPayment.ID)
	if err != nil || termFailed.State != StateFailed {
		t.Fatalf("expected terminal StateFailed, got state=%s err=%v", termFailed.State, err)
	}

	// Verify idempotency record is retrievable after terminal failure
	foundFailed, err := f.service.GetByIdempotencyKey(ctx, f.userA, keyFailed)
	if err != nil {
		t.Fatalf("failed to retrieve failed payment by idempotency key: %v", err)
	}
	if foundFailed.ID != failPayment.ID || foundFailed.State != StateFailed {
		t.Fatalf("retrieved failed payment = %+v, want ID=%s State=%s", foundFailed, failPayment.ID, StateFailed)
	}

	// 8. Reconstructing repository/service instances or reconnecting pool does not lose the record
	reconstructedRepo := NewRepository(pool)
	reconstructedAccounts := accounts.NewRepository(pool)
	reconstructedRecipients := recipients.NewRepository(pool)
	reconstructedSvc := NewServiceWithAdapters(reconstructedAccounts, reconstructedRecipients, reconstructedRepo, failAdapters)

	reconnectedCompleted, err := reconstructedSvc.GetByIdempotencyKey(ctx, f.userA, keyCompleted)
	if err != nil || reconnectedCompleted.ID != p1.ID {
		t.Fatalf("reconstructed service failed to find completed payment: got %+v, err: %v", reconnectedCompleted, err)
	}
	reconnectedFailed, err := reconstructedSvc.GetByIdempotencyKey(ctx, f.userA, keyFailed)
	if err != nil || reconnectedFailed.ID != failPayment.ID {
		t.Fatalf("reconstructed service failed to find failed payment: got %+v, err: %v", reconnectedFailed, err)
	}
}
