package circuit

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/health"
	"github.com/transactx/backend/internal/payments"
)

func newTestConfig() Config {
	return Config{
		FailureThreshold:     3,
		TimeoutThreshold:     0,
		RollingWindow:        10 * time.Second,
		OpenCooldown:         5 * time.Second,
		HalfOpenProbeLimit:   2,
		SuccessThreshold:     2,
		RestorationSteps:     3,
		SuccessPolicy:        SuccessPolicyDecrement,
		StepSuccessThreshold: 2,
	}
}

// 1. Initial CLOSED
func TestInitialClosed(t *testing.T) {
	cb, err := NewBreaker(newTestConfig())
	if err != nil {
		t.Fatalf("failed to create breaker: %v", err)
	}

	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	state := cb.GetState("RAIL-A", baseTime)
	if state != StateClosed {
		t.Fatalf("expected initial state CLOSED, got %s", state)
	}

	allowed, reason := cb.Allow("RAIL-A", baseTime)
	if !allowed {
		t.Fatalf("expected Allow() to be true for CLOSED target, got false (%s)", reason)
	}
	if reason != "CIRCUIT_CLOSED" {
		t.Fatalf("expected reason CIRCUIT_CLOSED, got %s", reason)
	}

	snap := cb.Snapshot("RAIL-A", baseTime)
	if snap.State != StateClosed || snap.FailureCount != 0 || snap.TimeoutCount != 0 {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
}

// 2. Failure count increments
func TestFailureCountIncrements(t *testing.T) {
	cb, _ := NewBreaker(newTestConfig())
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "test_failure_1", baseTime)
	snap1 := cb.Snapshot("RAIL-A", baseTime)
	if snap1.FailureCount != 1 {
		t.Fatalf("expected failure count 1, got %d", snap1.FailureCount)
	}

	cb.RecordFailure("RAIL-A", "test_failure_2", baseTime.Add(time.Second))
	snap2 := cb.Snapshot("RAIL-A", baseTime.Add(time.Second))
	if snap2.FailureCount != 2 {
		t.Fatalf("expected failure count 2, got %d", snap2.FailureCount)
	}
}

// 3. Failure threshold just below boundary
func TestFailureThresholdJustBelowBoundary(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 3
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	// 2 failures (threshold - 1)
	cb.RecordFailure("RAIL-A", "err", baseTime)
	cb.RecordFailure("RAIL-A", "err", baseTime.Add(time.Second))

	state := cb.GetState("RAIL-A", baseTime.Add(2*time.Second))
	if state != StateClosed {
		t.Fatalf("expected CLOSED below threshold, got %s", state)
	}

	allowed, _ := cb.Allow("RAIL-A", baseTime.Add(2*time.Second))
	if !allowed {
		t.Fatalf("expected target to be allowed below threshold")
	}
}

// 4. Threshold exactly reached
func TestThresholdExactlyReached(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 3
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "err1", baseTime)
	cb.RecordFailure("RAIL-A", "err2", baseTime.Add(time.Second))
	cb.RecordFailure("RAIL-A", "err3", baseTime.Add(2*time.Second))

	state := cb.GetState("RAIL-A", baseTime.Add(2*time.Second))
	if state != StateOpen {
		t.Fatalf("expected OPEN when threshold exactly reached, got %s", state)
	}
}

// 5. CLOSED -> OPEN transition event
func TestClosedToOpenTransition(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 2
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "err1", baseTime)
	cb.RecordFailure("RAIL-A", "err2", baseTime.Add(time.Second))

	events := cb.EventsForTarget("RAIL-A")
	if len(events) != 1 {
		t.Fatalf("expected 1 transition event, got %d", len(events))
	}
	ev := events[0]
	if ev.PreviousState != StateClosed || ev.NewState != StateOpen {
		t.Fatalf("expected CLOSED -> OPEN, got %s -> %s", ev.PreviousState, ev.NewState)
	}
	if ev.Reason != ReasonThresholdReached {
		t.Fatalf("expected reason %s, got %s", ReasonThresholdReached, ev.Reason)
	}
	if ev.ExecutionTargetID != "RAIL-A" {
		t.Fatalf("expected target RAIL-A, got %s", ev.ExecutionTargetID)
	}
}

// 6. OPEN excludes target
func TestOpenExcludesTarget(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "err", baseTime)

	allowed, reason := cb.Allow("RAIL-A", baseTime.Add(time.Second))
	if allowed {
		t.Fatalf("expected OPEN target to be excluded, got allowed=true")
	}
	if reason != "CIRCUIT_OPEN" {
		t.Fatalf("expected reason CIRCUIT_OPEN, got %s", reason)
	}
}

// 7. OPEN before cooldown stays OPEN
func TestOpenBeforeCooldownStaysOpen(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 10 * time.Second
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "err", baseTime)

	beforeCooldown := baseTime.Add(10*time.Second - time.Millisecond)
	state := cb.GetState("RAIL-A", beforeCooldown)
	if state != StateOpen {
		t.Fatalf("expected target to remain OPEN before cooldown, got %s", state)
	}

	allowed, _ := cb.Allow("RAIL-A", beforeCooldown)
	if allowed {
		t.Fatalf("expected target to be excluded before cooldown")
	}
}

// 8. Cooldown boundary
func TestCooldownBoundary(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 10 * time.Second
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "err", baseTime)

	justBefore := baseTime.Add(10*time.Second - time.Nanosecond)
	if cb.GetState("RAIL-A", justBefore) != StateOpen {
		t.Fatalf("expected OPEN 1ns before cooldown")
	}

	exactBoundary := baseTime.Add(10 * time.Second)
	if cb.GetState("RAIL-A", exactBoundary) != StateHalfOpen {
		t.Fatalf("expected HALF_OPEN exactly at cooldown boundary")
	}
}

// 9. OPEN -> HALF_OPEN
func TestOpenToHalfOpenTransition(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "err", baseTime)
	afterCooldown := baseTime.Add(5 * time.Second)

	state := cb.GetState("RAIL-A", afterCooldown)
	if state != StateHalfOpen {
		t.Fatalf("expected HALF_OPEN, got %s", state)
	}

	events := cb.EventsForTarget("RAIL-A")
	if len(events) != 2 {
		t.Fatalf("expected 2 transition events, got %d", len(events))
	}
	ev := events[1]
	if ev.PreviousState != StateOpen || ev.NewState != StateHalfOpen {
		t.Fatalf("expected OPEN -> HALF_OPEN, got %s -> %s", ev.PreviousState, ev.NewState)
	}
	if ev.Reason != ReasonCooldownExpired {
		t.Fatalf("expected reason %s, got %s", ReasonCooldownExpired, ev.Reason)
	}
}

// 10. HALF_OPEN allows configured probe count for health probes and excludes payment traffic
func TestHalfOpenAllowsConfiguredProbeCount(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 2
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "err", baseTime)
	probeTime := baseTime.Add(5 * time.Second)

	// Payment traffic is excluded in HALF_OPEN
	allowedTraffic, reason := cb.Allow("RAIL-A", probeTime)
	if allowedTraffic || reason != "CIRCUIT_HALF_OPEN" {
		t.Fatalf("expected payment traffic excluded in HALF_OPEN, got %v (%s)", allowedTraffic, reason)
	}

	// Recovery health probes are admitted up to probe limit
	allowed1 := cb.TryAcquireProbe("RAIL-A", probeTime)
	if !allowed1 {
		t.Fatalf("expected probe 1 allowed")
	}

	allowed2 := cb.TryAcquireProbe("RAIL-A", probeTime)
	if !allowed2 {
		t.Fatalf("expected probe 2 allowed")
	}
}

// 11. HALF_OPEN rejects excess probes
func TestHalfOpenRejectsExcessProbes(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 2
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "err", baseTime)
	probeTime := baseTime.Add(5 * time.Second)

	if !cb.TryAcquireProbe("RAIL-A", probeTime) {
		t.Fatal("expected probe 1 admitted")
	}
	if !cb.TryAcquireProbe("RAIL-A", probeTime) {
		t.Fatal("expected probe 2 admitted")
	}

	allowed3 := cb.TryAcquireProbe("RAIL-A", probeTime)
	if allowed3 {
		t.Fatalf("expected excess probe 3 to be rejected")
	}
}

// 12. Concurrent probes cannot exceed limit
func TestConcurrentProbesCannotExceedLimit(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 3
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "err", baseTime)
	probeTime := baseTime.Add(5 * time.Second)

	const concurrency = 50
	var allowedCount int64
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			if cb.TryAcquireProbe("RAIL-A", probeTime) {
				atomic.AddInt64(&allowedCount, 1)
			}
		}()
	}
	wg.Wait()

	if allowedCount != int64(cfg.HalfOpenProbeLimit) {
		t.Fatalf("expected exactly %d allowed probes under concurrency, got %d", cfg.HalfOpenProbeLimit, allowedCount)
	}
}

// 13. Successful probe increments
func TestSuccessfulProbeIncrements(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 3
	cfg.SuccessThreshold = 3
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "err", baseTime)
	probeTime := baseTime.Add(5 * time.Second)

	if !cb.TryAcquireProbe("RAIL-A", probeTime) {
		t.Fatal("expected probe admitted")
	}
	cb.RecordSuccess("RAIL-A", probeTime)

	snap := cb.Snapshot("RAIL-A", probeTime)
	if snap.SuccessfulProbes != 1 {
		t.Fatalf("expected successful probes 1, got %d", snap.SuccessfulProbes)
	}
	if snap.State != StateHalfOpen {
		t.Fatalf("expected still HALF_OPEN (1/3 successes), got %s", snap.State)
	}
}

// 14. HALF_OPEN -> CLOSED
func TestHalfOpenToClosed(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 2
	cfg.SuccessThreshold = 2
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "err", baseTime)
	probeTime := baseTime.Add(5 * time.Second)

	if !cb.TryAcquireProbe("RAIL-A", probeTime) {
		t.Fatal("expected probe 1 admitted")
	}
	cb.RecordSuccess("RAIL-A", probeTime)

	if !cb.TryAcquireProbe("RAIL-A", probeTime.Add(time.Second)) {
		t.Fatal("expected probe 2 admitted")
	}
	cb.RecordSuccess("RAIL-A", probeTime.Add(time.Second))

	state := cb.GetState("RAIL-A", probeTime.Add(2*time.Second))
	if state != StateClosed {
		t.Fatalf("expected CLOSED after meeting success threshold, got %s", state)
	}

	events := cb.EventsForTarget("RAIL-A")
	lastEvent := events[len(events)-1]
	if lastEvent.PreviousState != StateHalfOpen || lastEvent.NewState != StateClosed {
		t.Fatalf("expected HALF_OPEN -> CLOSED, got %s -> %s", lastEvent.PreviousState, lastEvent.NewState)
	}
	if lastEvent.Reason != ReasonProbesSucceeded {
		t.Fatalf("expected reason %s, got %s", ReasonProbesSucceeded, lastEvent.Reason)
	}
}

