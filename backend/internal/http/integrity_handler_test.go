package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/reconciliation"
	"github.com/transactx/backend/internal/users"
)

func TestIntegrityEndpointsAuthorizationAndHistory(t *testing.T) {
	manager, err := auth.NewJWTManager(strings.Repeat("i", 32), "integrity-test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	store := reconciliation.NewMemoryIntegrityRunStore()
	handler := &Handler{integrityRuns: store, integrityEngine: reconciliation.NewRuntimeIntegrityEngine(reconciliation.NewMemoryFinancialDataStore(), store)}
	mux := http.NewServeMux()
	admin := func(h http.HandlerFunc) http.Handler {
		return auth.Authentication(manager, auth.RequireRole(users.RoleOpsAdmin)(h))
	}
	mux.Handle("POST /api/ops/integrity/check", admin(handler.integrityCheck))
	mux.Handle("GET /api/ops/integrity/status", admin(handler.integrityStatus))
	mux.Handle("GET /api/ops/integrity/runs/{runID}", admin(handler.integrityRun))
	for _, tc := range []struct {
		role   string
		status int
	}{
		{"", http.StatusUnauthorized}, {"CUSTOMER", http.StatusForbidden}, {"MERCHANT", http.StatusForbidden}, {"OPS_ADMIN", http.StatusCreated},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/ops/integrity/check", strings.NewReader(`{}`))
		if tc.role != "" {
			req.Header.Set("Authorization", "Bearer "+tokenFor(t, manager, tc.role))
		}
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != tc.status {
			t.Fatalf("role %s: got %d: %s", tc.role, res.Code, res.Body.String())
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/ops/integrity/status", nil)
	req.Header.Set("Authorization", "Bearer "+tokenFor(t, manager, "OPS_ADMIN"))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"total":1`) {
		t.Fatalf("history missing: %d %s", res.Code, res.Body.String())
	}
}
