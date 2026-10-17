package recovery

import (
	"context"
	"errors"
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
func TestRecovery_LeaseExpiryAndEpoch_AllowsSecondWorkerClaim(t *testing.T) {
	td := setupTestDB(t)
	defer td.cleanFn()
	ctx := context.Background()

	pID := td.createTestPayment(t, "PROCESSING", time.Now().UTC())
	if err := td.repo.UpsertJob(ctx, pID, time.Now().UTC().Add(-10*time.Second)); err != nil {
		t.Fatal(err)
	}

	// Worker 1 claims with very short lease (100ms)
	w1Jobs, err := td.repo.ClaimDueJobs(ctx, "worker-1", 10, 100*time.Millisecond)
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

	// Wait authoritatively for lease to expire in PostgreSQL
	if w1Job.LeaseUntil != nil {
		if remain := time.Until(*w1Job.LeaseUntil); remain > 0 {
			time.Sleep(remain + 50*time.Millisecond)
		} else {
			time.Sleep(50 * time.Millisecond)
		}
	} else {
		time.Sleep(200 * time.Millisecond)
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

	// Worker 1 claims with short lease (100ms)
	w1Jobs, err := td.repo.ClaimDueJobs(ctx, "worker-1", 10, 100*time.Millisecond)
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

	// Wait authoritatively for lease to expire in PostgreSQL, Worker 2 claims it
	if w1Job.LeaseUntil != nil {
		if remain := time.Until(*w1Job.LeaseUntil); remain > 0 {
			time.Sleep(remain + 50*time.Millisecond)
		} else {
			time.Sleep(50 * time.Millisecond)
		}
	} else {
		time.Sleep(200 * time.Millisecond)
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
	if *job.LeaseOwner != "worker-2" {
		t.Fatalf("job owner = %s, want worker-2", *job.LeaseOwner)
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
