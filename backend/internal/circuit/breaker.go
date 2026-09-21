package circuit

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/transactx/backend/internal/health"
	"github.com/transactx/backend/internal/payments"
)

// TargetBreaker manages the deterministic circuit breaker state for a single executionTargetID.
type TargetBreaker struct {
	mu                   sync.Mutex
	targetID             string
	config               Config
	state                State
	openedAt             time.Time
	halfOpenedAt         time.Time
	failureTimestamps    []time.Time
	timeoutTimestamps    []time.Time
	consecutiveSuccesses int
	activeProbes         int
	successfulProbes     int
	restorationStep      int // 0 = not in restoration (normal CLOSED or OPEN/HALF_OPEN); 1..RestorationSteps = in recovery
	restorationRequests  int
	listener             func(TransitionEvent)
}

func newTargetBreaker(targetID string, config Config, listener func(TransitionEvent)) *TargetBreaker {
	return &TargetBreaker{
		targetID: targetID,
		config:   config,
		state:    StateClosed,
		listener: listener,
	}
}

// GetState returns the current state, evaluating automatic cooldown transition if applicable.
func (tb *TargetBreaker) GetState(now time.Time) State {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.evaluateCooldown(now)
	return tb.state
}

// Allow evaluates if a request to this target is eligible under the current circuit state.
// In OPEN and HALF_OPEN, payment requests are ineligible (HALF_OPEN is reserved for recovery health probes).
// In CLOSED gradual restoration, it applies deterministic restoration shedding.
func (tb *TargetBreaker) Allow(now time.Time) (bool, string) {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.evaluateCooldown(now)

	switch tb.state {
	case StateOpen:
		return false, "CIRCUIT_OPEN"

	case StateHalfOpen:
		return false, "CIRCUIT_HALF_OPEN"

	case StateClosed:
		if tb.restorationStep > 0 && tb.restorationStep < tb.config.RestorationSteps {
			tb.restorationRequests++
			quota := tb.restorationStep
			total := tb.config.RestorationSteps
			allowed := (tb.restorationRequests % total) < quota
			if !allowed {
				return false, "GRADUAL_RESTORATION_SHED"
			}
			return true, "CIRCUIT_CLOSED_RESTORING"
		}
		return true, "CIRCUIT_CLOSED"

	default:
		return true, "CIRCUIT_CLOSED"
	}
}

// RecordSuccess records a successful call or probe for this target.
func (tb *TargetBreaker) RecordSuccess(now time.Time) {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.evaluateCooldown(now)

	switch tb.state {
	case StateHalfOpen:
		if tb.activeProbes > 0 {
			tb.activeProbes--
		} else {
			// A probe result must correspond to a probe admission.
			return
		}
		tb.successfulProbes++
		tb.consecutiveSuccesses++
		if tb.successfulProbes >= tb.config.SuccessThreshold {
			if tb.config.RestorationSteps > 1 {
				tb.restorationStep = 1
				tb.restorationRequests = 0
			} else {
				tb.restorationStep = 0
			}
			tb.transitionTo(StateClosed, ReasonProbesSucceeded, now)
			tb.activeProbes = 0
			tb.successfulProbes = 0
			tb.failureTimestamps = nil
			tb.timeoutTimestamps = nil
		}

	case StateClosed:
		tb.consecutiveSuccesses++
		if tb.restorationStep > 0 && tb.restorationStep < tb.config.RestorationSteps {
			if tb.consecutiveSuccesses%tb.config.StepSuccessThreshold == 0 {
				tb.restorationStep++
				if tb.restorationStep >= tb.config.RestorationSteps {
					tb.restorationStep = 0 // fully restored!
				}
			}
		}
		tb.pruneOldTimestamps(now)
		if tb.config.SuccessPolicy == SuccessPolicyReset {
			tb.failureTimestamps = nil
			tb.timeoutTimestamps = nil
		} else {
			if len(tb.failureTimestamps) > 0 {
				tb.failureTimestamps = tb.failureTimestamps[1:]
			} else if len(tb.timeoutTimestamps) > 0 {
				tb.timeoutTimestamps = tb.timeoutTimestamps[1:]
			}
		}

	case StateOpen:
		// Target is open; probe or call completed after transition
	}
}