// 15. Failed probe -> OPEN
func TestFailedProbeToOpen(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 2
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "err", baseTime)
	probeTime := baseTime.Add(5 * time.Second)

	if !cb.TryAcquireProbe("RAIL-A", probeTime) {
		t.Fatal("expected probe admitted")
	}
	cb.RecordFailure("RAIL-A", "probe_failed", probeTime.Add(100*time.Millisecond))

	state := cb.GetState("RAIL-A", probeTime.Add(200*time.Millisecond))
	if state != StateOpen {
		t.Fatalf("expected failed probe to immediately transition to OPEN, got %s", state)
	}

	events := cb.EventsForTarget("RAIL-A")
	lastEvent := events[len(events)-1]
	if lastEvent.PreviousState != StateHalfOpen || lastEvent.NewState != StateOpen {
		t.Fatalf("expected HALF_OPEN -> OPEN, got %s -> %s", lastEvent.PreviousState, lastEvent.NewState)
	}
	if lastEvent.Reason != ReasonProbeFailed {
		t.Fatalf("expected reason %s, got %s", ReasonProbeFailed, lastEvent.Reason)
	}
}

// 16. Repeated failure (cycling transitions)
func TestRepeatedFailureTransitions(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 1
	cb, _ := NewBreaker(cfg)
	curTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	for cycle := 0; cycle < 3; cycle++ {
		cb.RecordFailure("RAIL-A", "err", curTime)
		if cb.GetState("RAIL-A", curTime) != StateOpen {
			t.Fatalf("cycle %d: expected OPEN", cycle)
		}

		curTime = curTime.Add(5 * time.Second)
		if cb.GetState("RAIL-A", curTime) != StateHalfOpen {
			t.Fatalf("cycle %d: expected HALF_OPEN", cycle)
		}

		if !cb.TryAcquireProbe("RAIL-A", curTime) {
			t.Fatalf("cycle %d: expected probe admitted", cycle)
		}
		cb.RecordFailure("RAIL-A", "probe_failed", curTime)
		if cb.GetState("RAIL-A", curTime) != StateOpen {
			t.Fatalf("cycle %d: expected OPEN after probe failure", cycle)
		}

		curTime = curTime.Add(time.Second)
	}
}

// 17. Rolling-window expiry
func TestRollingWindowExpiry(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 3
	cfg.RollingWindow = 10 * time.Second
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "err", baseTime)
	cb.RecordFailure("RAIL-A", "err", baseTime.Add(2*time.Second))

	t13 := baseTime.Add(13 * time.Second)
	cb.RecordFailure("RAIL-A", "err", t13)

	snap := cb.Snapshot("RAIL-A", t13)
	if snap.FailureCount != 1 {
		t.Fatalf("expected exactly 1 active failure within rolling window, got %d", snap.FailureCount)
	}
	if snap.State != StateClosed {
		t.Fatalf("expected CLOSED since earlier failures expired, got %s", snap.State)
	}
}

// 18. Timeout counted according to configuration
func TestTimeoutCountedAccordingToConfiguration(t *testing.T) {
	t.Run("TimeoutsCountAsFailures", func(t *testing.T) {
		cfg := newTestConfig()
		cfg.FailureThreshold = 2
		cfg.TimeoutThreshold = 0
		cb, _ := NewBreaker(cfg)
		baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

		cb.RecordFailure("RAIL-A", "err", baseTime)
		cb.RecordTimeout("RAIL-A", baseTime.Add(time.Second))

		if cb.GetState("RAIL-A", baseTime.Add(time.Second)) != StateOpen {
			t.Fatalf("expected OPEN when timeout + failure reaches threshold")
		}
	})

	t.Run("IndependentTimeoutThreshold", func(t *testing.T) {
		cfg := newTestConfig()
		cfg.FailureThreshold = 5
		cfg.TimeoutThreshold = 2
		cb, _ := NewBreaker(cfg)
		baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

		cb.RecordTimeout("RAIL-A", baseTime)
		if cb.GetState("RAIL-A", baseTime) != StateClosed {
			t.Fatalf("expected CLOSED after 1 timeout")
		}

		cb.RecordTimeout("RAIL-A", baseTime.Add(time.Second))
		if cb.GetState("RAIL-A", baseTime.Add(time.Second)) != StateOpen {
			t.Fatalf("expected OPEN after reaching independent timeout threshold")
		}

		events := cb.EventsForTarget("RAIL-A")
		if events[0].Reason != ReasonTimeoutThresholdReached {
			t.Fatalf("expected reason %s, got %s", ReasonTimeoutThresholdReached, events[0].Reason)
		}
	})
}

// 19. Successful request behavior
func TestSuccessfulRequestBehavior(t *testing.T) {
	t.Run("DecrementPolicy", func(t *testing.T) {
		cfg := newTestConfig()
		cfg.FailureThreshold = 3
		cfg.SuccessPolicy = SuccessPolicyDecrement
		cb, _ := NewBreaker(cfg)
		baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

		cb.RecordFailure("RAIL-A", "err1", baseTime)
		cb.RecordFailure("RAIL-A", "err2", baseTime.Add(time.Second))

		snap1 := cb.Snapshot("RAIL-A", baseTime.Add(time.Second))
		if snap1.FailureCount != 2 {
			t.Fatalf("expected 2 failures, got %d", snap1.FailureCount)
		}

		cb.RecordSuccess("RAIL-A", baseTime.Add(2*time.Second))
		snap2 := cb.Snapshot("RAIL-A", baseTime.Add(2*time.Second))
		if snap2.FailureCount != 1 {
			t.Fatalf("expected failure count decremented to 1, got %d", snap2.FailureCount)
		}
	})

	t.Run("ResetPolicy", func(t *testing.T) {
		cfg := newTestConfig()
		cfg.FailureThreshold = 3
		cfg.SuccessPolicy = SuccessPolicyReset
		cb, _ := NewBreaker(cfg)
		baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

		cb.RecordFailure("RAIL-A", "err1", baseTime)
		cb.RecordFailure("RAIL-A", "err2", baseTime.Add(time.Second))

		cb.RecordSuccess("RAIL-A", baseTime.Add(2*time.Second))
		snap := cb.Snapshot("RAIL-A", baseTime.Add(2*time.Second))
		if snap.FailureCount != 0 {
			t.Fatalf("expected failure count reset to 0, got %d", snap.FailureCount)
		}
	})
}

// 20. Deterministic repeated state transitions
func TestDeterministicRepeatedStateTransitions(t *testing.T) {
	simulate := func() []TransitionEvent {
		cfg := newTestConfig()
		cfg.FailureThreshold = 2
		cfg.OpenCooldown = 5 * time.Second
		cfg.HalfOpenProbeLimit = 2
		cfg.SuccessThreshold = 2
		cb, _ := NewBreaker(cfg)

		base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
		cb.RecordFailure("TARGET", "f1", base)
		cb.RecordFailure("TARGET", "f2", base.Add(time.Second))
		cb.TryAcquireProbe("TARGET", base.Add(5*time.Second))
		cb.RecordSuccess("TARGET", base.Add(5*time.Second))
		cb.TryAcquireProbe("TARGET", base.Add(6*time.Second))
		cb.RecordSuccess("TARGET", base.Add(6*time.Second))
		return cb.Events()
	}

	run1 := simulate()
	run2 := simulate()

	if len(run1) != len(run2) {
		t.Fatalf("run1 events %d != run2 events %d", len(run1), len(run2))
	}
	for i := range run1 {
		if run1[i].PreviousState != run2[i].PreviousState ||
			run1[i].NewState != run2[i].NewState ||
			run1[i].Reason != run2[i].Reason {
			t.Fatalf("event mismatch at %d: %+v vs %+v", i, run1[i], run2[i])
		}
	}
}

// 21. Multiple execution targets maintain isolated circuit state
func TestMultipleExecutionTargetsIsolated(t *testing.T) {
	cb, _ := NewBreaker(newTestConfig())
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "err1", baseTime)
	cb.RecordFailure("RAIL-A", "err2", baseTime.Add(time.Second))
	cb.RecordFailure("RAIL-A", "err3", baseTime.Add(2*time.Second))

	if cb.GetState("RAIL-A", baseTime.Add(2*time.Second)) != StateOpen {
		t.Fatalf("expected RAIL-A to be OPEN")
	}

	if cb.GetState("RAIL-B", baseTime.Add(2*time.Second)) != StateClosed {
		t.Fatalf("expected RAIL-B to remain CLOSED")
	}

	allowedB, _ := cb.Allow("RAIL-B", baseTime.Add(2*time.Second))
	if !allowedB {
		t.Fatalf("expected RAIL-B to be allowed")
	}

	snapA := cb.Snapshot("RAIL-A", baseTime.Add(2*time.Second))
	snapB := cb.Snapshot("RAIL-B", baseTime.Add(2*time.Second))

	if snapA.FailureCount != 3 || snapB.FailureCount != 0 {
		t.Fatalf("expected RAIL-A failures=3 and RAIL-B failures=0, got %d and %d", snapA.FailureCount, snapB.FailureCount)
	}
}

// Isolation tests: RAIL-A != RAIL-B
func TestIsolation_RailAOpenDoesNotOpenRailB(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 2
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "err1", baseTime)
	cb.RecordFailure("RAIL-A", "err2", baseTime.Add(time.Second))

	if cb.GetState("RAIL-A", baseTime.Add(time.Second)) != StateOpen {
		t.Fatalf("expected RAIL-A to be OPEN")
	}
	if cb.GetState("RAIL-B", baseTime.Add(time.Second)) != StateClosed {
		t.Fatalf("expected RAIL-B to remain CLOSED")
	}

	allowedA, _ := cb.Allow("RAIL-A", baseTime.Add(time.Second))
	allowedB, _ := cb.Allow("RAIL-B", baseTime.Add(time.Second))

	if allowedA {
		t.Fatalf("expected RAIL-A to be disallowed")
	}
	if !allowedB {
		t.Fatalf("expected RAIL-B to be allowed")
	}
}

func TestIsolation_HalfOpenProbeBudgetIsolated(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 2
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "err", baseTime)
	cb.RecordFailure("RAIL-B", "err", baseTime)

	halfOpenTime := baseTime.Add(5 * time.Second)

	allowA1 := cb.TryAcquireProbe("RAIL-A", halfOpenTime)
	allowA2 := cb.TryAcquireProbe("RAIL-A", halfOpenTime)
	allowA3 := cb.TryAcquireProbe("RAIL-A", halfOpenTime)

	if !allowA1 || !allowA2 {
		t.Fatalf("expected first 2 probes on RAIL-A to be allowed")
	}
	if allowA3 {
		t.Fatalf("expected 3rd probe on RAIL-A to be rejected")
	}

	allowB1 := cb.TryAcquireProbe("RAIL-B", halfOpenTime)
	allowB2 := cb.TryAcquireProbe("RAIL-B", halfOpenTime)
	allowB3 := cb.TryAcquireProbe("RAIL-B", halfOpenTime)

	if !allowB1 || !allowB2 {
		t.Fatalf("expected first 2 probes on RAIL-B to be allowed")
	}
	if allowB3 {
		t.Fatalf("expected 3rd probe on RAIL-B to be rejected")
	}
}

func TestIsolation_ConcurrentOperationsAcrossRails(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 5
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			cb.RecordFailure("RAIL-A", "err", baseTime.Add(time.Duration(i)*time.Millisecond))
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			cb.RecordSuccess("RAIL-B", baseTime.Add(time.Duration(i)*time.Millisecond))
		}
	}()

	wg.Wait()

	snapA := cb.Snapshot("RAIL-A", baseTime.Add(time.Second))
	snapB := cb.Snapshot("RAIL-B", baseTime.Add(time.Second))

	if snapA.State != StateOpen {
		t.Fatalf("expected RAIL-A to be OPEN, got %s", snapA.State)
	}
	if snapB.State != StateClosed {
		t.Fatalf("expected RAIL-B to be CLOSED, got %s", snapB.State)
	}
	if snapB.FailureCount != 0 {
		t.Fatalf("expected 0 failures on RAIL-B, got %d", snapB.FailureCount)
	}
}

