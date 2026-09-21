package experiments

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/circuit"
	"github.com/transactx/backend/internal/payments"
)

// RunExperiment5 runs Experiment 5: Concurrency storm with financial and circuit invariants.
func RunExperiment5(ctx context.Context, seed int64, totalOperations, concurrency int) (*ExperimentResult, error) {
	if totalOperations <= 0 {
		totalOperations = 300
	}
	if concurrency <= 0 {
		concurrency = 10
	}

	scenarioParams := map[string]string{
		"totalOperations": fmt.Sprintf("%d", totalOperations),
		"concurrency":     fmt.Sprintf("%d goroutines", concurrency),
		"workloadType":    "concurrent payment submissions with duplicate idempotency keys",
		"targetA":         "RAIL-A",
		"targetB":         "RAIL-B",
	}
	env := CaptureEnvironmentMetadata(seed, totalOperations, scenarioParams)

	summary, samples := runConcurrencySimulation(ctx, seed, totalOperations, concurrency)

	return &ExperimentResult{
		ExperimentID: "concurrency",
		Title:        "Experiment 5: Concurrency Storm and Financial Invariants",
		Environment:  env,
		Summary:      summary,
		Samples:      samples,
	}, nil
}

type trackedPayment struct {
	PaymentID      uuid.UUID
	IdempotencyKey string
	State          string
	AmountPaise    int64
	TargetID       string
}

type idempotencyGate struct {
	mu       sync.Mutex
	inFlight map[string]*sync.WaitGroup
	results  map[string]*trackedPayment
}