// RecordFailure records a failure for this target.
func (tb *TargetBreaker) RecordFailure(reason string, now time.Time) {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.evaluateCooldown(now)

	switch tb.state {
	case StateHalfOpen:
		if tb.activeProbes == 0 {
			// No admitted probe = no state transition, ignore outcome
			return
		}
		tb.activeProbes--
		tb.activeProbes = 0
		tb.successfulProbes = 0
		tb.consecutiveSuccesses = 0
		tb.restorationStep = 0
		tb.transitionTo(StateOpen, ReasonProbeFailed, now)

	case StateClosed:
		tb.consecutiveSuccesses = 0
		tb.failureTimestamps = append(tb.failureTimestamps, now)
		tb.pruneOldTimestamps(now)
		effective := len(tb.failureTimestamps)
		if tb.config.TimeoutThreshold == 0 {
			effective += len(tb.timeoutTimestamps)
		}
		if effective >= tb.config.FailureThreshold {
			tb.restorationStep = 0
			tb.transitionTo(StateOpen, ReasonThresholdReached, now)
		}

	case StateOpen:
		tb.failureTimestamps = append(tb.failureTimestamps, now)
		tb.pruneOldTimestamps(now)
	}
}

// RecordTimeout records a timeout for this target.
func (tb *TargetBreaker) RecordTimeout(now time.Time) {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.evaluateCooldown(now)

	switch tb.state {
	case StateHalfOpen:
		if tb.activeProbes == 0 {
			// No admitted probe = no state transition, ignore outcome
			return
		}
		tb.activeProbes--
		tb.activeProbes = 0
		tb.successfulProbes = 0
		tb.consecutiveSuccesses = 0
		tb.restorationStep = 0
		tb.transitionTo(StateOpen, ReasonProbeFailed, now)

	case StateClosed:
		tb.consecutiveSuccesses = 0
		tb.timeoutTimestamps = append(tb.timeoutTimestamps, now)
		tb.pruneOldTimestamps(now)
		if tb.config.TimeoutThreshold > 0 {
			if len(tb.timeoutTimestamps) >= tb.config.TimeoutThreshold {
				tb.restorationStep = 0
				tb.transitionTo(StateOpen, ReasonTimeoutThresholdReached, now)
			}
		} else {
			if (len(tb.failureTimestamps) + len(tb.timeoutTimestamps)) >= tb.config.FailureThreshold {
				tb.restorationStep = 0
				tb.transitionTo(StateOpen, ReasonThresholdReached, now)
			}
		}

	case StateOpen:
		tb.timeoutTimestamps = append(tb.timeoutTimestamps, now)
		tb.pruneOldTimestamps(now)
	}
}

// TryAcquireProbe attempts to acquire an active probe slot if the target is in HALF_OPEN.
// Evaluates cooldown first if target is OPEN.
func (tb *TargetBreaker) TryAcquireProbe(now time.Time) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.evaluateCooldown(now)
	if tb.state != StateHalfOpen {
		return false
	}
	if tb.activeProbes >= tb.config.HalfOpenProbeLimit {
		return false
	}
	tb.activeProbes++
	return true
}

// BeforeHealthSample evaluates whether a health sample can be executed for this target.
// Returns allowed=true if the health check may proceed.
// If the target is HALF_OPEN, it attempts to acquire a probe slot, returning isProbe=true if acquired.
// If the target is OPEN and cooldown has not expired, it returns allowed=false, isProbe=false.
// If the target is CLOSED, it returns allowed=true, isProbe=false.
func (tb *TargetBreaker) BeforeHealthSample(now time.Time) (bool, bool) {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.evaluateCooldown(now)

	switch tb.state {
	case StateClosed:
		return true, false

	case StateHalfOpen:
		if tb.activeProbes >= tb.config.HalfOpenProbeLimit {
			return false, false
		}
		tb.activeProbes++
		return true, true

	case StateOpen:
		return false, false

	default:
		return true, false
	}
}

