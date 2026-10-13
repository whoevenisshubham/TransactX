// Package storetest provides the reusable InvestigationStore conformance gate
// for M2's durable PostgreSQL adapter. Passing this suite with the memory store
// proves the harness, not production durability.
package storetest

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/transactx/backend/internal/investigator"
)

// Factory must return a clean, isolated store for each subtest. The supplied
// limits are aggregate input/output token and integer micro-USD budgets; zero
// token limits mean generation is disabled. Implementations should register
// cleanup through t.Cleanup.
type Factory func(t *testing.T, limits investigator.UsageAllowance) investigator.InvestigationStore

// Run executes the durable semantics required by the M3 service. M2 should
// call Run from its PostgreSQL integration test with a schema-isolated factory.
func Run(t *testing.T, factory Factory) {
	t.Helper()
	if factory == nil {
		t.Fatal("store factory is required")
	}

	t.Run("idempotency_scope_and_copy_isolation", func(t *testing.T) {
		store := factory(t, investigator.UsageAllowance{})
		scope := testScope("actor-a")
		request := testRequest(scope, "key-a", "hash-a")
		job, created, err := store.Create(context.Background(), request)
		if err != nil || !created || job.ID == "" || job.Status != investigator.StatusQueued {
			t.Fatalf("create = %+v, created=%t, err=%v", job, created, err)
		}
		replay, replayCreated, err := store.Create(context.Background(), request)
		if err != nil || replayCreated || replay.ID != job.ID {
			t.Fatalf("replay = %+v, created=%t, err=%v", replay, replayCreated, err)
		}
		conflict := request
		conflict.RequestHash = testHash("hash-b")
		if _, _, err := store.Create(context.Background(), conflict); !errors.Is(err, investigator.ErrIdempotencyConflict) {
			t.Fatalf("idempotency conflict = %v", err)
		}

		otherScope := testScope("actor-b")
		other, otherCreated, err := store.Create(context.Background(), testRequest(otherScope, request.IdempotencyKey, request.RequestHash))
		if err != nil || !otherCreated || other.ID == job.ID {
			t.Fatalf("actor-scoped idempotency = %+v, created=%t, err=%v", other, otherCreated, err)
		}
		if _, _, err := store.Create(context.Background(), testRequest(scope, "actor-limit", "actor-limit")); !errors.Is(err, investigator.ErrActorLimitExceeded) {
			t.Fatalf("actor active limit = %v", err)
		}
		thirdScope := testScope("actor-c")
		if _, _, err := store.Create(context.Background(), testRequest(thirdScope, "global-limit", "global-limit")); !errors.Is(err, investigator.ErrGlobalLimitExceeded) {
			t.Fatalf("global active limit = %v", err)
		}

		job.Question = "caller mutation"
		persisted, err := store.Get(context.Background(), scope, replay.ID)
		if err != nil || persisted.Question != request.Question {
			t.Fatalf("store retained caller-owned job state: %+v, err=%v", persisted, err)
		}
		foreignActor := scope
		foreignActor.ActorID = testScope("actor-foreign").ActorID
		if _, err := store.Get(context.Background(), foreignActor, replay.ID); !errors.Is(err, investigator.ErrJobNotFound) {
			t.Fatalf("foreign actor read = %v", err)
		}
		foreignPayment := scope
		foreignPayment.SelectedPaymentID = "22222222-2222-4222-8222-222222222222"
		if _, err := store.Get(context.Background(), foreignPayment, replay.ID); !errors.Is(err, investigator.ErrJobNotFound) {
			t.Fatalf("foreign payment read = %v", err)
		}
		invalid := request
		invalid.Scope.Role = "CUSTOMER"
		if _, _, err := store.Create(context.Background(), invalid); !errors.Is(err, investigator.ErrScopeViolation) {
			t.Fatalf("untrusted create scope = %v", err)
		}
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		if _, _, err := store.Create(cancelled, testRequest(scope, "cancelled", "cancelled")); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled create = %v", err)
		}
	})

	t.Run("lease_fencing_and_checkpoint_resume", func(t *testing.T) {
		store := factory(t, investigator.UsageAllowance{})
		scope := testScope("actor-lease")
		job := mustCreate(t, store, testRequest(scope, "lease-key", "lease-hash"))
		const leaseDuration = 250 * time.Millisecond
		first, err := store.Claim(context.Background(), job.ID, "worker-a", leaseDuration)
		if err != nil || first.LeaseEpoch != 1 || first.LeaseOwner != "worker-a" || first.Status != investigator.StatusRunning {
			t.Fatalf("first claim = %+v, err=%v", first, err)
		}
		if _, err := store.Claim(context.Background(), job.ID, "worker-b", leaseDuration); !errors.Is(err, investigator.ErrJobNotClaimable) {
			t.Fatalf("active lease was claimable: %v", err)
		}

		first.EvidenceAttempts = 1
		first.SourceHashes = []investigator.SourceHash{{ToolName: investigator.ToolReadPaymentEvidence, SHA256: "sha256:checkpoint"}}
		if err := store.SaveCheckpoint(context.Background(), first); err != nil {
			t.Fatalf("save first checkpoint: %v", err)
		}
		tampered := first
		tampered.ActorID = "actor-tampered"
		if err := store.SaveCheckpoint(context.Background(), tampered); !errors.Is(err, investigator.ErrScopeViolation) {
			t.Fatalf("tampered checkpoint = %v", err)
		}

		second := waitForClaim(t, store, job.ID, "worker-b", leaseDuration)
		if second.LeaseEpoch != first.LeaseEpoch+1 || second.EvidenceAttempts != 1 || len(second.SourceHashes) != 1 {
			t.Fatalf("resumed checkpoint = %+v", second)
		}
		stale := first
		stale.EvidenceAttempts = 99
		if err := store.SaveCheckpoint(context.Background(), stale); !errors.Is(err, investigator.ErrLeaseLost) {
			t.Fatalf("stale lease checkpoint = %v", err)
		}
		second.Status = investigator.StatusCompleted
		second.CompletedAt = time.Now().UTC()
		second.Report = &investigator.InvestigationReport{SchemaVersion: "tx.investigation.v1", Mode: investigator.ModeEvidenceOnly}
		if err := store.SaveCheckpoint(context.Background(), second); err != nil {
			t.Fatalf("complete checkpoint: %v", err)
		}
		stored, err := store.Get(context.Background(), scope, job.ID)
		if err != nil || stored.Status != investigator.StatusCompleted || stored.Report == nil {
			t.Fatalf("stored completion = %+v, err=%v", stored, err)
		}
	})

	t.Run("global_provider_call_bound", func(t *testing.T) {
		store := factory(t, investigator.UsageAllowance{InputTokens: 100, OutputTokens: 100, CostMicrousd: 100})
		first := mustClaim(t, store, mustCreate(t, store, testRequest(testScope("actor-provider-a"), "provider-key-a", "provider-hash-a")), "provider-worker-a", time.Minute)
		second := mustClaim(t, store, mustCreate(t, store, testRequest(testScope("actor-provider-b"), "provider-key-b", "provider-hash-b")), "provider-worker-b", time.Minute)

		first.ProviderCallPending = true
		if err := store.SaveCheckpoint(context.Background(), first); err != nil {
			t.Fatalf("start first provider call: %v", err)
		}
		second.ProviderCallPending = true
		if err := store.SaveCheckpoint(context.Background(), second); !errors.Is(err, investigator.ErrProviderCallLimit) {
			t.Fatalf("parallel provider call = %v", err)
		}
		first.ProviderCallPending = false
		if err := store.SaveCheckpoint(context.Background(), first); err != nil {
			t.Fatalf("finish first provider call: %v", err)
		}
		if err := store.SaveCheckpoint(context.Background(), second); err != nil {
			t.Fatalf("start second provider call after release: %v", err)
		}
	})

	t.Run("transactional_usage_reservations", func(t *testing.T) {
		store := factory(t, investigator.UsageAllowance{InputTokens: 100, OutputTokens: 100, CostMicrousd: 100})
		first := mustClaim(t, store, mustCreate(t, store, testRequest(testScope("actor-budget-a"), "budget-key-a", "budget-hash-a")), "budget-worker-a", time.Minute)
		second := mustClaim(t, store, mustCreate(t, store, testRequest(testScope("actor-budget-b"), "budget-key-b", "budget-hash-b")), "budget-worker-b", time.Minute)
		if _, err := store.ReserveUsage(context.Background(), first, investigator.UsageAllowance{}); !errors.Is(err, investigator.ErrUsageBudget) {
			t.Fatalf("zero reservation = %v", err)
		}
		missing := first
		missing.ID = "00000000-0000-4000-8000-000000000099"
		if _, err := store.ReserveUsage(context.Background(), missing, investigator.UsageAllowance{InputTokens: 1, OutputTokens: 1, CostMicrousd: 1}); !errors.Is(err, investigator.ErrJobNotFound) {
			t.Fatalf("missing job reservation = %v", err)
		}

		type result struct {
			id  string
			err error
			job investigator.Job
		}
		start := make(chan struct{})
		results := make(chan result, 2)
		var workers sync.WaitGroup
		for _, job := range []investigator.Job{first, second} {
			workers.Add(1)
			go func(job investigator.Job) {
				defer workers.Done()
				<-start
				id, err := store.ReserveUsage(context.Background(), job, investigator.UsageAllowance{InputTokens: 80, OutputTokens: 80, CostMicrousd: 80})
				results <- result{id: id, err: err, job: job}
			}(job)
		}
		close(start)
		workers.Wait()
		close(results)

		var reservedJob investigator.Job
		succeeded, rejected := 0, 0
		for result := range results {
			switch {
			case result.err == nil && result.id != "":
				succeeded++
				reservedJob = result.job
			case errors.Is(result.err, investigator.ErrUsageBudget):
				rejected++
			default:
				t.Fatalf("unexpected reservation result: %+v", result)
			}
		}
		if succeeded != 1 || rejected != 1 {
			t.Fatalf("parallel reservations succeeded=%d rejected=%d", succeeded, rejected)
		}

		reservationID, err := store.ReserveUsage(context.Background(), reservedJob, investigator.UsageAllowance{InputTokens: 80, OutputTokens: 80, CostMicrousd: 80})
		if err != nil {
			t.Fatalf("replay reservation: %v", err)
		}
		reservedJob.UsageReservationID = reservationID
		actualInput, actualOutput, actualCostMicrousd := int64(60), int64(50), int64(40)
		reservedJob.Usage = investigator.Usage{InputTokens: &actualInput, OutputTokens: &actualOutput, CostMicrousd: &actualCostMicrousd}
		reservedJob.Status = investigator.StatusCompleted
		reservedJob.CompletedAt = time.Now().UTC()
		reservedJob.Report = &investigator.InvestigationReport{SchemaVersion: "tx.investigation.v1", Mode: investigator.ModeEvidenceOnly}
		if err := store.CompleteUsage(context.Background(), reservedJob); err != nil {
			t.Fatalf("complete usage: %v", err)
		}
		if err := store.CompleteUsage(context.Background(), reservedJob); err != nil {
			t.Fatalf("idempotent completion = %v", err)
		}
		remainingJob := first
		if reservedJob.ID == first.ID {
			remainingJob = second
		}
		remaining, err := store.ReserveUsage(context.Background(), remainingJob, investigator.UsageAllowance{InputTokens: 40, OutputTokens: 50, CostMicrousd: 60})
		if err != nil || remaining == "" {
			t.Fatalf("remaining budget reservation = %q, %v", remaining, err)
		}
	})

	t.Run("uncertain_usage_is_lease_fenced_and_retains_budget", func(t *testing.T) {
		store := factory(t, investigator.UsageAllowance{InputTokens: 100, OutputTokens: 100, CostMicrousd: 100})
		job := mustClaim(t, store, mustCreate(t, store, testRequest(testScope("actor-uncertain"), "uncertain-key", "uncertain-hash")), "worker-a", 250*time.Millisecond)
		reservationID, err := store.ReserveUsage(context.Background(), job, investigator.UsageAllowance{InputTokens: 80, OutputTokens: 80, CostMicrousd: 80})
		if err != nil {
			t.Fatalf("reserve uncertain usage: %v", err)
		}
		job.UsageReservationID = reservationID
		if err := store.SaveCheckpoint(context.Background(), job); err != nil {
			t.Fatalf("checkpoint reservation: %v", err)
		}
		resumed := waitForClaim(t, store, job.ID, "worker-b", time.Minute)
		staleCompletion := job
		staleCompletion.Status = investigator.StatusCompleted
		staleCompletion.Report = &investigator.InvestigationReport{SchemaVersion: "tx.investigation.v1", Mode: investigator.ModeEvidenceOnly}
		if err := store.CompleteUsage(context.Background(), staleCompletion); !errors.Is(err, investigator.ErrLeaseLost) {
			t.Fatalf("stale completion transition = %v", err)
		}
		if err := store.MarkUsageUncertain(context.Background(), job); !errors.Is(err, investigator.ErrLeaseLost) {
			t.Fatalf("stale uncertain transition = %v", err)
		}
		if err := store.MarkUsageUncertain(context.Background(), resumed); err != nil {
			t.Fatalf("mark uncertain: %v", err)
		}
		if err := store.MarkUsageUncertain(context.Background(), resumed); err != nil {
			t.Fatalf("idempotent uncertain transition: %v", err)
		}
		resumed.Status = investigator.StatusCompleted
		resumed.Report = &investigator.InvestigationReport{SchemaVersion: "tx.investigation.v1", Mode: investigator.ModeEvidenceOnly}
		if err := store.CompleteUsage(context.Background(), resumed); !errors.Is(err, investigator.ErrUsageBudget) {
			t.Fatalf("complete uncertain usage = %v", err)
		}
		other := mustClaim(t, store, mustCreate(t, store, testRequest(testScope("actor-after-uncertain"), "after-key", "after-hash")), "worker-c", time.Minute)
		if _, err := store.ReserveUsage(context.Background(), other, investigator.UsageAllowance{InputTokens: 1, OutputTokens: 1, CostMicrousd: 21}); !errors.Is(err, investigator.ErrUsageBudget) {
			t.Fatalf("uncertain reservation released budget: %v", err)
		}
		if id, err := store.ReserveUsage(context.Background(), other, investigator.UsageAllowance{InputTokens: 20, OutputTokens: 20, CostMicrousd: 20}); err != nil || id == "" {
			t.Fatalf("remaining budget after uncertainty = %q, %v", id, err)
		}
	})
}

