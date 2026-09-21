package payments

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/health"
)

type routingHealth struct {
	snapshots map[string]health.HealthSnapshot
}

func (provider routingHealth) GetSnapshot(_ context.Context, targetID string, _ time.Time) (health.HealthSnapshot, error) {
	return provider.snapshots[targetID], nil
}

type spyBankAdapter struct {
	successfulBankAdapter
	holdCalls   int
	creditCalls int
}

func (s *spyBankAdapter) HoldFunds(ctx context.Context, req bank.HoldFundsRequest) (bank.HoldResult, error) {
	s.holdCalls++
	return s.successfulBankAdapter.HoldFunds(ctx, req)
}

func (s *spyBankAdapter) ProvisionalCredit(ctx context.Context, req bank.ProvisionalCreditRequest) (bank.OperationResult, error) {
	s.creditCalls++
	return s.successfulBankAdapter.ProvisionalCredit(ctx, req)
}

func routeCandidate(id, target string, source, destination uuid.UUID) RouteCandidate {
	return RouteCandidate{
		CandidateID:        id,
		SourceBankID:       source,
		DestinationBankID:  destination,
		ExecutionTargetID:  target,
		SourceAdapter:      successfulBankAdapter{},
		DestinationAdapter: successfulBankAdapter{},
	}
}

func routeCandidateWithAdapters(id, target string, source, destination uuid.UUID, srcAdapter, dstAdapter bank.BankAdapter) RouteCandidate {
	return RouteCandidate{
		CandidateID:        id,
		SourceBankID:       source,
		DestinationBankID:  destination,
		ExecutionTargetID:  target,
		SourceAdapter:      srcAdapter,
		DestinationAdapter: dstAdapter,
	}
}

// 1. Single legitimate candidate
func TestSelectRouteSingleCandidatePreservesOwnership(t *testing.T) {
	source, destination := uuid.New(), uuid.New()
	candidate := routeCandidate("only", "BANK-A", source, destination)
	decision, err := SelectRoute(context.Background(), []RouteCandidate{candidate}, SelectionModeAdaptive, routingHealth{map[string]health.HealthSnapshot{"BANK-A": {TargetID: "BANK-A", SampleCount: 1, AvailabilityScore: 1, Score: 0.8}}}, nil, time.Unix(1, 0))
	if err != nil || decision.Candidate != candidate || decision.Candidate.SourceBankID != source || decision.Candidate.DestinationBankID != destination {
		t.Fatalf("decision = %+v, err = %v", decision, err)
	}
}

