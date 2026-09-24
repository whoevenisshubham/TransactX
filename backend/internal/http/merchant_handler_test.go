package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

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
func TestMerchantReceiveInfoResponseFieldsContract(t *testing.T) {
	manager := newMerchantTestManager(t)
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

// TestMerchantContractReceiveInfo exercises the successful 200 contract for
// GET /api/merchant/receive-info when authenticated as MERCHANT.
// It uses a real PostgreSQL instance when DATABASE_URL is set, verifying:
//   - HTTP 200
//   - Valid JSON response envelope with {"data": {...}}
//   - paymentIdentifier matches the merchant's registered identifier
//   - accountNumber matches the merchant's account number (TX-...)
//   - accountStatus is "ACTIVE"
//   - All forbidden internal identifiers (UUIDs, bank IDs, account IDs) are absent
func TestMerchantContractReceiveInfo(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set: skipping live PostgreSQL merchant receive-info contract test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	defer pool.Close()

	manager := newMerchantTestManager(t)
	authService := auth.NewService(pool, manager, "BANK-DEV-001")
	handler := NewHandler(pool, slog.Default(), authService, manager)

	suffix := uuid.New().String()[:8]
	merchantName := "Merchant " + suffix
	merchantPhone := "98" + suffix
	paymentID := "merchant-" + suffix + "@transactx"
	password := "merchant-password-1"

	// Register a new user with role = MERCHANT
	regBody := fmt.Sprintf(`{"name":%q,"phone":%q,"paymentIdentifier":%q,"password":%q,"role":"MERCHANT"}`,
		merchantName, merchantPhone, paymentID, password)
	regReq := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regRec := httptest.NewRecorder()
	handler.ServeHTTP(regRec, regReq)
	if regRec.Code != http.StatusCreated {
		t.Fatalf("register merchant: status = %d body = %s", regRec.Code, regRec.Body.String())
	}

	// Login as the merchant to obtain a genuine JWT token and user ID
	loginBody := fmt.Sprintf(`{"identifier":%q,"password":%q}`, paymentID, password)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login merchant: status = %d body = %s", loginRec.Code, loginRec.Body.String())
	}

	var loginEnvelope struct {
		Data struct {
			Token string `json:"token"`
			User  struct {
				ID   string `json:"id"`
				Role string `json:"role"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.NewDecoder(loginRec.Body).Decode(&loginEnvelope); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	merchantToken := loginEnvelope.Data.Token
	merchantUserID := loginEnvelope.Data.User.ID
	if merchantToken == "" {
		t.Fatal("login returned empty token")
	}

	// Cleanup test data on completion
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE user_id = $1`, merchantUserID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, merchantUserID)
	})

	// Fetch expected account number from DB to verify against handler response
	var expectedAccountNum, expectedStatus string
	err = pool.QueryRow(ctx, `SELECT account_number, status FROM accounts WHERE user_id = $1`, merchantUserID).Scan(&expectedAccountNum, &expectedStatus)
	if err != nil {
		t.Fatalf("query merchant account: %v", err)
	}

	// Call GET /api/merchant/receive-info
	req := httptest.NewRequest(http.MethodGet, "/api/merchant/receive-info", nil)
	req.Header.Set("Authorization", "Bearer "+merchantToken)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	// 1. Verify HTTP 200
	if recorder.Code != http.StatusOK {
		t.Fatalf("receive-info: status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}

	// 2. Verify response JSON is valid
	var envelope struct {
		RequestID string                      `json:"requestId"`
		Data      merchantReceiveInfoResponse `json:"data"`
	}
	rawBody := recorder.Body.Bytes()
	if err := json.Unmarshal(rawBody, &envelope); err != nil {
		t.Fatalf("invalid json response: %v, body = %s", err, string(rawBody))
	}

	// 3. Verify paymentIdentifier is correct
	if envelope.Data.PaymentIdentifier != paymentID {
		t.Errorf("paymentIdentifier = %q, want %q", envelope.Data.PaymentIdentifier, paymentID)
	}

	// 4. Verify accountNumber is correct
	if envelope.Data.AccountNumber != expectedAccountNum {
		t.Errorf("accountNumber = %q, want %q", envelope.Data.AccountNumber, expectedAccountNum)
	}

	// 5. Verify accountStatus is correct
	if envelope.Data.AccountStatus != expectedStatus {
		t.Errorf("accountStatus = %q, want %q", envelope.Data.AccountStatus, expectedStatus)
	}

	// 6. Verify forbidden internal fields are absent
	forbidden := []string{
		"senderAccountId",
		"receiverAccountId",
		"initiatedByUserId",
		"sourceBankId",
		"destinationBankId",
		"sourceBankAccountId",
		"destinationBankAccountId",
		"routeBankId",
		"bankAccountId",
		merchantUserID,
	}
	bodyStr := string(rawBody)
	for _, field := range forbidden {
		if strings.Contains(bodyStr, field) {
			t.Errorf("response contains forbidden internal field %q: %s", field, bodyStr)
		}
	}
}