type staticSnapshotProvider struct {
	snapshots map[string]health.HealthSnapshot
}

func (s staticSnapshotProvider) GetSnapshot(ctx context.Context, targetID string, now time.Time) (health.HealthSnapshot, error) {
	if snap, ok := s.snapshots[targetID]; ok {
		return snap, nil
	}
	return health.HealthSnapshot{
		TargetID:          targetID,
		Score:             0.90,
		AvailabilityScore: 1.0,
		SampleCount:       10,
	}, nil
}

// 22. OPEN target cannot be selected by M2-4
func TestM24Integration_OpenTargetCannotBeSelected(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "connection_refused", baseTime)

	hook := cb.EligibilityHook(func() time.Time { return baseTime })

	bankA := uuid.New()
	bankB := uuid.New()
	adapter := &strictFailBankAdapter{}
	candidates := []payments.RouteCandidate{
		{CandidateID: "cand-a", ExecutionTargetID: "RAIL-A", SourceBankID: bankA, DestinationBankID: bankB, SourceAdapter: adapter, DestinationAdapter: adapter},
		{CandidateID: "cand-b", ExecutionTargetID: "RAIL-B", SourceBankID: bankA, DestinationBankID: bankB, SourceAdapter: adapter, DestinationAdapter: adapter},
	}

	provider := staticSnapshotProvider{
		snapshots: map[string]health.HealthSnapshot{
			"RAIL-A": {TargetID: "RAIL-A", Score: 0.95, AvailabilityScore: 1.0, SampleCount: 10},
			"RAIL-B": {TargetID: "RAIL-B", Score: 0.80, AvailabilityScore: 1.0, SampleCount: 10},
		},
	}

	decision, err := payments.SelectRoute(context.Background(), candidates, payments.SelectionModeAdaptive, provider, hook, baseTime)
	if err != nil {
		t.Fatalf("expected selection to succeed with alternate target: %v", err)
	}

	if decision.Candidate.ExecutionTargetID != "RAIL-B" {
		t.Fatalf("expected RAIL-B to be selected when RAIL-A is OPEN, got %s", decision.Candidate.ExecutionTargetID)
	}

	onlyOpen := []payments.RouteCandidate{
		{CandidateID: "cand-a", ExecutionTargetID: "RAIL-A", SourceBankID: bankA, DestinationBankID: bankB, SourceAdapter: adapter, DestinationAdapter: adapter},
	}
	_, errOnlyOpen := payments.SelectRoute(context.Background(), onlyOpen, payments.SelectionModeAdaptive, provider, hook, baseTime)
	if errOnlyOpen != payments.ErrNoRouteCandidate {
		t.Fatalf("expected ErrNoRouteCandidate when all candidates are OPEN, got %v", errOnlyOpen)
	}
}

// 23. HALF_OPEN targets are excluded from payment routing; recovery health probe restores eligibility
func TestM24Integration_HalfOpenExcludedFromRoutingAndHealthProbeRecovers(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 1
	cfg.SuccessThreshold = 1
	cfg.RestorationSteps = 1
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "err", baseTime)
	probeTime := baseTime.Add(5 * time.Second)

	hook := cb.EligibilityHook(func() time.Time { return probeTime })

	bankA := uuid.New()
	bankB := uuid.New()
	adapter := &strictFailBankAdapter{}
	candidates := []payments.RouteCandidate{
		{CandidateID: "cand-a", ExecutionTargetID: "RAIL-A", SourceBankID: bankA, DestinationBankID: bankB, SourceAdapter: adapter, DestinationAdapter: adapter},
		{CandidateID: "cand-b", ExecutionTargetID: "RAIL-B", SourceBankID: bankA, DestinationBankID: bankB, SourceAdapter: adapter, DestinationAdapter: adapter},
	}

	provider := staticSnapshotProvider{
		snapshots: map[string]health.HealthSnapshot{
			"RAIL-A": {TargetID: "RAIL-A", Score: 0.95, AvailabilityScore: 1.0, SampleCount: 10},
			"RAIL-B": {TargetID: "RAIL-B", Score: 0.80, AvailabilityScore: 1.0, SampleCount: 10},
		},
	}

	// 1. In HALF_OPEN, RAIL-A must be excluded from payment routing despite higher score
	decision1, err1 := payments.SelectRoute(context.Background(), candidates, payments.SelectionModeAdaptive, provider, hook, probeTime)
	if err1 != nil {
		t.Fatalf("expected request 1 to succeed with alternate target: %v", err1)
	}
	if decision1.Candidate.ExecutionTargetID != "RAIL-B" {
		t.Fatalf("expected request 1 to select RAIL-B because RAIL-A is HALF_OPEN, got %s", decision1.Candidate.ExecutionTargetID)
	}

	// Verify payment route selection did NOT consume any probe budget
	snapA := cb.Snapshot("RAIL-A", probeTime)
	if snapA.ActiveProbes != 0 {
		t.Fatalf("expected ActiveProbes=0 on RAIL-A (no probe consumption by payment routing), got %d", snapA.ActiveProbes)
	}

	// 2. Health probe is admitted and recovers RAIL-A
	admitted := cb.TryAcquireProbe("RAIL-A", probeTime)
	if !admitted {
		t.Fatal("expected health probe to be admitted")
	}
	cb.RecordSuccess("RAIL-A", probeTime)

	snapAAfter := cb.Snapshot("RAIL-A", probeTime.Add(time.Millisecond))
	if snapAAfter.State != StateClosed {
		t.Fatalf("expected RAIL-A to recover to CLOSED, got %s", snapAAfter.State)
	}

	// 3. Now RAIL-A is CLOSED and eligible again, and its higher score causes it to be selected
	decision2, err2 := payments.SelectRoute(context.Background(), candidates, payments.SelectionModeAdaptive, provider, hook, probeTime.Add(2*time.Millisecond))
	if err2 != nil {
		t.Fatalf("expected request 2 to succeed: %v", err2)
	}
	if decision2.Candidate.ExecutionTargetID != "RAIL-A" {
		t.Fatalf("expected request 2 to select recovered RAIL-A, got %s", decision2.Candidate.ExecutionTargetID)
	}
}

// 24. Gradual restoration behavior
func TestM24Integration_GradualRestorationShedsTraffic(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 2
	cfg.SuccessThreshold = 2
	cfg.RestorationSteps = 3
	cfg.StepSuccessThreshold = 3
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "err", baseTime)

	probeTime := baseTime.Add(5 * time.Second)
	if !cb.TryAcquireProbe("RAIL-A", probeTime) {
		t.Fatal("expected probe 1 admitted")
	}
	cb.RecordSuccess("RAIL-A", probeTime)
	if !cb.TryAcquireProbe("RAIL-A", probeTime.Add(time.Millisecond)) {
		t.Fatal("expected probe 2 admitted")
	}
	cb.RecordSuccess("RAIL-A", probeTime.Add(time.Millisecond))

	snap := cb.Snapshot("RAIL-A", probeTime.Add(2*time.Millisecond))
	if snap.State != StateClosed || snap.RestorationStep != 1 {
		t.Fatalf("expected RAIL-A in CLOSED with restorationStep=1, got state=%s, step=%d", snap.State, snap.RestorationStep)
	}

	currentTime := probeTime.Add(10 * time.Millisecond)
	hook := cb.EligibilityHook(func() time.Time { return currentTime })

	bankA := uuid.New()
	bankB := uuid.New()
	adapter := &strictFailBankAdapter{}
	candidates := []payments.RouteCandidate{
		{CandidateID: "cand-a", ExecutionTargetID: "RAIL-A", SourceBankID: bankA, DestinationBankID: bankB, SourceAdapter: adapter, DestinationAdapter: adapter},
		{CandidateID: "cand-b", ExecutionTargetID: "RAIL-B", SourceBankID: bankA, DestinationBankID: bankB, SourceAdapter: adapter, DestinationAdapter: adapter},
	}

	provider := staticSnapshotProvider{
		snapshots: map[string]health.HealthSnapshot{
			"RAIL-A": {TargetID: "RAIL-A", Score: 0.99, AvailabilityScore: 1.0, SampleCount: 10},
			"RAIL-B": {TargetID: "RAIL-B", Score: 0.80, AvailabilityScore: 1.0, SampleCount: 10},
		},
	}

	railACount := 0
	railBCount := 0
	for i := 0; i < 9; i++ {
		dec, err := payments.SelectRoute(context.Background(), candidates, payments.SelectionModeAdaptive, provider, hook, currentTime)
		if err != nil {
			t.Fatalf("route selection failed: %v", err)
		}
		if dec.Candidate.ExecutionTargetID == "RAIL-A" {
			railACount++
		} else if dec.Candidate.ExecutionTargetID == "RAIL-B" {
			railBCount++
		}
	}

	if railACount == 0 {
		t.Fatalf("expected recovering RAIL-A to receive some traffic during gradual restoration")
	}
	if railBCount == 0 {
		t.Fatalf("expected recovering RAIL-A to shed traffic to RAIL-B, but RAIL-A absorbed 100%% of traffic!")
	}
	if railACount > railBCount {
		t.Fatalf("at step 1 of 3, RAIL-A should receive less traffic than RAIL-B, got railA=%d, railB=%d", railACount, railBCount)
	}
}

type mockRepository struct {
	mu     sync.Mutex
	events []TransitionEvent
}