// 2. Two legitimate execution targets for the same source/destination logical bank ownership
func TestSelectRouteTwoLegitimateExecutionTargets(t *testing.T) {
	source, destination := uuid.New(), uuid.New()
	targetA := routeCandidate("direct", "RAIL-A", source, destination)
	targetB := routeCandidate("rail-b", "RAIL-B", source, destination)
	candidates := []RouteCandidate{targetA, targetB}

	provider := routingHealth{map[string]health.HealthSnapshot{
		"RAIL-A": {TargetID: "RAIL-A", SampleCount: 2, AvailabilityScore: 1, Score: 0.6},
		"RAIL-B": {TargetID: "RAIL-B", SampleCount: 2, AvailabilityScore: 1, Score: 0.9},
	}}

	decision, err := SelectRoute(context.Background(), candidates, SelectionModeAdaptive, provider, nil, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Candidate.CandidateID != "rail-b" || decision.Candidate.ExecutionTargetID != "RAIL-B" {
		t.Fatalf("selected = %+v, want rail-b on RAIL-B", decision.Candidate)
	}
	if decision.Candidate.SourceBankID != source || decision.Candidate.DestinationBankID != destination {
		t.Fatalf("bank ownership altered: source=%v, dest=%v", decision.Candidate.SourceBankID, decision.Candidate.DestinationBankID)
	}
}

// 3. Three legitimate execution targets (direct, RAIL-A, RAIL-B)
func TestSelectRouteThreeLegitimateExecutionTargets(t *testing.T) {
	source, destination := uuid.New(), uuid.New()
	candidates := []RouteCandidate{
		routeCandidate("direct", "DIRECT", source, destination),
		routeCandidate("rail-a", "RAIL-A", source, destination),
		routeCandidate("rail-b", "RAIL-B", source, destination),
	}

	provider := routingHealth{map[string]health.HealthSnapshot{
		"DIRECT": {TargetID: "DIRECT", SampleCount: 5, AvailabilityScore: 1, Score: 0.55},
		"RAIL-A": {TargetID: "RAIL-A", SampleCount: 5, AvailabilityScore: 1, Score: 0.85},
		"RAIL-B": {TargetID: "RAIL-B", SampleCount: 5, AvailabilityScore: 1, Score: 0.70},
	}}

	decision, err := SelectRoute(context.Background(), candidates, SelectionModeAdaptive, provider, nil, time.Unix(10, 0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Candidate.CandidateID != "rail-a" || decision.Candidate.ExecutionTargetID != "RAIL-A" {
		t.Fatalf("selected = %+v, want rail-a", decision.Candidate)
	}
	if decision.Score != 0.85 {
		t.Fatalf("score = %v, want 0.85", decision.Score)
	}
}

// 4. Adaptive chooses healthier target
func TestSelectRouteAdaptiveChoosesHealthierTarget(t *testing.T) {
	source, destination := uuid.New(), uuid.New()
	targetLow := routeCandidate("degraded", "RAIL-DEGRADED", source, destination)
	targetHigh := routeCandidate("healthy", "RAIL-HEALTHY", source, destination)

	provider := routingHealth{map[string]health.HealthSnapshot{
		"RAIL-DEGRADED": {TargetID: "RAIL-DEGRADED", SampleCount: 10, AvailabilityScore: 1, Score: 0.32},
		"RAIL-HEALTHY":  {TargetID: "RAIL-HEALTHY", SampleCount: 10, AvailabilityScore: 1, Score: 0.94},
	}}

	decision, err := SelectRoute(context.Background(), []RouteCandidate{targetLow, targetHigh}, SelectionModeAdaptive, provider, nil, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Candidate.CandidateID != "healthy" || decision.Candidate.ExecutionTargetID != "RAIL-HEALTHY" {
		t.Fatalf("expected healthy target, got %+v", decision.Candidate)
	}
	if decision.Reason != ReasonAdaptive {
		t.Fatalf("reason = %q, want %q", decision.Reason, ReasonAdaptive)
	}
}

// 5. Unhealthy candidate excluded
func TestSelectRouteUnhealthyCandidateExcluded(t *testing.T) {
	source, destination := uuid.New(), uuid.New()
	targetUnhealthy := routeCandidate("unhealthy", "RAIL-UNHEALTHY", source, destination)
	targetHealthy := routeCandidate("healthy", "RAIL-HEALTHY", source, destination)

	provider := routingHealth{map[string]health.HealthSnapshot{
		"RAIL-UNHEALTHY": {TargetID: "RAIL-UNHEALTHY", SampleCount: 5, AvailabilityScore: 1, Score: 0.0}, // Score <= 0
		"RAIL-HEALTHY":   {TargetID: "RAIL-HEALTHY", SampleCount: 5, AvailabilityScore: 1, Score: 0.75},
	}}

	decision, err := SelectRoute(context.Background(), []RouteCandidate{targetUnhealthy, targetHealthy}, SelectionModeAdaptive, provider, nil, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Candidate.CandidateID != "healthy" {
		t.Fatalf("selected unhealthy candidate: %+v", decision.Candidate)
	}

	// When all candidates are unhealthy, returns ErrNoRouteCandidate
	_, errAllUnhealthy := SelectRoute(context.Background(), []RouteCandidate{targetUnhealthy}, SelectionModeAdaptive, provider, nil, time.Unix(1, 0))
	if errAllUnhealthy != ErrNoRouteCandidate {
		t.Fatalf("expected ErrNoRouteCandidate, got %v", errAllUnhealthy)
	}
}

// 6. Unavailable candidate excluded
func TestSelectRouteUnavailableCandidateExcluded(t *testing.T) {
	source, destination := uuid.New(), uuid.New()
	targetUnavailable := routeCandidate("unavailable", "RAIL-UNAVAILABLE", source, destination)
	targetAvailable := routeCandidate("available", "RAIL-AVAILABLE", source, destination)

	provider := routingHealth{map[string]health.HealthSnapshot{
		"RAIL-UNAVAILABLE": {TargetID: "RAIL-UNAVAILABLE", SampleCount: 3, AvailabilityScore: 0.0, Score: 0.9}, // Availability == 0
		"RAIL-AVAILABLE":   {TargetID: "RAIL-AVAILABLE", SampleCount: 3, AvailabilityScore: 1.0, Score: 0.5},
	}}

	decision, err := SelectRoute(context.Background(), []RouteCandidate{targetUnavailable, targetAvailable}, SelectionModeAdaptive, provider, nil, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Candidate.CandidateID != "available" {
		t.Fatalf("selected unavailable candidate: %+v", decision.Candidate)
	}
}

// 7. Equal score -> executionTargetID stable tie-break
func TestSelectRouteEqualScoreStableTieBreak(t *testing.T) {
	source, destination := uuid.New(), uuid.New()
	// Target IDs: "RAIL-2" and "RAIL-1" (RAIL-1 < RAIL-2 lexicographically)
	candidate2 := routeCandidate("candidate-2", "RAIL-2", source, destination)
	candidate1 := routeCandidate("candidate-1", "RAIL-1", source, destination)

	provider := routingHealth{map[string]health.HealthSnapshot{
		"RAIL-1": {TargetID: "RAIL-1", SampleCount: 4, AvailabilityScore: 1, Score: 0.80},
		"RAIL-2": {TargetID: "RAIL-2", SampleCount: 4, AvailabilityScore: 1, Score: 0.80},
	}}

	// Provide in reverse order to ensure sorting breaks tie by ExecutionTargetID
	decision, err := SelectRoute(context.Background(), []RouteCandidate{candidate2, candidate1}, SelectionModeAdaptive, provider, nil, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Candidate.ExecutionTargetID != "RAIL-1" {
		t.Fatalf("expected tie-break to choose RAIL-1, got %q", decision.Candidate.ExecutionTargetID)
	}
}

// 8. Repeated identical input -> identical decision
func TestSelectRouteRepeatedIdenticalInput(t *testing.T) {
	source, destination := uuid.New(), uuid.New()
	candidates := []RouteCandidate{
		routeCandidate("cand-c", "TARGET-C", source, destination),
		routeCandidate("cand-a", "TARGET-A", source, destination),
		routeCandidate("cand-b", "TARGET-B", source, destination),
	}
	provider := routingHealth{map[string]health.HealthSnapshot{
		"TARGET-A": {TargetID: "TARGET-A", SampleCount: 2, AvailabilityScore: 1, Score: 0.70},
		"TARGET-B": {TargetID: "TARGET-B", SampleCount: 2, AvailabilityScore: 1, Score: 0.70},
		"TARGET-C": {TargetID: "TARGET-C", SampleCount: 2, AvailabilityScore: 1, Score: 0.60},
	}}

	first, err := SelectRoute(context.Background(), candidates, SelectionModeAdaptive, provider, nil, time.Unix(50, 0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := 0; i < 20; i++ {
		repeated, rErr := SelectRoute(context.Background(), candidates, SelectionModeAdaptive, provider, nil, time.Unix(50, 0))
		if rErr != nil || repeated != first {
			t.Fatalf("run %d: decision diverged: %+v vs %+v", i, repeated, first)
		}
	}
}

// 9. STATIC runtime selection with configured baseline
func TestSelectRouteStaticRuntimeSelection(t *testing.T) {
	source, destination := uuid.New(), uuid.New()
	candidates := []RouteCandidate{
		routeCandidate("direct", "RAIL-A", source, destination),
		routeCandidate("rail-b", "RAIL-B", source, destination),
		routeCandidate("rail-c", "RAIL-C", source, destination),
	}

	// 9a. Select configured baseline candidate
	decision, err := SelectRoute(context.Background(), candidates, SelectionModeStatic, nil, nil, time.Unix(1, 0), "rail-b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Candidate.CandidateID != "rail-b" || decision.Reason != ReasonStatic || decision.Mode != SelectionModeStatic {
		t.Fatalf("decision mismatch: %+v", decision)
	}

	// 9b. Fails safely if configured baseline is not eligible/available
	_, errNotFound := SelectRoute(context.Background(), candidates, SelectionModeStatic, nil, nil, time.Unix(1, 0), "non-existent")
	if errNotFound != ErrNoRouteCandidate {
		t.Fatalf("expected ErrNoRouteCandidate for non-existent baseline, got %v", errNotFound)
	}

	// 9c. Baseline fails safely if snapshot shows it is unavailable
	provider := routingHealth{map[string]health.HealthSnapshot{
		"RAIL-A": {TargetID: "RAIL-A", SampleCount: 1, AvailabilityScore: 1, Score: 0.8},
		"RAIL-B": {TargetID: "RAIL-B", SampleCount: 1, AvailabilityScore: 0, Score: 0.0}, // unavailable
		"RAIL-C": {TargetID: "RAIL-C", SampleCount: 1, AvailabilityScore: 1, Score: 0.8},
	}}
	_, errUnavailable := SelectRoute(context.Background(), candidates, SelectionModeStatic, provider, nil, time.Unix(1, 0), "rail-b")
	if errUnavailable != ErrNoRouteCandidate {
		t.Fatalf("expected ErrNoRouteCandidate for unavailable baseline, got %v", errUnavailable)
	}

	// 9d. Without explicit baseline, deterministically sorts by CandidateID
	defaultStatic, errDefault := SelectRoute(context.Background(), candidates, SelectionModeStatic, nil, nil, time.Unix(1, 0))
	if errDefault != nil || defaultStatic.Candidate.CandidateID != "direct" {
		t.Fatalf("default static = %+v, err = %v", defaultStatic, errDefault)
	}
}

// 10. ADAPTIVE runtime selection
func TestSelectRouteAdaptiveRuntimeSelection(t *testing.T) {
	source, destination := uuid.New(), uuid.New()
	candidates := []RouteCandidate{
		routeCandidate("cand-1", "TARGET-1", source, destination),
		routeCandidate("cand-2", "TARGET-2", source, destination),
	}
	provider := routingHealth{map[string]health.HealthSnapshot{
		"TARGET-1": {TargetID: "TARGET-1", SampleCount: 5, AvailabilityScore: 1, Score: 0.45},
		"TARGET-2": {TargetID: "TARGET-2", SampleCount: 5, AvailabilityScore: 1, Score: 0.91},
	}}

	decision, err := SelectRoute(context.Background(), candidates, SelectionModeAdaptive, provider, nil, time.Unix(100, 0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Candidate.CandidateID != "cand-2" || decision.Score != 0.91 || decision.Reason != ReasonAdaptive || decision.Mode != SelectionModeAdaptive {
		t.Fatalf("adaptive decision mismatch: %+v", decision)
	}
}

// 13 & 14. Same logical source/destination bank ownership preserved across all candidates
func TestServiceSameOwnershipMultipleExecutionTargets(t *testing.T) {
	sourceBankID := uuid.New()
	destBankID := uuid.New()

	target1 := ExecutionTarget{
		CandidateID:        "direct",
		ExecutionTargetID:  "RAIL-A",
		SourceAdapter:      successfulBankAdapter{},
		DestinationAdapter: successfulBankAdapter{},
	}
	target2 := ExecutionTarget{
		CandidateID:        "rail-b",
		ExecutionTargetID:  "RAIL-B",
		SourceAdapter:      successfulBankAdapter{},
		DestinationAdapter: successfulBankAdapter{},
	}

	key := RouteKey{SourceBankID: sourceBankID, DestinationBankID: destBankID}
	executionTargets := map[RouteKey][]ExecutionTarget{
		key: {target1, target2},
	}

	svc := NewServiceWithExecutionTargets(nil, nil, nil, nil, executionTargets, nil, SelectionModeAdaptive, "")

	candidates := svc.getCandidates(sourceBankID, destBankID, successfulBankAdapter{}, successfulBankAdapter{})
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}

	for i, c := range candidates {
		if c.SourceBankID != sourceBankID {
			t.Fatalf("candidate %d altered SourceBankID: got %v, want %v", i, c.SourceBankID, sourceBankID)
		}
		if c.DestinationBankID != destBankID {
			t.Fatalf("candidate %d altered DestinationBankID: got %v, want %v", i, c.DestinationBankID, destBankID)
		}
	}
}

// 19. No eligible candidate -> safe unavailable behavior
func TestSelectRouteNoEligibleCandidateSafeBehavior(t *testing.T) {
	source, destination := uuid.New(), uuid.New()
	candidates := []RouteCandidate{
		routeCandidate("invalid", "TARGET-1", source, destination),
	}
	// All candidates have 0 availability
	provider := routingHealth{map[string]health.HealthSnapshot{
		"TARGET-1": {TargetID: "TARGET-1", SampleCount: 2, AvailabilityScore: 0, Score: 0},
	}}

	decision, err := SelectRoute(context.Background(), candidates, SelectionModeAdaptive, provider, nil, time.Unix(1, 0))
	if err != ErrNoRouteCandidate {
		t.Fatalf("expected ErrNoRouteCandidate, got %v", err)
	}
	if decision.Reason != ReasonNoEligibleTarget {
		t.Fatalf("expected ReasonNoEligibleTarget, got %q", decision.Reason)
	}
}

// 22. Selected candidate's adapters are actually used during execution
func TestSelectedCandidateAdaptersUsed(t *testing.T) {
	source, destination := uuid.New(), uuid.New()
	spyA := &spyBankAdapter{}
	spyB := &spyBankAdapter{}

	candA := routeCandidateWithAdapters("cand-a", "TARGET-A", source, destination, spyA, spyA)
	candB := routeCandidateWithAdapters("cand-b", "TARGET-B", source, destination, spyB, spyB)

	provider := routingHealth{map[string]health.HealthSnapshot{
		"TARGET-A": {TargetID: "TARGET-A", SampleCount: 1, AvailabilityScore: 1, Score: 0.4},
		"TARGET-B": {TargetID: "TARGET-B", SampleCount: 1, AvailabilityScore: 1, Score: 0.9},
	}}

	decision, err := SelectRoute(context.Background(), []RouteCandidate{candA, candB}, SelectionModeAdaptive, provider, nil, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Candidate.CandidateID != "cand-b" {
		t.Fatalf("expected cand-b to be selected, got %q", decision.Candidate.CandidateID)
	}

	// Verify that the selected candidate's SourceAdapter is spyB, not spyA
	if decision.Candidate.SourceAdapter != spyB || decision.Candidate.DestinationAdapter != spyB {
		t.Fatalf("selected candidate does not contain spyB adapter")
	}
}

// 23. Route selection itself never calls bank adapters
func TestSelectRouteNeverCallsBankAdapters(t *testing.T) {
	source, destination := uuid.New(), uuid.New()
	spy := &spyBankAdapter{}
	candidates := []RouteCandidate{
		routeCandidateWithAdapters("cand", "TARGET", source, destination, spy, spy),
	}
	provider := routingHealth{map[string]health.HealthSnapshot{
		"TARGET": {TargetID: "TARGET", SampleCount: 1, AvailabilityScore: 1, Score: 0.8},
	}}

	_, err := SelectRoute(context.Background(), candidates, SelectionModeAdaptive, provider, nil, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spy.holdCalls != 0 || spy.creditCalls != 0 {
		t.Fatalf("SelectRoute invoked bank adapters! holdCalls=%d, creditCalls=%d", spy.holdCalls, spy.creditCalls)
	}
}

// Circuit eligibility hook
func TestSelectRouteCircuitEligibilityHook(t *testing.T) {
	source, destination := uuid.New(), uuid.New()
	cand1 := routeCandidate("cand-1", "TARGET-1", source, destination)
	cand2 := routeCandidate("cand-2", "TARGET-2", source, destination)

	provider := routingHealth{map[string]health.HealthSnapshot{
		"TARGET-1": {TargetID: "TARGET-1", SampleCount: 1, AvailabilityScore: 1, Score: 0.95},
		"TARGET-2": {TargetID: "TARGET-2", SampleCount: 1, AvailabilityScore: 1, Score: 0.80},
	}}

	// Hook filters out cand-1 (e.g. simulated circuit open in future M2-5)
	filterHook := func(_ context.Context, c RouteCandidate) bool {
		return c.CandidateID != "cand-1"
	}

	decision, err := SelectRoute(context.Background(), []RouteCandidate{cand1, cand2}, SelectionModeAdaptive, provider, filterHook, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Candidate.CandidateID != "cand-2" {
		t.Fatalf("expected cand-2 because cand-1 was filtered by eligibility hook, got %q", decision.Candidate.CandidateID)
	}
}

// 15, 16, 17, 18. Route decision persistence tests (require DATABASE_URL)
func TestRouteDecisionPersistsTargetAndHealthSnapshot(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set")
	}
	data := newRepositoryTestData(t)
	defer data.close(t)
	decision := RouteDecision{
		Candidate:  routeCandidate("candidate-a", "BANK-A", data.bankID, data.bankID),
		Score:      0.75,
		Snapshot:   health.HealthSnapshot{TargetID: "BANK-A", SampleCount: 2, AvailabilityScore: 1, Score: 0.75},
		Reason:     ReasonAdaptive,
		Mode:       SelectionModeAdaptive,
		SelectedAt: time.Now().UTC(),
	}
	payment, duplicate, err := data.repository.CreateRoutedWithDecisionIdempotent(context.Background(), settlementPayment(data, 100), "route-history", "route-history-hash", decision)
	if err != nil || duplicate {
		t.Fatalf("create = %+v, duplicate=%v, err=%v", payment, duplicate, err)
	}
	var target, candidate, reason, mode, eventType string
	var score float64
	var samples int
	if err := data.pool.QueryRow(context.Background(), `
		SELECT execution_target_id, candidate_id, selected_score, (health_snapshot->>'sampleCount')::int, reason_code, selection_mode, event_type
		FROM payment_route_decisions WHERE payment_id = $1`, payment.ID).Scan(&target, &candidate, &score, &samples, &reason, &mode, &eventType); err != nil {
		t.Fatal(err)
	}
	if target != "BANK-A" || candidate != "candidate-a" || score != 0.75 || samples != 2 || reason != ReasonAdaptive || mode != string(SelectionModeAdaptive) || eventType != "PAYMENT_ROUTED" {
		t.Fatalf("route history mismatch: target=%q candidate=%q score=%v samples=%d reason=%q mode=%q eventType=%q",
			target, candidate, score, samples, reason, mode, eventType)
	}
}