// ReleaseProbe releases an active probe slot without recording an outcome.
func (tb *TargetBreaker) ReleaseProbe() {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	if tb.state == StateHalfOpen && tb.activeProbes > 0 {
		tb.activeProbes--
	}
}

// Snapshot returns an observable snapshot of this target breaker.
func (tb *TargetBreaker) Snapshot(now time.Time) TargetSnapshot {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.evaluateCooldown(now)
	tb.pruneOldTimestamps(now)

	progress := 1.0
	if tb.state == StateOpen {
		progress = 0.0
	} else if tb.state == StateHalfOpen {
		progress = float64(tb.successfulProbes) / float64(tb.config.SuccessThreshold) * 0.5
	} else if tb.restorationStep > 0 {
		progress = float64(tb.restorationStep) / float64(tb.config.RestorationSteps)
	}

	return TargetSnapshot{
		ExecutionTargetID:    tb.targetID,
		State:                tb.state,
		OpenedAt:             tb.openedAt,
		HalfOpenedAt:         tb.halfOpenedAt,
		FailureCount:         len(tb.failureTimestamps),
		TimeoutCount:         len(tb.timeoutTimestamps),
		ConsecutiveSuccesses: tb.consecutiveSuccesses,
		ActiveProbes:         tb.activeProbes,
		SuccessfulProbes:     tb.successfulProbes,
		RestorationStep:      tb.restorationStep,
		MaxRestorationSteps:  tb.config.RestorationSteps,
		RestorationProgress:  progress,
		LastEvaluatedAt:      now,
	}
}

func (tb *TargetBreaker) evaluateCooldown(now time.Time) {
	if tb.state == StateOpen && !tb.openedAt.IsZero() {
		if now.Sub(tb.openedAt) >= tb.config.OpenCooldown {
			tb.transitionTo(StateHalfOpen, ReasonCooldownExpired, now)
		}
	}
}

func (tb *TargetBreaker) pruneOldTimestamps(now time.Time) {
	windowStart := now.Add(-tb.config.RollingWindow)
	prune := func(ts []time.Time) []time.Time {
		idx := 0
		for idx < len(ts) && ts[idx].Before(windowStart) {
			idx++
		}
		if idx > 0 {
			return ts[idx:]
		}
		return ts
	}
	tb.failureTimestamps = prune(tb.failureTimestamps)
	tb.timeoutTimestamps = prune(tb.timeoutTimestamps)
}

func (tb *TargetBreaker) transitionTo(newState State, reason string, now time.Time) {
	prevState := tb.state
	tb.state = newState
	if newState == StateOpen {
		tb.openedAt = now
		tb.halfOpenedAt = time.Time{}
		tb.activeProbes = 0
		tb.successfulProbes = 0
	} else if newState == StateHalfOpen {
		tb.halfOpenedAt = now
		tb.activeProbes = 0
		tb.successfulProbes = 0
	} else if newState == StateClosed {
		tb.openedAt = time.Time{}
		tb.halfOpenedAt = time.Time{}
		tb.activeProbes = 0
		tb.successfulProbes = 0
	}

	event := TransitionEvent{
		ExecutionTargetID:    tb.targetID,
		PreviousState:        prevState,
		NewState:             newState,
		Reason:               reason,
		TransitionedAt:       now,
		FailureCount:         len(tb.failureTimestamps),
		TimeoutCount:         len(tb.timeoutTimestamps),
		ConsecutiveSuccesses: tb.consecutiveSuccesses,
		ActiveProbes:         tb.activeProbes,
		SuccessfulProbes:     tb.successfulProbes,
		RestorationStep:      tb.restorationStep,
		CooldownDurationMs:   tb.config.OpenCooldown.Milliseconds(),
		RollingWindowMs:      tb.config.RollingWindow.Milliseconds(),
		EventType:            "CIRCUIT_STATE_TRANSITION",
	}

	slog.Info("circuit breaker state transition",
		"execution_target_id", tb.targetID,
		"previous_state", prevState,
		"new_state", newState,
		"reason", reason,
		"failure_count", event.FailureCount,
		"timeout_count", event.TimeoutCount,
		"active_probes", event.ActiveProbes,
		"successful_probes", event.SuccessfulProbes,
		"restoration_step", event.RestorationStep,
	)

	if tb.listener != nil {
		tb.listener(event)
	}
}

