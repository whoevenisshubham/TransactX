package health

import (
	"sort"
	"time"
)

func Classify(available bool, latency, timeoutThreshold time.Duration) Outcome {
	if timeoutThreshold > 0 && latency >= timeoutThreshold {
		return OutcomeTimeout
	}
	if available {
		return OutcomeSuccess
	}
	return OutcomeFailure
}

func Snapshot(targetID string, samples []HealthSample, now time.Time, config Config) HealthSnapshot {
	if !config.valid() {
		panic("invalid health configuration")
	}
	windowStart := now.Add(-config.Window)
	filtered := make([]HealthSample, 0, min(len(samples), config.MaxSamples))
	for _, sample := range samples {
		if sample.TargetID == targetID && !sample.SampledAt.Before(windowStart) && !sample.SampledAt.After(now) {
			filtered = append(filtered, sample)
		}
	}
	if len(filtered) > config.MaxSamples {
		sort.SliceStable(filtered, func(left, right int) bool { return filtered[left].SampledAt.After(filtered[right].SampledAt) })
		filtered = filtered[:config.MaxSamples]
	}
	result := HealthSnapshot{TargetID: targetID, WindowStartedAt: windowStart, ComputedAt: now, SampleCount: len(filtered)}
	if len(filtered) < config.MinSamples || len(filtered) == 0 {
		return result
	}
	latencies := make([]time.Duration, 0, len(filtered))
	var available, successful, timeouts int
	for _, sample := range filtered {
		if sample.Available {
			available++
		}
		if sample.Outcome == OutcomeSuccess {
			successful++
		}
		if sample.Outcome == OutcomeTimeout {
			timeouts++
		}
		latencies = append(latencies, sample.Latency)
	}
	sort.Slice(latencies, func(left, right int) bool { return latencies[left] < latencies[right] })
	p95 := latencies[(95*len(latencies)+99)/100-1]
	result.AvailabilityScore = float64(available) / float64(len(filtered))
	result.SuccessScore = float64(successful) / float64(len(filtered))
	result.LatencyPenalty = clamp(float64(p95-config.LatencyLowerBound)/float64(config.LatencyUpperBound-config.LatencyLowerBound), 0, 1)
	result.TimeoutPenalty = float64(timeouts) / float64(len(filtered))
	raw := config.AvailabilityWeight*result.AvailabilityScore + config.SuccessWeight*result.SuccessScore - config.LatencyWeight*result.LatencyPenalty - config.TimeoutWeight*result.TimeoutPenalty
	result.Score = clamp(raw, 0, 1)
	return result
}

func clamp(value, lower, upper float64) float64 {
	if value < lower {
		return lower
	}
	if value > upper {
		return upper
	}
	return value
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}
