package recovery

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/transactx/backend/internal/payments"
)

// PaymentResumer defines the narrow interface used by the scheduler to resume payments.
// The scheduler cannot change account balances or create transactions directly.
type PaymentResumer interface {
	Resume(ctx context.Context, paymentID uuid.UUID) (payments.ResumeResult, error)
	GetPayment(ctx context.Context, paymentID uuid.UUID) (*payments.Payment, error)
}

type JitterFn func(base time.Duration) time.Duration

// DefaultJitter adds between 0 and 50% randomized jitter.
func DefaultJitter(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	half := base / 2
	if half <= 0 {
		return base
	}
	return half + time.Duration(rand.Int63n(int64(half)))
}

// FixedJitter returns a deterministic jitter function for testing.
func FixedJitter(ratio float64) JitterFn {
	return func(base time.Duration) time.Duration {
		return time.Duration(float64(base) * ratio)
	}
}

// CalculateBackoff computes exponential backoff with jitter bounded by cap.
func CalculateBackoff(attemptCount int, base, maxCap time.Duration, jitter JitterFn) time.Duration {
	if attemptCount <= 0 {
		attemptCount = 1
	}
	// 2^(attempt-1)
	mult := math.Pow(2, float64(attemptCount-1))
	computed := time.Duration(float64(base) * mult)
	if computed > maxCap || mult > 1e6 {
		computed = maxCap
	}
	if jitter != nil {
		computed = jitter(computed)
	}
	if computed > maxCap {
		computed = maxCap
	}
	return computed
}

type Config struct {
	WorkerID       string
	WorkerLabel    string
	BuildSHA       string
	Concurrency    int
	StaleAge       time.Duration
	ScanInterval   time.Duration
	LeaseDuration  time.Duration
	AttemptTimeout time.Duration
	BackoffBase    time.Duration
	BackoffCap     time.Duration
	MaxAttempts    int
	JitterFn       JitterFn
	JobFamilies    []string
}

func (c *Config) setDefaults() {
	if c.WorkerID == "" {
		c.WorkerID = "worker-" + uuid.New().String()[:8]
	}
	if c.WorkerLabel == "" {
		c.WorkerLabel = "transactx-recovery-worker"
	}
	if c.BuildSHA == "" {
		c.BuildSHA = "dev"
	}
	if c.Concurrency <= 0 {
		c.Concurrency = 2
	}
	if c.StaleAge <= 0 {
		c.StaleAge = DefaultStaleAge
	}
	if c.ScanInterval <= 0 {
		c.ScanInterval = DefaultScanInterval
	}
	if c.LeaseDuration <= 0 {
		c.LeaseDuration = DefaultLeaseDuration
	}
	if c.AttemptTimeout <= 0 {
		c.AttemptTimeout = DefaultAttemptTimeout
	}
	if c.BackoffBase <= 0 {
		c.BackoffBase = DefaultBackoffBase
	}
	if c.BackoffCap <= 0 {
		c.BackoffCap = DefaultBackoffCap
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = DefaultMaxAttempts
	}
	if c.JitterFn == nil {
		c.JitterFn = DefaultJitter
	}
	if len(c.JobFamilies) == 0 {
		c.JobFamilies = []string{"recovery"}
	}
}

type Metrics struct {
	DiscoveredCount  int64
	ClaimedCount     int64
	CompletedCount   int64
	FailedCount      int64
	AttentionCount   int64
	DeferredCount    int64
	InterruptedCount int64
	LostClaimCount   int64
	HeartbeatCount   int64
}

type Scheduler struct {
	repo       *Repository
	resumer    PaymentResumer
	config     Config
	metrics    Metrics
	sem        chan struct{}
	startedAt  time.Time
	mu         sync.Mutex
	workerUUID uuid.UUID
}

func NewScheduler(repo *Repository, resumer PaymentResumer, cfg Config) (*Scheduler, error) {
	cfg.setDefaults()
	// Validate timing invariants
	if cfg.AttemptTimeout >= cfg.LeaseDuration {
		return nil, fmt.Errorf("attempt timeout (%v) must be strictly less than lease duration (%v)", cfg.AttemptTimeout, cfg.LeaseDuration)
	}

	return &Scheduler{
		repo:       repo,
		resumer:    resumer,
		config:     cfg,
		sem:        make(chan struct{}, cfg.Concurrency),
		startedAt:  time.Now().UTC(),
		workerUUID: uuid.New(),
	}, nil
}