func (m *mockRepository) RecordTransition(ctx context.Context, event TransitionEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

func (m *mockRepository) ListRecent(ctx context.Context, targetID string, limit int) ([]TransitionEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res []TransitionEvent
	for _, e := range m.events {
		if e.ExecutionTargetID == targetID {
			res = append(res, e)
		}
	}
	return res, nil
}

// 25. Circuit transition fact persisted/emitted with all required fields
func TestCircuitTransitionFactPersistedAndEmitted(t *testing.T) {
	repo := &mockRepository{}
	cfg := newTestConfig()
	cfg.FailureThreshold = 2
	cfg.OpenCooldown = 5 * time.Second
	cb, err := NewBreaker(cfg, repo)
	if err != nil {
		t.Fatalf("failed to create breaker: %v", err)
	}

	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	cb.RecordFailure("RAIL-A", "fail1", baseTime)
	cb.RecordFailure("RAIL-A", "fail2", baseTime.Add(time.Second))

	events := cb.EventsForTarget("RAIL-A")
	if len(events) != 1 {
		t.Fatalf("expected 1 event in memory, got %d", len(events))
	}

	ev := events[0]
	if ev.ExecutionTargetID != "RAIL-A" {
		t.Fatalf("expected target RAIL-A, got %s", ev.ExecutionTargetID)
	}
	if ev.PreviousState != StateClosed || ev.NewState != StateOpen {
		t.Fatalf("expected CLOSED -> OPEN, got %s -> %s", ev.PreviousState, ev.NewState)
	}
	if ev.Reason != ReasonThresholdReached {
		t.Fatalf("expected reason %s, got %s", ReasonThresholdReached, ev.Reason)
	}
	if !ev.TransitionedAt.Equal(baseTime.Add(time.Second)) {
		t.Fatalf("unexpected timestamp: %v", ev.TransitionedAt)
	}
	if ev.FailureCount != 2 {
		t.Fatalf("expected failure count 2, got %d", ev.FailureCount)
	}
	if ev.CooldownDurationMs != cfg.OpenCooldown.Milliseconds() {
		t.Fatalf("expected cooldown ms %d, got %d", cfg.OpenCooldown.Milliseconds(), ev.CooldownDurationMs)
	}
	if ev.RollingWindowMs != cfg.RollingWindow.Milliseconds() {
		t.Fatalf("expected rolling window ms %d, got %d", cfg.RollingWindow.Milliseconds(), ev.RollingWindowMs)
	}

	repo.mu.Lock()
	repoEventsCount := len(repo.events)
	repo.mu.Unlock()
	if repoEventsCount != 1 {
		t.Fatalf("expected 1 event in repository, got %d", repoEventsCount)
	}
}

// 26. Circuit code never calls monetary BankAdapter operations
type strictFailBankAdapter struct{}

func (f *strictFailBankAdapter) GetHealth(ctx context.Context) (bank.HealthResult, error) {
	panic("CheckHealth must never be called by circuit breaker")
}

func (f *strictFailBankAdapter) ResolveAccount(ctx context.Context, req bank.ResolveAccountRequest) (bank.AccountResult, error) {
	panic("MONETARY RESOLVE VIOLATION: Circuit code called ResolveAccount!")
}

func (f *strictFailBankAdapter) HoldFunds(ctx context.Context, req bank.HoldFundsRequest) (bank.HoldResult, error) {
	panic("MONETARY HOLD VIOLATION: Circuit code called HoldFunds!")
}

func (f *strictFailBankAdapter) ProvisionalCredit(ctx context.Context, req bank.ProvisionalCreditRequest) (bank.OperationResult, error) {
	panic("MONETARY CREDIT VIOLATION: Circuit code called ProvisionalCredit!")
}

func (f *strictFailBankAdapter) ConfirmHold(ctx context.Context, req bank.ConfirmHoldRequest) (bank.OperationResult, error) {
	panic("MONETARY CONFIRM VIOLATION: Circuit code called ConfirmHold!")
}

func (f *strictFailBankAdapter) ReleaseHold(ctx context.Context, req bank.ReleaseHoldRequest) (bank.OperationResult, error) {
	panic("MONETARY RELEASE VIOLATION: Circuit code called ReleaseHold!")
}

func (f *strictFailBankAdapter) ReverseProvisionalCredit(ctx context.Context, req bank.ReverseCreditRequest) (bank.OperationResult, error) {
	panic("MONETARY REVERSE VIOLATION: Circuit code called ReverseProvisionalCredit!")
}

func (f *strictFailBankAdapter) GetOperationStatus(ctx context.Context, req bank.OperationStatusRequest) (bank.OperationResult, error) {
	panic("MONETARY STATUS VIOLATION: Circuit code called GetOperationStatus!")
}

func (f *strictFailBankAdapter) GetLedgerSnapshot(ctx context.Context, scope bank.LedgerScope) (bank.LedgerSnapshot, error) {
	panic("MONETARY LEDGER VIOLATION: Circuit code called GetLedgerSnapshot!")
}

func TestCircuitNeverCallsMonetaryBankAdapter(t *testing.T) {
	cb, _ := NewBreaker(newTestConfig())
	now := time.Now()

	cb.GetState("RAIL-A", now)
	cb.Allow("RAIL-A", now)
	cb.RecordSuccess("RAIL-A", now)
	cb.RecordFailure("RAIL-A", "test", now)
	cb.RecordTimeout("RAIL-A", now)
	cb.Snapshot("RAIL-A", now)
	cb.Snapshots(now)
	cb.Events()
	cb.EventsForTarget("RAIL-A")

	tbType := reflect.TypeOf(TargetBreaker{})
	for i := 0; i < tbType.NumField(); i++ {
		field := tbType.Field(i)
		if field.Type == reflect.TypeOf((*bank.BankAdapter)(nil)).Elem() {
			t.Fatalf("TargetBreaker struct must not contain BankAdapter references")
		}
	}
}

// 27. Circuit code never changes payment/account/ledger state
func TestCircuitNeverMutatesPaymentAccountLedger(t *testing.T) {
	cb, _ := NewBreaker(newTestConfig())
	snap := cb.Snapshot("RAIL-A")

	snapType := reflect.TypeOf(snap)
	forbiddenFields := []string{"accountID", "account_id", "balance", "ledger", "paymentID", "payment_id", "sourceBankID", "destinationBankID"}
	for i := 0; i < snapType.NumField(); i++ {
		fieldName := snapType.Field(i).Name
		for _, forbidden := range forbiddenFields {
			if fieldName == forbidden {
				t.Fatalf("TargetSnapshot must not contain %s", forbidden)
			}
		}
	}
}

// 28. Concurrent state mutation remains race-safe
func TestConcurrentStateMutationRaceSafe(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 5
	cfg.OpenCooldown = 10 * time.Millisecond
	cfg.HalfOpenProbeLimit = 2
	cfg.SuccessThreshold = 2
	cb, _ := NewBreaker(cfg)

	const workers = 20
	const iterations = 100
	var wg sync.WaitGroup
	wg.Add(workers)

	startTime := time.Now()

	for w := 0; w < workers; w++ {
		workerID := w
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				tOffset := startTime.Add(time.Duration(i) * time.Millisecond)
				targetID := "RAIL-A"
				if workerID%2 == 1 {
					targetID = "RAIL-B"
				}

				switch i % 5 {
				case 0:
					cb.Allow(targetID, tOffset)
				case 1:
					cb.RecordSuccess(targetID, tOffset)
				case 2:
					cb.RecordFailure(targetID, "concurrent_fail", tOffset)
				case 3:
					cb.RecordTimeout(targetID, tOffset)
				case 4:
					cb.Snapshot(targetID, tOffset)
				}
			}
		}()
	}

	wg.Wait()

	snapA := cb.Snapshot("RAIL-A")
	snapB := cb.Snapshot("RAIL-B")

	if snapA.State != StateClosed && snapA.State != StateOpen && snapA.State != StateHalfOpen {
		t.Fatalf("corrupted state for RAIL-A: %s", snapA.State)
	}
	if snapB.State != StateClosed && snapB.State != StateOpen && snapB.State != StateHalfOpen {
		t.Fatalf("corrupted state for RAIL-B: %s", snapB.State)
	}
}

// 29. SuccessThreshold == HalfOpenProbeLimit probe accounting
func TestHalfOpenProbeAccounting_SuccessThresholdEqualsLimit(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 2
	cfg.SuccessThreshold = 2
	cfg.RestorationSteps = 1
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "fail", baseTime)
	halfOpenTime := baseTime.Add(5 * time.Second)

	// Payment routing rejected in HALF_OPEN and does NOT consume probe
	payAllowed, payReason := cb.Allow("RAIL-A", halfOpenTime)
	if payAllowed || payReason != "CIRCUIT_HALF_OPEN" {
		t.Fatalf("expected payment routing rejected in HALF_OPEN, got %v (%s)", payAllowed, payReason)
	}
	if snap := cb.Snapshot("RAIL-A", halfOpenTime); snap.ActiveProbes != 0 {
		t.Fatalf("expected activeProbes=0 after payment attempt, got %d", snap.ActiveProbes)
	}

	// Health Probe 1 admitted
	allowed1 := cb.TryAcquireProbe("RAIL-A", halfOpenTime)
	if !allowed1 {
		t.Fatal("expected probe 1 allowed")
	}
	snap1 := cb.Snapshot("RAIL-A", halfOpenTime)
	if snap1.ActiveProbes != 1 || snap1.SuccessfulProbes != 0 {
		t.Fatalf("expected ActiveProbes=1, SuccessfulProbes=0, got %+v", snap1)
	}

	// Probe 1 terminal success releases active slot and increments successfulProbes
	cb.RecordSuccess("RAIL-A", halfOpenTime)
	snap2 := cb.Snapshot("RAIL-A", halfOpenTime)
	if snap2.ActiveProbes != 0 || snap2.SuccessfulProbes != 1 {
		t.Fatalf("expected ActiveProbes=0, SuccessfulProbes=1 after probe 1 success, got %+v", snap2)
	}
	if snap2.State != StateHalfOpen {
		t.Fatalf("expected still HALF_OPEN after 1 of 2 successes, got %s", snap2.State)
	}

	// Health Probe 2 admitted
	allowed2 := cb.TryAcquireProbe("RAIL-A", halfOpenTime.Add(time.Second))
	if !allowed2 {
		t.Fatal("expected probe 2 allowed")
	}
	snap3 := cb.Snapshot("RAIL-A", halfOpenTime.Add(time.Second))
	if snap3.ActiveProbes != 1 || snap3.SuccessfulProbes != 1 {
		t.Fatalf("expected ActiveProbes=1, SuccessfulProbes=1, got %+v", snap3)
	}

	// Probe 2 terminal success reaches SuccessThreshold (2) -> transitions to CLOSED
	cb.RecordSuccess("RAIL-A", halfOpenTime.Add(time.Second))
	snap4 := cb.Snapshot("RAIL-A", halfOpenTime.Add(time.Second))
	if snap4.State != StateClosed {
		t.Fatalf("expected transition to CLOSED, got %s", snap4.State)
	}
	if snap4.ActiveProbes != 0 || snap4.SuccessfulProbes != 0 {
		t.Fatalf("expected ActiveProbes=0 and SuccessfulProbes=0 in CLOSED, got %+v", snap4)
	}

	// After recovery to CLOSED, payment routing is eligible
	payRecovered, _ := cb.Allow("RAIL-A", halfOpenTime.Add(2*time.Second))
	if !payRecovered {
		t.Fatal("expected payment routing allowed after recovery to CLOSED")
	}
}

// 30. SuccessThreshold > HalfOpenProbeLimit supports sequential probe slot reuse
func TestHalfOpenProbeAccounting_SuccessThresholdGreaterThanLimit(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 1 // Budget is 1 concurrent probe
	cfg.SuccessThreshold = 3   // Requires 3 sequential successful probes
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "fail", baseTime)
	halfOpenTime := baseTime.Add(5 * time.Second)

	for i := 1; i <= 3; i++ {
		probeTimestamp := halfOpenTime.Add(time.Duration(i) * time.Second)
		allowed := cb.TryAcquireProbe("RAIL-A", probeTimestamp)
		if !allowed {
			t.Fatalf("probe %d: expected probe to be allowed", i)
		}
		// Attempting another concurrent probe should be rejected
		concurrentAllowed := cb.TryAcquireProbe("RAIL-A", probeTimestamp)
		if concurrentAllowed {
			t.Fatalf("probe %d: expected concurrent probe to be rejected", i)
		}

		// Terminal success releases the single probe slot
		cb.RecordSuccess("RAIL-A", probeTimestamp)

		snap := cb.Snapshot("RAIL-A", probeTimestamp)
		if i < 3 {
			if snap.State != StateHalfOpen {
				t.Fatalf("probe %d: expected still HALF_OPEN, got %s", i, snap.State)
			}
			if snap.ActiveProbes != 0 {
				t.Fatalf("probe %d: expected ActiveProbes=0 (released), got %d", i, snap.ActiveProbes)
			}
			if snap.SuccessfulProbes != i {
				t.Fatalf("probe %d: expected SuccessfulProbes=%d, got %d", i, i, snap.SuccessfulProbes)
			}
		} else {
			if snap.State != StateClosed {
				t.Fatalf("probe 3: expected transition to CLOSED, got %s", snap.State)
			}
			if snap.ActiveProbes != 0 || snap.SuccessfulProbes != 0 {
				t.Fatalf("probe 3: expected ActiveProbes=0 and SuccessfulProbes=0, got %+v", snap)
			}
		}
	}
}

