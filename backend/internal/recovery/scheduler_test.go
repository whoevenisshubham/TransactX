package recovery

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/payments"
)

type testData struct {
	pool    *pgxpool.Pool
	repo    *Repository
	userID  uuid.UUID
	bankID  uuid.UUID
	acc1ID  uuid.UUID
	acc2ID  uuid.UUID
	cleanFn func()
}

func setupTestDB(t *testing.T) *testData {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}

	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	lockConn, err := pool.Acquire(ctx)
	if err != nil {
		pool.Close()
		t.Fatal(err)
	}
	if _, err := lockConn.Exec(ctx, `SELECT pg_advisory_lock(88880001)`); err != nil {
		lockConn.Release()
		pool.Close()
		t.Fatal(err)
	}

	_, _ = pool.Exec(ctx, `DELETE FROM recovery_attempts`)
	_, _ = pool.Exec(ctx, `DELETE FROM recovery_jobs`)

	userID := uuid.New()
	bankID := uuid.New()
	acc1ID := uuid.New()
	acc2ID := uuid.New()

	// Seed minimal user, bank, accounts for payments FK
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, name, phone, upi_id, password_hash, role)
		VALUES ($1, 'Recov User', $2, $3, 'hash', 'CUSTOMER')`,
		userID, userID.String()[:20], "recov-"+userID.String()[:12])
	if err != nil {
		_, _ = lockConn.Exec(context.Background(), `SELECT pg_advisory_unlock(88880001)`)
		lockConn.Release()
		pool.Close()
		t.Fatal(err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO banks (id, code, name, status)
		VALUES ($1, $2, 'Recov Bank', 'ACTIVE')`,
		bankID, "BANK-"+bankID.String()[:12])
	if err != nil {
		_, _ = lockConn.Exec(context.Background(), `SELECT pg_advisory_unlock(88880001)`)
		lockConn.Release()
		pool.Close()
		t.Fatal(err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, user_id, bank_id, account_number, status)
		VALUES ($1, $2, $3, $4, 'ACTIVE'), ($5, $6, $7, $8, 'ACTIVE')`,
		acc1ID, userID, bankID, "acc1-"+acc1ID.String()[:12],
		acc2ID, userID, bankID, "acc2-"+acc2ID.String()[:12])
	if err != nil {
		_, _ = lockConn.Exec(context.Background(), `SELECT pg_advisory_unlock(88880001)`)
		lockConn.Release()
		pool.Close()
		t.Fatal(err)
	}

	td := &testData{
		pool:   pool,
		repo:   NewRepository(pool),
		userID: userID,
		bankID: bankID,
		acc1ID: acc1ID,
		acc2ID: acc2ID,
		cleanFn: func() {
			bgCtx := context.Background()
			tx, err := pool.Begin(bgCtx)
			if err == nil {
				defer tx.Rollback(bgCtx)
				_, _ = tx.Exec(bgCtx, `SET LOCAL session_replication_role = 'replica'`)
				_, _ = tx.Exec(bgCtx, `DELETE FROM payment_state_transitions WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, userID)
				_, _ = tx.Exec(bgCtx, `DELETE FROM recovery_attempts WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, userID)
				_, _ = tx.Exec(bgCtx, `DELETE FROM recovery_jobs WHERE payment_id IN (SELECT id FROM payments WHERE initiated_by_user_id = $1)`, userID)
				_, _ = tx.Exec(bgCtx, `DELETE FROM payments WHERE initiated_by_user_id = $1`, userID)
				_, _ = tx.Exec(bgCtx, `DELETE FROM accounts WHERE user_id = $1`, userID)
				_, _ = tx.Exec(bgCtx, `DELETE FROM banks WHERE id = $1`, bankID)
				_, _ = tx.Exec(bgCtx, `DELETE FROM users WHERE id = $1`, userID)
				_ = tx.Commit(bgCtx)
			}
			_, _ = lockConn.Exec(bgCtx, `SELECT pg_advisory_unlock(88880001)`)
			lockConn.Release()
			pool.Close()
		},
	}
	return td
}

func (td *testData) createTestPayment(t *testing.T, state string, createdAt time.Time) uuid.UUID {
	t.Helper()
	pID := uuid.New()
	query := `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 1000, 'INR', 'ONLINE', $5, $6, $7)`
	_, err := td.pool.Exec(context.Background(), query, pID, td.userID, td.acc1ID, td.acc2ID, state, createdAt, createdAt)
	if err != nil {
		t.Fatalf("insert test payment: %v", err)
	}
	return pID
}

// 1. Jitter Bounds test (unit test)
func TestRecovery_JitterBounds(t *testing.T) {
	base := 2 * time.Second
	maxCap := 5 * time.Minute

	// Test deterministic fixed jitter
	b1 := CalculateBackoff(1, base, maxCap, FixedJitter(1.0))
	if b1 != 2*time.Second {
		t.Fatalf("attempt 1 backoff = %v, want 2s", b1)
	}
	b2 := CalculateBackoff(2, base, maxCap, FixedJitter(1.0))
	if b2 != 4*time.Second {
		t.Fatalf("attempt 2 backoff = %v, want 4s", b2)
	}
	b3 := CalculateBackoff(3, base, maxCap, FixedJitter(1.0))
	if b3 != 8*time.Second {
		t.Fatalf("attempt 3 backoff = %v, want 8s", b3)
	}

	// Cap test
	bLarge := CalculateBackoff(20, base, maxCap, FixedJitter(1.0))
	if bLarge != maxCap {
		t.Fatalf("large attempt backoff = %v, want %v", bLarge, maxCap)
	}

	// Default jitter bounds
	for attempt := 1; attempt <= 10; attempt++ {
		for i := 0; i < 50; i++ {
			val := CalculateBackoff(attempt, base, maxCap, DefaultJitter)
			if val < base/2 {
				t.Fatalf("backoff %v below base/2 for attempt %d", val, attempt)
			}
			if val > maxCap {
				t.Fatalf("backoff %v exceeds cap %v for attempt %d", val, maxCap, attempt)
			}
		}
	}
}

// 2. Two Claimers: No double execution
func TestRecovery_TwoClaimers_NoDoubleExecution(t *testing.T) {
	td := setupTestDB(t)
	defer td.cleanFn()
	ctx := context.Background()

	pID := td.createTestPayment(t, "PROCESSING", time.Now().UTC())
	if err := td.repo.UpsertJob(ctx, pID, time.Now().UTC().Add(-1*time.Minute)); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var claim1, claim2 []*Job
	var err1, err2 error

	wg.Add(2)
	go func() {
		defer wg.Done()
		claim1, err1 = td.repo.ClaimDueJobs(ctx, "worker-1", 1, 30*time.Second)
	}()
	go func() {
		defer wg.Done()
		claim2, err2 = td.repo.ClaimDueJobs(ctx, "worker-2", 1, 30*time.Second)
	}()
	wg.Wait()

	if err1 != nil || err2 != nil {
		t.Fatalf("claim errors: %v, %v", err1, err2)
	}

	totalClaimed := len(claim1) + len(claim2)
	if totalClaimed != 1 {
		t.Fatalf("expected exactly 1 claimed job across workers, got %d (w1=%d, w2=%d)", totalClaimed, len(claim1), len(claim2))
	}
}

// 3. Lease Expiry & Epoch progression allows second worker claim
// Deterministically proves Property A (active lease cannot be stolen) and Property B (expired lease reclaimed).
func TestRecovery_LeaseExpiryAndEpoch_AllowsSecondWorkerClaim(t *testing.T) {
	td := setupTestDB(t)
	defer td.cleanFn()
	ctx := context.Background()

	pID := td.createTestPayment(t, "PROCESSING", time.Now().UTC())
	if err := td.repo.UpsertJob(ctx, pID, time.Now().UTC().Add(-10*time.Second)); err != nil {
		t.Fatal(err)
	}

	// Property A: Worker 1 claims with robust lease (30s).
	// Under race detector or runner contention, 30s ensures the lease remains active.
	w1Jobs, err := td.repo.ClaimDueJobs(ctx, "worker-1", 10, 30*time.Second)
	if err != nil {
		t.Fatalf("worker-1 claim failed: %v", err)
	}
	var w1Job *Job
	for _, j := range w1Jobs {
		if j.PaymentID == pID {
			w1Job = j
			break
		}
	}
	if w1Job == nil {
		t.Fatalf("worker-1 claim failed: job for payment %s not found (claimed %d jobs)", pID, len(w1Jobs))
	}
	if w1Job.LeaseEpoch != 1 {
		t.Fatalf("expected epoch 1, got %d", w1Job.LeaseEpoch)
	}
	if w1Job.LeaseOwner == nil || *w1Job.LeaseOwner != "worker-1" {
		t.Fatalf("expected owner worker-1, got %v", w1Job.LeaseOwner)
	}

	// Verify lease is active in PostgreSQL
	var isActive bool
	err = td.pool.QueryRow(ctx, "SELECT lease_until > NOW() FROM recovery_jobs WHERE payment_id = $1", pID).Scan(&isActive)
	if err != nil {
		t.Fatalf("check active lease: %v", err)
	}
	if !isActive {
		t.Fatal("expected lease_until to be in the future")
	}

	// Immediately, Worker 2 cannot claim it (lease active)
	w2JobsEarly, err := td.repo.ClaimDueJobs(ctx, "worker-2", 10, 30*time.Second)
	if err != nil {
		t.Fatalf("worker-2 early claim failed: %v", err)
	}
	for _, j := range w2JobsEarly {
		if j.PaymentID == pID {
			t.Fatalf("worker-2 should not claim active lease for payment %s", pID)
		}
	}

	// Property B: Expire the lease deterministically in PostgreSQL
	tag, err := td.pool.Exec(ctx, "UPDATE recovery_jobs SET lease_until = NOW() - INTERVAL '1 second' WHERE payment_id = $1", pID)
	if err != nil {
		t.Fatalf("deterministic lease expire: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("expected 1 row expired, got %d", tag.RowsAffected())
	}

	// Worker 2 claims it now
	w2Jobs, err := td.repo.ClaimDueJobs(ctx, "worker-2", 10, 30*time.Second)
	if err != nil {
		t.Fatalf("worker-2 claim after expiry failed: %v", err)
	}
	var w2Job *Job
	for _, j := range w2Jobs {
		if j.PaymentID == pID {
			w2Job = j
			break
		}
	}
	if w2Job == nil {
		t.Fatalf("worker-2 claim after expiry failed: job for payment %s not claimed (claimed %d jobs)", pID, len(w2Jobs))
	}
	if w2Job.LeaseEpoch != 2 {
		t.Fatalf("expected epoch 2 on second claim, got %d", w2Job.LeaseEpoch)
	}
	if w2Job.LeaseOwner == nil || *w2Job.LeaseOwner != "worker-2" {
		t.Fatalf("expected owner worker-2, got %v", w2Job.LeaseOwner)
	}
	if w2Job.AttemptCount != 2 {
		t.Fatalf("expected attempt count 2, got %d", w2Job.AttemptCount)
	}

	// Stale Worker 1 heartbeat is rejected
	hbOk, err := td.repo.HeartbeatLease(ctx, pID, "worker-1", 1, 30*time.Second)
	if err != nil {
		t.Fatalf("worker-1 stale heartbeat error: %v", err)
	}
	if hbOk {
		t.Fatal("worker-1 stale heartbeat should have been rejected")
	}

	// Stale Worker 1 completion is rejected
	lateReq := CompleteAttemptRequest{
		AttemptID:  *w1Job.LastAttemptID,
		PaymentID:  pID,
		LeaseEpoch: 1,
		WorkerID:   "worker-1",
		NewStatus:  JobStatusResolved,
		DueAt:      time.Now().UTC(),
		StateAfter: "COMPLETED",
		Result:     "COMPLETED",
		ReasonCode: ReasonCompleted,
	}
	compOk, err := td.repo.CompleteAttempt(ctx, lateReq)
	if err != nil {
		t.Fatalf("worker-1 stale complete attempt error: %v", err)
	}
	if compOk {
		t.Fatal("worker-1 stale complete attempt should have been rejected")
	}

	// Worker 2's lease remains valid
	job, err := td.repo.GetJob(ctx, pID)
	if err != nil {
		t.Fatalf("get job after stale attempts: %v", err)
	}
	if job.Status != JobStatusRunning {
		t.Fatalf("job status = %s, want running", job.Status)
	}
	if job.LeaseEpoch != 2 {
		t.Fatalf("job epoch = %d, want 2", job.LeaseEpoch)
	}
	if job.LeaseOwner == nil || *job.LeaseOwner != "worker-2" {
		t.Fatalf("job owner = %v, want worker-2", job.LeaseOwner)
	}
}

// 4. Late Result Rejected when lease lost
func TestRecovery_LateResultRejected_WhenLeaseLost(t *testing.T) {
	td := setupTestDB(t)
	defer td.cleanFn()
	ctx := context.Background()

	pID := td.createTestPayment(t, "PROCESSING", time.Now().UTC())
	if err := td.repo.UpsertJob(ctx, pID, time.Now().UTC().Add(-10*time.Second)); err != nil {
		t.Fatal(err)
	}

	// Worker 1 claims with robust lease (30s)
	w1Jobs, err := td.repo.ClaimDueJobs(ctx, "worker-1", 10, 30*time.Second)
	if err != nil {
		t.Fatalf("worker-1 claim failed: %v", err)
	}
	var w1Job *Job
	for _, j := range w1Jobs {
		if j.PaymentID == pID {
			w1Job = j
			break
		}
	}
	if w1Job == nil {
		t.Fatalf("worker-1 claim failed: job for payment %s not found (claimed %d jobs)", pID, len(w1Jobs))
	}

	// Expire the lease deterministically in PostgreSQL, allowing Worker 2 to supersede
	_, err = td.pool.Exec(ctx, "UPDATE recovery_jobs SET lease_until = NOW() - INTERVAL '1 second' WHERE payment_id = $1", pID)
	if err != nil {
		t.Fatalf("deterministic lease expire: %v", err)
	}

	w2Jobs, err := td.repo.ClaimDueJobs(ctx, "worker-2", 10, 30*time.Second)
	if err != nil {
		t.Fatalf("worker-2 claim after expiry failed: %v", err)
	}
	var w2Job *Job
	for _, j := range w2Jobs {
		if j.PaymentID == pID {
			w2Job = j
			break
		}
	}
	if w2Job == nil {
		t.Fatalf("worker-2 claim after expiry failed: job for payment %s not claimed (claimed %d jobs)", pID, len(w2Jobs))
	}

	// Worker 1 now attempts to complete its attempt with epoch 1
	lateReq := CompleteAttemptRequest{
		AttemptID:  *w1Job.LastAttemptID,
		PaymentID:  pID,
		LeaseEpoch: w1Job.LeaseEpoch, // epoch 1
		WorkerID:   "worker-1",
		NewStatus:  JobStatusResolved,
		DueAt:      time.Now().UTC(),
		StateAfter: "COMPLETED",
		Result:     "COMPLETED",
		ReasonCode: ReasonCompleted,
	}

	ok, compErr := td.repo.CompleteAttempt(ctx, lateReq)
	if compErr != nil {
		t.Fatalf("unexpected error on late complete attempt: %v", compErr)
	}
	if ok {
		t.Fatal("expected CompleteAttempt to return false for lost lease, but returned true!")
	}

	// Verify job remains in Worker 2's hands (running, epoch 2, owner worker-2)
	job, err := td.repo.GetJob(ctx, pID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobStatusRunning {
		t.Fatalf("job status = %s, want running", job.Status)
	}
	if job.LeaseEpoch != 2 {
		t.Fatalf("job epoch = %d, want 2", job.LeaseEpoch)
	}
	if job.LeaseOwner == nil || *job.LeaseOwner != "worker-2" {
		t.Fatalf("job owner = %v, want worker-2", job.LeaseOwner)
	}
}

// 4a. Proof of natural PostgreSQL wall-clock expiration without racing early claims
func TestRecovery_LeaseExpiry_WallClockPassage(t *testing.T) {
	td := setupTestDB(t)
	defer td.cleanFn()
	ctx := context.Background()

	pID := td.createTestPayment(t, "PROCESSING", time.Now().UTC())
	if err := td.repo.UpsertJob(ctx, pID, time.Now().UTC().Add(-10*time.Second)); err != nil {
		t.Fatal(err)
	}

	// Worker 1 claims with 250ms lease
	w1Jobs, err := td.repo.ClaimDueJobs(ctx, "worker-1", 10, 250*time.Millisecond)
	if err != nil || len(w1Jobs) == 0 {
		t.Fatalf("worker-1 claim failed: %v", err)
	}

	// Poll PostgreSQL's own clock until NOW() > lease_until
	deadline := time.Now().Add(5 * time.Second)
	var expired bool
	for time.Now().Before(deadline) {
		err := td.pool.QueryRow(ctx, "SELECT NOW() > lease_until FROM recovery_jobs WHERE payment_id = $1", pID).Scan(&expired)
		if err == nil && expired {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !expired {
		t.Fatal("timed out waiting for PostgreSQL to expire lease via wall-clock passage")
	}

	// Worker 2 claims after natural PostgreSQL expiration
	w2Jobs, err := td.repo.ClaimDueJobs(ctx, "worker-2", 10, 30*time.Second)
	if err != nil {
		t.Fatalf("worker-2 claim failed: %v", err)
	}
	var w2Job *Job
	for _, j := range w2Jobs {
		if j.PaymentID == pID {
			w2Job = j
			break
		}
	}
	if w2Job == nil {
		t.Fatalf("expected worker-2 to claim expired job, but got none")
	}
	if w2Job.LeaseEpoch != 2 {
		t.Fatalf("expected epoch 2, got %d", w2Job.LeaseEpoch)
	}
	if w2Job.LeaseOwner == nil || *w2Job.LeaseOwner != "worker-2" {
		t.Fatalf("expected owner worker-2, got %v", w2Job.LeaseOwner)
	}
}

// 4b. Epoch increments monotonically across successive claims and never decreases or repeats
func TestRecovery_EpochProgression_Monotonic(t *testing.T) {
	td := setupTestDB(t)
	defer td.cleanFn()
	ctx := context.Background()

	pID := td.createTestPayment(t, "PROCESSING", time.Now().UTC())
	if err := td.repo.UpsertJob(ctx, pID, time.Now().UTC().Add(-10*time.Second)); err != nil {
		t.Fatal(err)
	}

	for epoch := int64(1); epoch <= 3; epoch++ {
		workerName := fmt.Sprintf("worker-%d", epoch)
		jobs, err := td.repo.ClaimDueJobs(ctx, workerName, 1, 30*time.Second)
		if err != nil {
			t.Fatalf("epoch %d claim failed: %v", epoch, err)
		}
		if len(jobs) != 1 || jobs[0].PaymentID != pID {
			t.Fatalf("expected 1 claimed job for payment %s at epoch %d, got %d", pID, epoch, len(jobs))
		}
		if jobs[0].LeaseEpoch != epoch {
			t.Fatalf("expected epoch %d, got %d", epoch, jobs[0].LeaseEpoch)
		}
		if jobs[0].LeaseOwner == nil || *jobs[0].LeaseOwner != workerName {
			t.Fatalf("expected owner %s, got %v", workerName, jobs[0].LeaseOwner)
		}

		// Expire deterministically before next claim
		_, err = td.pool.Exec(ctx, "UPDATE recovery_jobs SET lease_until = NOW() - INTERVAL '1 second' WHERE payment_id = $1", pID)
		if err != nil {
			t.Fatalf("expire epoch %d failed: %v", epoch, err)
		}
	}
}

// 4c. Strict ownership invariants: heartbeat and complete rejection for invalid worker, epoch, or expired lease
func TestRecovery_OwnershipInvariants_HeartbeatAndComplete(t *testing.T) {
	td := setupTestDB(t)
	defer td.cleanFn()
	ctx := context.Background()

	pID := td.createTestPayment(t, "PROCESSING", time.Now().UTC())
	if err := td.repo.UpsertJob(ctx, pID, time.Now().UTC().Add(-10*time.Second)); err != nil {
		t.Fatal(err)
	}

	jobs, err := td.repo.ClaimDueJobs(ctx, "worker-owner", 1, 30*time.Second)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("initial claim failed: %v", err)
	}
	job := jobs[0]

	// 1. Valid heartbeat extends lease
	ok, err := td.repo.HeartbeatLease(ctx, pID, "worker-owner", job.LeaseEpoch, 45*time.Second)
	if err != nil || !ok {
		t.Fatalf("valid heartbeat failed: ok=%v, err=%v", ok, err)
	}

	// 2. Imposter worker ID cannot heartbeat
	ok, err = td.repo.HeartbeatLease(ctx, pID, "worker-imposter", job.LeaseEpoch, 30*time.Second)
	if err != nil || ok {
		t.Fatalf("imposter heartbeat should fail: ok=%v, err=%v", ok, err)
	}

	// 3. Invalid epoch cannot heartbeat
	ok, err = td.repo.HeartbeatLease(ctx, pID, "worker-owner", job.LeaseEpoch+10, 30*time.Second)
	if err != nil || ok {
		t.Fatalf("invalid epoch heartbeat should fail: ok=%v, err=%v", ok, err)
	}

	// 4. Imposter worker ID cannot complete attempt
	reqWrongWorker := CompleteAttemptRequest{
		AttemptID:  *job.LastAttemptID,
		PaymentID:  pID,
		LeaseEpoch: job.LeaseEpoch,
		WorkerID:   "worker-imposter",
		NewStatus:  JobStatusResolved,
		DueAt:      time.Now().UTC(),
		StateAfter: "COMPLETED",
		Result:     "COMPLETED",
		ReasonCode: ReasonCompleted,
	}
	ok, err = td.repo.CompleteAttempt(ctx, reqWrongWorker)
	if err != nil || ok {
		t.Fatalf("imposter complete attempt should fail: ok=%v, err=%v", ok, err)
	}

	// 5. Stale epoch cannot complete attempt
	reqWrongEpoch := CompleteAttemptRequest{
		AttemptID:  *job.LastAttemptID,
		PaymentID:  pID,
		LeaseEpoch: job.LeaseEpoch + 5,
		WorkerID:   "worker-owner",
		NewStatus:  JobStatusResolved,
		DueAt:      time.Now().UTC(),
		StateAfter: "COMPLETED",
		Result:     "COMPLETED",
		ReasonCode: ReasonCompleted,
	}
	ok, err = td.repo.CompleteAttempt(ctx, reqWrongEpoch)
	if err != nil || ok {
		t.Fatalf("stale epoch complete attempt should fail: ok=%v, err=%v", ok, err)
	}

	// 6. Expired lease cannot heartbeat
	_, err = td.pool.Exec(ctx, "UPDATE recovery_jobs SET lease_until = NOW() - INTERVAL '1 second' WHERE payment_id = $1", pID)
	if err != nil {
		t.Fatal(err)
	}
	ok, err = td.repo.HeartbeatLease(ctx, pID, "worker-owner", job.LeaseEpoch, 30*time.Second)
	if err != nil || ok {
		t.Fatalf("expired lease heartbeat should fail: ok=%v, err=%v", ok, err)
	}
}

// 4d. Concurrent contention: 5 simultaneous workers competing for 1 job -> exactly 1 claims
func TestRecovery_ConcurrentWorkers_Contention(t *testing.T) {
	td := setupTestDB(t)
	defer td.cleanFn()
	ctx := context.Background()

	pID := td.createTestPayment(t, "PROCESSING", time.Now().UTC())
	if err := td.repo.UpsertJob(ctx, pID, time.Now().UTC().Add(-10*time.Second)); err != nil {
		t.Fatal(err)
	}

	const workerCount = 5
	var wg sync.WaitGroup
	claims := make([][]*Job, workerCount)
	errs := make([]error, workerCount)

	startBarrier := make(chan struct{})

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-startBarrier
			wName := fmt.Sprintf("contention-worker-%d", idx)
			claims[idx], errs[idx] = td.repo.ClaimDueJobs(ctx, wName, 1, 30*time.Second)
		}(i)
	}

	close(startBarrier)
	wg.Wait()

	totalClaimed := 0
	for i := 0; i < workerCount; i++ {
		if errs[i] != nil {
			t.Fatalf("worker %d claim failed: %v", i, errs[i])
		}
		totalClaimed += len(claims[i])
	}

	if totalClaimed != 1 {
		t.Fatalf("expected exactly 1 claimed job across %d workers, got %d", workerCount, totalClaimed)
	}

	// Verify exactly 1 attempt row created in recovery_attempts
	var attemptCount int
	err := td.pool.QueryRow(ctx, "SELECT count(*) FROM recovery_attempts WHERE payment_id = $1", pID).Scan(&attemptCount)
	if err != nil {
		t.Fatal(err)
	}
	if attemptCount != 1 {
		t.Fatalf("expected exactly 1 attempt record, got %d", attemptCount)
	}
}

// 4e. Financial business scenario: Interrupted saga recovery and single central ledger settlement conservation
func TestRecovery_BusinessScenario_InterruptedSaga_LedgerConservation(t *testing.T) {
	td := setupTestDB(t)
	defer td.cleanFn()
	ctx := context.Background()

	// 1. Seed accounts with initial balance
	initialBalance := int64(10000)
	paymentAmount := int64(2500) // ₹25.00
	_, err := td.pool.Exec(ctx, "UPDATE accounts SET balance_paise = $1 WHERE id IN ($2, $3)", initialBalance, td.acc1ID, td.acc2ID)
	if err != nil {
		t.Fatalf("seed balance: %v", err)
	}

	// 2. Create routed payment in PROCESSING state
	pID := uuid.New()
	query := `
		INSERT INTO payments (id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise, currency, origin, state, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'INR', 'ONLINE', 'PROCESSING', NOW(), NOW())`
	_, err = td.pool.Exec(ctx, query, pID, td.userID, td.acc1ID, td.acc2ID, paymentAmount)
	if err != nil {
		t.Fatalf("insert test payment: %v", err)
	}

	// Upsert recovery job
	if err := td.repo.UpsertJob(ctx, pID, time.Now().UTC().Add(-5*time.Second)); err != nil {
		t.Fatal(err)
	}

	// 3. Worker 1 claims job (epoch 1)
	w1Jobs, err := td.repo.ClaimDueJobs(ctx, "worker-1", 1, 30*time.Second)
	if err != nil || len(w1Jobs) != 1 {
		t.Fatalf("worker-1 claim failed: %v", err)
	}
	w1Job := w1Jobs[0]
	if w1Job.LeaseEpoch != 1 {
		t.Fatalf("expected epoch 1, got %d", w1Job.LeaseEpoch)
	}

	// 4. Worker 1 is interrupted: lease expires deterministically in PostgreSQL
	_, err = td.pool.Exec(ctx, "UPDATE recovery_jobs SET lease_until = NOW() - INTERVAL '1 second' WHERE payment_id = $1", pID)
	if err != nil {
		t.Fatalf("expire lease: %v", err)
	}

	// 5. Worker 2 claims job with epoch 2
	w2Jobs, err := td.repo.ClaimDueJobs(ctx, "worker-2", 1, 30*time.Second)
	if err != nil || len(w2Jobs) != 1 {
		t.Fatalf("worker-2 claim failed: %v", err)
	}
	w2Job := w2Jobs[0]
	if w2Job.LeaseEpoch != 2 {
		t.Fatalf("expected epoch 2, got %d", w2Job.LeaseEpoch)
	}

	// 6. Worker 1 wakes up and attempts to complete using stale epoch 1
	w1LateReq := CompleteAttemptRequest{
		AttemptID:  *w1Job.LastAttemptID,
		PaymentID:  pID,
		LeaseEpoch: 1,
		WorkerID:   "worker-1",
		NewStatus:  JobStatusResolved,
		DueAt:      time.Now().UTC(),
		StateAfter: "COMPLETED",
		Result:     "COMPLETED",
		ReasonCode: ReasonCompleted,
	}
	staleOk, err := td.repo.CompleteAttempt(ctx, w1LateReq)
	if err != nil {
		t.Fatalf("worker-1 stale complete attempt error: %v", err)
	}
	if staleOk {
		t.Fatal("worker-1 stale attempt completion must be rejected")
	}

	// 7. Worker 2 resumes payment, executes atomic settlement, and completes attempt with epoch 2
	tx, err := td.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	ledgerTxID := uuid.New()
	_, err = tx.Exec(ctx, "INSERT INTO ledger_transactions (id, payment_id, created_at) VALUES ($1, $2, NOW())", ledgerTxID, pID)
	if err != nil {
		t.Fatalf("insert ledger_transactions: %v", err)
	}

	// 1 debit for sender
	_, err = tx.Exec(ctx, "INSERT INTO ledger_entries (id, ledger_transaction_id, account_id, entry_type, amount_paise, created_at) VALUES ($1, $2, $3, 'DEBIT', $4, NOW())",
		uuid.New(), ledgerTxID, td.acc1ID, paymentAmount)
	if err != nil {
		t.Fatalf("insert debit: %v", err)
	}

	// 1 credit for receiver
	_, err = tx.Exec(ctx, "INSERT INTO ledger_entries (id, ledger_transaction_id, account_id, entry_type, amount_paise, created_at) VALUES ($1, $2, $3, 'CREDIT', $4, NOW())",
		uuid.New(), ledgerTxID, td.acc2ID, paymentAmount)
	if err != nil {
		t.Fatalf("insert credit: %v", err)
	}

	// Update payment state to COMPLETED
	_, err = tx.Exec(ctx, "UPDATE payments SET state = 'COMPLETED', completed_at = NOW(), updated_at = NOW() WHERE id = $1", pID)
	if err != nil {
		t.Fatalf("update payment to completed: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit settlement: %v", err)
	}

	// Complete attempt with Worker 2 (epoch 2)
	w2Req := CompleteAttemptRequest{
		AttemptID:  *w2Job.LastAttemptID,
		PaymentID:  pID,
		LeaseEpoch: 2,
		WorkerID:   "worker-2",
		NewStatus:  JobStatusResolved,
		DueAt:      time.Now().UTC(),
		StateAfter: "COMPLETED",
		Result:     "COMPLETED",
		ReasonCode: ReasonCompleted,
	}
	w2Ok, err := td.repo.CompleteAttempt(ctx, w2Req)
	if err != nil {
		t.Fatalf("worker-2 complete attempt error: %v", err)
	}
	if !w2Ok {
		t.Fatal("worker-2 attempt completion should succeed")
	}

	// 8. Financial Business Invariant Verifications:
	// a. Exactly 1 payment record
	var paymentCount int
	var paymentState string
	err = td.pool.QueryRow(ctx, "SELECT count(*), max(state) FROM payments WHERE id = $1", pID).Scan(&paymentCount, &paymentState)
	if err != nil || paymentCount != 1 || paymentState != "COMPLETED" {
		t.Fatalf("expected 1 completed payment, got count=%d, state=%s, err=%v", paymentCount, paymentState, err)
	}

	// b. Exactly 1 central ledger transaction
	var ledgerTxCount int
	err = td.pool.QueryRow(ctx, "SELECT count(*) FROM ledger_transactions WHERE payment_id = $1", pID).Scan(&ledgerTxCount)
	if err != nil || ledgerTxCount != 1 {
		t.Fatalf("expected 1 ledger transaction, got %d, err=%v", ledgerTxCount, err)
	}

	// c. Exactly 1 effective DEBIT and 1 effective CREDIT
	var debitCount, creditCount int
	var debitSum, creditSum int64
	err = td.pool.QueryRow(ctx, `
		SELECT 
			count(*) FILTER (WHERE entry_type = 'DEBIT'),
			coalesce(sum(amount_paise) FILTER (WHERE entry_type = 'DEBIT'), 0),
			count(*) FILTER (WHERE entry_type = 'CREDIT'),
			coalesce(sum(amount_paise) FILTER (WHERE entry_type = 'CREDIT'), 0)
		FROM ledger_entries WHERE ledger_transaction_id = $1`, ledgerTxID).
		Scan(&debitCount, &debitSum, &creditCount, &creditSum)
	if err != nil {
		t.Fatalf("query ledger entries: %v", err)
	}

	if debitCount != 1 || debitSum != paymentAmount {
		t.Fatalf("debit invariant violated: count=%d, sum=%d, want count=1 sum=%d", debitCount, debitSum, paymentAmount)
	}
	if creditCount != 1 || creditSum != paymentAmount {
		t.Fatalf("credit invariant violated: count=%d, sum=%d, want count=1 sum=%d", creditCount, creditSum, paymentAmount)
	}
	if debitSum != creditSum {
		t.Fatalf("double-entry invariant violated: debit=%d != credit=%d", debitSum, creditSum)
	}

	// d. Correct attempt history: 2 attempts (Worker 1 epoch 1, Worker 2 epoch 2)
	attempts, err := td.repo.GetAttempts(ctx, pID)
	if err != nil || len(attempts) != 2 {
		t.Fatalf("expected 2 attempts in history, got %d, err=%v", len(attempts), err)
	}
	var a1, a2 *Attempt
	for _, a := range attempts {
		if a.LeaseEpoch == 1 {
			a1 = a
		} else if a.LeaseEpoch == 2 {
			a2 = a
		}
	}
	if a1 == nil || a1.WorkerIdentity != "worker-1" {
		t.Fatalf("expected attempt 1 with worker-1, got %v", a1)
	}
	if a2 == nil || a2.WorkerIdentity != "worker-2" || a2.Result == nil || *a2.Result != "COMPLETED" {
		t.Fatalf("expected attempt 2 with worker-2 COMPLETED, got %v", a2)
	}

	// e. Job status is resolved
	finalJob, err := td.repo.GetJob(ctx, pID)
	if err != nil || finalJob.Status != JobStatusResolved {
		t.Fatalf("expected job resolved, got status=%v, err=%v", finalJob.Status, err)
	}
}

// 5. Attempt Exhaustion parks job in needs_attention
func TestRecovery_AttemptExhaustion_ParksNeedsAttention(t *testing.T) {
	td := setupTestDB(t)
	defer td.cleanFn()
	ctx := context.Background()

	pID := td.createTestPayment(t, "PROCESSING", time.Now().UTC())
	// Set job with 19 attempts
	_, err := td.pool.Exec(ctx, `
		INSERT INTO recovery_jobs (payment_id, status, due_at, lease_epoch, attempt_count)
		VALUES ($1, 'scheduled', NOW(), 0, 19)`, pID)
	if err != nil {
		t.Fatal(err)
	}

	// Mock resumer returning error
	mockResumer := &mockPaymentResumer{
		resumeFn: func(ctx context.Context, id uuid.UUID) (payments.ResumeResult, error) {
			return payments.ResumeResult{}, errors.New("participant connection timeout")
		},
		getFn: func(ctx context.Context, id uuid.UUID) (*payments.Payment, error) {
			return &payments.Payment{ID: id, State: payments.StateProcessing}, nil
		},
	}

	sched, err := NewScheduler(td.repo, mockResumer, Config{
		WorkerID:    "worker-exhaust",
		MaxAttempts: 20,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Claim and process
	claimed, err := sched.ClaimAndProcessBatch(ctx)
	if err != nil || claimed != 1 {
		t.Fatalf("claimed=%d, err=%v", claimed, err)
	}

	// Verify job transitioned to needs_attention with ATTEMPTS_EXHAUSTED
	job, err := td.repo.GetJob(ctx, pID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobStatusNeedsAttention {
		t.Fatalf("expected status needs_attention, got %s", job.Status)
	}
	if *job.LastReasonCode != ReasonAttemptsExhausted {
		t.Fatalf("expected reason ATTEMPTS_EXHAUSTED, got %v", job.LastReasonCode)
	}
}

// 6. Discovery scans stale payments and never un-parks needs_attention
func TestRecovery_Discovery_StalePagination_NeverUnparksNeedsAttention(t *testing.T) {
	td := setupTestDB(t)
	defer td.cleanFn()
	ctx := context.Background()

	now := time.Now().UTC()
	staleAge := 30 * time.Second

	// Payment 1: Stale (created 1 minute ago), nonterminal -> should be discovered
	pStale := td.createTestPayment(t, "ROUTING", now.Add(-1*time.Minute))

	// Payment 2: Fresh (created 5 seconds ago), nonterminal -> should NOT be discovered
	_ = td.createTestPayment(t, "ROUTING", now.Add(-5*time.Second))

	// Payment 3: Stale, but terminal COMPLETED -> should NOT be discovered
	_ = td.createTestPayment(t, "COMPLETED", now.Add(-2*time.Minute))

	// Payment 4: Stale, but has job in needs_attention -> should NOT be un-parked
	pAttention := td.createTestPayment(t, "PROCESSING", now.Add(-1*time.Minute))
	_, err := td.pool.Exec(ctx, `
		INSERT INTO recovery_jobs (payment_id, status, due_at, lease_epoch, attempt_count, last_reason_code)
		VALUES ($1, 'needs_attention', NOW(), 1, 20, 'ATTEMPTS_EXHAUSTED')`, pAttention)
	if err != nil {
		t.Fatal(err)
	}

	discovered, err := td.repo.DiscoverStalePayments(ctx, staleAge, 1000)
	if err != nil {
		t.Fatal(err)
	}

	foundStale := false
	for _, id := range discovered {
		if id == pStale {
			foundStale = true
		}
		if id == pAttention {
			t.Fatalf("discovery should never discover or reset needs_attention job %s", id)
		}
	}
	if !foundStale {
		t.Fatalf("expected stale payment %s to be discovered", pStale)
	}

	// Verify pAttention job status remains needs_attention
	jobAtt, err := td.repo.GetJob(ctx, pAttention)
	if err != nil || jobAtt.Status != JobStatusNeedsAttention {
		t.Fatalf("job attention altered: status=%v, err=%v", jobAtt.Status, err)
	}
}

// 7. Sweep Interrupted Attempts marks attempt INTERRUPTED and defers job
func TestRecovery_SweepInterruptedAttempt(t *testing.T) {
	td := setupTestDB(t)
	defer td.cleanFn()
	ctx := context.Background()

	pID := td.createTestPayment(t, "PROCESSING", time.Now().UTC())
	attemptID := uuid.New()
	expiredUntil := time.Now().UTC().Add(-10 * time.Second)

	// Insert running job with expired lease
	_, err := td.pool.Exec(ctx, `
		INSERT INTO recovery_jobs (payment_id, status, due_at, lease_owner, lease_until, lease_epoch, attempt_count, last_attempt_id)
		VALUES ($1, 'running', NOW(), 'worker-dead', $2, 1, 1, $3)`,
		pID, expiredUntil, attemptID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = td.pool.Exec(ctx, `
		INSERT INTO recovery_attempts (id, payment_id, lease_epoch, worker_identity, started_at, state_before)
		VALUES ($1, $2, 1, 'worker-dead', $3, 'PROCESSING')`,
		attemptID, pID, expiredUntil.Add(-20*time.Second))
	if err != nil {
		t.Fatal(err)
	}

	swept, err := td.repo.SweepInterruptedAttempts(ctx, 10, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if swept != 1 {
		t.Fatalf("swept = %d, want 1", swept)
	}

	// Verify attempt marked INTERRUPTED
	var attemptResult, reason string
	err = td.pool.QueryRow(ctx, "SELECT result, reason_code FROM recovery_attempts WHERE id = $1", attemptID).Scan(&attemptResult, &reason)
	if err != nil {
		t.Fatal(err)
	}
	if attemptResult != "INTERRUPTED" || reason != ReasonLeaseExpired {
		t.Fatalf("attempt result=%s reason=%s, want INTERRUPTED/LEASE_EXPIRED", attemptResult, reason)
	}

	// Verify job deferred
	job, err := td.repo.GetJob(ctx, pID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobStatusDeferred {
		t.Fatalf("job status = %s, want deferred", job.Status)
	}
	if job.LeaseOwner != nil || job.LeaseUntil != nil {
		t.Fatalf("job lease was not cleared: owner=%v, until=%v", job.LeaseOwner, job.LeaseUntil)
	}
}

// 8. Heartbeat and Queue Metrics
func TestRecovery_Heartbeat_And_QueueMetrics(t *testing.T) {
	td := setupTestDB(t)
	defer td.cleanFn()
	ctx := context.Background()

	wUUID := uuid.New()
	hb := &WorkerHeartbeat{
		WorkerID:    wUUID,
		WorkerLabel: "worker-test-metrics",
		StartedAt:   time.Now().UTC(),
		LastSeenAt:  time.Now().UTC(),
		JobFamilies: []string{"recovery"},
		BuildSHA:    "abc1234",
	}

	if err := td.repo.RecordWorkerHeartbeat(ctx, hb); err != nil {
		t.Fatalf("record heartbeat: %v", err)
	}

	// Read heartbeats from DB
	var readLabel, build string
	err := td.pool.QueryRow(ctx, "SELECT worker_label, build_sha FROM worker_heartbeats WHERE worker_id = $1", wUUID).Scan(&readLabel, &build)
	if err != nil {
		t.Fatal(err)
	}
	if readLabel != "worker-test-metrics" || build != "abc1234" {
		t.Fatalf("heartbeat read mismatch: label=%s, build=%s", readLabel, build)
	}

	// Seed 2 jobs and check metrics
	p1 := td.createTestPayment(t, "ROUTING", time.Now().UTC())
	p2 := td.createTestPayment(t, "PROCESSING", time.Now().UTC())
	_ = td.repo.UpsertJob(ctx, p1, time.Now().UTC().Add(-5*time.Second))
	_ = td.repo.UpsertJob(ctx, p2, time.Now().UTC().Add(10*time.Minute))

	total, due, _, _, _, _, err := td.repo.GetQueueMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if total < 2 {
		t.Fatalf("total jobs %d, want >= 2", total)
	}
	if due < 1 {
		t.Fatalf("due jobs %d, want >= 1", due)
	}
}

// 9. End to End: ProcessClaimedJob Converges Payment
func TestRecovery_EndToEnd_ProcessClaimedJob_ConvergesPayment(t *testing.T) {
	td := setupTestDB(t)
	defer td.cleanFn()
	ctx := context.Background()

	pID := td.createTestPayment(t, "PROCESSING", time.Now().UTC())
	if err := td.repo.UpsertJob(ctx, pID, time.Now().UTC().Add(-5*time.Second)); err != nil {
		t.Fatal(err)
	}

	resumer := &mockPaymentResumer{
		resumeFn: func(ctx context.Context, id uuid.UUID) (payments.ResumeResult, error) {
			// Update payment state to COMPLETED
			_, err := td.pool.Exec(ctx, "UPDATE payments SET state = 'COMPLETED', completed_at = NOW() WHERE id = $1", id)
			if err != nil {
				return payments.ResumeResult{}, err
			}
			return payments.ResumeResult{
				Payment:  payments.Payment{ID: id, State: payments.StateCompleted},
				Status:   payments.ResumeStatusCompleted,
				Terminal: true,
			}, nil
		},
		getFn: func(ctx context.Context, id uuid.UUID) (*payments.Payment, error) {
			return &payments.Payment{ID: id, State: payments.StateCompleted}, nil
		},
	}

	sched, err := NewScheduler(td.repo, resumer, Config{
		WorkerID: "worker-e2e",
	})
	if err != nil {
		t.Fatal(err)
	}

	claimed, err := sched.ClaimAndProcessBatch(ctx)
	if err != nil || claimed != 1 {
		t.Fatalf("claimed=%d, err=%v", claimed, err)
	}

	// Job should be resolved
	job, err := td.repo.GetJob(ctx, pID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobStatusResolved {
		t.Fatalf("job status = %s, want resolved", job.Status)
	}
	if *job.LastReasonCode != ReasonCompleted {
		t.Fatalf("reason code = %v, want COMPLETED", job.LastReasonCode)
	}

	// Check attempt records
	attempts, err := td.repo.GetAttempts(ctx, pID)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("attempts count=%d, err=%v", len(attempts), err)
	}
	if *attempts[0].Result != "COMPLETED" {
		t.Fatalf("attempt result = %v, want COMPLETED", attempts[0].Result)
	}
}

type mockPaymentResumer struct {
	resumeFn func(ctx context.Context, id uuid.UUID) (payments.ResumeResult, error)
	getFn    func(ctx context.Context, id uuid.UUID) (*payments.Payment, error)
}

func (m *mockPaymentResumer) Resume(ctx context.Context, paymentID uuid.UUID) (payments.ResumeResult, error) {
	if m.resumeFn != nil {
		return m.resumeFn(ctx, paymentID)
	}
	return payments.ResumeResult{
		Payment: payments.Payment{ID: paymentID, State: payments.StateProcessing},
		Status:  payments.ResumeStatusNoOp,
	}, nil
}

func (m *mockPaymentResumer) GetPayment(ctx context.Context, paymentID uuid.UUID) (*payments.Payment, error) {
	if m.getFn != nil {
		return m.getFn(ctx, paymentID)
	}
	return &payments.Payment{ID: paymentID, State: payments.StateProcessing}, nil
}
