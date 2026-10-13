package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/transactx/backend/internal/analytics"
	"github.com/transactx/backend/internal/audit"
	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/database"
	"github.com/transactx/backend/internal/investigator"
	"github.com/transactx/backend/internal/operations"
	"github.com/transactx/backend/internal/telemetry"
	"github.com/transactx/backend/internal/users"
)

func setupOperationsTestServer(t *testing.T, recoveryControlEnabled bool) (*httptest.Server, *pgxpool.Pool, *auth.JWTManager) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres@localhost:5432/transactx?sslmode=disable"
	}
	pool, err := database.NewPool(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}

	jwtManager, err := auth.NewJWTManager(strings.Repeat("o", 32), "ops-test", time.Hour)
	if err != nil {
		pool.Close()
		t.Fatalf("failed to create JWT manager: %v", err)
	}

	handler := NewHandlerWithExecutionTargetsCircuitChaosReconciliationAndRuntimeOptions(
		pool, nil, nil, jwtManager, nil, nil, nil, nil, "", "", nil, nil, nil,
		RuntimeOptions{
			SimulationMode:         true,
			AllowRegistration:      true,
			ChaosEnabled:           true,
			RecoveryControlEnabled: recoveryControlEnabled,
			AuditStore:             audit.NewPostgresStore(pool),
		},
	)

	server := httptest.NewServer(handler)
	return server, pool, jwtManager
}