// 31. Probe slot reuse after success
func TestHalfOpenProbeAccounting_ProbeSlotReuseAfterSuccess(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 1
	cfg.SuccessThreshold = 2
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "fail", baseTime)
	halfOpenTime := baseTime.Add(5 * time.Second)

	// Consume slot
	allowed1 := cb.TryAcquireProbe("RAIL-A", halfOpenTime)
	if !allowed1 {
		t.Fatal("expected probe 1 allowed")
	}
	// Slot exhausted
	allowed2 := cb.TryAcquireProbe("RAIL-A", halfOpenTime)
	if allowed2 {
		t.Fatal("expected slot exhausted")
	}

	// Terminal success releases slot
	cb.RecordSuccess("RAIL-A", halfOpenTime)

	// Slot can now be reused!
	allowed3 := cb.TryAcquireProbe("RAIL-A", halfOpenTime.Add(time.Second))
	if !allowed3 {
		t.Fatal("expected slot reused after success")
	}
}

// 32. Probe slot reuse after failure
func TestHalfOpenProbeAccounting_ProbeSlotReuseAfterFailure(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 1
	cfg.SuccessThreshold = 2
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "fail", baseTime)
	halfOpenTime := baseTime.Add(5 * time.Second)

	// Probe admitted
	allowed1 := cb.TryAcquireProbe("RAIL-A", halfOpenTime)
	if !allowed1 {
		t.Fatal("expected probe allowed")
	}

	// Terminal failure releases slot and re-opens circuit
	cb.RecordFailure("RAIL-A", "probe_failed", halfOpenTime)
	snap := cb.Snapshot("RAIL-A", halfOpenTime)
	if snap.State != StateOpen || snap.ActiveProbes != 0 {
		t.Fatalf("expected OPEN with ActiveProbes=0, got state=%s active=%d", snap.State, snap.ActiveProbes)
	}

	// While OPEN, requests are blocked
	openAllowed, reason := cb.Allow("RAIL-A", halfOpenTime.Add(time.Second))
	if openAllowed || reason != "CIRCUIT_OPEN" {
		t.Fatalf("expected CIRCUIT_OPEN, got %v (%s)", openAllowed, reason)
	}

	// After cooldown, returns to HALF_OPEN and probe slot is reused cleanly
	nextHalfOpen := halfOpenTime.Add(5 * time.Second)
	reusedAllowed := cb.TryAcquireProbe("RAIL-A", nextHalfOpen)
	if !reusedAllowed {
		t.Fatal("expected probe slot reuse after failure+cooldown")
	}
}

// 33. Concurrent probe limit guarantees no budget leak
func TestHalfOpenProbeAccounting_ConcurrentProbeLimit(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 3
	cfg.SuccessThreshold = 10
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "fail", baseTime)
	halfOpenTime := baseTime.Add(5 * time.Second)

	const concurrency = 100
	var allowedCount int64
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			allowed := cb.TryAcquireProbe("RAIL-A", halfOpenTime)
			if allowed {
				atomic.AddInt64(&allowedCount, 1)
			}
		}()
	}
	wg.Wait()

	if allowedCount != 3 {
		t.Fatalf("expected exactly 3 concurrent probes admitted, got %d", allowedCount)
	}
	snap := cb.Snapshot("RAIL-A", halfOpenTime)
	if snap.ActiveProbes != 3 {
		t.Fatalf("expected ActiveProbes=3, got %d", snap.ActiveProbes)
	}
}

// 34. activeProbes never becomes negative and unadmitted probe results are ignored
func TestHalfOpenProbeAccounting_NoNegativeActiveProbes(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 2
	cfg.SuccessThreshold = 2
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "fail", baseTime)
	halfOpenTime := baseTime.Add(5 * time.Second)

	// activeProbes is 0. Spurious/unadmitted outcomes must not decrement below 0
	// nor count toward success threshold.
	cb.RecordSuccess("RAIL-A", halfOpenTime)
	snap1 := cb.Snapshot("RAIL-A", halfOpenTime)
	if snap1.ActiveProbes < 0 || snap1.ActiveProbes != 0 {
		t.Fatalf("expected ActiveProbes=0, got %d", snap1.ActiveProbes)
	}
	if snap1.SuccessfulProbes != 0 {
		t.Fatalf("expected unadmitted success to not increment SuccessfulProbes, got %d", snap1.SuccessfulProbes)
	}

	cb.ReleaseProbe("RAIL-A")
	snap2 := cb.Snapshot("RAIL-A", halfOpenTime)
	if snap2.ActiveProbes < 0 || snap2.ActiveProbes != 0 {
		t.Fatalf("expected ActiveProbes=0 after ReleaseProbe, got %d", snap2.ActiveProbes)
	}

	// Normal CLOSED state must also never have negative activeProbes
	cb.RecordSuccess("RAIL-B", baseTime)
	snapB := cb.Snapshot("RAIL-B", baseTime)
	if snapB.ActiveProbes < 0 {
		t.Fatalf("expected non-negative ActiveProbes on RAIL-B, got %d", snapB.ActiveProbes)
	}
}

type mockRepositoryWithErrors struct {
	mu       sync.Mutex
	events   []TransitionEvent
	queryErr error
}

func (m *mockRepositoryWithErrors) RecordTransition(ctx context.Context, event TransitionEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

func (m *mockRepositoryWithErrors) ListRecent(ctx context.Context, targetID string, limit int) ([]TransitionEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.queryErr != nil {
		return nil, m.queryErr
	}
	var res []TransitionEvent
	for _, e := range m.events {
		if e.ExecutionTargetID == targetID {
			res = append(res, e)
		}
	}
	return res, nil
}

// 35. Durable PostgreSQL repository transition history and error propagation
func TestDurableTransitionHistory_RepositoryBackedAndErrorPropagation(t *testing.T) {
	repo := &mockRepositoryWithErrors{}
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cb, err := NewBreaker(cfg, repo)
	if err != nil {
		t.Fatalf("failed to create breaker: %v", err)
	}

	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	// Seed repository with mock durable events
	repo.events = []TransitionEvent{
		{ID: 2, ExecutionTargetID: "RAIL-A", PreviousState: StateOpen, NewState: StateHalfOpen, Reason: ReasonCooldownExpired, TransitionedAt: baseTime.Add(5 * time.Second)},
		{ID: 1, ExecutionTargetID: "RAIL-A", PreviousState: StateClosed, NewState: StateOpen, Reason: ReasonThresholdReached, TransitionedAt: baseTime},
	}

	// 1. Successful repository query
	events, err := cb.GetEventsForTarget(context.Background(), "RAIL-A", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 durable events, got %d", len(events))
	}
	if events[0].ID != 2 || events[1].ID != 1 {
		t.Fatalf("expected ordering transitioned_at DESC, id DESC, got IDs %d, %d", events[0].ID, events[1].ID)
	}

	// 2. Database read error propagation
	repo.queryErr = errors.New("connection reset by peer")
	_, err = cb.GetEventsForTarget(context.Background(), "RAIL-A", 10)
	if err == nil {
		t.Fatal("expected error to propagate from repository, got nil")
	}
	if !strings.Contains(err.Error(), "connection reset by peer") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// 36. Regression Test A: HALF_OPEN failure without admitted probe
func TestRegression_A_HalfOpenFailureWithoutAdmittedProbe(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	// Enter HALF_OPEN via cooldown
	cb.RecordFailure("RAIL-A", "fail", baseTime)
	halfOpenTime := baseTime.Add(5 * time.Second)
	if state := cb.GetState("RAIL-A", halfOpenTime); state != StateHalfOpen {
		t.Fatalf("expected HALF_OPEN, got %s", state)
	}

	snapBefore := cb.Snapshot("RAIL-A", halfOpenTime)
	if snapBefore.ActiveProbes != 0 {
		t.Fatalf("expected activeProbes == 0, got %d", snapBefore.ActiveProbes)
	}

	// Unadmitted failure must be ignored; state must remain HALF_OPEN
	cb.RecordFailure("RAIL-A", "unadmitted_fail", halfOpenTime)
	snapAfter := cb.Snapshot("RAIL-A", halfOpenTime)
	if snapAfter.State != StateHalfOpen {
		t.Fatalf("expected state to remain HALF_OPEN on unadmitted failure, got %s", snapAfter.State)
	}
	if snapAfter.ActiveProbes != 0 {
		t.Fatalf("expected activeProbes to remain 0, got %d", snapAfter.ActiveProbes)
	}
}

// 37. Regression Test B: HALF_OPEN timeout without admitted probe
func TestRegression_B_HalfOpenTimeoutWithoutAdmittedProbe(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	// Enter HALF_OPEN via cooldown
	cb.RecordFailure("RAIL-A", "fail", baseTime)
	halfOpenTime := baseTime.Add(5 * time.Second)
	if state := cb.GetState("RAIL-A", halfOpenTime); state != StateHalfOpen {
		t.Fatalf("expected HALF_OPEN, got %s", state)
	}

	snapBefore := cb.Snapshot("RAIL-A", halfOpenTime)
	if snapBefore.ActiveProbes != 0 {
		t.Fatalf("expected activeProbes == 0, got %d", snapBefore.ActiveProbes)
	}

	// Unadmitted timeout must be ignored; state must remain HALF_OPEN
	cb.RecordTimeout("RAIL-A", halfOpenTime)
	snapAfter := cb.Snapshot("RAIL-A", halfOpenTime)
	if snapAfter.State != StateHalfOpen {
		t.Fatalf("expected state to remain HALF_OPEN on unadmitted timeout, got %s", snapAfter.State)
	}
	if snapAfter.ActiveProbes != 0 {
		t.Fatalf("expected activeProbes to remain 0, got %d", snapAfter.ActiveProbes)
	}
}

// 38. Regression Test C: HALF_OPEN success without admitted probe
func TestRegression_C_HalfOpenSuccessWithoutAdmittedProbe(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.SuccessThreshold = 2
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	// Enter HALF_OPEN via cooldown
	cb.RecordFailure("RAIL-A", "fail", baseTime)
	halfOpenTime := baseTime.Add(5 * time.Second)
	if state := cb.GetState("RAIL-A", halfOpenTime); state != StateHalfOpen {
		t.Fatalf("expected HALF_OPEN, got %s", state)
	}

	// Unadmitted success must be ignored; successfulProbes unchanged and state remains HALF_OPEN
	cb.RecordSuccess("RAIL-A", halfOpenTime)
	snap := cb.Snapshot("RAIL-A", halfOpenTime)
	if snap.State != StateHalfOpen {
		t.Fatalf("expected state to remain HALF_OPEN on unadmitted success, got %s", snap.State)
	}
	if snap.SuccessfulProbes != 0 {
		t.Fatalf("expected successfulProbes to remain 0, got %d", snap.SuccessfulProbes)
	}
	if snap.ActiveProbes != 0 {
		t.Fatalf("expected activeProbes to remain 0, got %d", snap.ActiveProbes)
	}
}

// 39. Regression Test D: Admitted probe + failure transitions HALF_OPEN -> OPEN
func TestRegression_D_AdmittedProbeFailureTransitionsToOpen(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 2
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "fail", baseTime)
	halfOpenTime := baseTime.Add(5 * time.Second)

	// Admitted probe
	allowed := cb.TryAcquireProbe("RAIL-A", halfOpenTime)
	if !allowed {
		t.Fatal("expected probe allowed")
	}

	// Terminal failure on admitted probe transitions HALF_OPEN -> OPEN
	cb.RecordFailure("RAIL-A", "probe_failure", halfOpenTime.Add(10*time.Millisecond))
	snap := cb.Snapshot("RAIL-A", halfOpenTime.Add(10*time.Millisecond))
	if snap.State != StateOpen {
		t.Fatalf("expected transition to OPEN on admitted probe failure, got %s", snap.State)
	}
	if snap.ActiveProbes != 0 {
		t.Fatalf("expected activeProbes to be reset to 0, got %d", snap.ActiveProbes)
	}
}

// 40. Regression Test E: Admitted probe + timeout transitions HALF_OPEN -> OPEN
func TestRegression_E_AdmittedProbeTimeoutTransitionsToOpen(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 2
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "fail", baseTime)
	halfOpenTime := baseTime.Add(5 * time.Second)

	// Admitted probe
	allowed := cb.TryAcquireProbe("RAIL-A", halfOpenTime)
	if !allowed {
		t.Fatal("expected probe allowed")
	}

	// Terminal timeout on admitted probe transitions HALF_OPEN -> OPEN
	cb.RecordTimeout("RAIL-A", halfOpenTime.Add(10*time.Millisecond))
	snap := cb.Snapshot("RAIL-A", halfOpenTime.Add(10*time.Millisecond))
	if snap.State != StateOpen {
		t.Fatalf("expected transition to OPEN on admitted probe timeout, got %s", snap.State)
	}
	if snap.ActiveProbes != 0 {
		t.Fatalf("expected activeProbes to be reset to 0, got %d", snap.ActiveProbes)
	}
}

