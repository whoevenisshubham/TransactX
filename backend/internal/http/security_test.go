package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/audit"
	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/bankservice"
	"github.com/transactx/backend/internal/chaos"
	"github.com/transactx/backend/internal/circuit"
	"github.com/transactx/backend/internal/config"
	"github.com/transactx/backend/internal/database"
	"github.com/transactx/backend/internal/reconciliation"
	"github.com/transactx/backend/internal/users"
)

func TestRoleMatrix_OperationalEndpoints(t *testing.T) {
	jwtManager, err := auth.NewJWTManager(strings.Repeat("a", 32), "security-test", time.Hour)
	if err != nil {
		t.Fatalf("failed to create JWT manager: %v", err)
	}

	var pool *pgxpool.Pool
	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		pool, _ = database.NewPool(context.Background(), dbURL)
	}
	if pool == nil {
		t.Skip("DATABASE_URL not set or database unavailable")
	}
	defer pool.Close()

	var currentDB string
	if err := pool.QueryRow(context.Background(), "SELECT current_database()").Scan(&currentDB); err != nil {
		t.Fatalf("failed to query current_database(): %v", err)
	}
	lower := strings.ToLower(strings.TrimSpace(currentDB))
	if lower == "transactx" || lower == "postgres" || lower == "template0" || lower == "template1" ||
		(!strings.HasPrefix(lower, "transactx_disposable_") && !strings.HasPrefix(lower, "transactx_test_") && !strings.HasPrefix(lower, "transactx_m2_disposable_") && !strings.HasPrefix(lower, "transactx_cb_restore_")) {
		t.Fatalf("refusing operational endpoints test against non-disposable database %q", currentDB)
	}

	ctrl := chaos.NewController(nil)
	cb, _ := circuit.NewBreaker(circuit.DefaultConfig())
	var reconEngine *reconciliation.Engine
	if pool != nil {
		reconRepo := reconciliation.NewRunRepository(pool)
		reconEngine = reconciliation.NewEngine(reconciliation.KnownParticipants{"BANK-A": true}, reconRepo, nil, nil)
	}

	handler := NewHandlerWithExecutionTargetsCircuitChaosReconciliationAndRuntimeOptions(
		pool, nil, nil, jwtManager, nil, nil, nil, nil, "", "", cb, ctrl, reconEngine,
		RuntimeOptions{
			SimulationMode:         true,
			AllowRegistration:      true,
			ChaosEnabled:           true,
			RecoveryControlEnabled: true,
		},
	)

	adminToken, err := jwtManager.Issue(uuid.NewString(), users.RoleOpsAdmin, time.Now())
	if err != nil {
		t.Fatalf("failed to issue admin token: %v", err)
	}
	viewerToken, err := jwtManager.Issue(uuid.NewString(), users.RoleOpsViewer, time.Now())
	if err != nil {
		t.Fatalf("failed to issue viewer token: %v", err)
	}
	customerToken, err := jwtManager.Issue(uuid.NewString(), users.RoleCustomer, time.Now())
	if err != nil {
		t.Fatalf("failed to issue customer token: %v", err)
	}
	merchantToken, err := jwtManager.Issue(uuid.NewString(), users.RoleMerchant, time.Now())
	if err != nil {
		t.Fatalf("failed to issue merchant token: %v", err)
	}

	readEndpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/ops/circuit"},
		{http.MethodGet, "/api/ops/chaos/scenarios"},
		{http.MethodGet, "/api/ops/integrity/status"},
		{http.MethodGet, "/api/ops/routing/distribution"},
		{http.MethodGet, "/api/ops/runtime"},
		{http.MethodGet, "/api/ops/reconciliation/runs"},
	}

	mutationEndpoints := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/ops/chaos/start", `{"scenarioId":"sc-test","targetId":"BANK-A","faultType":"latency"}`},
		{http.MethodPost, "/api/ops/chaos/scenarios/sc-test/stop", `{}`},
		{http.MethodPost, "/api/ops/chaos/reset", `{"targetId":"BANK-A"}`},
		{http.MethodPost, "/api/ops/chaos/ledger-corruption-fixture", `{}`},
		{http.MethodPost, "/api/ops/integrity/check", `{}`},
		{http.MethodPost, "/api/ops/reconciliation/runs", `{"participantId":"BANK-A"}`},
		{http.MethodPost, "/api/ops/reconciliation/proof/verify", `{}`},
	}

	t.Run("UnauthenticatedGets401", func(t *testing.T) {
		for _, ep := range readEndpoints {
			req := httptest.NewRequest(ep.method, ep.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("[%s %s] expected 401 Unauthorized, got %d", ep.method, ep.path, rec.Code)
			}
		}
		for _, ep := range mutationEndpoints {
			req := httptest.NewRequest(ep.method, ep.path, strings.NewReader(ep.body))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("[%s %s] expected 401 Unauthorized, got %d", ep.method, ep.path, rec.Code)
			}
		}
	})

	t.Run("CustomerGets403Everywhere", func(t *testing.T) {
		for _, ep := range readEndpoints {
			req := httptest.NewRequest(ep.method, ep.path, nil)
			req.Header.Set("Authorization", "Bearer "+customerToken)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Errorf("[%s %s] customer expected 403 Forbidden, got %d", ep.method, ep.path, rec.Code)
			}
		}
		for _, ep := range mutationEndpoints {
			req := httptest.NewRequest(ep.method, ep.path, strings.NewReader(ep.body))
			req.Header.Set("Authorization", "Bearer "+customerToken)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Errorf("[%s %s] customer expected 403 Forbidden, got %d", ep.method, ep.path, rec.Code)
			}
		}
	})

	t.Run("MerchantGets403Everywhere", func(t *testing.T) {
		for _, ep := range readEndpoints {
			req := httptest.NewRequest(ep.method, ep.path, nil)
			req.Header.Set("Authorization", "Bearer "+merchantToken)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Errorf("[%s %s] merchant expected 403 Forbidden, got %d", ep.method, ep.path, rec.Code)
			}
		}
	})

	t.Run("ViewerAllowedOnReads", func(t *testing.T) {
		for _, ep := range readEndpoints {
			req := httptest.NewRequest(ep.method, ep.path, nil)
			req.Header.Set("Authorization", "Bearer "+viewerToken)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("[%s %s] viewer expected 200 OK, got %d: %s", ep.method, ep.path, rec.Code, rec.Body.String())
			}
		}
	})

	t.Run("ViewerDeniedOnMutations", func(t *testing.T) {
		for _, ep := range mutationEndpoints {
			req := httptest.NewRequest(ep.method, ep.path, strings.NewReader(ep.body))
			req.Header.Set("Authorization", "Bearer "+viewerToken)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Errorf("[%s %s] viewer expected 403 Forbidden on mutation, got %d", ep.method, ep.path, rec.Code)
			}
		}
	})

	t.Run("AdminAllowedOnReads", func(t *testing.T) {
		for _, ep := range readEndpoints {
			req := httptest.NewRequest(ep.method, ep.path, nil)
			req.Header.Set("Authorization", "Bearer "+adminToken)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("[%s %s] admin expected 200 OK, got %d: %s", ep.method, ep.path, rec.Code, rec.Body.String())
			}
		}
	})
}

