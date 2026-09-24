package reconciliation

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Run status values. RUNNING means execution is in progress. COMPLETED means
// the run finished normally; it may have zero or more discrepancies.
// FAILED is reserved for operational execution failures, not financial
// mismatches.
const (
	RunStatusRunning   = "RUNNING"
	RunStatusCompleted = "COMPLETED"
	RunStatusFailed    = "FAILED"
)

// Mismatch category constants used in discrepancy evidence.
const (
	MismatchBucketRoot           = "BUCKET_ROOT_MISMATCH"
	MismatchCanonicalRoot        = "CANONICAL_ROOT_MISMATCH"
	MismatchParticipantUnavail   = "PARTICIPANT_UNAVAILABLE"
	MismatchScopeMismatch        = "SCOPE_MISMATCH"
	MismatchVersionIncompatible  = "VERSION_INCOMPATIBLE"
)

// ErrRunNotFound is returned when a requested reconciliation run does not exist.
var ErrRunNotFound = errors.New("reconciliation run not found")

// ErrInvalidParticipant is returned when the requested participant ID is not
// among the configured authoritative participant identities.
var ErrInvalidParticipant = errors.New("invalid or unknown reconciliation participant")

// ErrInvalidRunScope is returned for malformed scope values in the API request.
var ErrInvalidRunScope = errors.New("invalid reconciliation run scope")

// Run is the durable record of one reconciliation execution.
// A Run containing discrepancies has status COMPLETED; status FAILED
// means an operational execution failure prevented a meaningful comparison.
type Run struct {
	ID               uuid.UUID  `json:"id"`
	ParticipantID    string     `json:"participantId"`
	ScopeFrom        time.Time  `json:"scopeFrom"`
	ScopeTo          time.Time  `json:"scopeTo"`
	Status           string     `json:"status"`
	CanonicalRoot    []byte     `json:"canonicalRoot,omitempty"`
	ParticipantRoot  []byte     `json:"participantRoot,omitempty"`
	CanonicalVersion string     `json:"canonicalVersion,omitempty"`
	AlgorithmVersion string     `json:"algorithmVersion,omitempty"`
	RecordCount      int64      `json:"recordCount"`
	DiscrepancyCount int64      `json:"discrepancyCount"`
	ErrorMessage     string     `json:"errorMessage,omitempty"`
	StartedAt        time.Time  `json:"startedAt"`
	CompletedAt      *time.Time `json:"completedAt,omitempty"`
}

// Discrepancy is durable evidence of one mismatching region within a
// reconciliation run. Multiple discrepancies are preserved; they are never
// collapsed into the first mismatch only.
type Discrepancy struct {
	ID               uuid.UUID         `json:"id"`
	RunID            uuid.UUID         `json:"runId"`
	ParticipantID    string            `json:"participantId"`
	BucketKey        string            `json:"bucketKey"`
	BucketPartition  string            `json:"bucketPartition"`
	BucketStart      time.Time         `json:"bucketStart"`
	BucketWidthNs    int64             `json:"bucketWidthNs"`
	ExpectedRoot     []byte            `json:"expectedRoot,omitempty"`
	ObservedRoot     []byte            `json:"observedRoot,omitempty"`
	MismatchCategory string            `json:"mismatchCategory"`
	Resolved         bool              `json:"resolved"`
	Evidence         map[string]string `json:"evidence,omitempty"`
	DetectedAt       time.Time         `json:"detectedAt"`
}

// RunListPage carries a bounded page of reconciliation runs.
type RunListPage struct {
	Items []Run  `json:"items"`
	Total int    `json:"total"`
	Limit int    `json:"limit"`
	// NextOffset is set when there are more items beyond this page.
	NextOffset *int `json:"nextOffset,omitempty"`
}

// DiscrepancyListPage carries a bounded page of discrepancy evidence.
type DiscrepancyListPage struct {
	Items []Discrepancy `json:"items"`
	Total int           `json:"total"`
	Limit int           `json:"limit"`
	NextOffset *int     `json:"nextOffset,omitempty"`
}
