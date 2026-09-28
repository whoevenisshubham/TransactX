package payments

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/health"
)

type SelectionMode string

const (
	SelectionModeStatic   SelectionMode = "STATIC"
	SelectionModeAdaptive SelectionMode = "ADAPTIVE"

	ReasonStatic           = "STATIC_CANDIDATE"
	ReasonAdaptive         = "ADAPTIVE_HEALTH_SCORE"
	ReasonNoEligibleTarget = "NO_ELIGIBLE_CANDIDATE"
)

var ErrNoRouteCandidate = errors.New("no eligible route candidate")

// ExecutionTarget represents a configured switch-level route/rail/endpoint execution path
// between participant adapters for a given logical route.
type ExecutionTarget struct {
	CandidateID        string
	ExecutionTargetID  string
	SourceAdapter      bank.BankAdapter
	DestinationAdapter bank.BankAdapter
}

// RouteKey identifies the logical sender bank and receiver bank for a routed payment.
type RouteKey struct {
	SourceBankID      uuid.UUID
	DestinationBankID uuid.UUID
}

// ToCandidate creates a RouteCandidate preserving the logical bank ownership.
func (t ExecutionTarget) ToCandidate(sourceBankID, destinationBankID uuid.UUID) RouteCandidate {
	return RouteCandidate{
		CandidateID:        t.CandidateID,
		SourceBankID:       sourceBankID,
		DestinationBankID:  destinationBankID,
		ExecutionTargetID:  t.ExecutionTargetID,
		SourceAdapter:      t.SourceAdapter,
		DestinationAdapter: t.DestinationAdapter,
	}
}

type RouteCandidate struct {
	CandidateID        string
	SourceBankID       uuid.UUID
	DestinationBankID  uuid.UUID
	ExecutionTargetID  string
	SourceAdapter      bank.BankAdapter
	DestinationAdapter bank.BankAdapter
}

type HealthSnapshotProvider interface {
	GetSnapshot(context.Context, string, time.Time) (health.HealthSnapshot, error)
}

// CircuitEligibility is intentionally optional until M2-5 owns circuit state.
type CircuitEligibility func(context.Context, RouteCandidate) bool

type RouteDecision struct {
	Candidate  RouteCandidate
	Score      float64
	Snapshot   health.HealthSnapshot
	Reason     string
	Mode       SelectionMode
	SelectedAt time.Time
}

func SelectRoute(ctx context.Context, candidates []RouteCandidate, mode SelectionMode, snapshots HealthSnapshotProvider, eligible CircuitEligibility, now time.Time, staticBaseline ...string) (RouteDecision, error) {
	baseline := ""
	if len(staticBaseline) > 0 {
		baseline = staticBaseline[0]
	}

	decisions := make([]RouteDecision, 0, len(candidates))
	for _, candidate := range candidates {
		if !validCandidate(candidate) || (eligible != nil && !eligible(ctx, candidate)) {
			continue
		}
		decision := RouteDecision{Candidate: candidate, Mode: mode, SelectedAt: now}
		if snapshots != nil {
			snapshot, err := snapshots.GetSnapshot(ctx, candidate.ExecutionTargetID, now)
			if err != nil {
				continue
			}
			if snapshot.SampleCount > 0 && (snapshot.AvailabilityScore == 0 || snapshot.Score <= 0) {
				continue
			}
			decision.Snapshot = snapshot
			decision.Score = snapshot.Score
		}
		decisions = append(decisions, decision)
	}
	if len(decisions) == 0 {
		return RouteDecision{Mode: mode, Reason: ReasonNoEligibleTarget, SelectedAt: now}, ErrNoRouteCandidate
	}
	if mode == SelectionModeStatic {
		if baseline != "" {
			var matching []RouteDecision
			for _, d := range decisions {
				if d.Candidate.CandidateID == baseline {
					matching = append(matching, d)
				}
			}
			if len(matching) == 0 {
				return RouteDecision{Mode: mode, Reason: ReasonNoEligibleTarget, SelectedAt: now}, ErrNoRouteCandidate
			}
			sort.Slice(matching, func(left, right int) bool {
				return matching[left].Candidate.ExecutionTargetID < matching[right].Candidate.ExecutionTargetID
			})
			matching[0].Reason = ReasonStatic
			return matching[0], nil
		}

		sort.Slice(decisions, func(left, right int) bool {
			if decisions[left].Candidate.CandidateID == decisions[right].Candidate.CandidateID {
				return decisions[left].Candidate.ExecutionTargetID < decisions[right].Candidate.ExecutionTargetID
			}
			return decisions[left].Candidate.CandidateID < decisions[right].Candidate.CandidateID
		})
		decisions[0].Reason = ReasonStatic
		return decisions[0], nil
	}
	sort.Slice(decisions, func(left, right int) bool {
		if decisions[left].Score == decisions[right].Score {
			if decisions[left].Candidate.ExecutionTargetID == decisions[right].Candidate.ExecutionTargetID {
				return decisions[left].Candidate.CandidateID < decisions[right].Candidate.CandidateID
			}
			return decisions[left].Candidate.ExecutionTargetID < decisions[right].Candidate.ExecutionTargetID
		}
		return decisions[left].Score > decisions[right].Score
	})
	decisions[0].Reason = ReasonAdaptive
	return decisions[0], nil
}

func validCandidate(candidate RouteCandidate) bool {
	return candidate.CandidateID != "" && candidate.SourceBankID != uuid.Nil && candidate.DestinationBankID != uuid.Nil && candidate.ExecutionTargetID != "" && candidate.SourceAdapter != nil && candidate.DestinationAdapter != nil
}
