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
