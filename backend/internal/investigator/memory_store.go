package investigator

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrIdempotencyConflict = errors.New("investigation idempotency conflict")
	ErrJobNotFound         = errors.New("investigation job not found")
	ErrJobNotClaimable     = errors.New("investigation job is not claimable")
	ErrLeaseLost           = errors.New("investigation lease lost")
	ErrUsageBudget         = errors.New("investigation usage budget unavailable")
)

type memoryUsageReservation struct {
	jobID                         string
	reservedInput, reservedOutput int64
	completed                     bool
}

// MemoryInvestigationStore is a concurrency-safe test/evaluation store. M2
// must supply the durable production implementation; runtime composition must
// never silently fall back to this store.
type MemoryInvestigationStore struct {
	mu           sync.Mutex
	now          func() time.Time
	jobs         map[string]Job
	idempotency  map[string]string
	reservations map[string]memoryUsageReservation
	inputLimit   int64
	outputLimit  int64
	reservedIn   int64
	reservedOut  int64
}

func NewMemoryInvestigationStore(now func() time.Time) *MemoryInvestigationStore {
	if now == nil {
		now = time.Now
	}
	return &MemoryInvestigationStore{
		now:          now,
		jobs:         make(map[string]Job),
		idempotency:  make(map[string]string),
		reservations: make(map[string]memoryUsageReservation),
	}
}

// SetUsageLimits is intended for deterministic C3 tests. Zero is the safe
// default and prevents provider reservations.
func (store *MemoryInvestigationStore) SetUsageLimits(input, output int64) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.inputLimit, store.outputLimit = input, output
}

func (store *MemoryInvestigationStore) Create(ctx context.Context, request NewJob) (Job, bool, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, false, err
	}
	if err := validateTrustedScope(request.Scope); err != nil || request.PaymentID != request.Scope.SelectedPaymentID || request.IdempotencyKey == "" || request.RequestHash == "" || request.Mode == "" {
		return Job{}, false, ErrScopeViolation
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	key := request.Scope.ActorID + "\x00" + request.IdempotencyKey
	if existingID, exists := store.idempotency[key]; exists {
		existing := store.jobs[existingID]
		if existing.RequestHash != request.RequestHash {
			return Job{}, false, ErrIdempotencyConflict
		}
		return cloneJob(existing), false, nil
	}
	job := Job{
		ID:             uuid.NewString(),
		ActorID:        request.Scope.ActorID,
		Role:           request.Scope.Role,
		PaymentID:      request.PaymentID,
		Question:       request.Question,
		IdempotencyKey: request.IdempotencyKey,
		RequestHash:    request.RequestHash,
		Status:         "queued",
		Mode:           request.Mode,
		DeadlineAt:     request.DeadlineAt,
		CreatedAt:      request.CreatedAt,
	}
	store.jobs[job.ID] = cloneJob(job)
	store.idempotency[key] = job.ID
	return cloneJob(job), true, nil
}

func (store *MemoryInvestigationStore) Claim(ctx context.Context, jobID, workerID string, leaseDuration time.Duration) (Job, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	if workerID == "" || leaseDuration <= 0 {
		return Job{}, ErrJobNotClaimable
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	job, exists := store.jobs[jobID]
	if !exists {
		return Job{}, ErrJobNotFound
	}
	now := store.now().UTC()
	if job.Status != "queued" && !(job.Status == "running" && !job.LeaseExpiresAt.After(now)) {
		return Job{}, ErrJobNotClaimable
	}
	job.Status = "running"
	job.LeaseOwner = workerID
	job.LeaseEpoch++
	job.LeaseExpiresAt = now.Add(leaseDuration)
	store.jobs[jobID] = cloneJob(job)
	return cloneJob(job), nil
}

func (store *MemoryInvestigationStore) SaveCheckpoint(ctx context.Context, job Job) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	current, exists := store.jobs[job.ID]
	if !exists {
		return ErrJobNotFound
	}
	if current.LeaseOwner != job.LeaseOwner || current.LeaseEpoch != job.LeaseEpoch || job.LeaseOwner == "" {
		return ErrLeaseLost
	}
	if current.ActorID != job.ActorID || current.Role != job.Role || current.PaymentID != job.PaymentID || current.Question != job.Question || current.RequestHash != job.RequestHash {
		return ErrScopeViolation
	}
	if job.Status != "running" && job.Status != "completed" && job.Status != "failed" && job.Status != "cancelled" {
		return errors.New("invalid investigation job status")
	}
	if job.Status == "completed" && job.Report == nil {
		return errors.New("completed investigation has no report")
	}
	store.jobs[job.ID] = cloneJob(job)
	return nil
}

func (store *MemoryInvestigationStore) Get(ctx context.Context, scope TrustedScope, jobID string) (Job, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	if err := validateTrustedScope(scope); err != nil {
		return Job{}, ErrJobNotFound
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	job, exists := store.jobs[jobID]
	if !exists || job.ActorID != scope.ActorID || job.PaymentID != scope.SelectedPaymentID {
		return Job{}, ErrJobNotFound
	}
	return cloneJob(job), nil
}

func (store *MemoryInvestigationStore) ReserveUsage(ctx context.Context, jobID string, input, output int64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if input < 0 || output < 0 || (input == 0 && output == 0) {
		return "", ErrUsageBudget
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.jobs[jobID]; !exists {
		return "", ErrJobNotFound
	}
	if store.inputLimit == 0 || store.outputLimit == 0 || store.reservedIn+input > store.inputLimit || store.reservedOut+output > store.outputLimit {
		return "", ErrUsageBudget
	}
	id := uuid.NewString()
	store.reservations[id] = memoryUsageReservation{jobID: jobID, reservedInput: input, reservedOutput: output}
	store.reservedIn += input
	store.reservedOut += output
	return id, nil
}

func (store *MemoryInvestigationStore) CompleteUsage(ctx context.Context, reservationID string, usage Usage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	reservation, exists := store.reservations[reservationID]
	if !exists || reservation.completed {
		return ErrUsageBudget
	}
	reservation.completed = true
	store.reservations[reservationID] = reservation
	return nil
}

func cloneJob(job Job) Job {
	raw, err := json.Marshal(job)
	if err != nil {
		panic(err)
	}
	var result Job
	if err := json.Unmarshal(raw, &result); err != nil {
		panic(err)
	}
	return result
}

var _ InvestigationStore = (*MemoryInvestigationStore)(nil)
