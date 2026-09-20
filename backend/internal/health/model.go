package health

import "time"

type Outcome string

const (
	OutcomeSuccess Outcome = "SUCCESS"
	OutcomeFailure Outcome = "FAILURE"
	OutcomeTimeout Outcome = "TIMEOUT"
)

type HealthSample struct {
	TargetID      string
	SampledAt     time.Time
	Available     bool
	Latency       time.Duration
	Outcome       Outcome
	CorrelationID string
}

type HealthSnapshot struct {
	TargetID          string    `json:"targetId"`
	Score             float64   `json:"score"`
	AvailabilityScore float64   `json:"availabilityScore"`
	SuccessScore      float64   `json:"successScore"`
	LatencyPenalty    float64   `json:"latencyPenalty"`
	TimeoutPenalty    float64   `json:"timeoutPenalty"`
	SampleCount       int       `json:"sampleCount"`
	WindowStartedAt   time.Time `json:"windowStartedAt"`
	ComputedAt        time.Time `json:"computedAt"`
}

type Config struct {
	Window             time.Duration
	MaxSamples         int
	MinSamples         int
	AvailabilityWeight float64
	SuccessWeight      float64
	LatencyWeight      float64
	TimeoutWeight      float64
	LatencyLowerBound  time.Duration
	LatencyUpperBound  time.Duration
	TimeoutThreshold   time.Duration
}

func DefaultConfig() Config {
	return Config{Window: 15 * time.Minute, MaxSamples: 500, MinSamples: 1, AvailabilityWeight: 0.35, SuccessWeight: 0.35, LatencyWeight: 0.20, TimeoutWeight: 0.10, LatencyLowerBound: 10 * time.Millisecond, LatencyUpperBound: time.Second, TimeoutThreshold: 2 * time.Second}
}

func (config Config) valid() bool {
	return config.Window > 0 && config.MaxSamples > 0 && config.MinSamples > 0 && config.MinSamples <= config.MaxSamples && config.LatencyLowerBound >= 0 && config.LatencyUpperBound > config.LatencyLowerBound && config.TimeoutThreshold > 0 && config.AvailabilityWeight >= 0 && config.SuccessWeight >= 0 && config.LatencyWeight >= 0 && config.TimeoutWeight >= 0
}