// CircuitBreaker manages circuit breaker instances across multiple executionTargetIDs.
type CircuitBreaker struct {
	mu         sync.RWMutex
	config     Config
	targets    map[string]*TargetBreaker
	events     []TransitionEvent
	repository Repository
}

// NewBreaker creates a new CircuitBreaker registry with the given configuration.
func NewBreaker(config Config, repository ...Repository) (*CircuitBreaker, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	var repo Repository
	if len(repository) > 0 {
		repo = repository[0]
	}
	return &CircuitBreaker{
		config:     config,
		targets:    make(map[string]*TargetBreaker),
		events:     make([]TransitionEvent, 0),
		repository: repo,
	}, nil
}

// Config returns the configuration.
func (cb *CircuitBreaker) Config() Config {
	return cb.config
}

// GetOrCreate returns or initializes a TargetBreaker for the given targetID.
func (cb *CircuitBreaker) GetOrCreate(targetID string) *TargetBreaker {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if tb, ok := cb.targets[targetID]; ok {
		return tb
	}
	tb := newTargetBreaker(targetID, cb.config, cb.onTransition)
	cb.targets[targetID] = tb
	return tb
}

func (cb *CircuitBreaker) onTransition(event TransitionEvent) {
	cb.mu.Lock()
	cb.events = append(cb.events, event)
	repo := cb.repository
	cb.mu.Unlock()

	if repo != nil {
		_ = repo.RecordTransition(context.Background(), event)
	}
}

// GetState returns the current state of the target breaker.
func (cb *CircuitBreaker) GetState(targetID string, now ...time.Time) State {
	currentTime := time.Now()
	if len(now) > 0 && !now[0].IsZero() {
		currentTime = now[0]
	}
	return cb.GetOrCreate(targetID).GetState(currentTime)
}

// Allow evaluates if a request to targetID is eligible.
func (cb *CircuitBreaker) Allow(targetID string, now ...time.Time) (bool, string) {
	currentTime := time.Now()
	if len(now) > 0 && !now[0].IsZero() {
		currentTime = now[0]
	}
	return cb.GetOrCreate(targetID).Allow(currentTime)
}

// RecordSuccess records a success for targetID.
func (cb *CircuitBreaker) RecordSuccess(targetID string, now ...time.Time) {
	currentTime := time.Now()
	if len(now) > 0 && !now[0].IsZero() {
		currentTime = now[0]
	}
	cb.GetOrCreate(targetID).RecordSuccess(currentTime)
}

// RecordFailure records a failure for targetID.
func (cb *CircuitBreaker) RecordFailure(targetID string, reason string, now ...time.Time) {
	currentTime := time.Now()
	if len(now) > 0 && !now[0].IsZero() {
		currentTime = now[0]
	}
	cb.GetOrCreate(targetID).RecordFailure(reason, currentTime)
}

// RecordTimeout records a timeout for targetID.
func (cb *CircuitBreaker) RecordTimeout(targetID string, now ...time.Time) {
	currentTime := time.Now()
	if len(now) > 0 && !now[0].IsZero() {
		currentTime = now[0]
	}
	cb.GetOrCreate(targetID).RecordTimeout(currentTime)
}

// TryAcquireProbe attempts to acquire an active probe slot for targetID if it is in HALF_OPEN.
func (cb *CircuitBreaker) TryAcquireProbe(targetID string, now ...time.Time) bool {
	currentTime := time.Now()
	if len(now) > 0 && !now[0].IsZero() {
		currentTime = now[0]
	}
	return cb.GetOrCreate(targetID).TryAcquireProbe(currentTime)
}

// BeforeHealthSample evaluates whether a health sample can execute for targetID.
// Implements health.ProbeGate.
func (cb *CircuitBreaker) BeforeHealthSample(targetID string, now ...time.Time) (bool, bool) {
	currentTime := time.Now()
	if len(now) > 0 && !now[0].IsZero() {
		currentTime = now[0]
	}
	return cb.GetOrCreate(targetID).BeforeHealthSample(currentTime)
}

