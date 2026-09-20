package payments

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/health"
)

type routingHealth struct {
	snapshots map[string]health.HealthSnapshot
}

func (provider routingHealth) GetSnapshot(_ context.Context, targetID string, _ time.Time) (health.HealthSnapshot, error) {
	return provider.snapshots[targetID], nil
}

func routeCandidate(id, target string, source, destination uuid.UUID) RouteCandidate {
	return RouteCandidate{CandidateID: id, SourceBankID: source, DestinationBankID: destination, ExecutionTargetID: target, SourceAdapter: successfulBankAdapter{}, DestinationAdapter: successfulBankAdapter{}}
}

func TestSelectRouteSingleCandidatePreservesOwnership(t *testing.T) {
	source, destination := uuid.New(), uuid.New()
	candidate := routeCandidate("only", "BANK-A", source, destination)
	decision, err := SelectRoute(context.Background(), []RouteCandidate{candidate}, SelectionModeAdaptive, routingHealth{map[string]health.HealthSnapshot{"BANK-A": {TargetID: "BANK-A", SampleCount: 1, AvailabilityScore: 1, Score: 0.8}}}, nil, time.Unix(1, 0))
	if err != nil || decision.Candidate != candidate || decision.Candidate.SourceBankID != source || decision.Candidate.DestinationBankID != destination {
		t.Fatalf("decision = %+v, err = %v", decision, err)
	}
}

func TestSelectRouteAdaptiveFiltersUnavailableAndUsesStableTieBreak(t *testing.T) {
	source, destination := uuid.New(), uuid.New()
	candidates := []RouteCandidate{routeCandidate("unhealthy", "BANK-Z", source, destination), routeCandidate("second", "BANK-B", source, destination), routeCandidate("first", "BANK-A", source, destination)}
	provider := routingHealth{map[string]health.HealthSnapshot{
		"BANK-Z": {TargetID: "BANK-Z", SampleCount: 1, AvailabilityScore: 0, Score: 1},
		"BANK-A": {TargetID: "BANK-A", SampleCount: 1, AvailabilityScore: 1, Score: 0.5},
		"BANK-B": {TargetID: "BANK-B", SampleCount: 1, AvailabilityScore: 1, Score: 0.5},
	}}
	decision, err := SelectRoute(context.Background(), candidates, SelectionModeAdaptive, provider, nil, time.Unix(1, 0))
	if err != nil || decision.Candidate.ExecutionTargetID != "BANK-A" || decision.Reason != ReasonAdaptive {
		t.Fatalf("decision = %+v, err = %v", decision, err)
	}
	for range 5 {
		repeated, repeatErr := SelectRoute(context.Background(), candidates, SelectionModeAdaptive, provider, nil, time.Unix(1, 0))
		if repeatErr != nil || repeated != decision {
			t.Fatalf("non-deterministic decision: %+v, %v", repeated, repeatErr)
		}
	}
}

func TestSelectRouteStaticIsDeterministic(t *testing.T) {
	source, destination := uuid.New(), uuid.New()
	candidates := []RouteCandidate{routeCandidate("z", "BANK-Z", source, destination), routeCandidate("a", "BANK-A", source, destination)}
	decision, err := SelectRoute(context.Background(), candidates, SelectionModeStatic, nil, nil, time.Unix(1, 0))
	if err != nil || decision.Candidate.CandidateID != "a" || decision.Reason != ReasonStatic {
		t.Fatalf("decision = %+v, err = %v", decision, err)
	}
}

func TestSelectRouteRejectsNoEligibleCandidate(t *testing.T) {
	source, destination := uuid.New(), uuid.New()
	_, err := SelectRoute(context.Background(), []RouteCandidate{routeCandidate("only", "BANK-A", source, destination)}, SelectionModeAdaptive, routingHealth{map[string]health.HealthSnapshot{"BANK-A": {SampleCount: 1}}}, nil, time.Unix(1, 0))
	if err != ErrNoRouteCandidate {
		t.Fatalf("err = %v, want %v", err, ErrNoRouteCandidate)
	}
}

func TestRouteDecisionPersistsTargetAndHealthSnapshot(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set")
	}
	data := newRepositoryTestData(t)
	defer data.close(t)
	decision := RouteDecision{Candidate: routeCandidate("candidate-a", "BANK-A", data.bankID, data.bankID), Score: 0.75, Snapshot: health.HealthSnapshot{TargetID: "BANK-A", SampleCount: 2, AvailabilityScore: 1, Score: 0.75}, Reason: ReasonAdaptive, Mode: SelectionModeAdaptive, SelectedAt: time.Now().UTC()}
	payment, duplicate, err := data.repository.CreateRoutedWithDecisionIdempotent(context.Background(), settlementPayment(data, 100), "route-history", "route-history-hash", decision)
	if err != nil || duplicate {
		t.Fatalf("create = %+v, duplicate=%v, err=%v", payment, duplicate, err)
	}
	var target, candidate string
	var score float64
	var samples int
	if err := data.pool.QueryRow(context.Background(), `SELECT execution_target_id, candidate_id, selected_score, (health_snapshot->>'sampleCount')::int FROM payment_route_decisions WHERE payment_id = $1`, payment.ID).Scan(&target, &candidate, &score, &samples); err != nil {
		t.Fatal(err)
	}
	if target != "BANK-A" || candidate != "candidate-a" || score != 0.75 || samples != 2 {
		t.Fatalf("route history = %q %q %v %d", target, candidate, score, samples)
	}
}
