package circuit

import (
	"time"
)

// State represents the explicit operational state of a circuit breaker.
type State string

const (
	StateClosed   State = "CLOSED"
	StateOpen     State = "OPEN"
	StateHalfOpen State = "HALF_OPEN"
)

func (s State) String() string {
	return string(s)
}

func (s State) Valid() bool {
	return s == StateClosed || s == StateOpen || s == StateHalfOpen
}

// Deterministic transition reason codes.
const (
	ReasonThresholdReached        = "FAILURE_THRESHOLD_REACHED"
	ReasonTimeoutThresholdReached = "TIMEOUT_THRESHOLD_REACHED"
	ReasonCooldownExpired         = "COOLDOWN_EXPIRED"
	ReasonProbeFailed             = "PROBE_FAILED"
	ReasonProbesSucceeded         = "SUCCESSFUL_PROBES_SATISFIED"
	ReasonManual                  = "MANUAL_TRANSITION"
)

// TransitionEvent represents an immutable record of a circuit state transition.
type TransitionEvent struct {
	ID                   int64          `json:"id,omitempty"`
	ExecutionTargetID    string         `json:"executionTargetId"`
	PreviousState        State          `json:"previousState"`
	NewState             State          `json:"newState"`
	Reason               string         `json:"reason"`
	TransitionedAt       time.Time      `json:"transitionedAt"`
	FailureCount         int            `json:"failureCount"`
	TimeoutCount         int            `json:"timeoutCount"`
	ConsecutiveSuccesses int            `json:"consecutiveSuccesses"`
	ActiveProbes         int            `json:"activeProbes"`
	SuccessfulProbes     int            `json:"successfulProbes"`
	RestorationStep      int            `json:"restorationStep"`
	CooldownDurationMs   int64          `json:"cooldownDurationMs"`
	RollingWindowMs      int64          `json:"rollingWindowMs"`
	Details              map[string]any `json:"details,omitempty"`
	EventType            string         `json:"eventType"` // "CIRCUIT_STATE_TRANSITION"
}

// TargetSnapshot provides an observable read model of an execution target's circuit state.
type TargetSnapshot struct {
	ExecutionTargetID    string    `json:"executionTargetId"`
	State                State     `json:"state"`
	OpenedAt             time.Time `json:"openedAt,omitempty"`
	HalfOpenedAt         time.Time `json:"halfOpenedAt,omitempty"`
	FailureCount         int       `json:"failureCount"`
	TimeoutCount         int       `json:"timeoutCount"`
	ConsecutiveSuccesses int       `json:"consecutiveSuccesses"`
	ActiveProbes         int       `json:"activeProbes"`
	SuccessfulProbes     int       `json:"successfulProbes"`
	RestorationStep      int       `json:"restorationStep"`
	MaxRestorationSteps  int       `json:"maxRestorationSteps"`
	RestorationProgress  float64   `json:"restorationProgress"`
	LastEvaluatedAt      time.Time `json:"lastEvaluatedAt"`
}
