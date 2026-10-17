package telemetry

import (
	"time"
)

// Config defines OpenTelemetry tracing, bounded storage, and metrics settings.
type Config struct {
	ServiceName     string        `json:"serviceName"`
	Environment     string        `json:"environment"`
	BuildSHA        string        `json:"buildSha"`
	SampleRatio     float64       `json:"sampleRatio"`
	StorageEnabled  bool          `json:"storageEnabled"`
	QueueCapacity   int           `json:"queueCapacity"`
	BatchTimeout    time.Duration `json:"batchTimeout"`
	MaxBatchSize    int           `json:"maxBatchSize"`
	RetentionHours  int           `json:"retentionHours"`
	MaxStoredSpans  int           `json:"maxStoredSpans"`
	OTLPEndpoint    string        `json:"otlpEndpoint"`
	ShutdownTimeout time.Duration `json:"shutdownTimeout"`
}

// DefaultConfig returns safe, bounded production-ready telemetry defaults.
func DefaultConfig(serviceName string) Config {
	return Config{
		ServiceName:     serviceName,
		Environment:     "development",
		BuildSHA:        "dev",
		SampleRatio:     0.2, // 20% head sampling by default
		StorageEnabled:  true,
		QueueCapacity:   1000, // strictly bounded queue
		BatchTimeout:    250 * time.Millisecond,
		MaxBatchSize:    100,
		RetentionHours:  24,    // 24-hour default retention
		MaxStoredSpans:  20000, // maximum stored spans cap
		ShutdownTimeout: 5 * time.Second,
	}
}

// TestConfig returns configuration pinned to 100% sampling for deterministic test assertions.
func TestConfig(serviceName string) Config {
	cfg := DefaultConfig(serviceName)
	cfg.SampleRatio = 1.0
	cfg.BatchTimeout = 10 * time.Millisecond
	cfg.ShutdownTimeout = 2 * time.Second
	return cfg
}
