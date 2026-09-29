package http

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/transactx/backend/internal/reconciliation"
)

// corruptionFixtureRuns is request-local evidence storage for the isolated demo.
type corruptionFixtureRuns struct {
	run           reconciliation.Run
	discrepancies []reconciliation.Discrepancy
}

func (s *corruptionFixtureRuns) CreateRun(_ context.Context, participantID string, scope reconciliation.Scope) (reconciliation.Run, error) {
	s.run = reconciliation.Run{ID: uuid.New(), ParticipantID: participantID, ScopeFrom: scope.From, ScopeTo: scope.To, Status: reconciliation.RunStatusRunning, StartedAt: time.Now().UTC()}
	return s.run, nil
}
func (s *corruptionFixtureRuns) CompleteRun(_ context.Context, id uuid.UUID, canonicalRoot, participantRoot []byte, canonicalVersion, algorithmVersion string, recordCount, discrepancyCount int64, metrics reconciliation.RunMetrics) (reconciliation.Run, error) {
	if id != s.run.ID {
		return reconciliation.Run{}, reconciliation.ErrRunNotFound
	}
	now := time.Now().UTC()
	s.run.Status = reconciliation.RunStatusCompleted
	s.run.CanonicalRoot = canonicalRoot
	s.run.ParticipantRoot = participantRoot
	s.run.CanonicalVersion = canonicalVersion
	s.run.AlgorithmVersion = algorithmVersion
	s.run.RecordCount = recordCount
	s.run.DiscrepancyCount = discrepancyCount
	s.run.ElapsedNs = metrics.ElapsedNs
	s.run.NodesVisited = metrics.NodesVisited
	s.run.RecordsInspected = metrics.RecordsInspected
	s.run.BytesExamined = metrics.BytesExamined
	s.run.DivergentBuckets = metrics.DivergentBuckets
	s.run.DivergentRecords = metrics.DivergentRecords
	s.run.CompletedAt = &now
	return s.run, nil
}
func (s *corruptionFixtureRuns) FailRun(_ context.Context, id uuid.UUID, message string) (reconciliation.Run, error) {
	if id != s.run.ID {
		return reconciliation.Run{}, reconciliation.ErrRunNotFound
	}
	now := time.Now().UTC()
	s.run.Status = reconciliation.RunStatusFailed
	s.run.ErrorMessage = message
	s.run.CompletedAt = &now
	return s.run, nil
}
func (s *corruptionFixtureRuns) GetRun(_ context.Context, id uuid.UUID) (reconciliation.Run, error) {
	if id != s.run.ID {
		return reconciliation.Run{}, reconciliation.ErrRunNotFound
	}
	return s.run, nil
}
func (s *corruptionFixtureRuns) ListRuns(_ context.Context, _ reconciliation.ListRunsRequest) (reconciliation.RunListPage, error) {
	return reconciliation.RunListPage{Items: []reconciliation.Run{s.run}, Total: 1, Limit: 1}, nil
}
func (s *corruptionFixtureRuns) SaveDiscrepancy(_ context.Context, disc reconciliation.Discrepancy) (reconciliation.Discrepancy, error) {
	disc.ID = uuid.New()
	s.discrepancies = append(s.discrepancies, disc)
	return disc, nil
}
func (s *corruptionFixtureRuns) ListDiscrepancies(_ context.Context, req reconciliation.ListDiscrepanciesRequest) (reconciliation.DiscrepancyListPage, error) {
	if req.RunID != s.run.ID {
		return reconciliation.DiscrepancyListPage{}, reconciliation.ErrRunNotFound
	}
	return reconciliation.DiscrepancyListPage{Items: s.discrepancies, Total: len(s.discrepancies), Limit: len(s.discrepancies)}, nil
}