func TestOperations_Worklist_RoleAndFilters(t *testing.T) {
	server, pool, jwt := setupOperationsTestServer(t, true)
	defer server.Close()
	defer pool.Close()

	adminToken, _ := jwt.Issue(uuid.NewString(), users.RoleOpsAdmin, time.Now())
	viewerToken, _ := jwt.Issue(uuid.NewString(), users.RoleOpsViewer, time.Now())
	customerToken, _ := jwt.Issue(uuid.NewString(), users.RoleCustomer, time.Now())

	client := server.Client()

	// 1. Unauthenticated gets 401
	resp, err := client.Get(server.URL + "/api/ops/worklist")
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 unauthenticated, got %v (%d)", err, resp.StatusCode)
	}

	// 2. Customer gets 403
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/ops/worklist", nil)
	req.Header.Set("Authorization", "Bearer "+customerToken)
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 customer, got %v (%d)", err, resp.StatusCode)
	}

	// 3. Viewer gets 200
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/ops/worklist?limit=10", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 viewer, got %v (%d)", err, resp.StatusCode)
	}
	var viewerBody struct {
		Data operations.Worklist `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&viewerBody); err != nil {
		t.Fatalf("failed to decode worklist body: %v", err)
	}
	if viewerBody.Data.Projection.Status == "" {
		t.Fatalf("expected non-empty projection status")
	}

	// 4. Admin gets 200 with invalid filter rejection (400)
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/ops/worklist?state=INVALID_STATE_XYZ", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 on invalid state filter, got %d", resp.StatusCode)
	}

	// 5. Valid state filter
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/ops/worklist?state=PROCESSING,ROUTING", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on valid state filter, got %d", resp.StatusCode)
	}
}

func TestOperations_PaymentEvidenceAndExport(t *testing.T) {
	server, pool, jwt := setupOperationsTestServer(t, true)
	defer server.Close()
	defer pool.Close()

	viewerToken, _ := jwt.Issue(uuid.NewString(), users.RoleOpsViewer, time.Now())
	client := server.Client()

	// 1. Unknown payment ID returns 404
	randomID := uuid.New()
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/ops/payments/%s/evidence", server.URL, randomID), nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown payment, got %v (%d)", err, resp.StatusCode)
	}

	// 2. Create test payment in database
	paymentID := createOpsTestPayment(t, pool)

	// 3. Fetch evidence for existing payment
	req, _ = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/ops/payments/%s/evidence", server.URL, paymentID), nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for evidence read, got %v (%d)", err, resp.StatusCode)
	}

	var evBody struct {
		Data operations.EvidenceBundle `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&evBody); err != nil {
		t.Fatalf("failed to decode evidence bundle: %v", err)
	}

	if evBody.Data.PaymentID != paymentID {
		t.Fatalf("expected payment ID %s, got %s", paymentID, evBody.Data.PaymentID)
	}
	if !strings.HasPrefix(evBody.Data.SnapshotID, "sha256:") {
		t.Fatalf("expected sha256: snapshot ID, got %s", evBody.Data.SnapshotID)
	}
	if evBody.Data.Payment.EvidenceID == "" {
		t.Fatalf("expected non-empty evidenceId on payment snapshot")
	}

	// 4. Test Export API
	req, _ = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/ops/payments/%s/evidence/export", server.URL, paymentID), nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for evidence export, got %v (%d)", err, resp.StatusCode)
	}

	var exportBody struct {
		Data operations.EvidenceExportBundle `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&exportBody); err != nil {
		t.Fatalf("failed to decode export bundle: %v", err)
	}
	if exportBody.Data.Manifest.PaymentID != paymentID {
		t.Fatalf("manifest payment ID mismatch: %s vs %s", paymentID, exportBody.Data.Manifest.PaymentID)
	}
	if exportBody.Data.Manifest.BundleSHA256 == "" {
		t.Fatalf("expected non-empty bundleSha256 in manifest")
	}
}

func TestOperations_RecoveryAttempts_Pagination(t *testing.T) {
	server, pool, jwt := setupOperationsTestServer(t, true)
	defer server.Close()
	defer pool.Close()

	viewerToken, _ := jwt.Issue(uuid.NewString(), users.RoleOpsViewer, time.Now())
	client := server.Client()

	paymentID := createOpsTestPayment(t, pool)

	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/ops/payments/%s/recovery-attempts?limit=10", server.URL, paymentID), nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for recovery attempts, got %v (%d)", err, resp.StatusCode)
	}

	var pageBody struct {
		Data operations.RecoveryAttemptsPage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pageBody); err != nil {
		t.Fatalf("decode recovery attempts: %v", err)
	}
	if pageBody.Data.Items == nil {
		t.Fatalf("expected non-nil items slice in response")
	}
}

func TestOperations_RecoveryRecheck_GatedAndIdempotent(t *testing.T) {
	// 1. Test Gated Disabled
	serverDisabled, pool, jwt := setupOperationsTestServer(t, false)
	defer serverDisabled.Close()

	var adminUserID string
	err := pool.QueryRow(context.Background(), "SELECT id::text FROM users WHERE role = 'OPS_ADMIN' LIMIT 1").Scan(&adminUserID)
	if err != nil {
		adminUserID = uuid.NewString()
		_, _ = pool.Exec(context.Background(), "INSERT INTO users (id, name, phone, upi_id, password_hash, role) VALUES ($1, 'Admin', $2, $3, 'hash', 'OPS_ADMIN')", adminUserID, "admin-"+adminUserID[:8], "admin@tx")
	}

	adminToken, _ := jwt.Issue(adminUserID, users.RoleOpsAdmin, time.Now())
	viewerToken, _ := jwt.Issue(uuid.NewString(), users.RoleOpsViewer, time.Now())

	paymentID := createOpsTestPayment(t, pool)

	body := bytes.NewBufferString(`{"reason": "Testing manual operator recheck"}`)
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/ops/payments/%s/recovery/recheck", serverDisabled.URL, paymentID), body)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Idempotency-Key", "recheck-test-key-1")
	resp, err := serverDisabled.Client().Do(req)
	if err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 RECOVERY_CONTROL_DISABLED when flag is false, got %d", resp.StatusCode)
	}

	// 2. Test Enabled Server
	serverEnabled, poolEnabled, _ := setupOperationsTestServer(t, true)
	defer serverEnabled.Close()
	defer poolEnabled.Close()
	defer pool.Close()

	// Viewer denied (403)
	body = bytes.NewBufferString(`{"reason": "Testing viewer deny"}`)
	req, _ = http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/ops/payments/%s/recovery/recheck", serverEnabled.URL, paymentID), body)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Idempotency-Key", "recheck-test-key-viewer")
	resp, err = serverEnabled.Client().Do(req)
	if err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 on viewer attempting recheck mutation, got %d", resp.StatusCode)
	}

	// Missing Idempotency-Key (400)
	body = bytes.NewBufferString(`{"reason": "Missing key"}`)
	req, _ = http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/ops/payments/%s/recovery/recheck", serverEnabled.URL, paymentID), body)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err = serverEnabled.Client().Do(req)
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 on missing idempotency key, got %d", resp.StatusCode)
	}

	// Admin executes recheck
	idempKey := "recheck-" + uuid.NewString()
	body = bytes.NewBufferString(`{"reason": "Operator observed participant restored"}`)
	req, _ = http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/ops/payments/%s/recovery/recheck", serverEnabled.URL, paymentID), body)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Idempotency-Key", idempKey)
	resp, err = serverEnabled.Client().Do(req)
	if err != nil || (resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK) {
		t.Fatalf("expected 202 Accepted or 200 OK, got %v (%d)", err, resp.StatusCode)
	}

	// Idempotent retry returns identical status
	body = bytes.NewBufferString(`{"reason": "Operator observed participant restored"}`)
	req, _ = http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/ops/payments/%s/recovery/recheck", serverEnabled.URL, paymentID), body)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Idempotency-Key", idempKey)
	respRetry, err := serverEnabled.Client().Do(req)
	if err != nil || respRetry.StatusCode != resp.StatusCode {
		t.Fatalf("expected identical status code on idempotent retry, got %d vs %d", respRetry.StatusCode, resp.StatusCode)
	}

	// Conflicting payload with same key returns 409
	conflictBody := bytes.NewBufferString(`{"reason": "Different reason string entirely"}`)
	req, _ = http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/ops/payments/%s/recovery/recheck", serverEnabled.URL, paymentID), conflictBody)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Idempotency-Key", idempKey)
	respConflict, err := serverEnabled.Client().Do(req)
	if err != nil || respConflict.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 Conflict on altered payload with same key, got %d", respConflict.StatusCode)
	}
}

func TestOperations_TraceDetails(t *testing.T) {
	server, pool, jwtManager := setupOperationsTestServer(t, true)
	defer server.Close()
	defer pool.Close()

	adminToken, _ := jwtManager.Issue(uuid.NewString(), users.RoleOpsAdmin, time.Now())
	viewerToken, _ := jwtManager.Issue(uuid.NewString(), users.RoleOpsViewer, time.Now())
	customerToken, _ := jwtManager.Issue(uuid.NewString(), users.RoleCustomer, time.Now())

	traceID := "4bf92f3577b34da6a3ce929d0e0e4736"

	// 1. Unauthenticated gets 401
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/ops/traces/"+traceID, nil)
	resp, err := server.Client().Do(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %v (%d)", err, resp.StatusCode)
	}

	// 2. Customer gets 403 Forbidden
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/ops/traces/"+traceID, nil)
	req.Header.Set("Authorization", "Bearer "+customerToken)
	resp, err = server.Client().Do(req)
	if err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for customer, got %v (%d)", err, resp.StatusCode)
	}

	// 3. Malformed trace ID returns 400 Bad Request
	badIDs := []string{
		"4BF92F3577B34DA6A3CE929D0E0E4736",
		"short-id-123",
		"4bf92f3577b34da6a3ce929d0e0e473g",
	}
	for _, bid := range badIDs {
		req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/ops/traces/"+bid, nil)
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		resp, err = server.Client().Do(req)
		if err != nil || resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for bad trace ID %q, got %d", bid, resp.StatusCode)
		}
	}

	// 4. Missing trace returns 200 with sourceStatus: not_collected
	missingID := "00000000000000000000000000000001"
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/ops/traces/"+missingID, nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	resp, err = server.Client().Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for missing trace, got %v (%d)", err, resp.StatusCode)
	}
	var notCollectedResp struct {
		Data telemetry.TraceResponse `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&notCollectedResp)
	if notCollectedResp.Data.SourceStatus != "not_collected" {
		t.Errorf("expected sourceStatus not_collected, got %s", notCollectedResp.Data.SourceStatus)
	}

	// 5. Existing trace returns 200 with available spans
	testTraceID := "abcdef0123456789abcdef0123456789"
	spanID := "0123456789abcdef"
	now := time.Now().UTC()
	insertQuery := `
		INSERT INTO trace_spans (
			span_id, trace_id, parent_span_id, service_name, span_name, span_kind,
			start_time, end_time, duration_ms, status_code, status_message,
			attributes, events, links, payment_id
		) VALUES (
			$1, $2, NULL, 'transactx-api', 'POST /api/payments', 'SERVER',
			$3, $4, 42, 'OK', NULL,
			'{"http.method": "POST", "http.route": "/api/payments"}'::jsonb,
			'[]'::jsonb, '[]'::jsonb, NULL
		) ON CONFLICT (trace_id, span_id) DO NOTHING`
	_, _ = pool.Exec(context.Background(), insertQuery, spanID, testTraceID, now.Add(-42*time.Millisecond), now)

	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/ops/traces/"+testTraceID, nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err = server.Client().Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for existing trace, got %v (%d)", err, resp.StatusCode)
	}
	var collectedResp struct {
		Data telemetry.TraceResponse `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&collectedResp)
	if collectedResp.Data.SourceStatus != "available" {
		t.Errorf("expected sourceStatus available, got %s", collectedResp.Data.SourceStatus)
	}
	if len(collectedResp.Data.Spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(collectedResp.Data.Spans))
	}
	if collectedResp.Data.Spans[0].SpanID != spanID {
		t.Errorf("expected span ID %s, got %s", spanID, collectedResp.Data.Spans[0].SpanID)
	}
}

func TestOperations_Analytics_WindowsAndMetricsSummary(t *testing.T) {
	server, pool, jwt := setupOperationsTestServer(t, true)
	defer server.Close()
	defer pool.Close()

	adminToken, _ := jwt.Issue(uuid.NewString(), users.RoleOpsAdmin, time.Now())
	viewerToken, _ := jwt.Issue(uuid.NewString(), users.RoleOpsViewer, time.Now())
	customerToken, _ := jwt.Issue(uuid.NewString(), users.RoleCustomer, time.Now())

	client := server.Client()

	// 1. Runtime status reports analyticsEnabled: true, modelStatus: "unavailable"
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/ops/runtime", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from runtime status, got %v (%d)", err, resp.StatusCode)
	}
	var runtimeResp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&runtimeResp); err != nil {
		t.Fatalf("failed to decode runtime status: %v", err)
	}
	if analyticsEnabled, ok := runtimeResp.Data["analyticsEnabled"].(bool); !ok || !analyticsEnabled {
		t.Errorf("expected analyticsEnabled == true, got %v", runtimeResp.Data["analyticsEnabled"])
	}
	if modelStatus, ok := runtimeResp.Data["modelStatus"].(string); !ok || modelStatus != "unavailable" {
		t.Errorf("expected modelStatus == 'unavailable', got %v", runtimeResp.Data["modelStatus"])
	}

	// 2. Windows endpoint - 401 unauthenticated
	resp, err = client.Get(server.URL + "/api/ops/analytics/windows")
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 unauthenticated, got %v (%d)", err, resp.StatusCode)
	}

	// 3. Windows endpoint - 403 customer
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/ops/analytics/windows", nil)
	req.Header.Set("Authorization", "Bearer "+customerToken)
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 customer, got %v (%d)", err, resp.StatusCode)
	}

	// 4. Windows endpoint - viewer gets 200 with schema compliance
	testWinID := uuid.New()
	windowFrom := time.Now().UTC().Truncate(5 * time.Minute).Add(-10 * time.Minute)
	windowTo := windowFrom.Add(5 * time.Minute)
	insertWinQuery := `
		INSERT INTO operational_aggregate_windows (
			id, entity_type, entity_id, window_from, window_to, sample_count,
			feature_schema_version, features, raw_aggregates, model_status
		) VALUES (
			$1, 'bank', 'BANK_TEST_OPS', $2, $3, 25,
			'tx.features.v1', '{"timeoutRate": 0.04, "failureRate": 0.08}'::jsonb, '{}'::jsonb, 'ready'
		) ON CONFLICT (entity_type, entity_id, window_from, feature_schema_version) DO NOTHING`
	_, _ = pool.Exec(context.Background(), insertWinQuery, testWinID, windowFrom, windowTo)

	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/ops/analytics/windows?entityType=bank&entityId=BANK_TEST_OPS&limit=5", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 viewer, got %v (%d)", err, resp.StatusCode)
	}
	var winPageResp struct {
		Data analytics.AnalyticsPage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&winPageResp); err != nil {
		t.Fatalf("failed to decode windows page: %v", err)
	}
	if len(winPageResp.Data.Items) == 0 {
		t.Fatalf("expected at least 1 analytics window item")
	}
	found := false
	for _, it := range winPageResp.Data.Items {
		if it.EntityID == "BANK_TEST_OPS" && it.EntityType == "bank" {
			found = true
			if it.FeatureSchemaVersion != analytics.FeatureSchemaVersionV1 {
				t.Errorf("expected schema version %s, got %s", analytics.FeatureSchemaVersionV1, it.FeatureSchemaVersion)
			}
			if it.SampleCount != 25 {
				t.Errorf("expected sampleCount 25, got %d", it.SampleCount)
			}
			if it.Features.TimeoutRate == nil || *it.Features.TimeoutRate != 0.04 {
				t.Errorf("expected timeoutRate 0.04, got %v", it.Features.TimeoutRate)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected to find inserted BANK_TEST_OPS window in returned items")
	}

	// 5. Windows endpoint - validation error cases
	badReqs := []struct {
		url          string
		expectedCode int
	}{
		{server.URL + "/api/ops/analytics/windows?from=not-a-date", http.StatusBadRequest},
		{server.URL + "/api/ops/analytics/windows?to=not-a-date", http.StatusBadRequest},
		{server.URL + "/api/ops/analytics/windows?limit=invalid", http.StatusBadRequest},
		{server.URL + "/api/ops/analytics/windows?cursor=bad!cursor", http.StatusBadRequest},
	}
	for _, tc := range badReqs {
		r, _ := http.NewRequest(http.MethodGet, tc.url, nil)
		r.Header.Set("Authorization", "Bearer "+viewerToken)
		res, err := client.Do(r)
		if err != nil || res.StatusCode != tc.expectedCode {
			t.Errorf("for url %s expected status %d, got %v (%d)", tc.url, tc.expectedCode, err, res.StatusCode)
		}
	}

	// 6. Metrics summary - 401 unauthenticated
	resp, err = client.Get(server.URL + "/api/ops/metrics/summary")
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 unauthenticated on metrics summary, got %v (%d)", err, resp.StatusCode)
	}

	// 7. Metrics summary - 403 customer
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/ops/metrics/summary", nil)
	req.Header.Set("Authorization", "Bearer "+customerToken)
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 customer on metrics summary, got %v (%d)", err, resp.StatusCode)
	}

	// 8. Metrics summary - 200 admin
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/ops/metrics/summary", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 admin on metrics summary, got %v (%d)", err, resp.StatusCode)
	}
	var metricsSummaryResp struct {
		Data analytics.MetricsSummary `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&metricsSummaryResp); err != nil {
		t.Fatalf("failed to decode metrics summary: %v", err)
	}
	if len(metricsSummaryResp.Data.Sources) == 0 {
		t.Errorf("expected non-empty sources in metrics summary")
	}
	if metricsSummaryResp.Data.Payments.Attempted == "" {
		t.Errorf("expected decimal string for payments.attempted")
	}

	// 9. Metrics summary - invalid asOf returns 400
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/ops/metrics/summary?asOf=bad-time", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad asOf, got %v (%d)", err, resp.StatusCode)
	}
}

