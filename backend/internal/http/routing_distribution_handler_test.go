package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRoutingDistributionRequiresOpsAdmin(t *testing.T) {
	handler, manager := makeReconHandler(t, []string{"BANK-A"}, newMemReconStore())
	for _, tc := range []struct {
		role   string
		status int
	}{
		{"", http.StatusUnauthorized},
		{"CUSTOMER", http.StatusForbidden},
		{"MERCHANT", http.StatusForbidden},
		{"OPS_ADMIN", http.StatusNotFound}, // No database in this isolated handler test.
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/ops/routing/distribution", nil)
		if tc.role != "" {
			req.Header.Set("Authorization", "Bearer "+tokenFor(t, manager, tc.role))
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != tc.status {
			t.Fatalf("role %s: got %d: %s", tc.role, res.Code, res.Body.String())
		}
	}
}
