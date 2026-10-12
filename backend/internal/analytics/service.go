package analytics

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Service coordinates window aggregation and analytics queries.
type Service struct {
	repo             Repository
	entities         []EntityTarget
	logger           *slog.Logger
	mu               sync.Mutex
	aggregationAge   time.Duration
	lastDuration     time.Duration
	windowsProcessed int64
}

// EntityTarget represents an aggregation subject (e.g. a bank or execution target).
type EntityTarget struct {
	Type string // "bank" | "execution_target"
	ID   string // "BANK_A", "BANK_B", etc.
}

// NewService constructs an Analytics Service.
func NewService(repo Repository, entities []EntityTarget, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if len(entities) == 0 {
		entities = []EntityTarget{
			{Type: "bank", ID: "BANK_A"},
			{Type: "bank", ID: "BANK_B"},
		}
	}
	return &Service{
		repo:     repo,
		entities: entities,
		logger:   logger.With("component", "analytics"),
	}
}

// AddEntity appends an entity target for window aggregation.
func (s *Service) AddEntity(entityType, entityID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entities {
		if e.Type == entityType && e.ID == entityID {
			return
		}
	}
	s.entities = append(s.entities, EntityTarget{Type: entityType, ID: entityID})
}

// AggregateCompletedWindows runs the aggregation loop for all completed 5-minute windows
// that have elapsed since the stored cursor up to the latest completed window boundary.
func (s *Service) AggregateCompletedWindows(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	startTime := time.Now()
	now := startTime.UTC()
	latestCompleted := now.Truncate(WindowDuration)

	cursor, err := s.repo.GetCursor(ctx, CursorNameFiveMinuteAggregator)
	if err != nil {
		return 0, fmt.Errorf("read aggregate cursor: %w", err)
	}

	// If no cursor recorded, or cursor is older than 2 hours, align cursor to the window prior to latestCompleted
	if cursor.IsZero() || latestCompleted.Sub(cursor) > 2*time.Hour {
		cursor = latestCompleted.Add(-WindowDuration)
	} else {
		cursor = cursor.Truncate(WindowDuration)
	}

	var processedCount int

	for cursor.Before(latestCompleted) {
		select {
		case <-ctx.Done():
			return processedCount, ctx.Err()
		default:
		}

		windowFrom := cursor
		windowTo := cursor.Add(WindowDuration)

		for _, entity := range s.entities {
			win, raw, err := s.repo.AggregateWindow(ctx, entity.Type, entity.ID, windowFrom, windowTo)
			if err != nil {
				s.logger.Warn("aggregate window failed",
					"entity_type", entity.Type,
					"entity_id", entity.ID,
					"from", windowFrom,
					"to", windowTo,
					"error", err,
				)
				continue
			}

			if err := s.repo.UpsertWindow(ctx, win, raw); err != nil {
				s.logger.Error("upsert aggregate window failed",
					"entity_type", entity.Type,
					"entity_id", entity.ID,
					"from", windowFrom,
					"to", windowTo,
					"error", err,
				)
				return processedCount, fmt.Errorf("upsert aggregate window: %w", err)
			}
			processedCount++
		}

		cursor = windowTo
		if err := s.repo.SetCursor(ctx, CursorNameFiveMinuteAggregator, cursor); err != nil {
			s.logger.Error("update cursor failed", "cursor", cursor, "error", err)
			return processedCount, fmt.Errorf("update cursor: %w", err)
		}
	}

	duration := time.Since(startTime)
	s.lastDuration = duration
	s.windowsProcessed += int64(processedCount)
	if !cursor.IsZero() {
		s.aggregationAge = now.Sub(cursor)
	}

	if processedCount > 0 {
		s.logger.Info("completed aggregate windows",
			"processed", processedCount,
			"cursor", cursor,
			"duration_ms", duration.Milliseconds(),
		)
	}

	return processedCount, nil
}

// QueryWindows delegates to repository.
func (s *Service) QueryWindows(ctx context.Context, filter WindowFilter) (*AnalyticsPage, error) {
	return s.repo.QueryWindows(ctx, filter)
}

// GetMetricsSummary delegates to repository.
func (s *Service) GetMetricsSummary(ctx context.Context, asOf time.Time) (*MetricsSummary, error) {
	return s.repo.ComputeMetricsSummary(ctx, asOf)
}

// AggregationMetrics returns operational age and duration metrics for the aggregation loop.
func (s *Service) AggregationMetrics() (age time.Duration, duration time.Duration, totalProcessed int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.aggregationAge, s.lastDuration, s.windowsProcessed
}
