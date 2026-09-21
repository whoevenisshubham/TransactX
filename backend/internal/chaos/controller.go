package chaos

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrUnauthorized = errors.New("unauthorized: chaos operations require OPS_ADMIN role")
)

type Controller struct {
	mu             sync.RWMutex
	repo           Repository
	activeByTarget map[string]*ChaosScenario
	scenariosByID  map[string]*ChaosScenario
	targetInvocations map[string]int // tracks deterministic drop counts per target/scenario
	nowFunc        func() time.Time
	sleepFunc      func(ctx context.Context, d time.Duration) error
}

func NewController(repo Repository) *Controller {
	c := &Controller{
		repo:              repo,
		activeByTarget:    make(map[string]*ChaosScenario),
		scenariosByID:     make(map[string]*ChaosScenario),
		targetInvocations: make(map[string]int),
		nowFunc:           time.Now,
		sleepFunc: func(ctx context.Context, d time.Duration) error {
			if d <= 0 {
				return nil
			}
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
	return c
}

func (c *Controller) SetNowFunc(fn func() time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nowFunc = fn
}

func (c *Controller) SetSleepFunc(fn func(ctx context.Context, d time.Duration) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sleepFunc = fn
}

func (c *Controller) now() time.Time {
	if c.nowFunc != nil {
		return c.nowFunc()
	}
	return time.Now()
}

func (c *Controller) sleep(ctx context.Context, d time.Duration) error {
	if c.sleepFunc != nil {
		return c.sleepFunc(ctx, d)
	}
	if d <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

func (c *Controller) verifyRole(actorRole string) error {
	if actorRole != "OPS_ADMIN" {
		return ErrUnauthorized
	}
	return nil
}

func (c *Controller) Start(ctx context.Context, req StartRequest, actorID, actorRole string) (*ChaosScenario, error) {
	if err := c.verifyRole(actorRole); err != nil {
		return nil, err
	}

	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Normalize alias types
	if req.Type == ScenarioTypeTransient || req.Type == ScenarioTypeMessageDrop {
		req.Type = ScenarioTypeTransientDrop
	}

	now := c.now()
	duration := time.Duration(req.Parameters.DurationMs) * time.Millisecond
	expiresAt := now.Add(duration)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if existing target has active scenario, cleaning up if expired
	if existing, ok := c.activeByTarget[req.TargetID]; ok {
		if now.After(existing.ExpiresAt) {
			c.expireLocked(ctx, existing, now)
		} else if existing.Active {
			return nil, fmt.Errorf("%w: target %s has active scenario %s", ErrTargetConflict, req.TargetID, existing.ScenarioID)
		}
	}

	scenario := &ChaosScenario{
		ID:         uuid.New(),
		ScenarioID: req.ScenarioID,
		Type:       req.Type,
		TargetID:   req.TargetID,
		Parameters: req.Parameters,
		StartedAt:  now,
		ExpiresAt:  expiresAt,
		Active:     true,
		Mode:       ExecutionModeSimulation,
		CreatedBy:  actorID,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	c.activeByTarget[req.TargetID] = scenario
	c.scenariosByID[scenario.ScenarioID] = scenario
	c.targetInvocations[scenario.ScenarioID] = 0

	// Persist
	if c.repo != nil {
		_ = c.repo.CreateScenario(ctx, *scenario)
		_ = c.repo.RecordEvent(ctx, ChaosEvent{
			ScenarioID: scenario.ScenarioID,
			EventType:  EventTypeChaosStarted,
			TargetID:   scenario.TargetID,
			FaultType:  string(scenario.Type),
			ActorID:    actorID,
			ActorRole:  actorRole,
			Parameters: map[string]any{
				"durationMs":   req.Parameters.DurationMs,
				"latencyMs":    req.Parameters.LatencyMs,
				"dropCount":    req.Parameters.DropCount,
				"dropRate":     req.Parameters.DropRate,
				"errorMessage": req.Parameters.ErrorMessage,
			},
			Details: map[string]any{
				"mode":      ExecutionModeSimulation,
				"expiresAt": expiresAt.Format(time.RFC3339),
			},
			OccurredAt: now,
		})
	}

	copyScenario := *scenario
	return &copyScenario, nil
}

func (c *Controller) Stop(ctx context.Context, scenarioID string, actorID, actorRole string) (*ChaosScenario, error) {
	if err := c.verifyRole(actorRole); err != nil {
		return nil, err
	}

	if scenarioID == "" {
		return nil, ErrInvalidScenarioID
	}

	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()

	scenario, ok := c.scenariosByID[scenarioID]
	if !ok {
		// Attempt to load from repo if not in memory
		if c.repo != nil {
			persisted, err := c.repo.GetScenario(ctx, scenarioID)
			if err == nil && persisted != nil {
				scenario = persisted
				c.scenariosByID[scenarioID] = scenario
			}
		}
	}

	if scenario == nil {
		return nil, ErrScenarioNotFound
	}

	// Repeated stop is safe and idempotent
	if !scenario.Active {
		copyScenario := *scenario
		return &copyScenario, nil
	}

	scenario.Active = false
	scenario.StoppedAt = &now
	scenario.StoppedBy = &actorID
	scenario.UpdatedAt = now

	delete(c.activeByTarget, scenario.TargetID)

	if c.repo != nil {
		_ = c.repo.UpdateScenario(ctx, *scenario)
		_ = c.repo.RecordEvent(ctx, ChaosEvent{
			ScenarioID: scenario.ScenarioID,
			EventType:  EventTypeChaosStopped,
			TargetID:   scenario.TargetID,
			FaultType:  string(scenario.Type),
			ActorID:    actorID,
			ActorRole:  actorRole,
			Details: map[string]any{
				"stoppedAt": now.Format(time.RFC3339),
			},
			OccurredAt: now,
		})
	}

	copyScenario := *scenario
	return &copyScenario, nil
}

func (c *Controller) Reset(ctx context.Context, targetID string, actorID, actorRole string) error {
	if err := c.verifyRole(actorRole); err != nil {
		return err
	}

	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()

	var toStop []*ChaosScenario
	if targetID != "" {
		if s, ok := c.activeByTarget[targetID]; ok {
			toStop = append(toStop, s)
		}
	} else {
		for _, s := range c.activeByTarget {
			toStop = append(toStop, s)
		}
	}

	for _, s := range toStop {
		s.Active = false
		s.StoppedAt = &now
		s.StoppedBy = &actorID
		s.UpdatedAt = now
		delete(c.activeByTarget, s.TargetID)

		if c.repo != nil {
			_ = c.repo.UpdateScenario(ctx, *s)
			_ = c.repo.RecordEvent(ctx, ChaosEvent{
				ScenarioID: s.ScenarioID,
				EventType:  EventTypeChaosReset,
				TargetID:   s.TargetID,
				FaultType:  string(s.Type),
				ActorID:    actorID,
				ActorRole:  actorRole,
				Details: map[string]any{
					"resetAt": now.Format(time.RFC3339),
				},
				OccurredAt: now,
			})
		}
	}

	return nil
}

func (c *Controller) GetScenario(ctx context.Context, scenarioID string) (*ChaosScenario, error) {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()

	s, ok := c.scenariosByID[scenarioID]
	if !ok {
		if c.repo != nil {
			persisted, err := c.repo.GetScenario(ctx, scenarioID)
			if err != nil {
				return nil, err
			}
			s = persisted
			c.scenariosByID[scenarioID] = s
		} else {
			return nil, ErrScenarioNotFound
		}
	}

	if s.Active && now.After(s.ExpiresAt) {
		c.expireLocked(ctx, s, now)
	}

	copyScenario := *s
	return &copyScenario, nil
}

func (c *Controller) ListScenarios(ctx context.Context, activeOnly bool) ([]ChaosScenario, error) {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()

	// Expire any active scenarios that have passed expiresAt
	for _, s := range c.activeByTarget {
		if now.After(s.ExpiresAt) {
			c.expireLocked(ctx, s, now)
		}
	}

	if c.repo != nil {
		return c.repo.ListScenarios(ctx, activeOnly, 50)
	}

	var results []ChaosScenario
	for _, s := range c.scenariosByID {
		if activeOnly && !s.Active {
			continue
		}
		results = append(results, *s)
	}
	return results, nil
}

// GetActiveFault returns the active scenario for the given target if present and not expired.
// Scoped to targetID: Target A cannot return Target B's fault.
func (c *Controller) GetActiveFault(targetID string) (*ChaosScenario, bool) {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()

	s, ok := c.activeByTarget[targetID]
	if !ok || !s.Active {
		return nil, false
	}

	if now.After(s.ExpiresAt) {
		c.expireLocked(context.Background(), s, now)
		return nil, false
	}

	copyScenario := *s
	return &copyScenario, true
}

// CheckAndRecordInvocation returns whether a fault should be triggered on this invocation,
// handling drop counts deterministically.
func (c *Controller) CheckAndRecordInvocation(scenarioID string, params ScenarioParameters) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if params.DropCount > 0 {
		count := c.targetInvocations[scenarioID]
		if count < params.DropCount {
			c.targetInvocations[scenarioID] = count + 1
			return true // drop this one
		}
		return false // past drop count, let through
	}

	// If DropRate is set or default drop all
	if params.DropRate > 0 {
		if params.DropRate >= 1.0 {
			return true
		}
		// For deterministic behavior without randomness, alternate or count
		count := c.targetInvocations[scenarioID]
		c.targetInvocations[scenarioID] = count + 1
		// 0.5 rate drops every alternate (even counts)
		step := int(1.0 / params.DropRate)
		if step <= 1 || count%step == 0 {
			return true
		}
		return false
	}

	return true
}

func (c *Controller) expireLocked(ctx context.Context, s *ChaosScenario, now time.Time) {
	if !s.Active {
		return
	}
	s.Active = false
	exp := s.ExpiresAt
	s.StoppedAt = &exp
	systemActor := "SYSTEM_AUTO_EXPIRY"
	s.StoppedBy = &systemActor
	s.UpdatedAt = now
	delete(c.activeByTarget, s.TargetID)

	if c.repo != nil {
		_ = c.repo.UpdateScenario(ctx, *s)
		_ = c.repo.RecordEvent(ctx, ChaosEvent{
			ScenarioID: s.ScenarioID,
			EventType:  EventTypeChaosExpired,
			TargetID:   s.TargetID,
			FaultType:  string(s.Type),
			ActorID:    "SYSTEM",
			ActorRole:  "SYSTEM",
			Details: map[string]any{
				"expiredAt": s.ExpiresAt.Format(time.RFC3339),
			},
			OccurredAt: now,
		})
	}
}
