package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/reconciliation"
)

func makeIntegrityHandler(t *testing.T) (http.Handler, *auth.JWTManager, *reconciliation.MemoryFinancialDataStore, *reconciliation.MemoryIntegrityRunStore) {
	t.Helper()
	manager, err := auth.NewJWTManager(strings.Repeat("i", 32), "integrity-test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	dataStore := reconciliation.NewMemoryFinancialDataStore()
	runStore := reconciliation.NewMemoryIntegrityRunStore()
	engine := reconciliation.NewRuntimeIntegrityEngine(dataStore, runStore)

	handler := &Handler{
		integrityEngine:   engine,
		integrityRunStore: runStore,
	}

	mux := http.NewServeMux()
	mux.Handle("POST /api/ops/integrity/runs", auth.Authentication(manager, auth.RequireRole("OPS_ADMIN")(http.HandlerFunc(handler.integrityCreateRun))))
	mux.Handle("GET /api/ops/integrity/runs", auth.Authentication(manager, auth.RequireRole("OPS_ADMIN")(http.HandlerFunc(handler.integrityListRuns))))
	mux.Handle("GET /api/ops/integrity/runs/{runID}", auth.Authentication(manager, auth.RequireRole("OPS_ADMIN")(http.HandlerFunc(handler.integrityGetRun))))
	mux.Handle("GET /api/ops/integrity/checks", auth.Authentication(manager, auth.RequireRole("OPS_ADMIN")(http.HandlerFunc(handler.integrityListChecks))))

	return mux, manager, dataStore, runStore
}

func TestIntegrityEndpointsRejectPublicRoles(t *testing.T) {
	mux, manager, _, _ := makeIntegrityHandler(t)

	endpoints := []struct {
		method string
		path   string
		body   []byte
	}{
		{http.MethodPost, "/api/ops/integrity/runs", []byte("{}")},
		{http.MethodGet, "/api/ops/integrity/runs", nil},
		{http.MethodGet, "/api/ops/integrity/runs/" + uuid.New().String(), nil},
		{http.MethodGet, "/api/ops/integrity/checks", nil},
	}

	// 1. Unauthenticated -> 401
	for _, ep := range endpoints {
		t.Run("Unauthenticated_"+ep.method+"_"+ep.path, func(t *testing.T) {
			var bodyReader *bytes.Reader
			if ep.body != nil {
				bodyReader = bytes.NewReader(ep.body)
			} else {
				bodyReader = bytes.NewReader(nil)
			}
			req := httptest.NewRequest(ep.method, ep.path, bodyReader)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}

	// 2. CUSTOMER and MERCHANT -> 403
	for _, role := range []string{"CUSTOMER", "MERCHANT"} {
		token := tokenFor(t, manager, role)
		for _, ep := range endpoints {
			t.Run(role+"_"+ep.method+"_"+ep.path, func(t *testing.T) {
				var bodyReader *bytes.Reader
				if ep.body != nil {
					bodyReader = bytes.NewReader(ep.body)
				} else {
					bodyReader = bytes.NewReader(nil)
				}
				req := httptest.NewRequest(ep.method, ep.path, bodyReader)
				req.Header.Set("Authorization", "Bearer "+token)
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
				if rec.Code != http.StatusForbidden {
					t.Fatalf("expected 403 for %s on %s %s, got %d", role, ep.method, ep.path, rec.Code)
				}
			})
		}
	}
}

func TestIntegrityCreateRunOpsAdmin(t *testing.T) {
	mux, manager, _, _ := makeIntegrityHandler(t)
	token := tokenFor(t, manager, "OPS_ADMIN")

	req := httptest.NewRequest(http.MethodPost, "/api/ops/integrity/runs", bytes.NewReader([]byte("{}")))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Data["status"] != "COMPLETED" {
		t.Fatalf("expected status COMPLETED, got %v", resp.Data["status"])
	}
	checks, ok := resp.Data["checks"].([]any)
	if !ok || len(checks) != len(reconciliation.AuthoritativeCheckRegistry) {
		t.Fatalf("expected %d check results, got %v", len(reconciliation.AuthoritativeCheckRegistry), resp.Data["checks"])
	}
}

func TestIntegrityGetAndListRuns(t *testing.T) {
	mux, manager, _, _ := makeIntegrityHandler(t)
	token := tokenFor(t, manager, "OPS_ADMIN")

	// Create run
	createReq := httptest.NewRequest(http.MethodPost, "/api/ops/integrity/runs", bytes.NewReader([]byte("{}")))
	createReq.Header.Set("Authorization", "Bearer "+token)
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create run failed: %d %s", createRec.Code, createRec.Body.String())
	}

	var createResp struct {
		Data map[string]any `json:"data"`
	}
	_ = json.NewDecoder(createRec.Body).Decode(&createResp)
	runID := createResp.Data["runId"].(string)

	// Get run by ID
	getReq := httptest.NewRequest(http.MethodGet, "/api/ops/integrity/runs/"+runID, nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}

	// List runs
	listReq := httptest.NewRequest(http.MethodGet, "/api/ops/integrity/runs", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", listRec.Code, listRec.Body.String())
	}

	var listResp struct {
		Data map[string]any `json:"data"`
	}
	_ = json.NewDecoder(listRec.Body).Decode(&listResp)
	items, ok := listResp.Data["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected 1 item in list, got %v", listResp.Data["items"])
	}
}

func TestIntegrityListChecks(t *testing.T) {
	mux, manager, _, _ := makeIntegrityHandler(t)
	token := tokenFor(t, manager, "OPS_ADMIN")

	req := httptest.NewRequest(http.MethodGet, "/api/ops/integrity/checks", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Data []reconciliation.IntegrityCheckDefinition `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode checks response: %v", err)
	}

	if len(resp.Data) != len(reconciliation.AuthoritativeCheckRegistry) {
		t.Fatalf("expected %d check definitions, got %d", len(reconciliation.AuthoritativeCheckRegistry), len(resp.Data))
	}
}