// ReleaseProbe releases an active probe slot for targetID without recording an outcome.
func (cb *CircuitBreaker) ReleaseProbe(targetID string) {
	cb.GetOrCreate(targetID).ReleaseProbe()
}

// Repository returns the configured repository, if any.
func (cb *CircuitBreaker) Repository() Repository {
	return cb.repository
}

// RecordHealthSample maps a health monitor sample into the circuit breaker.
func (cb *CircuitBreaker) RecordHealthSample(sample health.HealthSample) {
	switch sample.Outcome {
	case health.OutcomeSuccess:
		cb.RecordSuccess(sample.TargetID, sample.SampledAt)
	case health.OutcomeFailure:
		cb.RecordFailure(sample.TargetID, "HEALTH_PROBE_FAILURE", sample.SampledAt)
	case health.OutcomeTimeout:
		cb.RecordTimeout(sample.TargetID, sample.SampledAt)
	}
}

// Events returns a copy of all transition events recorded in memory.
func (cb *CircuitBreaker) Events() []TransitionEvent {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	res := make([]TransitionEvent, len(cb.events))
	copy(res, cb.events)
	return res
}

// EventsForTarget returns all transition events for a specific target.
func (cb *CircuitBreaker) EventsForTarget(targetID string) []TransitionEvent {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	var res []TransitionEvent
	for _, e := range cb.events {
		if e.ExecutionTargetID == targetID {
			res = append(res, e)
		}
	}
	return res
}

// GetEventsForTarget returns transition events for targetID.
// If a Repository is configured, it queries durable history from PostgreSQL (ordered transitioned_at DESC, id DESC).
// If no repository is configured, it falls back to in-memory history ordered newest first.
func (cb *CircuitBreaker) GetEventsForTarget(ctx context.Context, targetID string, limit ...int) ([]TransitionEvent, error) {
	lim := 50
	if len(limit) > 0 && limit[0] > 0 {
		lim = limit[0]
	}
	if cb.repository != nil {
		return cb.repository.ListRecent(ctx, targetID, lim)
	}

	cb.mu.RLock()
	defer cb.mu.RUnlock()
	var res []TransitionEvent
	for i := len(cb.events) - 1; i >= 0; i-- {
		if cb.events[i].ExecutionTargetID == targetID {
			res = append(res, cb.events[i])
			if len(res) >= lim {
				break
			}
		}
	}
	return res, nil
}

// Snapshot returns a snapshot of targetID's state.
func (cb *CircuitBreaker) Snapshot(targetID string, now ...time.Time) TargetSnapshot {
	currentTime := time.Now()
	if len(now) > 0 && !now[0].IsZero() {
		currentTime = now[0]
	}
	return cb.GetOrCreate(targetID).Snapshot(currentTime)
}

// Snapshots returns snapshots of all known target breakers.
func (cb *CircuitBreaker) Snapshots(now ...time.Time) map[string]TargetSnapshot {
	currentTime := time.Now()
	if len(now) > 0 && !now[0].IsZero() {
		currentTime = now[0]
	}
	cb.mu.RLock()
	targets := make([]*TargetBreaker, 0, len(cb.targets))
	for _, tb := range cb.targets {
		targets = append(targets, tb)
	}
	cb.mu.RUnlock()

	result := make(map[string]TargetSnapshot, len(targets))
	for _, tb := range targets {
		result[tb.targetID] = tb.Snapshot(currentTime)
	}
	return result
}

// EligibilityHook returns a payments.CircuitEligibility hook that queries this circuit breaker.
func (cb *CircuitBreaker) EligibilityHook(nowFn ...func() time.Time) payments.CircuitEligibility {
	return func(ctx context.Context, candidate payments.RouteCandidate) bool {
		now := time.Now()
		if len(nowFn) > 0 && nowFn[0] != nil {
			now = nowFn[0]()
		}
		allowed, _ := cb.Allow(candidate.ExecutionTargetID, now)
		return allowed
	}
}
