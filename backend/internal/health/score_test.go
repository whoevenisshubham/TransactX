package health

import (
	"testing"
	"time"
)

func sample(target string, at time.Time, available bool, latency time.Duration, outcome Outcome) HealthSample {
	return HealthSample{TargetID: target, SampledAt: at, Available: available, Latency: latency, Outcome: outcome}
}

func TestSnapshotEmptyWindow(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	result := Snapshot("BANK-A", nil, now, DefaultConfig())
	if result.SampleCount != 0 || result.Score != 0 || result.AvailabilityScore != 0 {
		t.Fatalf("snapshot = %+v", result)
	}
}

func TestClassifyUsesConfiguredTimeoutThreshold(t *testing.T) {
	if Classify(true, 100*time.Millisecond, time.Second) != OutcomeSuccess {
		t.Fatal("fast available probe was not successful")
	}
	if Classify(false, 100*time.Millisecond, time.Second) != OutcomeFailure {
		t.Fatal("unavailable probe was not a failure")
	}
	if Classify(true, time.Second, time.Second) != OutcomeTimeout {
		t.Fatal("threshold probe was not a timeout")
	}
}

func TestSnapshotAllSuccessAndFormula(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	samples := []HealthSample{sample("BANK-A", now.Add(-time.Minute), true, 10*time.Millisecond, OutcomeSuccess), sample("BANK-A", now.Add(-2*time.Minute), true, 505*time.Millisecond, OutcomeSuccess), sample("BANK-A", now.Add(-3*time.Minute), true, time.Second, OutcomeSuccess)}
	result := Snapshot("BANK-A", samples, now, DefaultConfig())
	wantLatency := 1.0
	want := 0.35 + 0.35 - 0.20*wantLatency
	if result.AvailabilityScore != 1 || result.SuccessScore != 1 || result.TimeoutPenalty != 0 || result.LatencyPenalty != wantLatency || result.Score != want {
		t.Fatalf("snapshot = %+v, want latency=%v score=%v", result, wantLatency, want)
	}
}

func TestSnapshotOutcomesTargetsAndCutoff(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	samples := []HealthSample{sample("A", now.Add(-time.Minute), true, 20*time.Millisecond, OutcomeSuccess), sample("A", now.Add(-2*time.Minute), false, 20*time.Millisecond, OutcomeFailure), sample("A", now.Add(-3*time.Minute), false, 20*time.Millisecond, OutcomeTimeout), sample("B", now.Add(-time.Minute), true, 20*time.Millisecond, OutcomeSuccess), sample("A", now.Add(-16*time.Minute), true, 20*time.Millisecond, OutcomeSuccess)}
	result := Snapshot("A", samples, now, DefaultConfig())
	if result.SampleCount != 3 || result.AvailabilityScore != 1.0/3.0 || result.SuccessScore != 1.0/3.0 || result.TimeoutPenalty != 1.0/3.0 {
		t.Fatalf("snapshot = %+v", result)
	}
	if Snapshot("B", samples, now, DefaultConfig()).SampleCount != 1 {
		t.Fatal("target samples contaminated")
	}
}

func TestLatencyBoundariesAndClamping(t *testing.T) {
	config := DefaultConfig()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, testCase := range []struct {
		latency time.Duration
		want    float64
	}{{0, 0}, {10 * time.Millisecond, 0}, {505 * time.Millisecond, .5}, {time.Second, 1}, {2 * time.Second, 1}} {
		result := Snapshot("BANK-A", []HealthSample{sample("BANK-A", now, true, testCase.latency, OutcomeSuccess)}, now, config)
		if result.LatencyPenalty != testCase.want {
			t.Fatalf("latency %v penalty=%v want %v", testCase.latency, result.LatencyPenalty, testCase.want)
		}
	}
	negative := Config{Window: time.Minute, MaxSamples: 1, MinSamples: 1, AvailabilityWeight: 0, SuccessWeight: 0, LatencyWeight: 2, TimeoutWeight: 0, LatencyLowerBound: 0, LatencyUpperBound: time.Millisecond, TimeoutThreshold: time.Second}
	if Snapshot("BANK-A", []HealthSample{sample("BANK-A", now, true, time.Second, OutcomeSuccess)}, now, negative).Score != 0 {
		t.Fatal("score did not clamp low")
	}
	positive := Config{Window: time.Minute, MaxSamples: 1, MinSamples: 1, AvailabilityWeight: 2, SuccessWeight: 2, LatencyWeight: 0, TimeoutWeight: 0, LatencyLowerBound: 0, LatencyUpperBound: time.Second, TimeoutThreshold: time.Second}
	if Snapshot("BANK-A", []HealthSample{sample("BANK-A", now, true, 0, OutcomeSuccess)}, now, positive).Score != 1 {
		t.Fatal("score did not clamp high")
	}
}

func TestSnapshotDeterministic(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	samples := []HealthSample{sample("BANK-A", now.Add(-time.Minute), true, 30*time.Millisecond, OutcomeSuccess), sample("BANK-A", now.Add(-2*time.Minute), false, 40*time.Millisecond, OutcomeFailure)}
	if Snapshot("BANK-A", samples, now, DefaultConfig()) != Snapshot("BANK-A", samples, now, DefaultConfig()) {
		t.Fatal("same inputs produced different snapshots")
	}
}
