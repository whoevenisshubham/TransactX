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

	"github.com/transactx/backend/internal/audit"
	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/database"
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

	// 2. Fetch existing payment from database
	var paymentID uuid.UUID
	err = pool.QueryRow(context.Background(), "SELECT id FROM payments ORDER BY created_at DESC LIMIT 1").Scan(&paymentID)
	if err != nil {
		t.Skip("no payments in database to test evidence read")
	}

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

	var paymentID uuid.UUID
	err := pool.QueryRow(context.Background(), "SELECT id FROM payments ORDER BY created_at DESC LIMIT 1").Scan(&paymentID)
	if err != nil {
		t.Skip("no payments in database")
	}

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

	var paymentID uuid.UUID
	err = pool.QueryRow(context.Background(), "SELECT id FROM payments ORDER BY created_at DESC LIMIT 1").Scan(&paymentID)
	if err != nil {
		pool.Close()
		t.Skip("no payments in database")
	}

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

