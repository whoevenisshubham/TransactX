package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/transactx/backend/internal/accounts"
	"github.com/transactx/backend/internal/app"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/config"
	"github.com/transactx/backend/internal/payments"
	"github.com/transactx/backend/internal/recipients"
	"github.com/transactx/backend/internal/recovery"
)

var errBankOutage = &bank.AdapterError{Code: bank.ErrCodeBankUnavailable, Message: "bank outage / network unreachable"}

type controllableBankAdapter struct {
	mu      sync.RWMutex
	healthy bool
}

func newControllableBankAdapter() *controllableBankAdapter {
	return &controllableBankAdapter{healthy: true}
}

func (a *controllableBankAdapter) SetHealthy(h bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.healthy = h
}

func (a *controllableBankAdapter) isHealthy() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.healthy
}

func (a *controllableBankAdapter) GetHealth(ctx context.Context) (bank.HealthResult, error) {
	if !a.isHealthy() {
		return bank.HealthResult{Available: false}, errBankOutage
	}
	return bank.HealthResult{Available: true}, nil
}

func (a *controllableBankAdapter) ResolveAccount(ctx context.Context, req bank.ResolveAccountRequest) (bank.AccountResult, error) {
	if !a.isHealthy() {
		return bank.AccountResult{Status: bank.AccountInvalid}, errBankOutage
	}
	return bank.AccountResult{AccountID: req.AccountID, Status: bank.AccountActive}, nil
}

func (a *controllableBankAdapter) HoldFunds(ctx context.Context, req bank.HoldFundsRequest) (bank.HoldResult, error) {
	if !a.isHealthy() {
		return bank.HoldResult{}, errBankOutage
	}
	return bank.HoldResult{
		OperationResult: bank.OperationResult{
			PaymentID:     req.PaymentID,
			OperationID:   req.OperationID,
			BankReference: "ref-" + req.OperationID.String()[:8],
			Status:        bank.OperationSucceeded,
		},
		HoldID: req.OperationID,
	}, nil
}

func (a *controllableBankAdapter) ProvisionalCredit(ctx context.Context, req bank.ProvisionalCreditRequest) (bank.OperationResult, error) {
	if !a.isHealthy() {
		return bank.OperationResult{}, errBankOutage
	}
	return bank.OperationResult{
		PaymentID:     req.PaymentID,
		OperationID:   req.OperationID,
		BankReference: "ref-" + req.OperationID.String()[:8],
		Status:        bank.OperationSucceeded,
	}, nil
}

func (a *controllableBankAdapter) ConfirmHold(ctx context.Context, req bank.ConfirmHoldRequest) (bank.OperationResult, error) {
	if !a.isHealthy() {
		return bank.OperationResult{}, errBankOutage
	}
	return bank.OperationResult{
		PaymentID:     req.PaymentID,
		OperationID:   req.OperationID,
		BankReference: "ref-" + req.OperationID.String()[:8],
		Status:        bank.OperationSucceeded,
	}, nil
}

func (a *controllableBankAdapter) ReleaseHold(ctx context.Context, req bank.ReleaseHoldRequest) (bank.OperationResult, error) {
	if !a.isHealthy() {
		return bank.OperationResult{}, errBankOutage
	}
	return bank.OperationResult{
		PaymentID:     req.PaymentID,
		OperationID:   req.OperationID,
		BankReference: "ref-" + req.OperationID.String()[:8],
		Status:        bank.OperationSucceeded,
	}, nil
}

func (a *controllableBankAdapter) ReverseProvisionalCredit(ctx context.Context, req bank.ReverseCreditRequest) (bank.OperationResult, error) {
	if !a.isHealthy() {
		return bank.OperationResult{}, errBankOutage
	}
	return bank.OperationResult{
		PaymentID:     req.PaymentID,
		OperationID:   req.OperationID,
		BankReference: "ref-" + req.OperationID.String()[:8],
		Status:        bank.OperationSucceeded,
	}, nil
}

