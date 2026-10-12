package analytics

import (
	"errors"
	"time"
)

// ErrInvalidCursor indicates a malformed or undecodable pagination cursor.
var ErrInvalidCursor = errors.New("invalid cursor")

// FeatureSchemaVersionV1 is the frozen feature vector schema identity.
const FeatureSchemaVersionV1 = "tx.features.v1"

// MinimumSampleCount is the required sample threshold to produce confident model scoring.
const MinimumSampleCount = 20

// WindowDuration is the bounded size of each completed operational aggregation window.
const WindowDuration = 5 * time.Minute

// CursorNameFiveMinuteAggregator is the cursor key in operational_aggregate_cursors.
const CursorNameFiveMinuteAggregator = "five_minute_window_aggregator"

// FeatureVector defines the ordered operational features for tx.features.v1.
// All fields must be serializable to JSON; missing or unsupported measurements are null.
type FeatureVector struct {
	TimeoutRate                *float64 `json:"timeoutRate"`
	FailureRate                *float64 `json:"failureRate"`
	P95LatencyMs               *float64 `json:"p95LatencyMs"`
	OldestRecoveryAgeMs        *float64 `json:"oldestRecoveryAgeMs"`
	RecoveryAttemptsPerPayment *float64 `json:"recoveryAttemptsPerPayment"`
	HealthAvailabilityRate     *float64 `json:"healthAvailabilityRate"`
}

// Warning represents a structured diagnostic alert or baseline deviation.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ModelStatusInfo captures the anomaly model scoring status for an operational window.
type ModelStatusInfo struct {
	Version   *string  `json:"version"`
	Status    string   `json:"status"` // ready, insufficient_data, stale, unavailable, out_of_distribution
	Score     *float64 `json:"score"`
	Threshold *float64 `json:"threshold"`
}

// AnalyticsWindow represents a completed durable aggregate window conforming to v2.schema.json.
type AnalyticsWindow struct {
	ID                   string          `json:"id"`
	EntityType           string          `json:"entityType"` // "bank" | "execution_target"
	EntityID             string          `json:"entityId"`
	From                 time.Time       `json:"from"`
	To                   time.Time       `json:"to"`
	SampleCount          int             `json:"sampleCount"`
	FeatureSchemaVersion string          `json:"featureSchemaVersion"`
	Features             FeatureVector   `json:"features"`
	BaselineAlert        *Warning        `json:"baselineAlert"`
	Model                ModelStatusInfo `json:"model"`
	Advisory             bool            `json:"advisory"`
}

// AnalyticsPage represents a paginated collection of analytics windows.
type AnalyticsPage struct {
	Items      []AnalyticsWindow `json:"items"`
	NextCursor *string           `json:"nextCursor"`
	AsOf       time.Time         `json:"asOf"`
}

// Source captures observation provenance and status for metrics.
type Source struct {
	Name       string     `json:"name"`
	Status     string     `json:"status"` // available, unavailable, not_applicable, not_collected
	ObservedAt *time.Time `json:"observedAt"`
	ErrorCode  *string    `json:"errorCode"`
}

// PaymentMetrics captures payment counts as decimal strings.
type PaymentMetrics struct {
	Attempted string `json:"attempted"`
	Completed string `json:"completed"`
	Failed    string `json:"failed"`
	Pending   string `json:"pending"`
}

// RecoveryMetrics captures durable recovery queue depth and oldest age.
type RecoveryMetrics struct {
	Due            string `json:"due"`
	Running        string `json:"running"`
	NeedsAttention string `json:"needsAttention"`
	OldestDueAgeMs *int64 `json:"oldestDueAgeMs"`
}

// OutboxMetrics captures outbox delivery depth and oldest age.
type OutboxMetrics struct {
	Pending            string `json:"pending"`
	Quarantined        string `json:"quarantined"`
	OldestPendingAgeMs *int64 `json:"oldestPendingAgeMs"`
}

// RuntimeMetrics captures process observation facts.
type RuntimeMetrics struct {
	WorkerLastHeartbeatAt *time.Time `json:"workerLastHeartbeatAt"`
	PoolWaitMs            *float64   `json:"poolWaitMs"`
}

// MetricsSummary represents operational summary metrics conforming to v2.schema.json.
type MetricsSummary struct {
	WindowFrom time.Time       `json:"windowFrom"`
	WindowTo   time.Time       `json:"windowTo"`
	AsOf       time.Time       `json:"asOf"`
	Payments   PaymentMetrics  `json:"payments"`
	Recovery   RecoveryMetrics `json:"recovery"`
	Outbox     OutboxMetrics   `json:"outbox"`
	Runtime    RuntimeMetrics  `json:"runtime"`
	Sources    []Source        `json:"sources"`
}

// WindowFilter defines query parameters for paginating operational aggregate windows.
type WindowFilter struct {
	EntityType string
	EntityID   string
	From       *time.Time
	To         *time.Time
	Cursor     string
	Limit      int
}

// WindowCursor is the decoded structure for cursor pagination.
type WindowCursor struct {
	WindowFrom time.Time `json:"windowFrom"`
	ID         string    `json:"id"`
}