func TestOperations_Investigation_CreateAndFetch(t *testing.T) {
	server, pool, jwt := setupOperationsTestServer(t, true)
	defer server.Close()
	defer pool.Close()

	ctx := context.Background()
	_, _ = pool.Exec(ctx, "DELETE FROM investigation_jobs;")

	adminToken, _ := jwt.Issue(uuid.NewString(), users.RoleOpsAdmin, time.Now())
	viewerToken, _ := jwt.Issue(uuid.NewString(), users.RoleOpsViewer, time.Now())
	customerToken, _ := jwt.Issue(uuid.NewString(), users.RoleCustomer, time.Now())

	client := server.Client()

	// 1. Role gating on POST /api/ops/investigations
	// 1a. Unauthenticated gets 401
	body := []byte(`{"paymentId":"` + uuid.NewString() + `","question":"Why is this pending?"}`)
	resp, err := client.Post(server.URL+"/api/ops/investigations", "application/json", bytes.NewReader(body))
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 unauthenticated on create investigation, got %v (%d)", err, resp.StatusCode)
	}

	// 1b. Customer gets 403
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/ops/investigations", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+customerToken)
	req.Header.Set("Idempotency-Key", "test-key-cust")
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 customer on create investigation, got %v (%d)", err, resp.StatusCode)
	}

	// 2. Validation on POST /api/ops/investigations
	// 2a. Missing Idempotency-Key header gets 400
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/ops/investigations", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 on missing Idempotency-Key header, got %v (%d)", err, resp.StatusCode)
	}

	// 2b. Invalid paymentId (non-UUID) gets 400
	badPaymentBody := []byte(`{"paymentId":"not-a-uuid","question":"Why is this pending?"}`)
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/ops/investigations", bytes.NewReader(badPaymentBody))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Idempotency-Key", "test-key-bad-pid")
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 on invalid paymentId, got %v (%d)", err, resp.StatusCode)
	}

	// 2c. Empty question gets 400
	emptyQBody := []byte(`{"paymentId":"` + uuid.NewString() + `","question":""}`)
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/ops/investigations", bytes.NewReader(emptyQBody))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Idempotency-Key", "test-key-empty-q")
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 on empty question, got %v (%d)", err, resp.StatusCode)
	}

	// 3. GET /api/ops/investigations/{investigationID} validation & role checks
	// 3a. Unauthenticated gets 401
	randomInvID := uuid.New()
	resp, err = client.Get(fmt.Sprintf("%s/api/ops/investigations/%s", server.URL, randomInvID))
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 unauthenticated on get investigation, got %v (%d)", err, resp.StatusCode)
	}

	// 3b. Customer gets 403
	req, _ = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/ops/investigations/%s", server.URL, randomInvID), nil)
	req.Header.Set("Authorization", "Bearer "+customerToken)
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 customer on get investigation, got %v (%d)", err, resp.StatusCode)
	}

	// 3c. Invalid UUID gets 400
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/ops/investigations/not-a-uuid", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 on invalid investigation UUID, got %v (%d)", err, resp.StatusCode)
	}

	// 3d. Non-existent UUID gets 404
	req, _ = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/ops/investigations/%s", server.URL, randomInvID), nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 on missing investigation, got %v (%d)", err, resp.StatusCode)
	}

	// 4. Create valid investigation by OPS_VIEWER
	paymentID := createOpsTestPayment(t, pool)
	idempKey := "idemp-ops-" + uuid.NewString()

	createReqBody := []byte(fmt.Sprintf(`{"paymentId":"%s","question":"Why is this payment pending reconciliation?"}`, paymentID))
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/ops/investigations", bytes.NewReader(createReqBody))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Idempotency-Key", idempKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 on valid create investigation, got %v (%d)", err, resp.StatusCode)
	}

	var acceptedResp struct {
		Data investigator.InvestigationAccepted `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&acceptedResp); err != nil {
		t.Fatalf("failed to decode accepted response: %v", err)
	}
	if acceptedResp.Data.InvestigationID == uuid.Nil {
		t.Fatalf("expected valid investigationId in response")
	}
	if acceptedResp.Data.Status != "queued" {
		t.Errorf("expected status queued, got %s", acceptedResp.Data.Status)
	}
	if acceptedResp.Data.Mode != "evidence_only" {
		t.Errorf("expected mode evidence_only, got %s", acceptedResp.Data.Mode)
	}
	invID := acceptedResp.Data.InvestigationID

	// 5. Idempotency Replay: same key, same payload -> 202 with same investigationID
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/ops/investigations", bytes.NewReader(createReqBody))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Idempotency-Key", idempKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 on idempotent replay, got %v (%d)", err, resp.StatusCode)
	}
	var replayResp struct {
		Data investigator.InvestigationAccepted `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&replayResp)
	if replayResp.Data.InvestigationID != invID {
		t.Fatalf("expected replayed investigation ID %s, got %s", invID, replayResp.Data.InvestigationID)
	}

	// 6. Idempotency Conflict: same key, different question -> 409 Conflict
	conflictingBody := []byte(fmt.Sprintf(`{"paymentId":"%s","question":"Different question for conflict"}`, paymentID))
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/ops/investigations", bytes.NewReader(conflictingBody))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Idempotency-Key", idempKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 conflict on altered payload, got %v (%d)", err, resp.StatusCode)
	}

	// 7. GET /api/ops/investigations/{investigationID} before execution
	req, _ = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/ops/investigations/%s", server.URL, invID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on get queued investigation, got %v (%d)", err, resp.StatusCode)
	}
	var queuedInvResp struct {
		Data investigator.Investigation `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&queuedInvResp); err != nil {
		t.Fatalf("failed to decode queued investigation: %v", err)
	}
	if queuedInvResp.Data.ID != invID {
		t.Fatalf("investigation ID mismatch: %s vs %s", invID, queuedInvResp.Data.ID)
	}
	if queuedInvResp.Data.Status != "queued" {
		t.Fatalf("expected status queued, got %s", queuedInvResp.Data.Status)
	}

	// 8. Execute the job via Investigator Store & Service
	invStore, err := investigator.NewPostgresInvestigationStore(pool, time.Now)
	if err != nil {
		t.Fatalf("failed to create investigation store: %v", err)
	}
	corpusRepo, err := investigator.NewPostgresCorpusRepository(pool)
	if err != nil {
		t.Fatalf("failed to create corpus repo: %v", err)
	}
	opsRepo := operations.NewRepository(pool)
	opsSvc := operations.NewService(opsRepo, nil, nil)
	bridgedEvidence := investigator.BridgedEvidenceReader{
		Source: &httpOpsEvidenceSource{ops: opsSvc},
	}
	deps := investigator.ToolDependencies{
		Evidence: bridgedEvidence,
		Runbooks: corpusRepo,
		Recovery: &httpOpsRecoveryReader{ops: opsSvc},
	}
	cfg := investigator.DefaultServiceConfig()
	cfg.JobTimeout = 2 * time.Minute
	invSvc, err := investigator.NewService(cfg, invStore, deps)
	if err != nil {
		t.Fatalf("failed to create investigator service: %v", err)
	}

	_, err = invSvc.Run(ctx, invID.String(), "worker-integration-test")
	if err != nil {
		t.Fatalf("execute job failed: %v", err)
	}

	// 9. GET /api/ops/investigations/{investigationID} after execution
	req, _ = http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/ops/investigations/%s", server.URL, invID), nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	resp, err = client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on get completed investigation, got %v (%d)", err, resp.StatusCode)
	}
	var completedInvResp struct {
		Data investigator.Investigation `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&completedInvResp); err != nil {
		t.Fatalf("failed to decode completed investigation: %v", err)
	}
	if completedInvResp.Data.Status != "completed" {
		t.Fatalf("expected status completed, got %s", completedInvResp.Data.Status)
	}
	if completedInvResp.Data.Report == nil {
		t.Fatalf("expected non-nil report on completed investigation")
	}
	if completedInvResp.Data.Report.SchemaVersion != "tx.investigation.v1" {
		t.Errorf("expected schemaVersion tx.investigation.v1, got %s", completedInvResp.Data.Report.SchemaVersion)
	}
	if completedInvResp.Data.Report.Mode != "evidence_only" {
		t.Errorf("expected report mode evidence_only, got %s", completedInvResp.Data.Report.Mode)
	}
	if len(completedInvResp.Data.Report.Facts) == 0 {
		t.Errorf("expected facts in report, got 0")
	}
	if len(completedInvResp.Data.ToolTimeline) < 2 {
		t.Errorf("expected tool timeline with at least 2 steps, got %d", len(completedInvResp.Data.ToolTimeline))
	}
}