func (s *Scheduler) Metrics() Metrics {
	return Metrics{
		DiscoveredCount:  atomic.LoadInt64(&s.metrics.DiscoveredCount),
		ClaimedCount:     atomic.LoadInt64(&s.metrics.ClaimedCount),
		CompletedCount:   atomic.LoadInt64(&s.metrics.CompletedCount),
		FailedCount:      atomic.LoadInt64(&s.metrics.FailedCount),
		AttentionCount:   atomic.LoadInt64(&s.metrics.AttentionCount),
		DeferredCount:    atomic.LoadInt64(&s.metrics.DeferredCount),
		InterruptedCount: atomic.LoadInt64(&s.metrics.InterruptedCount),
		LostClaimCount:   atomic.LoadInt64(&s.metrics.LostClaimCount),
		HeartbeatCount:   atomic.LoadInt64(&s.metrics.HeartbeatCount),
	}
}

// DiscoverStale runs a single discovery pass for stale non-terminal payments.
func (s *Scheduler) DiscoverStale(ctx context.Context, limit int) ([]uuid.UUID, error) {
	discovered, err := s.repo.DiscoverStalePayments(ctx, s.config.StaleAge, limit)
	if err != nil {
		return nil, err
	}
	if len(discovered) > 0 {
		atomic.AddInt64(&s.metrics.DiscoveredCount, int64(len(discovered)))
	}
	return discovered, nil
}

// ClaimAndProcessBatch claims up to available slots and processes them.
func (s *Scheduler) ClaimAndProcessBatch(ctx context.Context) (int, error) {
	// Determine available capacity
	available := cap(s.sem) - len(s.sem)
	if available <= 0 {
		return 0, nil
	}

	jobs, err := s.repo.ClaimDueJobs(ctx, s.config.WorkerID, available, s.config.LeaseDuration)
	if err != nil {
		return 0, fmt.Errorf("claim due jobs: %w", err)
	}
	if len(jobs) == 0 {
		return 0, nil
	}

	atomic.AddInt64(&s.metrics.ClaimedCount, int64(len(jobs)))

	var wg sync.WaitGroup
	for _, job := range jobs {
		s.sem <- struct{}{}
		wg.Add(1)
		go func(j *Job) {
			defer func() {
				<-s.sem
				wg.Done()
			}()
			if err := s.ProcessJob(ctx, j); err != nil {
				slog.Error("error processing recovery job", "payment_id", j.PaymentID, "error", err)
			}
		}(job)
	}
	wg.Wait()

	return len(jobs), nil
}

