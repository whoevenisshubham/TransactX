package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/circuit"
	"github.com/transactx/backend/internal/payments"
)

func TestCircuitEndpointsRejectPublicRoles(t *testing.T) {
	manager, err := auth.NewJWTManager(strings.Repeat("c", 32), "circuit-test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	cb, _ := circuit.NewBreaker(circuit.DefaultConfig())
	handler := NewHandlerWithExecutionTargetsAndCircuit(nil, nil, nil, manager, nil, nil, nil, nil, payments.SelectionModeAdaptive, "", cb)

	endpoints := []string{
		"/api/ops/circuit",
		"/api/ops/circuit/RAIL-A",
		"/api/ops/circuit/RAIL-A/events",
	}

	for _, role := range []string{"CUSTOMER", "MERCHANT"} {
		token, issueErr := manager.Issue("11111111-1111-4111-8111-111111111111", role, time.Now())
		if issueErr != nil {
			t.Fatal(issueErr)
		}
		for _, ep := range endpoints {
			t.Run(role+"_"+ep, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, ep, nil)
				req.Header.Set("Authorization", "Bearer "+token)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusForbidden {
					t.Fatalf("expected 403 Forbidden for role %s on %s, got %d", role, ep, rec.Code)
				}
			})
		}
	}
}

func TestCircuitEndpointsAllowOpsAdmin(t *testing.T) {
	manager, err := auth.NewJWTManager(strings.Repeat("c", 32), "circuit-test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, err := manager.Issue("11111111-1111-4111-8111-111111111111", "OPS_ADMIN", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	cfg := circuit.DefaultConfig()
	cfg.FailureThreshold = 1
	cb, _ := circuit.NewBreaker(cfg)
	cb.RecordFailure("RAIL-A", "probe_failed", time.Now())

	handler := NewHandlerWithExecutionTargetsAndCircuit(nil, nil, nil, manager, nil, nil, nil, nil, payments.SelectionModeAdaptive, "", cb)

	t.Run("SnapshotsList", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/ops/circuit", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		var resp struct {
			Data map[string]circuit.TargetSnapshot `json:"data"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if snap, ok := resp.Data["RAIL-A"]; !ok || snap.State != circuit.StateOpen {
			t.Fatalf("expected RAIL-A in OPEN state, got: %+v", resp.Data)
		}
	})

	t.Run("TargetSnapshot", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/ops/circuit/RAIL-A", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		var resp struct {
			Data circuit.TargetSnapshot `json:"data"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp.Data.ExecutionTargetID != "RAIL-A" || resp.Data.State != circuit.StateOpen {
			t.Fatalf("unexpected snapshot: %+v", resp.Data)
		}
	})

	t.Run("TargetEvents", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/ops/circuit/RAIL-A/events", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		var resp struct {
			Data []circuit.TransitionEvent `json:"data"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(resp.Data) != 1 || resp.Data[0].NewState != circuit.StateOpen {
			t.Fatalf("unexpected events: %+v", resp.Data)
		}
	})
}

type mockCircuitRepo struct {
	events []circuit.TransitionEvent
	err    error
}

func (r *mockCircuitRepo) RecordTransition(ctx context.Context, event circuit.TransitionEvent) error {
	return nil
}

func (r *mockCircuitRepo) ListRecent(ctx context.Context, targetID string, limit int) ([]circuit.TransitionEvent, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.events, nil
}

func TestCircuitEventsRepositoryBackedAndErrorPropagation(t *testing.T) {
	manager, err := auth.NewJWTManager(strings.Repeat("c", 32), "circuit-test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, err := manager.Issue("11111111-1111-4111-8111-111111111111", "OPS_ADMIN", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	repo := &mockCircuitRepo{
		events: []circuit.TransitionEvent{
			{ID: 10, ExecutionTargetID: "RAIL-A", PreviousState: circuit.StateOpen, NewState: circuit.StateHalfOpen, Reason: circuit.ReasonCooldownExpired, TransitionedAt: time.Now()},
			{ID: 9, ExecutionTargetID: "RAIL-A", PreviousState: circuit.StateClosed, NewState: circuit.StateOpen, Reason: circuit.ReasonThresholdReached, TransitionedAt: time.Now().Add(-5 * time.Second)},
		},
	}
	cb, _ := circuit.NewBreaker(circuit.DefaultConfig(), repo)
	handler := NewHandlerWithExecutionTargetsAndCircuit(nil, slog.Default(), nil, manager, nil, nil, nil, nil, payments.SelectionModeAdaptive, "", cb)

	t.Run("RepositorySuccess", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/ops/circuit/RAIL-A/events", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
		var resp struct {
			Data []circuit.TransitionEvent `json:"data"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(resp.Data) != 2 {
			t.Fatalf("expected 2 events from repository, got %d", len(resp.Data))
		}
		if resp.Data[0].ID != 10 || resp.Data[1].ID != 9 {
			t.Fatalf("expected ordering preserved, got IDs %d, %d", resp.Data[0].ID, resp.Data[1].ID)
		}
	})

	t.Run("RepositoryErrorPropagatedAs500", func(t *testing.T) {
		repo.err = errors.New("database connection failed")
		req := httptest.NewRequest(http.MethodGet, "/api/ops/circuit/RAIL-A/events", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 Internal Server Error, got %d", rec.Code)
		}
		var resp struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}
		if resp.Error.Code != "INTERNAL_ERROR" || !strings.Contains(resp.Error.Message, "failed to retrieve circuit events") {
			t.Fatalf("unexpected error response: %+v", resp)
		}
	})
}
