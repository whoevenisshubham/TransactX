package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/health"
)

type healthTestRepository struct{ samples []health.HealthSample }

func (repository *healthTestRepository) Record(_ context.Context, sample health.HealthSample) error {
	repository.samples = append(repository.samples, sample)
	return nil
}

func (repository *healthTestRepository) ListRecent(context.Context, string, time.Time, time.Time, int) ([]health.HealthSample, error) {
	return nil, nil
}

type healthTestChecker struct{}

func (healthTestChecker) GetHealth(context.Context) (bank.HealthResult, error) {
	return bank.HealthResult{Available: true}, nil
}

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
			request := httptest.NewRequest(http.MethodPost, "/api/ops/health/BANK-A/sample", nil)
			request.Header.Set("Authorization", "Bearer "+token)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
			}
		})
	}
}

func TestHealthSampleAllowsOpsAdmin(t *testing.T) {
	manager, err := auth.NewJWTManager(strings.Repeat("h", 32), "health-test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, err := manager.Issue("11111111-1111-4111-8111-111111111111", "OPS_ADMIN", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	repository := &healthTestRepository{}
	service := health.NewService(repository, health.DefaultConfig())
	handler := NewHandlerWithBankAdaptersAndHealthTargets(nil, nil, nil, manager, nil, map[string]health.HealthChecker{"BANK-A": healthTestChecker{}}, service)
	request := httptest.NewRequest(http.MethodPost, "/api/ops/health/BANK-A/sample", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Request-ID", "health-sample-request")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if len(repository.samples) != 1 || repository.samples[0].TargetID != "BANK-A" || repository.samples[0].CorrelationID != "health-sample-request" {
		t.Fatalf("unexpected persisted samples: %+v", repository.samples)
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