// 41. Regression Test F: Probe slot reuse
func TestRegression_F_ProbeSlotReuse(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 1
	cfg.SuccessThreshold = 2
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "fail", baseTime)
	halfOpenTime := baseTime.Add(5 * time.Second)

	// 1. TryAcquireProbe() admits probe 1
	allowed1 := cb.TryAcquireProbe("RAIL-A", halfOpenTime)
	if !allowed1 {
		t.Fatal("expected probe 1 allowed")
	}
	// Concurrent attempt rejected
	if allowedExceeded := cb.TryAcquireProbe("RAIL-A", halfOpenTime); allowedExceeded {
		t.Fatal("expected probe limit exceeded")
	}

	// Payment routing rejected in HALF_OPEN
	payAllowed, payReason := cb.Allow("RAIL-A", halfOpenTime)
	if payAllowed || payReason != "CIRCUIT_HALF_OPEN" {
		t.Fatalf("expected payment routing rejected in HALF_OPEN, got %v (%s)", payAllowed, payReason)
	}

	// 2. RecordSuccess() releases slot
	cb.RecordSuccess("RAIL-A", halfOpenTime)
	snap := cb.Snapshot("RAIL-A", halfOpenTime)
	if snap.ActiveProbes != 0 {
		t.Fatalf("expected activeProbes returns to 0, got %d", snap.ActiveProbes)
	}

	// 3. TryAcquireProbe() again succeeds
	allowed2 := cb.TryAcquireProbe("RAIL-A", halfOpenTime.Add(time.Second))
	if !allowed2 {
		t.Fatal("expected TryAcquireProbe() again to succeed")
	}
}

// 42. Regression Test G: SuccessThreshold > HalfOpenProbeLimit allows sequential probes to close circuit
func TestRegression_G_SuccessThresholdGreaterThanProbeLimit(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 2
	cfg.SuccessThreshold = 4 // 4 sequential probes with max 2 concurrent
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "fail", baseTime)
	halfOpenTime := baseTime.Add(5 * time.Second)

	for i := 1; i <= 4; i++ {
		ts := halfOpenTime.Add(time.Duration(i) * time.Second)
		allowed := cb.TryAcquireProbe("RAIL-A", ts)
		if !allowed {
			t.Fatalf("probe %d: expected probe allowed", i)
		}
		cb.RecordSuccess("RAIL-A", ts)
	}

	finalSnap := cb.Snapshot("RAIL-A", halfOpenTime.Add(10*time.Second))
	if finalSnap.State != StateClosed {
		t.Fatalf("expected sequential probes to eventually close the circuit, got state=%s", finalSnap.State)
	}
}

// 43. Regression Test H: Concurrent probe limit enforces exact max active admissions
func TestRegression_H_ConcurrentProbeLimit(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 3
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "fail", baseTime)
	halfOpenTime := baseTime.Add(5 * time.Second)

	const totalRequests = 50
	var allowedCount int64
	var wg sync.WaitGroup
	wg.Add(totalRequests)

	for i := 0; i < totalRequests; i++ {
		go func() {
			defer wg.Done()
			allowed := cb.TryAcquireProbe("RAIL-A", halfOpenTime)
			if allowed {
				atomic.AddInt64(&allowedCount, 1)
			}
		}()
	}
	wg.Wait()

	if allowedCount != 3 {
		t.Fatalf("expected exactly 3 concurrent probe admissions, got %d", allowedCount)
	}
	snap := cb.Snapshot("RAIL-A", halfOpenTime)
	if snap.ActiveProbes != 3 {
		t.Fatalf("expected ActiveProbes=3, got %d", snap.ActiveProbes)
	}
}

// 44. Regression Test I: activeProbes never becomes negative
func TestRegression_I_ActiveProbesNeverNegative(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 2
	cfg.SuccessThreshold = 2
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	cb.RecordFailure("RAIL-A", "fail", baseTime)
	halfOpenTime := baseTime.Add(5 * time.Second)

	// Repeatedly exercise success/failure/timeout/release paths when activeProbes == 0
	for i := 0; i < 10; i++ {
		cb.RecordSuccess("RAIL-A", halfOpenTime)
		cb.RecordFailure("RAIL-A", "spurious_fail", halfOpenTime)
		cb.RecordTimeout("RAIL-A", halfOpenTime)
		cb.ReleaseProbe("RAIL-A")

		snap := cb.Snapshot("RAIL-A", halfOpenTime)
		if snap.ActiveProbes < 0 {
			t.Fatalf("iteration %d: activeProbes became negative: %d", i, snap.ActiveProbes)
		}
		if snap.State != StateHalfOpen {
			t.Fatalf("iteration %d: state mutated without admitted probe: %s", i, snap.State)
		}
	}
}

// Helpers for Health Probe Admission Tests

type testHealthChecker func(context.Context) (bank.HealthResult, error)

func (c testHealthChecker) GetHealth(ctx context.Context) (bank.HealthResult, error) {
	return c(ctx)
}

type testHealthRepo struct {
	mu      sync.Mutex
	samples []health.HealthSample
}

func (r *testHealthRepo) Record(_ context.Context, sample health.HealthSample) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.samples = append(r.samples, sample)
	return nil
}

func (r *testHealthRepo) ListRecent(_ context.Context, targetID string, from, to time.Time, limit int) ([]health.HealthSample, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []health.HealthSample
	for _, s := range r.samples {
		if s.TargetID == targetID && !s.SampledAt.Before(from) && !s.SampledAt.After(to) {
			result = append(result, s)
		}
	}
	return result, nil
}

func newWiredHealthService(cb *CircuitBreaker) *health.Service {
	repo := &testHealthRepo{}
	hs := health.NewService(repo, health.DefaultConfig())
	hs.SetSampleObserver(cb.RecordHealthSample)
	hs.SetProbeGate(cb)
	return hs
}

// 45. Requirement 1: HALF_OPEN health success can recover to CLOSED
func TestRequirement1_HalfOpenHealthSuccessRecoversToClosed(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 2
	cfg.SuccessThreshold = 2
	cfg.RestorationSteps = 1 // direct to CLOSED
	cb, _ := NewBreaker(cfg)
	hs := newWiredHealthService(cb)

	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	cb.RecordFailure("RAIL-A", "fail", baseTime)

	halfOpenTime := baseTime.Add(5 * time.Second)
	checker := testHealthChecker(func(context.Context) (bank.HealthResult, error) {
		return bank.HealthResult{Available: true}, nil
	})

	// Probe 1
	s1, err1 := hs.Sample(context.Background(), "RAIL-A", checker, halfOpenTime.Add(time.Second))
	if err1 != nil {
		t.Fatalf("probe 1 failed: %v", err1)
	}
	if s1.Outcome != health.OutcomeSuccess {
		t.Fatalf("probe 1 outcome: %v", s1.Outcome)
	}
	snap1 := cb.Snapshot("RAIL-A", halfOpenTime.Add(time.Second))
	if snap1.State != StateHalfOpen || snap1.SuccessfulProbes != 1 || snap1.ActiveProbes != 0 {
		t.Fatalf("expected StateHalfOpen with 1 successful probe and 0 active, got: %+v", snap1)
	}

	// Probe 2 (meets SuccessThreshold=2)
	s2, err2 := hs.Sample(context.Background(), "RAIL-A", checker, halfOpenTime.Add(2*time.Second))
	if err2 != nil {
		t.Fatalf("probe 2 failed: %v", err2)
	}
	if s2.Outcome != health.OutcomeSuccess {
		t.Fatalf("probe 2 outcome: %v", s2.Outcome)
	}
	snap2 := cb.Snapshot("RAIL-A", halfOpenTime.Add(2*time.Second))
	if snap2.State != StateClosed {
		t.Fatalf("expected StateClosed after reaching success threshold, got: %s", snap2.State)
	}
	if snap2.ActiveProbes != 0 || snap2.SuccessfulProbes != 0 {
		t.Fatalf("expected reset counters in CLOSED state, got: %+v", snap2)
	}
}

// 46. Requirement 2: HALF_OPEN health failure reopens to OPEN
func TestRequirement2_HalfOpenHealthFailureReopensToOpen(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 2
	cfg.SuccessThreshold = 2
	cb, _ := NewBreaker(cfg)
	hs := newWiredHealthService(cb)

	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	cb.RecordFailure("RAIL-A", "fail", baseTime)

	halfOpenTime := baseTime.Add(5 * time.Second)
	checker := testHealthChecker(func(context.Context) (bank.HealthResult, error) {
		return bank.HealthResult{Available: false}, nil
	})

	sample, err := hs.Sample(context.Background(), "RAIL-A", checker, halfOpenTime.Add(time.Second))
	if err != nil {
		t.Fatalf("health sample error: %v", err)
	}
	if sample.Outcome != health.OutcomeFailure {
		t.Fatalf("expected OutcomeFailure, got %v", sample.Outcome)
	}

	snap := cb.Snapshot("RAIL-A", halfOpenTime.Add(time.Second))
	if snap.State != StateOpen {
		t.Fatalf("expected StateOpen after health failure in HALF_OPEN, got: %s", snap.State)
	}
	if snap.ActiveProbes != 0 || snap.SuccessfulProbes != 0 {
		t.Fatalf("expected reset counters, got: %+v", snap)
	}
}

