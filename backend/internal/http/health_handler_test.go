package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/health"
)

func TestHealthSnapshotRejectsPublicRoles(t *testing.T) {
	manager, err := auth.NewJWTManager(strings.Repeat("h", 32), "health-test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	service := health.NewService(nil, health.DefaultConfig())
	handler := NewHandlerWithHealth(nil, nil, nil, manager, service)
	for _, role := range []string{"CUSTOMER", "MERCHANT"} {
		t.Run(role, func(t *testing.T) {
			token, issueErr := manager.Issue("11111111-1111-4111-8111-111111111111", role, time.Now())
			if issueErr != nil {
				t.Fatal(issueErr)
			}
			request := httptest.NewRequest(http.MethodGet, "/api/ops/health/BANK-A", nil)
			request.Header.Set("Authorization", "Bearer "+token)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
			}
		})
	}
}

func TestHealthSnapshotRejectsMalformedTarget(t *testing.T) {
	manager, err := auth.NewJWTManager(strings.Repeat("h", 32), "health-test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, err := manager.Issue("11111111-1111-4111-8111-111111111111", "OPS_ADMIN", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithHealth(nil, nil, nil, manager, health.NewService(nil, health.DefaultConfig()))
	request := httptest.NewRequest(http.MethodGet, "/api/ops/health/%20", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}