func createOpsTestPayment(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	ctx := context.Background()
	bankID := uuid.New()
	bankCode := "BANK_TEST_" + uuid.NewString()[:8]
	_, _ = pool.Exec(ctx, "INSERT INTO banks (id, code, name, status) VALUES ($1, $2, 'Bank Test', 'ACTIVE')", bankID, bankCode)

	senderID := uuid.New()
	senderPhone := "+9199" + uuid.NewString()[:8]
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, phone, name, upi_id, password_hash, role)
		VALUES ($1, $2, 'Test User', $3, 'hash', 'CUSTOMER');
	`, senderID, senderPhone, senderPhone+"@upi")
	if err != nil {
		t.Fatalf("insert test user: %v", err)
	}

	accID := uuid.New()
	_, _ = pool.Exec(ctx, `
		INSERT INTO accounts (id, user_id, bank_id, account_number, balance_paise, status)
		VALUES ($1, $2, $3, $4, 5000000, 'ACTIVE');
	`, accID, senderID, bankID, "ACC_"+uuid.NewString()[:8])

	paymentID := uuid.New()
	query := `
		INSERT INTO payments (
			id, initiated_by_user_id, sender_account_id, receiver_account_id, amount_paise,
			currency, state, source_bank_id, destination_bank_id,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $3, 50000, 'INR', 'PENDING_RECONCILIATION', $4, $4, NOW(), NOW()
		);
	`
	_, err = pool.Exec(ctx, query, paymentID, senderID, accID, bankID)
	if err != nil {
		t.Fatalf("failed to insert test payment: %v", err)
	}
	return paymentID
}

