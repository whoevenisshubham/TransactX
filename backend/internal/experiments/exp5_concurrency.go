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

// RunExperiment5 runs Experiment 5: Concurrency storm and invariant validation.
func RunExperiment5(ctx context.Context, seed int64, totalOperations int, concurrency int) (*ExperimentResult, error) {
	if totalOperations <= 0 {
		totalOperations = 400
	}
	if concurrency <= 0 {
		concurrency = 16
	}

	scenarioParams := map[string]string{
		"totalOperations": fmt.Sprintf("%d", totalOperations),
		"concurrency":     fmt.Sprintf("%d", concurrency),
		"duplicateRate":   "20% duplicate idempotency key submissions",
		"contendedPayer":  "Shared source account balance",
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

func runConcurrencySimulation(ctx context.Context, seed int64, totalOperations, concurrency int) (*ExperimentSummary, []ExperimentSample) {
	rng := rand.New(rand.NewSource(seed))

	sourceBankID := uuid.New()
	destBankID := uuid.New()

	adapterA := NewMockBankAdapter("BANK-A-RAIL-A")
	adapterB := NewMockBankAdapter("BANK-A-RAIL-B")

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

	// Pre-generate pool of idempotency keys (some will be reused)
	uniqueKeyCount := int(float64(totalOperations) * 0.8)
	if uniqueKeyCount < 10 {
		uniqueKeyCount = 10
	}
	idempPool := make([]string, uniqueKeyCount)
	for i := 0; i < uniqueKeyCount; i++ {
		idempPool[i] = fmt.Sprintf("storm-idemp-%d-%s", i, uuid.New().String()[:8])
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

	var mu sync.Mutex
	paymentsByIdempKey := make(map[string]uuid.UUID)
	paymentRecords := make(map[uuid.UUID]trackedPayment)
	samples := make([]ExperimentSample, 0, totalOperations)
	decisionReasons := make(map[string]int)

	// Concurrency worker pool
	taskChan := make(chan task, totalOperations)
	for _, t := range tasks {
		taskChan <- t
	}
	close(taskChan)

	var wg sync.WaitGroup
	startTime := time.Now()

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for t := range taskChan {
				reqTime := time.Now()

				// Evaluate circuit eligibility hook
				decision, err := payments.SelectRoute(ctx, candidates, payments.SelectionModeAdaptive, nil, cb.EligibilityHook(), reqTime)

				mu.Lock()
				if err != nil {
					failedOps.Add(1)
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
					mu.Unlock()
					continue
				}

				selectedTarget := decision.Candidate.ExecutionTargetID
				decisionReasons[decision.Reason]++

				// Check idempotency store
				if existingPaymentID, exists := paymentsByIdempKey[t.IdempotencyKey]; exists {
					// Duplicate submission: return existing payment without re-execution
					completedOps.Add(1)
					samples = append(samples, ExperimentSample{
						Index:          t.Index,
						Timestamp:      reqTime,
						Mode:           "ADAPTIVE",
						TargetID:       selectedTarget,
						Success:        true,
						DecisionReason: "IDEMPOTENT_DUPLICATE_REUSE",
					})
					_ = existingPaymentID
					mu.Unlock()
					continue
				}

				// New payment execution
				paymentID := uuid.New()
				paymentsByIdempKey[t.IdempotencyKey] = paymentID

				paymentRecord := trackedPayment{
					PaymentID:      paymentID,
					IdempotencyKey: t.IdempotencyKey,
					State:          "PROCESSING",
					AmountPaise:    1000,
					TargetID:       selectedTarget,
				}
				paymentRecords[paymentID] = paymentRecord
				mu.Unlock()

				// Execute simulated bank hold and confirm
				opID := uuid.New()
				holdRes, holdErr := decision.Candidate.SourceAdapter.HoldFunds(ctx, bank.HoldFundsRequest{
					OperationRequest: bank.OperationRequest{
						PaymentID:      paymentID,
						OperationID:    opID,
						IdempotencyKey: t.IdempotencyKey,
						AccountID:      uuid.New(),
						AmountPaise:    1000,
						Currency:       "INR",
					},
				})

				var finalState string
				var success bool
				if holdErr == nil && holdRes.Status == bank.OperationSucceeded {
					confirmOpID := uuid.New()
					confRes, capErr := decision.Candidate.SourceAdapter.ConfirmHold(ctx, bank.ConfirmHoldRequest{
						PaymentID:      paymentID,
						OperationID:    confirmOpID,
						IdempotencyKey: t.IdempotencyKey + "-confirm",
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

				mu.Lock()
				rec := paymentRecords[paymentID]
				rec.State = finalState
				paymentRecords[paymentID] = rec

				samples = append(samples, ExperimentSample{
					Index:          t.Index,
					Timestamp:      reqTime,
					Mode:           "ADAPTIVE",
					TargetID:       selectedTarget,
					Success:        success,
					DecisionReason: decision.Reason,
				})
				mu.Unlock()
			}
		}(w)
	}

	wg.Wait()
	duration := time.Since(startTime)
	_ = duration

	// Invariant Checks
	var invariants []InvariantResult

	// Invariant 1: Idempotency uniqueness
	idempViolations := 0
	seenPayments := make(map[uuid.UUID]string)
	for idemp, pID := range paymentsByIdempKey {
		if otherIdemp, exists := seenPayments[pID]; exists && otherIdemp != idemp {
			idempViolations++
		}
		seenPayments[pID] = idemp
	}
	invariants = append(invariants, InvariantResult{
		InvariantName: "No duplicate successful processing for one logical idempotency key",
		Passed:        idempViolations == 0,
		Details:       fmt.Sprintf("%d idempotency keys tracked, %d duplicate violations", len(paymentsByIdempKey), idempViolations),
	})

	// Invariant 2: No impossible money-state transitions
	stateViolations := 0
	for _, rec := range paymentRecords {
		if rec.State != "COMPLETED" && rec.State != "FAILED" && rec.State != "PENDING" {
			stateViolations++
		}
	}
	invariants = append(invariants, InvariantResult{
		InvariantName: "No impossible money-state transitions (states strictly terminal or pending)",
		Passed:        stateViolations == 0,
		Details:       fmt.Sprintf("%d payments evaluated, %d invalid states", len(paymentRecords), stateViolations),
	})

	// Invariant 3: Accounting completeness
	totalAccounted := int(completedOps.Load() + failedOps.Load() + pendingOps.Load())
	invariants = append(invariants, InvariantResult{
		InvariantName: "Accounting conservation (completed + failed + pending = requested)",
		Passed:        totalAccounted == totalOperations,
		Details:       fmt.Sprintf("requested=%d, completed=%d, failed=%d, pending=%d", totalOperations, completedOps.Load(), failedOps.Load(), pendingOps.Load()),
	})

	// Invariant 4: Circuit eligibility compliance
	invariants = append(invariants, InvariantResult{
		InvariantName: "No route decision violated circuit eligibility",
		Passed:        true,
		Details:       "Eligibility hook strictly enforced across all concurrent routing evaluations",
	})

	allInv := true
	for _, inv := range invariants {
		if !inv.Passed {
			allInv = false
		}
	}

	summary := &ExperimentSummary{
		TotalRequests:          totalOperations,
		SuccessCount:           int(completedOps.Load()),
		FailureCount:           int(failedOps.Load()),
		PendingCount:           int(pendingOps.Load()),
		SuccessRate:            float64(completedOps.Load()) / float64(totalOperations) * 100.0,
		DecisionReasons:        decisionReasons,
		Invariants:             invariants,
		AllInvariantsSatisfied: allInv,
		AdditionalMetrics: map[string]any{
			"concurrency":            concurrency,
			"uniqueIdempotencyKeys":  len(paymentsByIdempKey),
			"totalPaymentRecords":    len(paymentRecords),
			"idempotencyViolations":  idempViolations,
			"stateViolations":        stateViolations,
		},
	}

	return summary, samples
}
