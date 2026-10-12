package recovery

import (
	"time"

	"github.com/google/uuid"
)

type JobStatus string

const (
	JobStatusScheduled      JobStatus = "scheduled"
	JobStatusRunning        JobStatus = "running"
	JobStatusDeferred       JobStatus = "deferred"
	JobStatusNeedsAttention JobStatus = "needs_attention"
	JobStatusResolved       JobStatus = "resolved"
)

const (
	ReasonCompleted           = "COMPLETED"
	ReasonFailed              = "FAILED"
	ReasonAttention           = "NEEDS_ATTENTION"
	ReasonAttemptsExhausted   = "ATTEMPTS_EXHAUSTED"
	ReasonInterrupted         = "INTERRUPTED"
	ReasonLeaseExpired        = "LEASE_EXPIRED"
	ReasonTargetNotConfigured = "TARGET_NOT_CONFIGURED"
	ReasonTransientError      = "TRANSIENT_ERROR"
	ReasonNoOp                = "NO_OP"
)

const (
	DefaultMaxAttempts       = 20
	DefaultLeaseDuration     = 60 * time.Second
	DefaultHeartbeatInterval = 15 * time.Second
	DefaultAttemptTimeout    = 20 * time.Second
	DefaultStaleAge          = 30 * time.Second
	DefaultScanInterval      = 10 * time.Second
	DefaultBackoffBase       = 2 * time.Second
	DefaultBackoffCap        = 5 * time.Minute
)

// Job represents a recovery queue item for a payment.
type Job struct {
	PaymentID      uuid.UUID  `json:"payment_id"`
	Status         JobStatus  `json:"status"`
	DueAt          time.Time  `json:"due_at"`
	LeaseOwner     *string    `json:"lease_owner,omitempty"`
	LeaseUntil     *time.Time `json:"lease_until,omitempty"`
	LeaseEpoch     int64      `json:"lease_epoch"`
	AttemptCount   int        `json:"attempt_count"`
	LastReasonCode *string    `json:"last_reason_code,omitempty"`
	LastAttemptID  *uuid.UUID `json:"last_attempt_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// Attempt represents an individual execution attempt under a lease.
type Attempt struct {
	ID             uuid.UUID   `json:"id"`
	PaymentID      uuid.UUID   `json:"payment_id"`
	LeaseEpoch     int64       `json:"lease_epoch"`
	WorkerIdentity string      `json:"worker_identity"`
	StartedAt      time.Time   `json:"started_at"`
	FinishedAt     *time.Time  `json:"finished_at,omitempty"`
	StateBefore    string      `json:"state_before"`
	StateAfter     *string     `json:"state_after,omitempty"`
	Result         *string     `json:"result,omitempty"`
	ReasonCode     *string     `json:"reason_code,omitempty"`
	OperationIDs   []uuid.UUID `json:"operation_ids,omitempty"`
	TraceID        *string     `json:"trace_id,omitempty"`
	CreatedAt      time.Time   `json:"created_at"`
}

// WorkerHeartbeat records operational telemetry about an active worker process.
type WorkerHeartbeat struct {
	WorkerID    uuid.UUID `json:"worker_id"`
	WorkerLabel string    `json:"worker_label"`
	StartedAt   time.Time `json:"started_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
	JobFamilies []string  `json:"job_families"`
	BuildSHA    string    `json:"build_sha"`
}

// CompleteAttemptRequest carries the outcome of an execution attempt.
type CompleteAttemptRequest struct {
	AttemptID    uuid.UUID
	PaymentID    uuid.UUID
	LeaseEpoch   int64
	WorkerID     string
	NewStatus    JobStatus
	DueAt        time.Time
	StateAfter   string
	Result       string
	ReasonCode   string
	OperationIDs []uuid.UUID
	TraceID      string
}
