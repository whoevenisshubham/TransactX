package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/chaos"
)

func TestChaosEndpointsRejectPublicRoles(t *testing.T) {
	manager, err := auth.NewJWTManager(strings.Repeat("c", 32), "chaos-test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	ctrl := chaos.NewController(nil)
	handler := NewHandlerWithChaos(nil, nil, nil, manager, ctrl)

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/ops/chaos/start"},
		{http.MethodGet, "/api/ops/chaos/scenarios"},
		{http.MethodGet, "/api/ops/chaos/scenarios/sc-1"},
		{http.MethodPost, "/api/ops/chaos/scenarios/sc-1/stop"},
		{http.MethodPost, "/api/ops/chaos/reset"},
	}

	// 1. Unauthenticated (no bearer token) -> 401 Unauthorized
	for _, ep := range endpoints {
		t.Run("Unauthenticated_"+ep.method+"_"+ep.path, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, strings.NewReader(`{}`))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
			}
		})
	}

	// 2. CUSTOMER and MERCHANT -> 403 Forbidden
	for _, role := range []string{"CUSTOMER", "MERCHANT"} {
		token, issueErr := manager.Issue("11111111-1111-4111-8111-111111111111", role, time.Now())
		if issueErr != nil {
			t.Fatal(issueErr)
		}
		for _, ep := range endpoints {
			t.Run(role+"_"+ep.method+"_"+ep.path, func(t *testing.T) {
				req := httptest.NewRequest(ep.method, ep.path, strings.NewReader(`{}`))
				req.Header.Set("Authorization", "Bearer "+token)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusForbidden {
					t.Fatalf("expected 403 Forbidden for role %s on %s %s, got %d", role, ep.method, ep.path, rec.Code)
				}
			})
		}
	}
}

func TestChaosEndpointsAllowOpsAdmin(t *testing.T) {
	manager, err := auth.NewJWTManager(strings.Repeat("c", 32), "chaos-test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, err := manager.Issue("11111111-1111-4111-8111-111111111111", "OPS_ADMIN", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	ctrl := chaos.NewController(nil)
	handler := NewHandlerWithChaos(nil, nil, nil, manager, ctrl)

	// 1. POST /api/ops/chaos/start
	t.Run("StartScenario", func(t *testing.T) {
		body, _ := json.Marshal(chaos.StartRequest{
			ScenarioID: "sc-http-1",
			Type:       chaos.ScenarioTypeBankOutage,
			TargetID:   "RAIL-A",
			Parameters: chaos.ScenarioParameters{DurationMs: 10000},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/ops/chaos/start", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp struct {
			Data chaos.ChaosScenario `json:"data"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode start response: %v", err)
		}
		if resp.Data.ScenarioID != "sc-http-1" || !resp.Data.Active {
			t.Fatalf("unexpected scenario returned: %+v", resp.Data)
		}
	})

	// 2. GET /api/ops/chaos/scenarios
	t.Run("ListScenarios", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/ops/chaos/scenarios", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp struct {
			Data []chaos.ChaosScenario `json:"data"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode list response: %v", err)
		}
		if len(resp.Data) == 0 {
			t.Fatalf("expected at least 1 scenario, got 0")
		}
	})

	// 3. GET /api/ops/chaos/scenarios/sc-http-1
	t.Run("GetScenario", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/ops/chaos/scenarios/sc-http-1", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp struct {
			Data chaos.ChaosScenario `json:"data"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode get response: %v", err)
		}
		if resp.Data.ScenarioID != "sc-http-1" {
			t.Fatalf("expected scenario sc-http-1, got %s", resp.Data.ScenarioID)
		}
	})

	// 4. POST /api/ops/chaos/scenarios/sc-http-1/stop
	t.Run("StopScenario", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/ops/chaos/scenarios/sc-http-1/stop", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp struct {
			Data chaos.ChaosScenario `json:"data"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode stop response: %v", err)
		}
		if resp.Data.Active {
			t.Fatalf("expected stopped scenario to be inactive")
		}
	})

	// 5. POST /api/ops/chaos/reset
	t.Run("ResetChaos", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/ops/chaos/reset", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}