// 47. Requirement 3: HALF_OPEN health timeout reopens to OPEN
func TestRequirement3_HalfOpenHealthTimeoutReopensToOpen(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 2
	cfg.SuccessThreshold = 2
	cb, _ := NewBreaker(cfg)
	hs := newWiredHealthService(cb)

	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	cb.RecordFailure("RAIL-A", "fail", baseTime)

	halfOpenTime := baseTime.Add(5 * time.Second)
	checker := testHealthChecker(func(ctx context.Context) (bank.HealthResult, error) {
		return bank.HealthResult{}, context.DeadlineExceeded
	})

	sample, err := hs.Sample(context.Background(), "RAIL-A", checker, halfOpenTime.Add(time.Second))
	if err != nil {
		t.Fatalf("health sample error: %v", err)
	}
	if sample.Outcome != health.OutcomeTimeout {
		t.Fatalf("expected OutcomeTimeout, got %v", sample.Outcome)
	}

	snap := cb.Snapshot("RAIL-A", halfOpenTime.Add(time.Second))
	if snap.State != StateOpen {
		t.Fatalf("expected StateOpen after health timeout in HALF_OPEN, got: %s", snap.State)
	}
	if snap.ActiveProbes != 0 || snap.SuccessfulProbes != 0 {
		t.Fatalf("expected reset counters, got: %+v", snap)
	}
}

// 48. Requirement 4: No-admission outcome remains ignored
func TestRequirement4_NoAdmissionOutcomeRemainsIgnored(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 2
	cfg.SuccessThreshold = 2
	cb, _ := NewBreaker(cfg)

	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	cb.RecordFailure("RAIL-A", "fail", baseTime)

	halfOpenTime := baseTime.Add(5 * time.Second)
	// Target enters HALF_OPEN via cooldown evaluation
	if st := cb.GetState("RAIL-A", halfOpenTime); st != StateHalfOpen {
		t.Fatalf("expected HALF_OPEN, got %s", st)
	}

	// Directly record health sample with activeProbes == 0 (no admission)
	cb.RecordHealthSample(health.HealthSample{
		TargetID:  "RAIL-A",
		Outcome:   health.OutcomeSuccess,
		SampledAt: halfOpenTime.Add(time.Second),
	})
	snap := cb.Snapshot("RAIL-A", halfOpenTime.Add(time.Second))
	if snap.State != StateHalfOpen || snap.SuccessfulProbes != 0 {
		t.Fatalf("unadmitted success must be ignored, got: %+v", snap)
	}

	// Directly record unadmitted failure
	cb.RecordHealthSample(health.HealthSample{
		TargetID:  "RAIL-A",
		Outcome:   health.OutcomeFailure,
		SampledAt: halfOpenTime.Add(2 * time.Second),
	})
	snap2 := cb.Snapshot("RAIL-A", halfOpenTime.Add(2*time.Second))
	if snap2.State != StateHalfOpen {
		t.Fatalf("unadmitted failure must be ignored, got: %+v", snap2)
	}

	// Directly record unadmitted timeout
	cb.RecordHealthSample(health.HealthSample{
		TargetID:  "RAIL-A",
		Outcome:   health.OutcomeTimeout,
		SampledAt: halfOpenTime.Add(3 * time.Second),
	})
	snap3 := cb.Snapshot("RAIL-A", halfOpenTime.Add(3*time.Second))
	if snap3.State != StateHalfOpen {
		t.Fatalf("unadmitted timeout must be ignored, got: %+v", snap3)
	}
}

// 49. Requirement 5: Health probe concurrency never exceeds HalfOpenProbeLimit
func TestRequirement5_HealthProbeConcurrencyNeverExceedsLimit(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 2
	cfg.SuccessThreshold = 10
	cb, _ := NewBreaker(cfg)
	hs := newWiredHealthService(cb)

	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	cb.RecordFailure("RAIL-A", "fail", baseTime)
	halfOpenTime := baseTime.Add(5 * time.Second)

	blocker := make(chan struct{})
	var currentActive int64
	var maxActive int64
	var rejectedCount int64
	var completedCount int64

	checker := testHealthChecker(func(ctx context.Context) (bank.HealthResult, error) {
		act := atomic.AddInt64(&currentActive, 1)
		for {
			oldMax := atomic.LoadInt64(&maxActive)
			if act <= oldMax || atomic.CompareAndSwapInt64(&maxActive, oldMax, act) {
				break
			}
		}
		<-blocker
		atomic.AddInt64(&currentActive, -1)
		return bank.HealthResult{Available: true}, nil
	})

	const totalAttempts = 10
	var wg sync.WaitGroup
	wg.Add(totalAttempts)

	for i := 0; i < totalAttempts; i++ {
		go func() {
			defer wg.Done()
			_, err := hs.Sample(context.Background(), "RAIL-A", checker, halfOpenTime)
			if errors.Is(err, health.ErrProbeRejected) {
				atomic.AddInt64(&rejectedCount, 1)
			} else if err == nil {
				atomic.AddInt64(&completedCount, 1)
			}
		}()
	}

	// Give time for concurrent routines to attempt admission
	time.Sleep(50 * time.Millisecond)

	// Max concurrent active probes must not exceed HalfOpenProbeLimit (2)
	peak := atomic.LoadInt64(&maxActive)
	if peak > 2 {
		t.Fatalf("peak concurrent active probes %d exceeded HalfOpenProbeLimit=2", peak)
	}

	// Unblock admitted probes
	close(blocker)
	wg.Wait()

	if completedCount != 2 {
		t.Fatalf("expected exactly 2 completed probes, got %d", completedCount)
	}
	if rejectedCount != 8 {
		t.Fatalf("expected exactly 8 rejected probes, got %d", rejectedCount)
	}
}

// 50. Requirement 6: Probe slot is reusable after completion
func TestRequirement6_ProbeSlotReusableAfterCompletion(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 1
	cfg.SuccessThreshold = 3
	cfg.RestorationSteps = 1
	cb, _ := NewBreaker(cfg)
	hs := newWiredHealthService(cb)

	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	cb.RecordFailure("RAIL-A", "fail", baseTime)
	halfOpenTime := baseTime.Add(5 * time.Second)

	checker := testHealthChecker(func(context.Context) (bank.HealthResult, error) {
		return bank.HealthResult{Available: true}, nil
	})

	// Run 3 probes sequentially with HalfOpenProbeLimit=1
	for i := 1; i <= 3; i++ {
		sampleTime := halfOpenTime.Add(time.Duration(i) * time.Second)
		sample, err := hs.Sample(context.Background(), "RAIL-A", checker, sampleTime)
		if err != nil {
			t.Fatalf("probe %d failed: %v", i, err)
		}
		if sample.Outcome != health.OutcomeSuccess {
			t.Fatalf("probe %d outcome = %v", i, sample.Outcome)
		}
	}

	snap := cb.Snapshot("RAIL-A", halfOpenTime.Add(10*time.Second))
	if snap.State != StateClosed {
		t.Fatalf("expected StateClosed after sequential slot reuse, got %s", snap.State)
	}
}

// 51. Requirement 7: SuccessThreshold > HalfOpenProbeLimit works sequentially
func TestRequirement7_SuccessThresholdGreaterThanProbeLimitSequential(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 2
	cfg.SuccessThreshold = 4
	cfg.RestorationSteps = 1
	cb, _ := NewBreaker(cfg)
	hs := newWiredHealthService(cb)

	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	cb.RecordFailure("RAIL-A", "fail", baseTime)
	halfOpenTime := baseTime.Add(5 * time.Second)

	checker := testHealthChecker(func(context.Context) (bank.HealthResult, error) {
		return bank.HealthResult{Available: true}, nil
	})

	for i := 1; i <= 4; i++ {
		st := halfOpenTime.Add(time.Duration(i) * time.Second)
		s, err := hs.Sample(context.Background(), "RAIL-A", checker, st)
		if err != nil {
			t.Fatalf("probe %d failed: %v", i, err)
		}
		if s.Outcome != health.OutcomeSuccess {
			t.Fatalf("probe %d outcome: %v", i, s.Outcome)
		}
		snap := cb.Snapshot("RAIL-A", st)
		if i < 4 {
			if snap.State != StateHalfOpen || snap.SuccessfulProbes != i {
				t.Fatalf("probe %d: expected StateHalfOpen with SuccessfulProbes=%d, got: %+v", i, i, snap)
			}
		} else {
			if snap.State != StateClosed {
				t.Fatalf("probe 4: expected StateClosed, got %s", snap.State)
			}
		}
	}
}

// 52. Requirement 8: CLOSED health samples still affect failure/timeout windows
func TestRequirement8_ClosedHealthSamplesAffectFailureTimeoutWindows(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 3
	cfg.RollingWindow = 10 * time.Second
	cb, _ := NewBreaker(cfg)
	hs := newWiredHealthService(cb)

	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	failChecker := testHealthChecker(func(context.Context) (bank.HealthResult, error) {
		return bank.HealthResult{Available: false}, nil
	})
	timeoutChecker := testHealthChecker(func(context.Context) (bank.HealthResult, error) {
		return bank.HealthResult{}, context.DeadlineExceeded
	})

	// Sample 1: failure
	_, err1 := hs.Sample(context.Background(), "RAIL-A", failChecker, baseTime)
	if err1 != nil {
		t.Fatal(err1)
	}
	snap1 := cb.Snapshot("RAIL-A", baseTime)
	if snap1.State != StateClosed || snap1.FailureCount != 1 {
		t.Fatalf("expected CLOSED with 1 failure, got: %+v", snap1)
	}

	// Sample 2: timeout
	_, err2 := hs.Sample(context.Background(), "RAIL-A", timeoutChecker, baseTime.Add(time.Second))
	if err2 != nil {
		t.Fatal(err2)
	}
	snap2 := cb.Snapshot("RAIL-A", baseTime.Add(time.Second))
	if snap2.State != StateClosed || snap2.TimeoutCount != 1 {
		t.Fatalf("expected CLOSED with 1 timeout, got: %+v", snap2)
	}

	// Sample 3: failure (triggers threshold: 2 failures + 1 timeout = 3)
	_, err3 := hs.Sample(context.Background(), "RAIL-A", failChecker, baseTime.Add(2*time.Second))
	if err3 != nil {
		t.Fatal(err3)
	}
	snap3 := cb.Snapshot("RAIL-A", baseTime.Add(2*time.Second))
	if snap3.State != StateOpen {
		t.Fatalf("expected OPEN after reaching threshold from health samples, got: %s", snap3.State)
	}
}

// 53. Requirement 9: OPEN health target does not execute recovery probe before cooldown
func TestRequirement9_OpenHealthTargetDoesNotProbeBeforeCooldown(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 10 * time.Second
	cb, _ := NewBreaker(cfg)
	hs := newWiredHealthService(cb)

	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	cb.RecordFailure("RAIL-A", "fail", baseTime)

	var checkerInvoked bool
	checker := testHealthChecker(func(context.Context) (bank.HealthResult, error) {
		checkerInvoked = true
		return bank.HealthResult{Available: true}, nil
	})

	// Sample at 3s elapsed (cooldown is 10s)
	sampleTime := baseTime.Add(3 * time.Second)
	_, err := hs.Sample(context.Background(), "RAIL-A", checker, sampleTime)
	if !errors.Is(err, health.ErrProbeRejected) {
		t.Fatalf("expected ErrProbeRejected before cooldown, got: %v", err)
	}
	if checkerInvoked {
		t.Fatalf("checker must NOT be invoked before cooldown has elapsed")
	}

	snap := cb.Snapshot("RAIL-A", sampleTime)
	if snap.State != StateOpen {
		t.Fatalf("expected target to remain OPEN, got: %s", snap.State)
	}
}

