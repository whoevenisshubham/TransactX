package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/users"
)

// newMerchantTestJWT issues a JWT for the given role without a real DB.
func newMerchantTestJWT(t *testing.T, manager *auth.JWTManager, userID, role string) string {
	t.Helper()
	token, err := manager.Issue(userID, role, time.Now())
	if err != nil {
		t.Fatalf("issue JWT: %v", err)
	}
	return token
}

func newMerchantTestManager(t *testing.T) *auth.JWTManager {
	t.Helper()
	manager, err := auth.NewJWTManager(strings.Repeat("m", 32), "merchant-test", time.Hour)
	if err != nil {
		t.Fatalf("new JWT manager: %v", err)
	}
	return manager
}

// TestMerchantReceiveInfoUnauthenticated verifies that
// GET /api/merchant/receive-info with no token returns 401.
func TestMerchantReceiveInfoUnauthenticated(t *testing.T) {
	manager := newMerchantTestManager(t)
	handler := NewHandler(nil, slog.Default(), nil, manager)

	request := httptest.NewRequest(http.MethodGet, "/api/merchant/receive-info", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	var envelope map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	errObj, _ := envelope["error"].(map[string]any)
	if errObj["code"] != "UNAUTHORIZED" {
		t.Fatalf("error code = %v, want UNAUTHORIZED", errObj["code"])
	}
}

// TestMerchantReceiveInfoForbiddenForCustomer verifies that
// a CUSTOMER token on GET /api/merchant/receive-info gets 403.
func TestMerchantReceiveInfoForbiddenForCustomer(t *testing.T) {
	manager := newMerchantTestManager(t)
	token := newMerchantTestJWT(t, manager, "11111111-1111-4111-8111-111111111111", users.RoleCustomer)
	handler := NewHandler(nil, slog.Default(), nil, manager)

	request := httptest.NewRequest(http.MethodGet, "/api/merchant/receive-info", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for CUSTOMER role on merchant endpoint", recorder.Code)
	}
	var envelope map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	errObj, _ := envelope["error"].(map[string]any)
	if errObj["code"] != "FORBIDDEN" {
		t.Fatalf("error code = %v, want FORBIDDEN", errObj["code"])
	}
}

// TestMerchantReceiveInfoForbiddenForOpsAdmin verifies that
// an OPS_ADMIN token on GET /api/merchant/receive-info gets 403.
// OPS_ADMIN has its own ops routes and must not gain merchant data.
func TestMerchantReceiveInfoForbiddenForOpsAdmin(t *testing.T) {
	manager := newMerchantTestManager(t)
	token := newMerchantTestJWT(t, manager, "22222222-2222-4222-8222-222222222222", users.RoleOpsAdmin)
	handler := NewHandler(nil, slog.Default(), nil, manager)

	request := httptest.NewRequest(http.MethodGet, "/api/merchant/receive-info", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for OPS_ADMIN role on merchant endpoint", recorder.Code)
	}
}

// TestMerchantReceiveInfoResponseFieldsContract verifies that no internal
// database identifiers appear in any response from the merchant endpoint.
// It exercises the 403 path (CUSTOMER token reaches auth, is rejected before
// touching the DB) and checks the error body for forbidden fields.
// The 200-path field contract is exercised by TestMerchantContractReceiveInfo
// when DATABASE_URL is set.
func TestMerchantReceiveInfoResponseFieldsContract(t *testing.T) {
	manager := newMerchantTestManager(t)
	// Use a CUSTOMER token — the route rejects with 403 before any DB call,
	// so no nil-pointer panic occurs with a nil pool.
	customerToken := newMerchantTestJWT(t, manager, "33333333-3333-4333-8333-333333333333", users.RoleCustomer)
	handler := NewHandler(nil, slog.Default(), nil, manager)

	request := httptest.NewRequest(http.MethodGet, "/api/merchant/receive-info", nil)
	request.Header.Set("Authorization", "Bearer "+customerToken)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}

	body := recorder.Body.Bytes()
	forbidden := []string{
		"senderAccountId", "receiverAccountId", "initiatedByUserId",
		"sourceBankId", "destinationBankId", "sourceBankAccountId",
		"destinationBankAccountId", "routeBankId", "bankAccountId",
	}
	bodyStr := string(body)
	for _, field := range forbidden {
		if strings.Contains(bodyStr, field) {
			t.Fatalf("response contains forbidden internal field %q: %s", field, bodyStr)
		}
	}
}