func (a *controllableBankAdapter) GetOperationStatus(ctx context.Context, req bank.OperationStatusRequest) (bank.OperationResult, error) {
	if !a.isHealthy() {
		return bank.OperationResult{}, errBankOutage
	}
	return bank.OperationResult{
		PaymentID:     req.PaymentID,
		OperationID:   req.OperationID,
		BankReference: "ref-" + req.OperationID.String()[:8],
		Status:        bank.OperationSucceeded,
	}, nil
}

func (a *controllableBankAdapter) GetOperationPresence(ctx context.Context, paymentID, operationID uuid.UUID) (bank.OperationPresenceResult, error) {
	if !a.isHealthy() {
		return bank.OperationPresenceResult{}, errBankOutage
	}
	return bank.OperationPresenceResult{
		PaymentID:   paymentID,
		OperationID: operationID,
		Presence:    bank.PresencePresent,
		ObservedAt:  time.Now().UTC(),
	}, nil
}

func (a *controllableBankAdapter) GetLedgerSnapshot(ctx context.Context, scope bank.LedgerScope) (bank.LedgerSnapshot, error) {
	return bank.LedgerSnapshot{}, nil
}

var _ bank.BankAdapter = (*controllableBankAdapter)(nil)
var _ bank.OperationPresenceReader = (*controllableBankAdapter)(nil)

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("failed connecting to test db: %v", err)
	}
	return pool
}

// 1. Tests that worker fails cleanly when PostgreSQL is unavailable at startup
func TestWorker_DBStartupUnavailable(t *testing.T) {
	t.Setenv("APP_DEVELOPMENT", "true")
	t.Setenv("DATABASE_URL", "postgres://postgres:invalid@127.0.0.1:54399/transactx?connect_timeout=1")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config load failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = app.BuildEnvironment(ctx, cfg, nil)
	if err == nil {
		t.Fatal("expected error connecting to unavailable database, got nil")
	}
}