// 54. Requirement 10: M2-4 route eligibility tests continue to pass
func TestRequirement10_M24RouteEligibilityPreserved(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 1
	cb, _ := NewBreaker(cfg)

	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	hook := cb.EligibilityHook(func() time.Time { return baseTime })

	// Initially CLOSED -> eligible
	candA := payments.RouteCandidate{ExecutionTargetID: "RAIL-A"}
	if !hook(context.Background(), candA) {
		t.Fatal("expected RAIL-A to be eligible when CLOSED")
	}

	// Trip RAIL-A -> OPEN
	cb.RecordFailure("RAIL-A", "fail", baseTime)
	if hook(context.Background(), candA) {
		t.Fatal("expected RAIL-A to be ineligible when OPEN")
	}

	// RAIL-B remains CLOSED and eligible
	candB := payments.RouteCandidate{ExecutionTargetID: "RAIL-B"}
	if !hook(context.Background(), candB) {
		t.Fatal("expected RAIL-B to remain eligible")
	}
}

// 55. Requirement 11: Target A and B circuit state remain isolated
func TestRequirement11_TargetIsolationUnderHealthSampling(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 2
	cfg.SuccessThreshold = 2
	cb, _ := NewBreaker(cfg)
	hs := newWiredHealthService(cb)

	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	// Trip target A only
	cb.RecordFailure("TARGET-A", "fail", baseTime)

	halfOpenTime := baseTime.Add(5 * time.Second)
	failChecker := testHealthChecker(func(context.Context) (bank.HealthResult, error) {
		return bank.HealthResult{Available: false}, nil
	})

	// Fail TARGET-A health probe
	_, _ = hs.Sample(context.Background(), "TARGET-A", failChecker, halfOpenTime.Add(time.Second))

	snapA := cb.Snapshot("TARGET-A", halfOpenTime.Add(time.Second))
	snapB := cb.Snapshot("TARGET-B", halfOpenTime.Add(time.Second))

	if snapA.State != StateOpen {
		t.Fatalf("expected TARGET-A to be OPEN, got %s", snapA.State)
	}
	if snapB.State != StateClosed || snapB.FailureCount != 0 {
		t.Fatalf("expected TARGET-B to remain cleanly CLOSED, got: %+v", snapB)
	}
}

type spyBankAdapter struct {
	holdFundsCalls     int64
	provisionalCalls   int64
	confirmHoldCalls   int64
	releaseHoldCalls   int64
	reverseCreditCalls int64
}

func (s *spyBankAdapter) GetHealth(context.Context) (bank.HealthResult, error) {
	return bank.HealthResult{Available: true}, nil
}
func (s *spyBankAdapter) ResolveAccount(context.Context, bank.ResolveAccountRequest) (bank.AccountResult, error) {
	return bank.AccountResult{}, nil
}
func (s *spyBankAdapter) HoldFunds(context.Context, bank.HoldFundsRequest) (bank.HoldResult, error) {
	atomic.AddInt64(&s.holdFundsCalls, 1)
	return bank.HoldResult{}, nil
}
func (s *spyBankAdapter) ProvisionalCredit(context.Context, bank.ProvisionalCreditRequest) (bank.OperationResult, error) {
	atomic.AddInt64(&s.provisionalCalls, 1)
	return bank.OperationResult{}, nil
}
func (s *spyBankAdapter) ConfirmHold(context.Context, bank.ConfirmHoldRequest) (bank.OperationResult, error) {
	atomic.AddInt64(&s.confirmHoldCalls, 1)
	return bank.OperationResult{}, nil
}
func (s *spyBankAdapter) ReleaseHold(context.Context, bank.ReleaseHoldRequest) (bank.OperationResult, error) {
	atomic.AddInt64(&s.releaseHoldCalls, 1)
	return bank.OperationResult{}, nil
}
func (s *spyBankAdapter) ReverseProvisionalCredit(context.Context, bank.ReverseCreditRequest) (bank.OperationResult, error) {
	atomic.AddInt64(&s.reverseCreditCalls, 1)
	return bank.OperationResult{}, nil
}
func (s *spyBankAdapter) GetOperationStatus(context.Context, bank.OperationStatusRequest) (bank.OperationResult, error) {
	return bank.OperationResult{}, nil
}
func (s *spyBankAdapter) GetLedgerSnapshot(context.Context, bank.LedgerScope) (bank.LedgerSnapshot, error) {
	return bank.LedgerSnapshot{}, nil
}

// 56. Requirement 12: No monetary BankAdapter method is invoked by circuit logic
func TestRequirement12_NoMonetaryBankAdapterMethodInvokedByCircuit(t *testing.T) {
	spy := &spyBankAdapter{}

	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 2
	cfg.SuccessThreshold = 1
	cfg.RestorationSteps = 1
	cb, _ := NewBreaker(cfg)
	hs := newWiredHealthService(cb)

	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	// Exercise full lifecycle
	cb.RecordFailure("TARGET-A", "fail", baseTime)
	halfOpenTime := baseTime.Add(5 * time.Second)

	checker := testHealthChecker(func(ctx context.Context) (bank.HealthResult, error) {
		return spy.GetHealth(ctx)
	})
	_, err := hs.Sample(context.Background(), "TARGET-A", checker, halfOpenTime.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}

	snap := cb.Snapshot("TARGET-A", halfOpenTime.Add(2*time.Second))
	if snap.State != StateClosed {
		t.Fatalf("expected StateClosed, got %s", snap.State)
	}

	// Verify ZERO monetary methods invoked on the adapter
	if spy.holdFundsCalls != 0 || spy.provisionalCalls != 0 || spy.confirmHoldCalls != 0 || spy.releaseHoldCalls != 0 || spy.reverseCreditCalls != 0 {
		t.Fatalf("monetary methods invoked on BankAdapter: hold=%d, prov=%d, confirm=%d, release=%d, reverse=%d",
			spy.holdFundsCalls, spy.provisionalCalls, spy.confirmHoldCalls, spy.releaseHoldCalls, spy.reverseCreditCalls)
	}
}

// 57. Requirement 14: Unknown payment outcomes remain untouched/pending; circuit code does not reinterpret them
func TestRequirement14_UnknownPaymentOutcomesRemainUntouchedPending(t *testing.T) {
	cfg := newTestConfig()
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	// Simulate payment context where a payment outcome is pending/unknown
	// Circuit breaker does not infer payment outcomes into health state.
	// Only explicit health monitoring samples or deliberate recorded events update circuit breaker.
	snapBefore := cb.Snapshot("RAIL-A", baseTime)
	if snapBefore.State != StateClosed {
		t.Fatalf("expected initial state CLOSED, got %s", snapBefore.State)
	}

	// Verify that payment route eligibility check Allow() does not record an outcome or mutate state
	allowed, reason := cb.Allow("RAIL-A", baseTime)
	if !allowed || reason != "CIRCUIT_CLOSED" {
		t.Fatalf("expected Allow() to return true, got %v (%s)", allowed, reason)
	}

	snapAfter := cb.Snapshot("RAIL-A", baseTime)
	if snapAfter.FailureCount != 0 || snapAfter.TimeoutCount != 0 || snapAfter.State != StateClosed {
		t.Fatalf("circuit breaker state was modified by payment check: %+v", snapAfter)
	}

	// In the event of an unknown payment outcome (e.g. pending saga), no circuit breaker method
	// may assume failure or fabricate health observations.
	if snapAfter.ActiveProbes != 0 || snapAfter.SuccessfulProbes != 0 {
		t.Fatalf("expected probe counts to remain 0, got active=%d success=%d", snapAfter.ActiveProbes, snapAfter.SuccessfulProbes)
	}
}

// 58. Regression: HALF_OPEN payment route selection cannot consume or leak the health probe budget
func TestRegression_HalfOpenPaymentRouteSelectionCannotConsumeOrLeakProbeBudget(t *testing.T) {
	cfg := newTestConfig()
	cfg.FailureThreshold = 1
	cfg.OpenCooldown = 5 * time.Second
	cfg.HalfOpenProbeLimit = 2
	cfg.SuccessThreshold = 2
	cfg.RestorationSteps = 1
	cb, _ := NewBreaker(cfg)
	baseTime := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

	// Trip RAIL-A to OPEN
	cb.RecordFailure("RAIL-A", "fail", baseTime)

	// Advance time past cooldown to enter HALF_OPEN
	halfOpenTime := baseTime.Add(5 * time.Second)
	if st := cb.GetState("RAIL-A", halfOpenTime); st != StateHalfOpen {
		t.Fatalf("expected state HALF_OPEN, got %s", st)
	}

	hook := cb.EligibilityHook(func() time.Time { return halfOpenTime })
	bankA := uuid.New()
	bankB := uuid.New()
	adapter := &strictFailBankAdapter{}
	candidates := []payments.RouteCandidate{
		{CandidateID: "cand-a", ExecutionTargetID: "RAIL-A", SourceBankID: bankA, DestinationBankID: bankB, SourceAdapter: adapter, DestinationAdapter: adapter},
		{CandidateID: "cand-b", ExecutionTargetID: "RAIL-B", SourceBankID: bankA, DestinationBankID: bankB, SourceAdapter: adapter, DestinationAdapter: adapter},
	}
	provider := staticSnapshotProvider{
		snapshots: map[string]health.HealthSnapshot{
			"RAIL-A": {TargetID: "RAIL-A", Score: 0.99, AvailabilityScore: 1.0, SampleCount: 10},
			"RAIL-B": {TargetID: "RAIL-B", Score: 0.50, AvailabilityScore: 1.0, SampleCount: 10},
		},
	}

	// Attempt multiple payment route selections while RAIL-A is HALF_OPEN
	for i := 0; i < 10; i++ {
		decision, err := payments.SelectRoute(context.Background(), candidates, payments.SelectionModeAdaptive, provider, hook, halfOpenTime)
		if err != nil {
			t.Fatalf("iteration %d: unexpected selection error: %v", i, err)
		}
		// RAIL-A must be excluded from payment routing; RAIL-B must be selected
		if decision.Candidate.ExecutionTargetID != "RAIL-B" {
			t.Fatalf("iteration %d: expected selection of RAIL-B, got %s", i, decision.Candidate.ExecutionTargetID)
		}

		// Also directly verify cb.Allow("RAIL-A") rejects payment routing
		allowed, reason := cb.Allow("RAIL-A", halfOpenTime)
		if allowed || reason != "CIRCUIT_HALF_OPEN" {
			t.Fatalf("iteration %d: expected Allow() = false, CIRCUIT_HALF_OPEN, got %v (%s)", i, allowed, reason)
		}

		// Verify zero probe budget consumed or leaked
		snap := cb.Snapshot("RAIL-A", halfOpenTime)
		if snap.ActiveProbes != 0 {
			t.Fatalf("iteration %d: payment route selection leaked activeProbes=%d", i, snap.ActiveProbes)
		}
	}

	// Now prove that the full recovery health probe budget (2 slots) is completely intact and available
	probe1Admitted := cb.TryAcquireProbe("RAIL-A", halfOpenTime)
	if !probe1Admitted {
		t.Fatal("expected health recovery probe 1 to be admitted")
	}
	probe2Admitted := cb.TryAcquireProbe("RAIL-A", halfOpenTime)
	if !probe2Admitted {
		t.Fatal("expected health recovery probe 2 to be admitted")
	}
	// Third probe exceeds the budget
	probe3Admitted := cb.TryAcquireProbe("RAIL-A", halfOpenTime)
	if probe3Admitted {
		t.Fatal("expected health recovery probe 3 to be rejected (budget exhausted)")
	}

	snapProbes := cb.Snapshot("RAIL-A", halfOpenTime)
	if snapProbes.ActiveProbes != 2 {
		t.Fatalf("expected exactly 2 active health probes, got %d", snapProbes.ActiveProbes)
	}
}