func testScope(actorID string) investigator.TrustedScope {
	return investigator.TrustedScope{
		ActorID:           uuid.NewSHA1(uuid.NameSpaceOID, []byte(actorID)).String(),
		Role:              "OPS_VIEWER",
		SelectedPaymentID: "11111111-1111-4111-8111-111111111111",
	}
}

func testRequest(scope investigator.TrustedScope, idempotencyKey, requestHash string) investigator.NewJob {
	now := time.Now().UTC()
	return investigator.NewJob{
		Scope:          scope,
		PaymentID:      scope.SelectedPaymentID,
		Question:       "What evidence explains the current state?",
		IdempotencyKey: idempotencyKey,
		RequestHash:    testHash(requestHash),
		Mode:           investigator.ModeEvidenceOnly,
		DeadlineAt:     now.Add(30 * time.Second),
		CreatedAt:      now,
	}
}

func testHash(value string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}

func mustCreate(t *testing.T, store investigator.InvestigationStore, request investigator.NewJob) investigator.Job {
	t.Helper()
	job, created, err := store.Create(context.Background(), request)
	if err != nil || !created {
		t.Fatalf("create test job = %+v, created=%t, err=%v", job, created, err)
	}
	return job
}

func mustClaim(t *testing.T, store investigator.InvestigationStore, job investigator.Job, workerID string, leaseDuration time.Duration) investigator.Job {
	t.Helper()
	claimed, err := store.Claim(context.Background(), job.ID, workerID, leaseDuration)
	if err != nil {
		t.Fatalf("claim test job: %v", err)
	}
	return claimed
}

func waitForClaim(t *testing.T, store investigator.InvestigationStore, jobID, workerID string, leaseDuration time.Duration) investigator.Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		job, err := store.Claim(context.Background(), jobID, workerID, leaseDuration)
		if err == nil {
			return job
		}
		if !errors.Is(err, investigator.ErrJobNotClaimable) {
			t.Fatalf("claim after lease expiry: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("lease did not expire within conformance deadline")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