func TestPublicRoleEscalationPrevented(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := database.NewPool(context.Background(), dbURL)
	if err != nil {
		t.Skip("PostgreSQL not available")
	}
	defer pool.Close()

	jwtManager, err := auth.NewJWTManager(strings.Repeat("b", 32), "escalation-test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	authSvc := auth.NewService(pool, jwtManager, "BANK-A")
	handler := NewHandler(pool, nil, authSvc, jwtManager)

	privRoles := []string{"OPS_ADMIN", "OPS_VIEWER", "SUPERUSER", "ADMIN", "ROOT"}
	for _, role := range privRoles {
		t.Run("EscalationAttempt_"+role, func(t *testing.T) {
			payload := map[string]string{
				"name":              "Attacker",
				"phone":             "9988776655",
				"paymentIdentifier": "attacker@transactx",
				"password":          "StrongPassword123!",
				"role":              role,
			}
			body, _ := json.Marshal(payload)
			req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(body))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 Bad Request for privileged role %q, got %d", role, rec.Code)
			}
		})
	}
}

func TestControlFlags_GateEnforcement(t *testing.T) {
	jwtManager, err := auth.NewJWTManager(strings.Repeat("c", 32), "flag-test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	adminToken, err := jwtManager.Issue(uuid.NewString(), users.RoleOpsAdmin, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("RegistrationDisabled", func(t *testing.T) {
		handler := NewHandlerWithReconciliationAndRuntimeOptions(
			nil, nil, nil, jwtManager, nil, nil,
			RuntimeOptions{
				SimulationMode:    true,
				AllowRegistration: false,
				ChaosEnabled:      true,
			},
		)

		reqBody := `{"name":"User","phone":"9111111111","paymentIdentifier":"user@tx","password":"Pass12345!"}`
		req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(reqBody))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden when registration disabled, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "REGISTRATION_DISABLED") {
			t.Fatalf("expected error code REGISTRATION_DISABLED, got %s", rec.Body.String())
		}
	})

	t.Run("ChaosDisabled", func(t *testing.T) {
		ctrl := chaos.NewController(nil)
		handler := NewHandlerWithReconciliationAndRuntimeOptions(
			nil, nil, nil, jwtManager, ctrl, nil,
			RuntimeOptions{
				SimulationMode: true,
				ChaosEnabled:   false,
			},
		)

		endpoints := []struct {
			method string
			path   string
			body   string
		}{
			{http.MethodPost, "/api/ops/chaos/start", `{"scenarioId":"sc-1","targetId":"BANK-A","faultType":"latency"}`},
			{http.MethodPost, "/api/ops/chaos/scenarios/sc-1/stop", `{}`},
			{http.MethodPost, "/api/ops/chaos/reset", `{"targetId":"BANK-A"}`},
			{http.MethodPost, "/api/ops/chaos/ledger-corruption-fixture", `{}`},
		}

		for _, ep := range endpoints {
			req := httptest.NewRequest(ep.method, ep.path, strings.NewReader(ep.body))
			req.Header.Set("Authorization", "Bearer "+adminToken)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Errorf("[%s %s] expected 403 Forbidden when chaos disabled, got %d", ep.method, ep.path, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "CHAOS_DISABLED") {
				t.Errorf("[%s %s] expected CHAOS_DISABLED error code, got %s", ep.method, ep.path, rec.Body.String())
			}
		}
	})

	t.Run("RuntimeStatusAccurate", func(t *testing.T) {
		handler := NewHandlerWithReconciliationAndRuntimeOptions(
			nil, nil, nil, jwtManager, nil, nil,
			RuntimeOptions{
				SimulationMode:         true,
				AllowRegistration:      false,
				ChaosEnabled:           false,
				RecoveryControlEnabled: true,
			},
		)

		req := httptest.NewRequest(http.MethodGet, "/api/ops/runtime", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}
		var resp struct {
			Data struct {
				SimulationMode         bool `json:"simulationMode"`
				AllowRegistration      bool `json:"allowRegistration"`
				ChaosEnabled           bool `json:"chaosEnabled"`
				RecoveryControlEnabled bool `json:"recoveryControlEnabled"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode runtime status: %v", err)
		}
		if !resp.Data.SimulationMode || resp.Data.AllowRegistration || resp.Data.ChaosEnabled || !resp.Data.RecoveryControlEnabled {
			t.Fatalf("runtime status flags incorrect: %+v", resp.Data)
		}
	})
}

func TestParticipantServiceToken_Validation(t *testing.T) {
	const secretToken = "secret-token-participant-xyz-1234567890"

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := database.NewPool(context.Background(), dbURL)
	if err != nil {
		t.Skip("PostgreSQL not available")
	}
	defer pool.Close()

	service := bankservice.NewParticipantService(pool, "BANK-A", "bank_a")
	service.SetServiceToken(secretToken)
	handler := bankservice.Handler(service)

	presencePath := "/internal/operations/" + uuid.NewString() + "/presence?paymentId=" + uuid.NewString()

	t.Run("MissingTokenReturns401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, presencePath, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized for missing service token, got %d", rec.Code)
		}
	})

	t.Run("WrongTokenReturns401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, presencePath, nil)
		req.Header.Set("X-Service-Token", "wrong-token-123")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized for wrong service token, got %d", rec.Code)
		}
	})

	t.Run("ValidTokenViaHeaderSucceeds", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, presencePath, nil)
		req.Header.Set("X-Service-Token", secretToken)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		// Note: since no DB rows exist for this random UUID, presence returns 200 with presence="absent"
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for valid token, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("ValidTokenViaBearerSucceeds", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, presencePath, nil)
		req.Header.Set("Authorization", "Bearer "+secretToken)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for valid Bearer token, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestConfigValidation_ProductionParticipantTokens(t *testing.T) {
	origDev := os.Getenv("APP_DEVELOPMENT")
	origAURL := os.Getenv("BANK_A_URL")
	origAToken := os.Getenv("BANK_A_SERVICE_TOKEN")
	origJWT := os.Getenv("JWT_SECRET")
	defer func() {
		os.Setenv("APP_DEVELOPMENT", origDev)
		os.Setenv("BANK_A_URL", origAURL)
		os.Setenv("BANK_A_SERVICE_TOKEN", origAToken)
		os.Setenv("JWT_SECRET", origJWT)
	}()

	os.Setenv("APP_DEVELOPMENT", "false")
	os.Setenv("JWT_SECRET", strings.Repeat("s", 32))
	os.Setenv("BANK_A_URL", "http://127.0.0.1:8081")
	os.Setenv("BANK_A_SERVICE_TOKEN", "")

	_, err := config.Load()
	if err == nil {
		t.Fatalf("expected error when BANK_A_SERVICE_TOKEN is missing in production")
	}
	if !strings.Contains(err.Error(), "BANK_A_SERVICE_TOKEN is required outside development mode") {
		t.Fatalf("unexpected error message: %v", err)
	}

	os.Setenv("BANK_A_SERVICE_TOKEN", "valid-prod-token-12345678901234567890")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected valid config when token provided, got error: %v", err)
	}
	if cfg.BankAServiceToken != "valid-prod-token-12345678901234567890" {
		t.Fatalf("token mismatch: got %q", cfg.BankAServiceToken)
	}
}

func TestAuditLogRecording(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := database.NewPool(context.Background(), dbURL)
	if err != nil {
		t.Skip("PostgreSQL not available")
	}
	defer pool.Close()

	var currentDB string
	if err := pool.QueryRow(context.Background(), "SELECT current_database()").Scan(&currentDB); err != nil {
		t.Fatalf("failed to query current_database(): %v", err)
	}
	lower := strings.ToLower(strings.TrimSpace(currentDB))
	if lower == "transactx" || lower == "postgres" || lower == "template0" || lower == "template1" ||
		(!strings.HasPrefix(lower, "transactx_disposable_") && !strings.HasPrefix(lower, "transactx_test_") && !strings.HasPrefix(lower, "transactx_m2_disposable_") && !strings.HasPrefix(lower, "transactx_cb_restore_")) {
		t.Fatalf("refusing audit log test against non-disposable database %q", currentDB)
	}

	auditStore := audit.NewPostgresStore(pool)
	jwtManager, _ := auth.NewJWTManager(strings.Repeat("d", 32), "audit-test", time.Hour)
	adminID := uuid.New()
	adminToken, _ := jwtManager.Issue(adminID.String(), users.RoleOpsAdmin, time.Now())

	ctrl := chaos.NewController(chaos.NewRepository(pool))
	ctrl.SetTargetValidator(func(string) bool { return true })
	_ = ctrl.Reset(context.Background(), "BANK-A", adminID.String(), users.RoleOpsAdmin)

	handler := NewHandlerWithReconciliationAndRuntimeOptions(
		pool, nil, nil, jwtManager, ctrl, nil,
		RuntimeOptions{
			SimulationMode: true,
			ChaosEnabled:   true,
			AuditStore:     auditStore,
		},
	)

	reqID := "audit-req-" + uuid.NewString()[:8]
	scID := "sc-audit-" + uuid.NewString()[:8]
	reqBody := `{"scenarioId":"` + scID + `","type":"LATENCY","targetId":"BANK-A","parameters":{"latencyMs":100}}`
	req := httptest.NewRequest(http.MethodPost, "/api/ops/chaos/start", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("X-Request-ID", reqID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for chaos start, got %d: %s", rec.Code, rec.Body.String())
	}

	entries, err := auditStore.ListRecent(context.Background(), 10)
	if err != nil {
		t.Fatalf("failed to list audit entries: %v", err)
	}

	found := false
	for _, e := range entries {
		if e.RequestID == reqID {
			found = true
			if e.Operation != "CHAOS_START" || e.ActorRole != users.RoleOpsAdmin || e.Result != "SUCCESS" {
				t.Errorf("mismatched audit entry details: %+v", e)
			}
			break
		}
	}
	if !found {
		t.Errorf("audit log for chaos start was not found in audit table")
	}
}