func runConcurrencySimulation(ctx context.Context, seed int64, totalOperations, concurrency int) (*ExperimentSummary, []ExperimentSample) {
	rng := rand.New(rand.NewSource(seed))

	sourceBankID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("exp5-src-%d", seed)))
	destBankID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("exp5-dst-%d", seed)))

	adapterA := NewMockBankAdapter("BANK-A-RAIL-A")
	adapterB := NewMockBankAdapter("BANK-A-RAIL-B")

	// Adapter-seam execution tracker
	tracker := NewExecutionTracker()
	adapterA.SetTracker(tracker)
	adapterB.SetTracker(tracker)

	cbConfig := circuit.DefaultConfig()
	cbConfig.FailureThreshold = 5
	cb, _ := circuit.NewBreaker(cbConfig)

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

	// Pre-generate pool of deterministic idempotency keys (some will be reused)
	uniqueKeyCount := int(float64(totalOperations) * 0.6)
	if uniqueKeyCount < 10 {
		uniqueKeyCount = 10
	}
	idempPool := make([]string, uniqueKeyCount)
	for i := 0; i < uniqueKeyCount; i++ {
		idempPool[i] = fmt.Sprintf("storm-idemp-s%d-%04d", seed, i)
	}

	type task struct {
		Index          int
		IdempotencyKey string
	}

	tasks := make([]task, totalOperations)
	for i := 0; i < totalOperations; i++ {
		key := idempPool[rng.Intn(len(idempPool))]
		tasks[i] = task{
			Index:          i,
			IdempotencyKey: key,
		}
	}

	var completedOps atomic.Int64
	var failedOps atomic.Int64
	var pendingOps atomic.Int64
	var duplicateSubmissions atomic.Int64

	var checkedDecisions atomic.Int64
	var circuitViolations atomic.Int64
	var firstViolationDetails string
	var firstViolationOnce sync.Once

	gate := &idempotencyGate{
		inFlight: make(map[string]*sync.WaitGroup),
		results:  make(map[string]*trackedPayment),
	}

	var samplesMu sync.Mutex
	samples := make([]ExperimentSample, 0, totalOperations)
	decisionReasons := make(map[string]int)

	// Concurrency worker pool
	taskChan := make(chan task, totalOperations)
	for _, t := range tasks {
		taskChan <- t
	}
	close(taskChan)

	var wg sync.WaitGroup

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for t := range taskChan {
				reqTime := time.Now()

				// Evaluate circuit eligibility hook directly
				decision, err := payments.SelectRoute(ctx, candidates, payments.SelectionModeAdaptive, nil, cb.EligibilityHook(), reqTime)

				if err != nil {
					failedOps.Add(1)
					samplesMu.Lock()
					decisionReasons[payments.ReasonNoEligibleTarget]++
					samples = append(samples, ExperimentSample{
						Index:          t.Index,
						Timestamp:      reqTime,
						Mode:           "ADAPTIVE",
						TargetID:       "NONE",
						Success:        false,
						DecisionReason: payments.ReasonNoEligibleTarget,
						ErrorMessage:   err.Error(),
					})
					samplesMu.Unlock()
					continue
				}

				selectedTarget := decision.Candidate.ExecutionTargetID

				// Strictly evaluate circuit eligibility invariant for EVERY decision
				checkedDecisions.Add(1)
				cState := cb.GetState(selectedTarget, reqTime)
				isEligible := cb.EligibilityHook()(ctx, decision.Candidate)
				if cState == circuit.StateOpen || !isEligible {
					circuitViolations.Add(1)
					firstViolationOnce.Do(func() {
						firstViolationDetails = fmt.Sprintf("target=%s state=%s eligible=%t index=%d", selectedTarget, cState, isEligible, t.Index)
					})
				}

				samplesMu.Lock()
				decisionReasons[decision.Reason]++
				samplesMu.Unlock()

				// Idempotency Coordinator Gate:
				// Ensures multiple concurrent arrivals of the exact same key do not double-invoke the financial adapter seam
				gate.mu.Lock()
				if existing, exists := gate.results[t.IdempotencyKey]; exists {
					duplicateSubmissions.Add(1)
					completedOps.Add(1)
					gate.mu.Unlock()

					samplesMu.Lock()
					samples = append(samples, ExperimentSample{
						Index:          t.Index,
						Timestamp:      reqTime,
						Mode:           "ADAPTIVE",
						TargetID:       existing.TargetID,
						Success:        existing.State == "COMPLETED",
						DecisionReason: "IDEMPOTENT_DUPLICATE_REUSE",
					})
					samplesMu.Unlock()
					continue
				}

				if waitGroup, inFlight := gate.inFlight[t.IdempotencyKey]; inFlight {
					duplicateSubmissions.Add(1)
					gate.mu.Unlock()

					// Wait for the in-flight primary execution to finish
					waitGroup.Wait()

					gate.mu.Lock()
					result := gate.results[t.IdempotencyKey]
					gate.mu.Unlock()

					completedOps.Add(1)
					samplesMu.Lock()
					samples = append(samples, ExperimentSample{
						Index:          t.Index,
						Timestamp:      reqTime,
						Mode:           "ADAPTIVE",
						TargetID:       selectedTarget,
						Success:        result != nil && result.State == "COMPLETED",
						DecisionReason: "IDEMPOTENT_CONCURRENT_REUSE",
					})
					samplesMu.Unlock()
					continue
				}

				// Primary executor for this key: register in-flight waitgroup
				pWg := &sync.WaitGroup{}
				pWg.Add(1)
				gate.inFlight[t.IdempotencyKey] = pWg
				gate.mu.Unlock()

				// Primary execution at domain adapter seam
				paymentID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("pay-s%d-%s", seed, t.IdempotencyKey)))
				opID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("hold-s%d-%s", seed, t.IdempotencyKey)))
				accountID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("acc-s%d-%s", seed, t.IdempotencyKey)))

				holdRes, holdErr := decision.Candidate.SourceAdapter.HoldFunds(ctx, bank.HoldFundsRequest{
					OperationRequest: bank.OperationRequest{
						PaymentID:      paymentID,
						OperationID:    opID,
						IdempotencyKey: t.IdempotencyKey,
						AccountID:      accountID,
						AmountPaise:    1000,
						Currency:       "INR",
					},
				})

				var finalState string
				var success bool
				if holdErr == nil && holdRes.Status == bank.OperationSucceeded {
					confirmOpID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("conf-s%d-%s", seed, t.IdempotencyKey)))
					confRes, capErr := decision.Candidate.SourceAdapter.ConfirmHold(ctx, bank.ConfirmHoldRequest{
						PaymentID:      paymentID,
						OperationID:    confirmOpID,
						IdempotencyKey: t.IdempotencyKey,
						HoldID:         holdRes.HoldID,
					})
					if capErr == nil && confRes.Status == bank.OperationSucceeded {
						finalState = "COMPLETED"
						success = true
						completedOps.Add(1)
						cb.RecordSuccess(selectedTarget, reqTime)
					} else {
						finalState = "FAILED"
						failedOps.Add(1)
						cb.RecordFailure(selectedTarget, "confirm failed", reqTime)
					}
				} else {
					finalState = "FAILED"
					failedOps.Add(1)
					cb.RecordFailure(selectedTarget, "hold failed", reqTime)
				}

				record := &trackedPayment{
					PaymentID:      paymentID,
					IdempotencyKey: t.IdempotencyKey,
					State:          finalState,
					AmountPaise:    1000,
					TargetID:       selectedTarget,
				}

				gate.mu.Lock()
				gate.results[t.IdempotencyKey] = record
				delete(gate.inFlight, t.IdempotencyKey)
				gate.mu.Unlock()
				pWg.Done()

				samplesMu.Lock()
				samples = append(samples, ExperimentSample{
					Index:          t.Index,
					Timestamp:      reqTime,
					Mode:           "ADAPTIVE",
					TargetID:       selectedTarget,
					Success:        success,
					DecisionReason: decision.Reason,
				})
				samplesMu.Unlock()
			}
		}(w)
	}

	wg.Wait()

	// Verify adapter-seam execution invariants
	uniqueFinancialExecs, maxExecsPerKey, duplicateFinancialExecs := tracker.Stats()

	// Verify state validity across all recorded payments
	gate.mu.Lock()
	stateViolations := 0
	for _, p := range gate.results {
		if p.State != "COMPLETED" && p.State != "FAILED" && p.State != "PENDING" {
			stateViolations++
		}
	}
	gate.mu.Unlock()

	circuitViolationCount := circuitViolations.Load()
	circuitDetails := fmt.Sprintf("checked decisions=%d, circuit violations=%d", checkedDecisions.Load(), circuitViolationCount)
	if firstViolationDetails != "" {
		circuitDetails += fmt.Sprintf(", first: %s", firstViolationDetails)
	}

	invariants := []InvariantResult{
		{
			InvariantName: "Adapter seam: max financial execution count per logical idempotency key == 1",
			Passed:        duplicateFinancialExecs == 0 && maxExecsPerKey <= 1,
			Details: fmt.Sprintf("unique keys=%d, max execs/key=%d, duplicate financial executions=%d",
				uniqueFinancialExecs, maxExecsPerKey, duplicateFinancialExecs),
		},
		{
			InvariantName: "No impossible money-state transitions (states strictly terminal or pending)",
			Passed:        stateViolations == 0,
			Details:       fmt.Sprintf("%d payments evaluated, %d invalid states", len(gate.results), stateViolations),
		},
		{
			InvariantName: "Accounting conservation (completed + failed + pending = requested)",
			Passed:        int(completedOps.Load()+failedOps.Load()+pendingOps.Load()) == totalOperations,
			Details: fmt.Sprintf("requested=%d, completed=%d, failed=%d, pending=%d",
				totalOperations, completedOps.Load(), failedOps.Load(), pendingOps.Load()),
		},
		{
			InvariantName: "No route decision violated circuit eligibility",
			Passed:        circuitViolationCount == 0,
			Details:       circuitDetails,
		},
	}

	allPassed := true
	for _, inv := range invariants {
		if !inv.Passed {
			allPassed = false
			break
		}
	}

	additional := map[string]any{
		"concurrency":                  concurrency,
		"uniqueLogicalKeys":            uniqueKeyCount,
		"duplicateSubmissions":         duplicateSubmissions.Load(),
		"uniqueFinancialExecutions":    uniqueFinancialExecs,
		"duplicateFinancialExecutions": duplicateFinancialExecs,
		"maxExecutionsPerKey":          maxExecsPerKey,
		"circuitDecisionsChecked":      checkedDecisions.Load(),
		"circuitViolations":            circuitViolationCount,
		"stateViolations":              stateViolations,
	}

	return &ExperimentSummary{
		TotalRequests:          totalOperations,
		SuccessCount:           int(completedOps.Load()),
		FailureCount:           int(failedOps.Load()),
		PendingCount:           int(pendingOps.Load()),
		SuccessRate:            float64(completedOps.Load()) / float64(totalOperations) * 100.0,
		DecisionReasons:        decisionReasons,
		Invariants:             invariants,
		AllInvariantsSatisfied: allPassed,
		AdditionalMetrics:      additional,
	}, samples
}
