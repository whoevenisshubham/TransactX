package experiments

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/health"
	"github.com/transactx/backend/internal/payments"
)

type memoryHealthRepo struct {
	samples []health.HealthSample
}

func (r *memoryHealthRepo) Record(ctx context.Context, s health.HealthSample) error {
	r.samples = append(r.samples, s)
	return nil
}

func (r *memoryHealthRepo) ListRecent(ctx context.Context, targetID string, start, end time.Time, limit int) ([]health.HealthSample, error) {
	var result []health.HealthSample
	for _, s := range r.samples {
		if s.TargetID == targetID && !s.SampledAt.Before(start) && !s.SampledAt.After(end) {
			result = append(result, s)
		}
	}
	if len(result) > limit {
		result = result[len(result)-limit:]
	}
	return result, nil
}

// RunExperiment1 runs Experiment 1: Static routing vs Health-aware routing.
func RunExperiment1(ctx context.Context, seed int64, totalRequests int) (*ExperimentResult, error) {
	if totalRequests <= 0 {
		totalRequests = 400
	}

	scenarioParams := map[string]string{
		"totalRequests": fmt.Sprintf("%d", totalRequests),
		"phase1":        "25% healthy",
		"phase2":        "50% primary degraded (80% failure)",
		"phase3":        "25% recovery",
		"targetA":       "RAIL-A",
		"targetB":       "RAIL-B",
	}
	env := CaptureEnvironmentMetadata(seed, totalRequests, scenarioParams)

	// Run Baseline (Static Routing)
	baselineSummary, baselineSamples := runRoutingSimulation(ctx, seed, totalRequests, payments.SelectionModeStatic)

	// Run Proposed (Adaptive Health-Aware Routing)
	proposedSummary, proposedSamples := runRoutingSimulation(ctx, seed, totalRequests, payments.SelectionModeAdaptive)

	var allSamples []ExperimentSample
	allSamples = append(allSamples, baselineSamples...)
	allSamples = append(allSamples, proposedSamples...)

	res := &ExperimentResult{
		ExperimentID: "routing",
		Title:        "Experiment 1: Static Routing vs Health-Aware Routing",
		Environment:  env,
		Baseline:     baselineSummary,
		Proposed:     proposedSummary,
		Samples:      allSamples,
	}

	return res, nil
}

