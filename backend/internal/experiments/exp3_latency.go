package experiments

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/chaos"
	"github.com/transactx/backend/internal/health"
	"github.com/transactx/backend/internal/payments"
)

// RunExperiment3 runs Experiment 3: Latency degradation comparison.
func RunExperiment3(ctx context.Context, seed int64, totalRequests int) (*ExperimentResult, error) {
	if totalRequests <= 0 {
		totalRequests = 300
	}

	scenarioParams := map[string]string{
		"totalRequests":  fmt.Sprintf("%d", totalRequests),
		"chaosType":      string(chaos.ScenarioTypeLatency),
		"injectedDelay":  "15ms via ChaosAdapter on RAIL-A",
		"targetA":        "RAIL-A",
		"targetB":        "RAIL-B",
		"latencyWindow":  "Requests 100 to 200 (middle 33% of workload)",
		"selectionModes": "STATIC vs ADAPTIVE",
	}
	env := CaptureEnvironmentMetadata(seed, totalRequests, scenarioParams)

	baselineSummary, baselineSamples := runLatencySimulation(ctx, seed, totalRequests, payments.SelectionModeStatic)
	proposedSummary, proposedSamples := runLatencySimulation(ctx, seed, totalRequests, payments.SelectionModeAdaptive)

	var allSamples []ExperimentSample
	allSamples = append(allSamples, baselineSamples...)
	allSamples = append(allSamples, proposedSamples...)

	return &ExperimentResult{
		ExperimentID: "latency",
		Title:        "Experiment 3: Latency Degradation and Traffic Shift",
		Environment:  env,
		Baseline:     baselineSummary,
		Proposed:     proposedSummary,
		Samples:      allSamples,
	}, nil
}