// ProcessJob executes payment continuation under lease, heartbeating during execution,
// and records the attempt result.
func (s *Scheduler) ProcessJob(ctx context.Context, job *Job) error {
	attemptCtx, cancel := context.WithTimeout(ctx, s.config.AttemptTimeout)
	defer cancel()

	// Start background lease heartbeater
	heartbeatInterval := s.config.LeaseDuration / 4
	if heartbeatInterval < time.Second {
		heartbeatInterval = time.Second
	}
	heartbeatStop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ok, err := s.repo.HeartbeatLease(ctx, job.PaymentID, s.config.WorkerID, job.LeaseEpoch, s.config.LeaseDuration)
				if err != nil || !ok {
					return
				}
			case <-heartbeatStop:
				return
			case <-attemptCtx.Done():
				return
			}
		}
	}()
	defer close(heartbeatStop)

	// Execute payment continuation via canonical Resume
	resumeResult, err := s.resumer.Resume(attemptCtx, job.PaymentID)

	now := time.Now().UTC()
	var stateAfter string
	var resultOutcome string
	var reasonCode string
	var newJobStatus JobStatus
	var dueAt time.Time

	// Read state after continuation
	payment, readErr := s.resumer.GetPayment(ctx, job.PaymentID)
	if readErr == nil && payment != nil {
		stateAfter = string(payment.State)
	} else {
		stateAfter = "UNKNOWN"
	}

	if err != nil {
		resultOutcome = "ERROR"
		reasonCode = ReasonTransientError
		if job.AttemptCount >= s.config.MaxAttempts {
			newJobStatus = JobStatusNeedsAttention
			reasonCode = ReasonAttemptsExhausted
			dueAt = now
			atomic.AddInt64(&s.metrics.AttentionCount, 1)
		} else {
			newJobStatus = JobStatusDeferred
			backoff := CalculateBackoff(job.AttemptCount, s.config.BackoffBase, s.config.BackoffCap, s.config.JitterFn)
			dueAt = now.Add(backoff)
			atomic.AddInt64(&s.metrics.DeferredCount, 1)
		}
	} else {
		switch resumeResult.Status {
		case payments.ResumeStatusCompleted:
			resultOutcome = "COMPLETED"
			reasonCode = ReasonCompleted
			newJobStatus = JobStatusResolved
			dueAt = now
			atomic.AddInt64(&s.metrics.CompletedCount, 1)

		case payments.ResumeStatusFailed:
			resultOutcome = "FAILED"
			reasonCode = ReasonFailed
			newJobStatus = JobStatusResolved
			dueAt = now
			atomic.AddInt64(&s.metrics.FailedCount, 1)

		case payments.ResumeStatusAttention:
			resultOutcome = "NEEDS_ATTENTION"
			if resumeResult.Reason != "" {
				reasonCode = resumeResult.Reason
			} else {
				reasonCode = ReasonAttention
			}
			newJobStatus = JobStatusNeedsAttention
			dueAt = now
			atomic.AddInt64(&s.metrics.AttentionCount, 1)

		case payments.ResumeStatusNoOp:
			resultOutcome = "NO_OP"
			reasonCode = ReasonNoOp
			if payment != nil && (payment.State == payments.StateCompleted || payment.State == payments.StateFailed || payment.State == payments.StateReversed) {
				newJobStatus = JobStatusResolved
				dueAt = now
				atomic.AddInt64(&s.metrics.CompletedCount, 1)
			} else if job.AttemptCount >= s.config.MaxAttempts {
				newJobStatus = JobStatusNeedsAttention
				reasonCode = ReasonAttemptsExhausted
				dueAt = now
				atomic.AddInt64(&s.metrics.AttentionCount, 1)
			} else {
				newJobStatus = JobStatusDeferred
				backoff := CalculateBackoff(job.AttemptCount, s.config.BackoffBase, s.config.BackoffCap, s.config.JitterFn)
				dueAt = now.Add(backoff)
				atomic.AddInt64(&s.metrics.DeferredCount, 1)
			}

		default:
			resultOutcome = "UNKNOWN"
			reasonCode = ReasonTransientError
			newJobStatus = JobStatusDeferred
			backoff := CalculateBackoff(job.AttemptCount, s.config.BackoffBase, s.config.BackoffCap, s.config.JitterFn)
			dueAt = now.Add(backoff)
			atomic.AddInt64(&s.metrics.DeferredCount, 1)
		}
	}

	attemptID := uuid.Nil
	if job.LastAttemptID != nil {
		attemptID = *job.LastAttemptID
	}

	req := CompleteAttemptRequest{
		AttemptID:  attemptID,
		PaymentID:  job.PaymentID,
		LeaseEpoch: job.LeaseEpoch,
		WorkerID:   s.config.WorkerID,
		NewStatus:  newJobStatus,
		DueAt:      dueAt,
		StateAfter: stateAfter,
		Result:     resultOutcome,
		ReasonCode: reasonCode,
	}

	ok, compErr := s.repo.CompleteAttempt(ctx, req)
	if compErr != nil {
		return fmt.Errorf("complete attempt: %w", compErr)
	}
	if !ok {
		// Lease was lost during execution! Another claim or lease expiration superseded us.
		atomic.AddInt64(&s.metrics.LostClaimCount, 1)
		slog.Warn("recovery attempt lost lease; result discarded",
			"payment_id", job.PaymentID,
			"epoch", job.LeaseEpoch,
			"worker", s.config.WorkerID,
		)
	}

	return nil
}

// SweepInterrupted sweeps jobs that were running but whose leases expired.
func (s *Scheduler) SweepInterrupted(ctx context.Context, limit int) (int, error) {
	swept, err := s.repo.SweepInterruptedAttempts(ctx, limit, s.config.BackoffBase)
	if err != nil {
		return 0, err
	}
	if swept > 0 {
		atomic.AddInt64(&s.metrics.InterruptedCount, int64(swept))
	}
	return swept, nil
}

// Heartbeat writes the worker heartbeat evidence.
func (s *Scheduler) Heartbeat(ctx context.Context) error {
	hb := &WorkerHeartbeat{
		WorkerID:    s.workerUUID,
		WorkerLabel: s.config.WorkerLabel,
		StartedAt:   s.startedAt,
		LastSeenAt:  time.Now().UTC(),
		JobFamilies: s.config.JobFamilies,
		BuildSHA:    s.config.BuildSHA,
	}
	err := s.repo.RecordWorkerHeartbeat(ctx, hb)
	if err == nil {
		atomic.AddInt64(&s.metrics.HeartbeatCount, 1)
	}
	return err
}