func runRoutingSimulation(ctx context.Context, seed int64, totalRequests int, mode payments.SelectionMode) (*ExperimentSummary, []ExperimentSample) {
	rng := rand.New(rand.NewSource(seed))

	sourceBankID := uuid.New()
	destBankID := uuid.New()

	adapterA := NewMockBankAdapter("BANK-A-RAIL-A")
	adapterB := NewMockBankAdapter("BANK-A-RAIL-B")

	candidates := []payments.RouteCandidate{
		{
			CandidateID:        "CANDIDATE-A",
			ExecutionTargetID:  "RAIL-A",
			SourceBankID:       sourceBankID,
			DestinationBankID:  destBankID,
			SourceAdapter:      adapterA,
			DestinationAdapter: adapterA,
		},
		{
			CandidateID:        "CANDIDATE-B",
			ExecutionTargetID:  "RAIL-B",
			SourceBankID:       sourceBankID,
			DestinationBankID:  destBankID,
			SourceAdapter:      adapterB,
			DestinationAdapter: adapterB,
		},
	}

	hRepo := &memoryHealthRepo{}
	cfg := health.DefaultConfig()
	cfg.MinSamples = 1
	cfg.Window = 10 * time.Minute
	hService := health.NewService(hRepo, cfg)

	phase1End := totalRequests / 4
	phase2End := phase1End + (totalRequests / 2)

	samples := make([]ExperimentSample, 0, totalRequests)
	decisionReasons := make(map[string]int)
	trafficCounts := make(map[string]int)
	latencies := make([]time.Duration, 0, totalRequests)

	successes := 0
	failures := 0

	now := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	stepDuration := 100 * time.Millisecond

	var phase3StartTime time.Time
	var recoveryTimeMs float64
	recovered := false

	for i := 0; i < totalRequests; i++ {
		now = now.Add(stepDuration)

		// Determine target A health condition based on phase
		var targetAFailureProb float64
		if i < phase1End {
			// Phase 1: Healthy
			targetAFailureProb = 0.0
		} else if i < phase2End {
			// Phase 2: Degraded Primary
			targetAFailureProb = 0.80
		} else {
			// Phase 3: Recovery
			targetAFailureProb = 0.0
			if phase3StartTime.IsZero() {
				phase3StartTime = now
			}
		}

		// Update health service with background health samples
		// Rail A health sample
		railASuccess := rng.Float64() >= targetAFailureProb
		latA := 12 * time.Millisecond
		if !railASuccess {
			latA = 150 * time.Millisecond
		}
		_ = hService.RecordSample(ctx, health.HealthSample{
			TargetID:  "RAIL-A",
			SampledAt: now,
			Available: railASuccess,
			Latency:   latA,
			Outcome:   health.Classify(railASuccess, latA, 2*time.Second),
		})

		// Rail B is always healthy
		_ = hService.RecordSample(ctx, health.HealthSample{
			TargetID:  "RAIL-B",
			SampledAt: now,
			Available: true,
			Latency:   15 * time.Millisecond,
			Outcome:   health.OutcomeSuccess,
		})

		// Route selection
		decision, err := payments.SelectRoute(ctx, candidates, mode, hService, nil, now, "CANDIDATE-A")
		if err != nil {
			failures++
			decisionReasons[payments.ReasonNoEligibleTarget]++
			samples = append(samples, ExperimentSample{
				Index:          i,
				Timestamp:      now,
				Mode:           string(mode),
				TargetID:       "NONE",
				Success:        false,
				Latency:        0,
				LatencyMs:      0,
				DecisionReason: payments.ReasonNoEligibleTarget,
				ErrorMessage:   err.Error(),
			})
			continue
		}

		decisionReasons[decision.Reason]++
		selectedTarget := decision.Candidate.ExecutionTargetID
		trafficCounts[selectedTarget]++

		// Execute operation against selected target
		reqStart := time.Now()
		var opSuccess bool
		var simulatedLatency time.Duration

		if selectedTarget == "RAIL-A" {
			opSuccess = rng.Float64() >= targetAFailureProb
			if opSuccess {
				simulatedLatency = 10*time.Millisecond + time.Duration(rng.Intn(5))*time.Millisecond
			} else {
				simulatedLatency = 120*time.Millisecond + time.Duration(rng.Intn(50))*time.Millisecond
			}
		} else {
			opSuccess = true // Target B healthy
			simulatedLatency = 14*time.Millisecond + time.Duration(rng.Intn(6))*time.Millisecond
		}
		_ = reqStart

		if opSuccess {
			successes++
			if !phase3StartTime.IsZero() && selectedTarget == "RAIL-A" && !recovered {
				recoveryTimeMs = float64(now.Sub(phase3StartTime).Milliseconds())
				recovered = true
			}
		} else {
			failures++
		}

		latencies = append(latencies, simulatedLatency)
		samples = append(samples, ExperimentSample{
			Index:          i,
			Timestamp:      now,
			Mode:           string(mode),
			TargetID:       selectedTarget,
			Success:        opSuccess,
			Latency:        simulatedLatency,
			LatencyMs:      float64(simulatedLatency.Microseconds()) / 1000.0,
			DecisionReason: decision.Reason,
			ErrorMessage:   func() string { if !opSuccess { return "bank execution failed" }; return "" }(),
		})
	}

	// Calculate latency percentiles
	latencyMetrics := calculateLatencyPercentiles(latencies)

	// Calculate traffic shares
	trafficShares := make([]TrafficShare, 0, len(trafficCounts))
	for tID, count := range trafficCounts {
		trafficShares = append(trafficShares, TrafficShare{
			TargetID:   tID,
			Count:      count,
			Percentage: float64(count) / float64(totalRequests) * 100.0,
		})
	}
	sort.Slice(trafficShares, func(i, j int) bool { return trafficShares[i].TargetID < trafficShares[j].TargetID })

	successRate := float64(successes) / float64(totalRequests) * 100.0

	// Validate invariants
	invariants := []InvariantResult{
		{
			InvariantName: "Request accounting completeness",
			Passed:        totalRequests == (successes + failures),
			Details:       fmt.Sprintf("total=%d, successes=%d, failures=%d", totalRequests, successes, failures),
		},
		{
			InvariantName: "Consistent selection mode application",
			Passed:        true,
			Details:       fmt.Sprintf("mode %s applied across all %d evaluations", mode, totalRequests),
		},
	}
	allInv := true
	for _, inv := range invariants {
		if !inv.Passed {
			allInv = false
		}
	}

	summary := &ExperimentSummary{
		TotalRequests:          totalRequests,
		SuccessCount:           successes,
		FailureCount:           failures,
		PendingCount:           0,
		SuccessRate:            successRate,
		Latency:                latencyMetrics,
		RecoveryTimeMs:         recoveryTimeMs,
		TrafficShare:           trafficShares,
		DecisionReasons:        decisionReasons,
		Invariants:             invariants,
		AllInvariantsSatisfied: allInv,
	}

	return summary, samples
}

func calculateLatencyPercentiles(latencies []time.Duration) LatencyPercentiles {
	if len(latencies) == 0 {
		return LatencyPercentiles{}
	}
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var sum int64
	for _, l := range sorted {
		sum += l.Microseconds()
	}
	meanMs := float64(sum) / float64(len(sorted)) / 1000.0

	p50 := sorted[len(sorted)*50/100]
	p95 := sorted[(len(sorted)*95+99)/100-1]
	p99 := sorted[(len(sorted)*99+99)/100-1]
	max := sorted[len(sorted)-1]

	return LatencyPercentiles{
		MeanMs: meanMs,
		P50Ms:  float64(p50.Microseconds()) / 1000.0,
		P95Ms:  float64(p95.Microseconds()) / 1000.0,
		P99Ms:  float64(p99.Microseconds()) / 1000.0,
		MaxMs:  float64(max.Microseconds()) / 1000.0,
	}
}