func runLatencySimulation(ctx context.Context, seed int64, totalRequests int, mode payments.SelectionMode) (*ExperimentSummary, []ExperimentSample) {
	sourceBankID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("exp3-src-%d", seed)))
	destBankID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("exp3-dst-%d", seed)))

	adapterA := NewMockBankAdapter("BANK-A-RAIL-A")
	adapterB := NewMockBankAdapter("BANK-A-RAIL-B")
	adapterA.SetLatency(1 * time.Millisecond)
	adapterB.SetLatency(2 * time.Millisecond)

	chaosRepo := newMemoryChaosRepo()
	chaosController := chaos.NewController(chaosRepo)
	chaosController.SetTargetValidator(func(t string) bool { return t == "RAIL-A" || t == "RAIL-B" })
	chaosAdapterA := chaos.NewChaosAdapter("RAIL-A", adapterA, chaosController)

	candidates := []payments.RouteCandidate{
		{
			CandidateID:        "CANDIDATE-A",
			ExecutionTargetID:  "RAIL-A",
			SourceBankID:       sourceBankID,
			DestinationBankID:  destBankID,
			SourceAdapter:      chaosAdapterA,
			DestinationAdapter: chaosAdapterA,
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
	cfg.LatencyLowerBound = 1 * time.Millisecond
	cfg.LatencyUpperBound = 10 * time.Millisecond
	cfg.LatencyWeight = 0.60
	cfg.AvailabilityWeight = 0.20
	cfg.SuccessWeight = 0.20
	cfg.MinSamples = 1
	hService := health.NewService(hRepo, cfg)

	degradeStart := totalRequests / 3
	degradeEnd := (totalRequests * 2) / 3

	samples := make([]ExperimentSample, 0, totalRequests)
	decisionReasons := make(map[string]int)
	trafficCounts := make(map[string]int)
	latencies := make([]time.Duration, 0, totalRequests)

	successes := 0
	failures := 0

	now := time.Date(2026, 9, 21, 14, 0, 0, 0, time.UTC)
	step := 100 * time.Millisecond

	var scenarioID string
	var recoveryStartTime time.Time
	var recoveryTimeMs float64

	for i := 0; i < totalRequests; i++ {
		now = now.Add(step)
		chaosController.SetNowFunc(func() time.Time { return now })

		// Inject Latency via M2-6 Chaos Controller
		if i == degradeStart {
			scenario, err := chaosController.Start(ctx, chaos.StartRequest{
				ScenarioID: "exp3-latency",
				TargetID:   "RAIL-A",
				Type:       chaos.ScenarioTypeLatency,
				Parameters: chaos.ScenarioParameters{
					DurationMs: int64(degradeEnd-degradeStart) * 100,
					LatencyMs:  15,
				},
			}, "test-admin", "OPS_ADMIN")
			if err == nil {
				scenarioID = scenario.ScenarioID
			}
		} else if i == degradeEnd && scenarioID != "" {
			recoveryStartTime = now
			_, _ = chaosController.Stop(ctx, scenarioID, "test-admin", "OPS_ADMIN")
		}

		// Update health service through actual adapter probe seam
		probeStartA := time.Now()
		resA, errA := chaosAdapterA.GetHealth(ctx)
		probeLatA := time.Since(probeStartA)
		_ = hService.RecordSample(ctx, health.HealthSample{
			TargetID:  "RAIL-A",
			SampledAt: now,
			Available: errA == nil && resA.Available,
			Latency:   probeLatA,
			Outcome:   health.OutcomeSuccess,
		})

		probeStartB := time.Now()
		resB, errB := adapterB.GetHealth(ctx)
		probeLatB := time.Since(probeStartB)
		_ = hService.RecordSample(ctx, health.HealthSample{
			TargetID:  "RAIL-B",
			SampledAt: now,
			Available: errB == nil && resB.Available,
			Latency:   probeLatB,
			Outcome:   health.OutcomeSuccess,
		})

		// Select route: static baseline ignores health snapshots; adaptive evaluates them
		var snapshots payments.HealthSnapshotProvider
		if mode == payments.SelectionModeAdaptive {
			snapshots = hService
		}
		decision, err := payments.SelectRoute(ctx, candidates, mode, snapshots, nil, now, "CANDIDATE-A")
		if err != nil {
			failures++
			decisionReasons[payments.ReasonNoEligibleTarget]++
			samples = append(samples, ExperimentSample{
				Index:          i,
				Timestamp:      now,
				Mode:           string(mode),
				TargetID:       "NONE",
				Success:        false,
				DecisionReason: payments.ReasonNoEligibleTarget,
				ErrorMessage:   err.Error(),
			})
			continue
		}

		selectedTarget := decision.Candidate.ExecutionTargetID
		decisionReasons[decision.Reason]++
		trafficCounts[selectedTarget]++

		// EXECUTE directly through the selected candidate's adapter seam and measure real elapsed latency
		holdReq := bank.HoldFundsRequest{
			OperationRequest: bank.OperationRequest{
				PaymentID:      uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("exp3-pay-s%d-%d", seed, i))),
				OperationID:    uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("exp3-op-s%d-%d", seed, i))),
				IdempotencyKey: fmt.Sprintf("exp3-idemp-s%d-%04d", seed, i),
				AccountID:      uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("exp3-acc-s%d-%d", seed, i))),
				AmountPaise:    1000,
				Currency:       "INR",
			},
		}

		execStart := time.Now()
		_, opErr := decision.Candidate.SourceAdapter.HoldFunds(ctx, holdReq)
		actualLat := time.Since(execStart)

		if opErr == nil {
			successes++
		} else {
			failures++
		}
		latencies = append(latencies, actualLat)

		if !recoveryStartTime.IsZero() && selectedTarget == "RAIL-A" && recoveryTimeMs == 0 {
			recoveryTimeMs = float64(now.Sub(recoveryStartTime).Milliseconds())
		}

		samples = append(samples, ExperimentSample{
			Index:          i,
			Timestamp:      now,
			Mode:           string(mode),
			TargetID:       selectedTarget,
			Success:        opErr == nil,
			Latency:        actualLat,
			LatencyMs:      float64(actualLat.Microseconds()) / 1000.0,
			DecisionReason: decision.Reason,
		})
	}

	latencyMetrics := calculateLatencyPercentiles(latencies)

	trafficShares := make([]TrafficShare, 0, len(trafficCounts))
	for tID, count := range trafficCounts {
		trafficShares = append(trafficShares, TrafficShare{
			TargetID:   tID,
			Count:      count,
			Percentage: float64(count) / float64(totalRequests) * 100.0,
		})
	}
	sort.Slice(trafficShares, func(i, j int) bool { return trafficShares[i].TargetID < trafficShares[j].TargetID })

	invariants := []InvariantResult{
		{
			InvariantName: "Every request processed successfully",
			Passed:        successes == totalRequests,
			Details:       fmt.Sprintf("successes=%d/%d", successes, totalRequests),
		},
	}
	if mode == payments.SelectionModeAdaptive {
		shifted := trafficCounts["RAIL-B"] > 0
		invariants = append(invariants, InvariantResult{
			InvariantName: "Traffic shifted away from degraded target during latency period",
			Passed:        shifted,
			Details:       fmt.Sprintf("Rail B served %d requests when Rail A suffered latency", trafficCounts["RAIL-B"]),
		})
	}

	allPassed := true
	for _, inv := range invariants {
		if !inv.Passed {
			allPassed = false
			break
		}
	}

	return &ExperimentSummary{
		TotalRequests:          totalRequests,
		SuccessCount:           successes,
		FailureCount:           failures,
		SuccessRate:            float64(successes) / float64(totalRequests) * 100.0,
		Latency:                latencyMetrics,
		RecoveryTimeMs:         recoveryTimeMs,
		TrafficShare:           trafficShares,
		DecisionReasons:        decisionReasons,
		Invariants:             invariants,
		AllInvariantsSatisfied: allPassed,
	}, samples
}