// 2. Tests that API / UI health endpoints are fully unimpaired when worker process is absent
func TestWorker_Absent_UIHealthUnimpaired(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	t.Setenv("APP_DEVELOPMENT", "true")
	t.Setenv("JWT_SECRET", "super-secret-key-that-is-at-least-32-bytes-long-for-transactx-tests")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config load failed: %v", err)
	}

	env, err := app.BuildEnvironmentWithPool(context.Background(), cfg, pool, nil)
	if err != nil {
		t.Fatalf("build environment failed: %v", err)
	}

	// Create API handler with NO worker running
	handler := env.NewHTTPHandler()

	// Verify GET /health
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected /health 200 OK, got %d: %s", w.Code, w.Body.String())
	}
	var healthResp struct {
		Data struct {
			Status  string `json:"status"`
			Service string `json:"service"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &healthResp); err != nil {
		t.Fatalf("unmarshal /health response failed: %v", err)
	}
	if healthResp.Data.Status != "ok" || healthResp.Data.Service != "transactx-api" {
		t.Fatalf("unexpected /health response: %+v", healthResp)
	}

	// Verify GET /health/db
	reqDB := httptest.NewRequest(http.MethodGet, "/health/db", nil)
	wDB := httptest.NewRecorder()
	handler.ServeHTTP(wDB, reqDB)
	if wDB.Code != http.StatusOK {
		t.Fatalf("expected /health/db 200 OK, got %d: %s", wDB.Code, wDB.Body.String())
	}
}

// 3. Tests worker startup, initial heartbeat recording, and graceful shutdown
func TestWorker_Lifecycle_GracefulShutdown(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	t.Setenv("APP_DEVELOPMENT", "true")
	t.Setenv("JWT_SECRET", "super-secret-key-that-is-at-least-32-bytes-long-for-transactx-tests")
	t.Setenv("TX_RECOVERY_HEARTBEAT", "1s")
	t.Setenv("TX_RECOVERY_SCAN_INTERVAL", "1s")
	t.Setenv("TX_WORKER_LABEL", "test-graceful-worker")
	t.Setenv("TX_BUILD_SHA", "test-sha-1234")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config load failed: %v", err)
	}

	env, err := app.BuildEnvironmentWithPool(context.Background(), cfg, pool, nil)
	if err != nil {
		t.Fatalf("build environment failed: %v", err)
	}

	worker := app.NewWorker(env)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- worker.Run(ctx)
	}()

	// Wait for worker to mark running
	for i := 0; i < 20; i++ {
		if worker.IsRunning() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !worker.IsRunning() {
		t.Fatal("worker did not enter running state")
	}

	// Verify heartbeat row in PostgreSQL
	var count int
	var buildSHA string
	for i := 0; i < 20; i++ {
		err := pool.QueryRow(context.Background(), `
			SELECT COUNT(*), COALESCE(MAX(build_sha), '')
			FROM worker_heartbeats
			WHERE worker_label = 'test-graceful-worker'
		`).Scan(&count, &buildSHA)
		if err == nil && count > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if count == 0 {
		t.Fatal("worker heartbeat was not recorded in database")
	}
	if buildSHA != "test-sha-1234" {
		t.Fatalf("expected build_sha=test-sha-1234, got %s", buildSHA)
	}

	// Trigger graceful shutdown
	worker.Stop()

	select {
	case runErr := <-errCh:
		if runErr != nil {
			t.Fatalf("worker.Run returned unexpected error: %v", runErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not shut down gracefully within timeout")
	}

	if worker.IsRunning() {
		t.Fatal("worker still marked running after Stop()")
	}
}

// 4. Tests worker restart on unchanged database (new instance continues on same state)
func TestWorker_Restart_UnchangedDB(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	t.Setenv("APP_DEVELOPMENT", "true")
	t.Setenv("JWT_SECRET", "super-secret-key-that-is-at-least-32-bytes-long-for-transactx-tests")
	t.Setenv("TX_RECOVERY_HEARTBEAT", "1s")
	t.Setenv("TX_RECOVERY_SCAN_INTERVAL", "1s")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config load failed: %v", err)
	}

	// Instance 1
	cfg.WorkerLabel = "worker-inst-1"
	env1, err := app.BuildEnvironmentWithPool(context.Background(), cfg, pool, nil)
	if err != nil {
		t.Fatalf("build env 1 failed: %v", err)
	}
	w1 := app.NewWorker(env1)

	ctx1, cancel1 := context.WithCancel(context.Background())
	w1Done := make(chan error, 1)
	go func() {
		w1Done <- w1.Run(ctx1)
	}()

	// Wait for instance 1 to heartbeat
	time.Sleep(300 * time.Millisecond)
	var count1 int
	_ = pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM worker_heartbeats WHERE worker_label = 'worker-inst-1'`).Scan(&count1)
	if count1 == 0 {
		t.Fatal("worker-inst-1 did not heartbeat")
	}

	// Stop Instance 1
	cancel1()
	w1.Stop()
	<-w1Done

	// Instance 2 on same DB
	cfg.WorkerLabel = "worker-inst-2"
	env2, err := app.BuildEnvironmentWithPool(context.Background(), cfg, pool, nil)
	if err != nil {
		t.Fatalf("build env 2 failed: %v", err)
	}
	w2 := app.NewWorker(env2)

	ctx2, cancel2 := context.WithCancel(context.Background())
	w2Done := make(chan error, 1)
	go func() {
		w2Done <- w2.Run(ctx2)
	}()

	// Wait for instance 2 to heartbeat
	time.Sleep(300 * time.Millisecond)
	var count2 int
	_ = pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM worker_heartbeats WHERE worker_label = 'worker-inst-2'`).Scan(&count2)
	if count2 == 0 {
		t.Fatal("worker-inst-2 did not heartbeat on restarted DB")
	}

	// Stop Instance 2
	cancel2()
	w2.Stop()
	<-w2Done
}

// 5. Acceptance test: Outage deferral with exponential backoff -> restoration advances old routed payments to completion
func TestWorker_BankOutageAndRestoration_Convergence(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()

	lockConn, err := pool.Acquire(ctx)
	if err == nil {
		_, _ = lockConn.Exec(ctx, `SELECT pg_advisory_lock(88880001)`)
		defer func() {
			_, _ = lockConn.Exec(context.Background(), `SELECT pg_advisory_unlock(88880001)`)
			lockConn.Release()
		}()
	}

	// Seed test participants and accounts
	userID := uuid.New()
	bankID := uuid.New()
	sourceAccID := uuid.New()
	destAccID := uuid.New()
	paymentID := uuid.New()

	defer func() {
		tx, err := pool.Begin(context.Background())
		if err == nil {
			defer tx.Rollback(context.Background())
			_, _ = tx.Exec(context.Background(), `SET LOCAL session_replication_role = 'replica'`)
			_, _ = tx.Exec(context.Background(), `DELETE FROM recovery_attempts WHERE payment_id = $1`, paymentID)
			_, _ = tx.Exec(context.Background(), `DELETE FROM recovery_jobs WHERE payment_id = $1`, paymentID)
			_, _ = tx.Exec(context.Background(), `DELETE FROM payment_state_transitions WHERE payment_id = $1`, paymentID)
			_, _ = tx.Exec(context.Background(), `DELETE FROM outbox_deliveries WHERE event_id IN (SELECT id FROM outbox_events WHERE aggregate_id = $1)`, paymentID.String())
			_, _ = tx.Exec(context.Background(), `DELETE FROM outbox_events WHERE aggregate_id = $1`, paymentID.String())
			_, _ = tx.Exec(context.Background(), `DELETE FROM ops_payment_worklist WHERE payment_id = $1`, paymentID)
			_, _ = tx.Exec(context.Background(), `DELETE FROM payments WHERE id = $1`, paymentID)
			_, _ = tx.Exec(context.Background(), `DELETE FROM accounts WHERE user_id = $1`, userID)
			_, _ = tx.Exec(context.Background(), `DELETE FROM banks WHERE id = $1`, bankID)
			_, _ = tx.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
			_ = tx.Commit(context.Background())
		}
	}()

	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, name, phone, upi_id, password_hash, role)
		VALUES ($1, 'Worker Outage Test User', $2, $3, 'hash', 'CUSTOMER')`,
		userID, userID.String()[:20], "outage-"+userID.String()[:12])
	if err != nil {
		t.Fatalf("seed user failed: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO banks (id, code, name, status)
		VALUES ($1, $2, 'Worker Bank', 'ACTIVE')`,
		bankID, "BANK-"+bankID.String()[:12])
	if err != nil {
		t.Fatalf("seed bank failed: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, user_id, bank_id, account_number, status, balance_paise)
		VALUES ($1, $2, $3, $4, 'ACTIVE', 5000),
		       ($5, $6, $7, $8, 'ACTIVE', 0)`,
		sourceAccID, userID, bankID, "acc-s-"+sourceAccID.String()[:12],
		destAccID, userID, bankID, "acc-d-"+destAccID.String()[:12])
	if err != nil {
		t.Fatalf("seed accounts failed: %v", err)
	}

	adapter := newControllableBankAdapter()
	adaptersMap := map[uuid.UUID]bank.BankAdapter{
		bankID: adapter,
	}

	accountsRepo := accounts.NewRepository(pool)
	recipientsRepo := recipients.NewRepository(pool)
	paymentsRepo := payments.NewRepository(pool)
	paymentsSvc := payments.NewServiceWithAdapters(accountsRepo, recipientsRepo, paymentsRepo, adaptersMap)

	// Create payment in VALIDATING state
	_, err = pool.Exec(ctx, `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, state, origin, source_bank_id, destination_bank_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 1000, 'INR', 'VALIDATING', 'ONLINE', $5, $6, NOW() - interval '40 seconds', NOW() - interval '40 seconds')`,
		paymentID, userID, sourceAccID, destAccID, bankID, bankID)
	if err != nil {
		t.Fatalf("insert test payment failed: %v", err)
	}

	// Clean out any conflicting jobs and create scheduled job for this test payment
	_, _ = pool.Exec(ctx, `DELETE FROM recovery_attempts WHERE payment_id = $1`, paymentID)
	_, _ = pool.Exec(ctx, `DELETE FROM recovery_jobs WHERE payment_id = $1`, paymentID)
	_, err = pool.Exec(ctx, `
		INSERT INTO recovery_jobs (payment_id, status, due_at, lease_epoch, attempt_count, created_at, updated_at)
		VALUES ($1, 'scheduled', NOW() - interval '1 second', 0, 0, NOW(), NOW())`,
		paymentID)
	if err != nil {
		t.Fatalf("insert test recovery job failed: %v", err)
	}

	recoveryRepo := recovery.NewRepository(pool)
	scheduler, err := recovery.NewScheduler(recoveryRepo, paymentsSvc, recovery.Config{
		WorkerID:       "test-convergence-worker",
		WorkerLabel:    "test-convergence-worker",
		Concurrency:    2,
		StaleAge:       10 * time.Second,
		ScanInterval:   1 * time.Second,
		LeaseDuration:  30 * time.Second,
		AttemptTimeout: 5 * time.Second,
		BackoffBase:    2 * time.Second,
		MaxAttempts:    20,
	})
	if err != nil {
		t.Fatalf("new scheduler failed: %v", err)
	}

	// 1. Discovery runs cleanly
	_, err = scheduler.DiscoverStale(ctx, 10)
	if err != nil {
		t.Fatalf("discover stale failed: %v", err)
	}

	// 2. PHASE 1: Bank is in Outage!
	adapter.SetHealthy(false)

	claimed, err := scheduler.ClaimAndProcessBatch(ctx)
	if err != nil {
		t.Fatalf("claim and process batch failed: %v", err)
	}
	if claimed == 0 {
		t.Fatal("expected at least 1 job to be claimed")
	}

	// Check job status: must be deferred with backoff (not failed, not needs_attention)
	job, err := recoveryRepo.GetJob(ctx, paymentID)
	if err != nil {
		t.Fatalf("get job failed: %v", err)
	}
	if job == nil {
		t.Fatalf("job for %s not found in recovery_jobs", paymentID)
	}
	if job.Status != recovery.JobStatusDeferred {
		t.Fatalf("expected job to be deferred during outage, got %s", job.Status)
	}
	if job.AttemptCount != 1 {
		t.Fatalf("expected attempt_count=1, got %d", job.AttemptCount)
	}

	// Verify payment did NOT crash into terminal FAILED
	p, err := paymentsRepo.Get(ctx, paymentID)
	if err != nil {
		t.Fatalf("get payment failed: %v", err)
	}
	if p.State == payments.StateFailed {
		t.Fatal("payment prematurely marked FAILED during transient bank outage")
	}

	// 3. PHASE 2: Bank is Restored!
	adapter.SetHealthy(true)

	// Simulate due_at arrival
	_, err = pool.Exec(ctx, `UPDATE recovery_jobs SET due_at = NOW() - interval '1 second' WHERE payment_id = $1`, paymentID)
	if err != nil {
		t.Fatalf("advance due_at failed: %v", err)
	}

	// Worker processes next batch
	claimed2, err := scheduler.ClaimAndProcessBatch(ctx)
	if err != nil {
		t.Fatalf("second claim and process batch failed: %v", err)
	}
	if claimed2 == 0 {
		t.Fatal("expected restored job to be claimed")
	}

	// Verify job converged to resolved
	jobAfter, err := recoveryRepo.GetJob(ctx, paymentID)
	if err != nil {
		t.Fatalf("get job after restoration failed: %v", err)
	}
	if jobAfter == nil {
		t.Fatalf("job for %s not found after restoration", paymentID)
	}
	if jobAfter.Status != recovery.JobStatusResolved {
		reason := "none"
		if jobAfter.LastReasonCode != nil {
			reason = *jobAfter.LastReasonCode
		}
		t.Fatalf("expected job resolved after restoration, got %s (reason: %s)", jobAfter.Status, reason)
	}

	// Verify payment advanced to COMPLETED automatically without user resubmission!
	pAfter, err := paymentsRepo.Get(ctx, paymentID)
	if err != nil {
		t.Fatalf("get payment after restoration failed: %v", err)
	}
	if pAfter.State != payments.StateCompleted {
		t.Fatalf("expected payment StateCompleted after restoration, got %s", pAfter.State)
	}
}
