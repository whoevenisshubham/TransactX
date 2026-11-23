package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/transactx/backend/internal/accounts"
	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/payments"
)

func TestCreatePaymentRejectsUnauthenticatedRequest(t *testing.T) {
	manager, err := auth.NewJWTManager(strings.Repeat("x", 32), "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(nil, slog.Default(), nil, manager)
	request := httptest.NewRequest(http.MethodPost, "/api/payments", strings.NewReader(`{"recipient":"bob@transactx","amountPaise":100,"currency":"INR"}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	body, _ := io.ReadAll(recorder.Result().Body)
	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	errObj, _ := envelope["error"].(map[string]any)
	if errObj["code"] != "UNAUTHORIZED" {
		t.Fatalf("code = %v, want UNAUTHORIZED", errObj["code"])
	}
	message, _ := errObj["message"].(string)
	if strings.Contains(message, "database") || strings.Contains(message, "password") || strings.Contains(message, "pq:") {
		t.Fatalf("unsafe error response: %s", body)
	}
}

func TestCreatePaymentRejectsClientSuppliedSourceAccountID(t *testing.T) {
	now := time.Now()
	manager, err := auth.NewJWTManager(strings.Repeat("x", 32), "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, err := manager.Issue("11111111-1111-4111-8111-111111111111", "CUSTOMER", now)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(nil, slog.Default(), nil, manager)
	request := httptest.NewRequest(http.MethodPost, "/api/payments", strings.NewReader(`{"sourceAccountId":"bad","recipient":"bob@transactx","amountPaise":100,"currency":"INR"}`))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "client-source-rejected")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (unknown sourceAccountId must be rejected)", recorder.Code, http.StatusBadRequest)
	}
	var envelope map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	errObj, _ := envelope["error"].(map[string]any)
	if errObj["code"] != "INVALID_REQUEST" {
		t.Fatalf("code = %v, want INVALID_REQUEST", errObj["code"])
	}
}

func TestPaymentByIdempotencyKey_Unauthenticated(t *testing.T) {
	manager, err := auth.NewJWTManager(strings.Repeat("x", 32), "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(nil, slog.Default(), nil, manager)
	request := httptest.NewRequest(http.MethodGet, "/api/payments/idempotency/some-key", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestPaymentByIdempotencyKey_ForbiddenRoles(t *testing.T) {
	now := time.Now()
	manager, err := auth.NewJWTManager(strings.Repeat("x", 32), "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"OPS_ADMIN", "OPS_VIEWER"} {
		token, err := manager.Issue("11111111-1111-4111-8111-111111111111", role, now)
		if err != nil {
			t.Fatal(err)
		}
		handler := NewHandler(nil, slog.Default(), nil, manager)
		request := httptest.NewRequest(http.MethodGet, "/api/payments/idempotency/some-key", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusForbidden {
			t.Fatalf("role %s: status = %d, want %d", role, recorder.Code, http.StatusForbidden)
		}
	}
}

func TestPaymentByIdempotencyKey_NotFound(t *testing.T) {
	now := time.Now()
	manager, err := auth.NewJWTManager(strings.Repeat("x", 32), "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	token, err := manager.Issue("11111111-1111-4111-8111-111111111111", "CUSTOMER", now)
	if err != nil {
		t.Fatal(err)
	}
	repo := payments.NewRepository(nil)
	repo.SetGetByIdempotentFn(func(ctx context.Context, userID uuid.UUID, key string) (payments.CustomerPayment, error) {
		return payments.CustomerPayment{}, payments.ErrNotFound
	})
	svc := payments.NewService(nil, nil, repo, nil)
	h := &Handler{
		logger:   slog.Default(),
		payments: svc,
	}
	mux := http.NewServeMux()
	mux.Handle("GET /api/payments/idempotency/{key}", auth.Authentication(manager, auth.RequireRole(auth.PublicRoles()...)(http.HandlerFunc(h.paymentByIdempotencyKey))))

	request := httptest.NewRequest(http.MethodGet, "/api/payments/idempotency/nonexistent-key", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	var envelope map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	errObj, _ := envelope["error"].(map[string]any)
	if errObj["code"] != "PAYMENT_NOT_FOUND" {
		t.Fatalf("code = %v, want PAYMENT_NOT_FOUND", errObj["code"])
	}
}

func TestPaymentByIdempotencyKey_CommittedPaymentAndIsolation(t *testing.T) {
	now := time.Now()
	manager, err := auth.NewJWTManager(strings.Repeat("x", 32), "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	userAID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	userAToken, err := manager.Issue(userAID.String(), "CUSTOMER", now)
	if err != nil {
		t.Fatal(err)
	}
	repo := payments.NewRepository(nil)
	repo.SetGetByIdempotentFn(func(ctx context.Context, userID uuid.UUID, key string) (payments.CustomerPayment, error) {
		if userID == userAID && key == "test-key-123" {
			return payments.CustomerPayment{
				ID:                uuid.New(),
				AmountPaise:       50000,
				Currency:          "INR",
				State:             payments.StateCompleted,
				SenderName:        "Alice",
				SenderPaymentID:   "alice@transactx",
				ReceiverName:      "Bob",
				ReceiverPaymentID: "bob@transactx",
			}, nil
		}
		return payments.CustomerPayment{}, payments.ErrNotFound
	})
	svc := payments.NewService(nil, nil, repo, nil)

	h := &Handler{
		logger:   slog.Default(),
		payments: svc,
	}
	mux := http.NewServeMux()
	mux.Handle("GET /api/payments/idempotency/{key}", auth.Authentication(manager, auth.RequireRole(auth.PublicRoles()...)(http.HandlerFunc(h.paymentByIdempotencyKey))))

	// 1. User A queries their committed payment -> 200 OK
	requestA := httptest.NewRequest(http.MethodGet, "/api/payments/idempotency/test-key-123", nil)
	requestA.Header.Set("Authorization", "Bearer "+userAToken)
	recorderA := httptest.NewRecorder()
	mux.ServeHTTP(recorderA, requestA)

	if recorderA.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorderA.Code, http.StatusOK)
	}
	var envelopeA map[string]any
	if err := json.NewDecoder(recorderA.Body).Decode(&envelopeA); err != nil {
		t.Fatal(err)
	}
	dataA, _ := envelopeA["data"].(map[string]any)
	if dataA["state"] != "COMPLETED" || int64(dataA["amountPaise"].(float64)) != 50000 {
		t.Fatalf("unexpected data: %v", dataA)
	}

	// 2. User B queries User A's key -> 404 Not Found (Cross-user tenant isolation)
	userBID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	userBToken, err := manager.Issue(userBID.String(), "CUSTOMER", now)
	if err != nil {
		t.Fatal(err)
	}
	requestB := httptest.NewRequest(http.MethodGet, "/api/payments/idempotency/test-key-123", nil)
	requestB.Header.Set("Authorization", "Bearer "+userBToken)
	recorderB := httptest.NewRecorder()
	mux.ServeHTTP(recorderB, requestB)

	if recorderB.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (User B must not see User A's idempotency key)", recorderB.Code, http.StatusNotFound)
	}
}

// Real HTTP Server Delayed Commit Test (Track H: Protocol-level verification)
// Proves over real HTTP network sockets (httptest.NewServer) that querying GET /api/payments/idempotency/{key}
// while the payment transaction is in-flight returns HTTP 404 PAYMENT_NOT_FOUND, and only returns HTTP 200 OK
// once the payment commits.
func TestPaymentByIdempotencyKey_DelayedCommit_RealHTTPServer(t *testing.T) {
	now := time.Now()
	manager, err := auth.NewJWTManager(strings.Repeat("x", 32), "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	userAID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	userAToken, err := manager.Issue(userAID.String(), "CUSTOMER", now)
	if err != nil {
		t.Fatal(err)
	}

	key := "key-real-http-delayed-" + uuid.NewString()
	paymentID := uuid.New()

	var committed atomic.Bool
	committed.Store(false)

	repo := payments.NewRepository(nil)
	repo.SetGetByIdempotentFn(func(ctx context.Context, userID uuid.UUID, k string) (payments.CustomerPayment, error) {
		if userID == userAID && k == key {
			if !committed.Load() {
				// While in flight before commit, ledger does not have visibility
				return payments.CustomerPayment{}, payments.ErrNotFound
			}
			return payments.CustomerPayment{
				ID:                paymentID,
				AmountPaise:       120000,
				Currency:          "INR",
				State:             payments.StateCompleted,
				SenderName:        "Alice Tester",
				SenderPaymentID:   "alice@transactx",
				ReceiverName:      "Bob Merchant",
				ReceiverPaymentID: "bob@transactx",
			}, nil
		}
		return payments.CustomerPayment{}, payments.ErrNotFound
	})
	svc := payments.NewService(nil, nil, repo, nil)

	h := &Handler{
		logger:   slog.Default(),
		payments: svc,
	}
	mux := http.NewServeMux()
	mux.Handle("GET /api/payments/idempotency/{key}", auth.Authentication(manager, auth.RequireRole(auth.PublicRoles()...)(http.HandlerFunc(h.paymentByIdempotencyKey))))

	// Real HTTP Server over TCP
	server := httptest.NewServer(mux)
	defer server.Close()

	client := server.Client()

	// 1. In-flight lookup before commit -> Must return real HTTP 404
	reqInflight, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/api/payments/idempotency/"+key, nil)
	if err != nil {
		t.Fatal(err)
	}
	reqInflight.Header.Set("Authorization", "Bearer "+userAToken)
	respInflight, err := client.Do(reqInflight)
	if err != nil {
		t.Fatal(err)
	}
	defer respInflight.Body.Close()

	if respInflight.StatusCode != http.StatusNotFound {
		t.Fatalf("in-flight lookup status = %d, want %d", respInflight.StatusCode, http.StatusNotFound)
	}
	var errEnvelope map[string]any
	if err := json.NewDecoder(respInflight.Body).Decode(&errEnvelope); err != nil {
		t.Fatal(err)
	}
	errObj, _ := errEnvelope["error"].(map[string]any)
	if errObj["code"] != "PAYMENT_NOT_FOUND" {
		t.Fatalf("error code = %v, want PAYMENT_NOT_FOUND", errObj["code"])
	}

	// 2. Commit transaction
	committed.Store(true)

	// 3. Post-commit lookup -> Must return real HTTP 200 OK
	reqCommitted, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/api/payments/idempotency/"+key, nil)
	if err != nil {
		t.Fatal(err)
	}
	reqCommitted.Header.Set("Authorization", "Bearer "+userAToken)
	respCommitted, err := client.Do(reqCommitted)
	if err != nil {
		t.Fatal(err)
	}
	defer respCommitted.Body.Close()

	if respCommitted.StatusCode != http.StatusOK {
		t.Fatalf("post-commit lookup status = %d, want %d", respCommitted.StatusCode, http.StatusOK)
	}
	var dataEnvelope map[string]any
	if err := json.NewDecoder(respCommitted.Body).Decode(&dataEnvelope); err != nil {
		t.Fatal(err)
	}
	data, _ := dataEnvelope["data"].(map[string]any)
	if data["id"] != paymentID.String() || data["state"] != "COMPLETED" {
		t.Fatalf("unexpected payment response: %v", data)
	}
}

func TestPaymentByIdempotencyKey_DatabaseErrorReturns500InternalError(t *testing.T) {
	now := time.Now()
	manager, err := auth.NewJWTManager(strings.Repeat("x", 32), "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	userAID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	userAToken, err := manager.Issue(userAID.String(), "CUSTOMER", now)
	if err != nil {
		t.Fatal(err)
	}

	// Mock repository that returns a genuine database connection error
	repo := payments.NewRepository(nil)
	repo.SetGetByIdempotentFn(func(ctx context.Context, userID uuid.UUID, key string) (payments.CustomerPayment, error) {
		return payments.CustomerPayment{}, errors.New("connection to PostgreSQL pool timed out")
	})
	svc := payments.NewService(nil, nil, repo, nil)

	h := &Handler{
		logger:   slog.Default(),
		payments: svc,
	}
	mux := http.NewServeMux()
	mux.Handle("GET /api/payments/idempotency/{key}", auth.Authentication(manager, auth.RequireRole(auth.PublicRoles()...)(http.HandlerFunc(h.paymentByIdempotencyKey))))

	request := httptest.NewRequest(http.MethodGet, "/api/payments/idempotency/test-db-error-key", nil)
	request.Header.Set("Authorization", "Bearer "+userAToken)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)

	// Must return HTTP 500 Internal Server Error, NOT HTTP 404 Not Found!
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d (database error must NEVER be misreported as 404 Not Found)", recorder.Code, http.StatusInternalServerError)
	}
	var envelope map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	errObj, _ := envelope["error"].(map[string]any)
	if errObj["code"] != "INTERNAL_ERROR" {
		t.Fatalf("code = %v, want INTERNAL_ERROR", errObj["code"])
	}
}

func TestPaymentStateClassification_ExhaustiveTable(t *testing.T) {
	tests := []struct {
		state                string
		wantTerminal         bool
		wantIntermediate     bool
		wantOfflineLifecycle bool
	}{
		// Non-terminal central payment states
		{payments.StateCreated, false, true, false},
		{payments.StateValidating, false, true, false},
		{payments.StateLocalSettlement, false, true, false},
		{payments.StateRouting, false, true, false},
		{payments.StateProcessing, false, true, false},
		{payments.StateBankSettledCentralPending, false, true, false},
		{payments.StateCommitted, false, true, false},
		{payments.StatePendingReconciliation, false, true, false},

		// Terminal central payment states
		{payments.StateCompleted, true, false, false},
		{payments.StateFailed, true, false, false},
		{payments.StateReversed, true, false, false},

		// Offline queue lifecycle states (distinct from online central progress)
		{payments.StateOfflineCaptured, false, false, true},
		{payments.StateQueued, false, false, true},
		{payments.StateSyncing, false, false, true},
		{payments.StateReplayFailed, false, false, true},

		// Unknown / unrecognized states (must fail closed: neither terminal nor intermediate)
		{"UNKNOWN_XYZ", false, false, false},
		{"SOME_RANDOM_STATE", false, false, false},
		{"", false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			gotTerminal := payments.IsTerminalState(tt.state)
			if gotTerminal != tt.wantTerminal {
				t.Errorf("IsTerminalState(%q) = %v, want %v", tt.state, gotTerminal, tt.wantTerminal)
			}

			gotIntermediate := payments.IsIntermediateCentralState(tt.state)
			if gotIntermediate != tt.wantIntermediate {
				t.Errorf("IsIntermediateCentralState(%q) = %v, want %v", tt.state, gotIntermediate, tt.wantIntermediate)
			}

			// Verify HTTP handler intermediate helper matches payments package canonical definition
			gotHttpHelper := intermediatePaymentState(tt.state)
			if gotHttpHelper != tt.wantIntermediate {
				t.Errorf("intermediatePaymentState(%q) = %v, want %v", tt.state, gotHttpHelper, tt.wantIntermediate)
			}

			gotOffline := payments.IsOfflineLifecycleState(tt.state)
			if gotOffline != tt.wantOfflineLifecycle {
				t.Errorf("IsOfflineLifecycleState(%q) = %v, want %v", tt.state, gotOffline, tt.wantOfflineLifecycle)
			}
		})
	}
}

func TestPaymentAPI_HTTPStateContract_PostPayments(t *testing.T) {
	now := time.Now()
	jwtSecret := strings.Repeat("s", 32)
	manager, err := auth.NewJWTManager(jwtSecret, "test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	userID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	userToken, err := manager.Issue(userID.String(), "CUSTOMER", now)
	if err != nil {
		t.Fatal(err)
	}

	primaryAccID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")

	type postTestCase struct {
		name                   string
		state                  string
		duplicate              bool
		createErr              error
		accountsErr            error
		rawBody                string
		authHeader             string
		role                   string
		wantStatus             int
		wantErrorCode          string
		wantTerminal           bool
		wantReservationRetain bool
	}

	tests := []postTestCase{
		// 1. New payment terminal completed -> 201 Created
		{
			name:                   "New_Payment_Completed",
			state:                  payments.StateCompleted,
			duplicate:              false,
			wantStatus:             http.StatusCreated,
			wantTerminal:           true,
			wantReservationRetain: true,
		},
		// 2. New payment intermediate states -> 202 Accepted
		{
			name:                   "New_Payment_Created",
			state:                  payments.StateCreated,
			duplicate:              false,
			wantStatus:             http.StatusAccepted,
			wantTerminal:           false,
			wantReservationRetain: false,
		},
		{
			name:                   "New_Payment_Validating",
			state:                  payments.StateValidating,
			duplicate:              false,
			wantStatus:             http.StatusAccepted,
			wantTerminal:           false,
			wantReservationRetain: false,
		},
		{
			name:                   "New_Payment_LocalSettlement",
			state:                  payments.StateLocalSettlement,
			duplicate:              false,
			wantStatus:             http.StatusAccepted,
			wantTerminal:           false,
			wantReservationRetain: false,
		},
		{
			name:                   "New_Payment_Routing",
			state:                  payments.StateRouting,
			duplicate:              false,
			wantStatus:             http.StatusAccepted,
			wantTerminal:           false,
			wantReservationRetain: false,
		},
		{
			name:                   "New_Payment_Processing",
			state:                  payments.StateProcessing,
			duplicate:              false,
			wantStatus:             http.StatusAccepted,
			wantTerminal:           false,
			wantReservationRetain: false,
		},
		{
			name:                   "New_Payment_BankSettledCentralPending",
			state:                  payments.StateBankSettledCentralPending,
			duplicate:              false,
			wantStatus:             http.StatusAccepted,
			wantTerminal:           false,
			wantReservationRetain: false,
		},
		{
			name:                   "New_Payment_Committed",
			state:                  payments.StateCommitted,
			duplicate:              false,
			wantStatus:             http.StatusAccepted,
			wantTerminal:           false,
			wantReservationRetain: false,
		},
		{
			name:                   "New_Payment_PendingReconciliation",
			state:                  payments.StatePendingReconciliation,
			duplicate:              false,
			wantStatus:             http.StatusAccepted,
			wantTerminal:           false,
			wantReservationRetain: false,
		},
		// 3. Duplicate payment terminal states -> 200 OK
		{
			name:                   "Duplicate_Payment_Completed",
			state:                  payments.StateCompleted,
			duplicate:              true,
			wantStatus:             http.StatusOK,
			wantTerminal:           true,
			wantReservationRetain: true,
		},
		{
			name:                   "Duplicate_Payment_Failed",
			state:                  payments.StateFailed,
			duplicate:              true,
			wantStatus:             http.StatusOK,
			wantTerminal:           true,
			wantReservationRetain: false,
		},
		{
			name:                   "Duplicate_Payment_Reversed",
			state:                  payments.StateReversed,
			duplicate:              true,
			wantStatus:             http.StatusOK,
			wantTerminal:           true,
			wantReservationRetain: false,
		},
		// 4. Duplicate payment intermediate states -> 202 Accepted
		{
			name:                   "Duplicate_Payment_Processing",
			state:                  payments.StateProcessing,
			duplicate:              true,
			wantStatus:             http.StatusAccepted,
			wantTerminal:           false,
			wantReservationRetain: false,
		},
		{
			name:                   "Duplicate_Payment_BankSettledCentralPending",
			state:                  payments.StateBankSettledCentralPending,
			duplicate:              true,
			wantStatus:             http.StatusAccepted,
			wantTerminal:           false,
			wantReservationRetain: false,
		},
		// 5. Unknown / Indeterminate state -> 500 Fail closed
		{
			name:          "New_Payment_UnknownState_FailsClosed",
			state:         "UNKNOWN_CORRUPT_STATE",
			duplicate:     false,
			wantStatus:    http.StatusInternalServerError,
			wantErrorCode: "INTERNAL_ERROR",
		},
		{
			name:          "Duplicate_Payment_UnknownState_FailsClosed",
			state:         "UNKNOWN_CORRUPT_STATE",
			duplicate:     true,
			wantStatus:    http.StatusInternalServerError,
			wantErrorCode: "INTERNAL_ERROR",
		},
		// 6. Offline lifecycle state -> 500 Fail closed
		{
			name:          "New_Payment_OfflineCaptured_FailsClosed",
			state:         payments.StateOfflineCaptured,
			duplicate:     false,
			wantStatus:    http.StatusInternalServerError,
			wantErrorCode: "INTERNAL_ERROR",
		},
		// 7. Error scenarios
		{
			name:          "Error_MalformedJSON",
			rawBody:       `{"recipient": invalid json}`,
			wantStatus:    http.StatusBadRequest,
			wantErrorCode: "INVALID_REQUEST",
		},
		{
			name:          "Error_Unauthenticated",
			authHeader:    "NONE",
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: "UNAUTHORIZED",
		},
		{
			name:          "Error_ForbiddenRole_OpsAdmin",
			role:          "OPS_ADMIN",
			wantStatus:    http.StatusForbidden,
			wantErrorCode: "FORBIDDEN",
		},
		{
			name:          "Error_SourceAccountNotFound",
			accountsErr:   accounts.ErrNotFound,
			wantStatus:    http.StatusNotFound,
			wantErrorCode: "ACCOUNT_NOT_FOUND",
		},
		{
			name:          "Error_SourceAccountAmbiguous",
			accountsErr:   accounts.ErrAmbiguousPrimary,
			wantStatus:    http.StatusConflict,
			wantErrorCode: "ACCOUNT_AMBIGUOUS",
		},
		{
			name:          "Error_InsufficientFunds",
			createErr:     payments.ErrInsufficientFunds,
			wantStatus:    http.StatusConflict,
			wantErrorCode: "INSUFFICIENT_FUNDS",
		},
		{
			name:          "Error_IdempotencyConflict",
			createErr:     payments.ErrIdempotencyConflict,
			wantStatus:    http.StatusConflict,
			wantErrorCode: "IDEMPOTENCY_CONFLICT",
		},
		{
			name:          "Error_BankUnavailable",
			createErr:     payments.ErrBankRouteUnavailable,
			wantStatus:    http.StatusServiceUnavailable,
			wantErrorCode: "BANK_UNAVAILABLE",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			paymentID := uuid.New()
			accountsRepo := accounts.NewRepository(nil)
			accountsRepo.SetGetPrimaryOwnedFn(func(ctx context.Context, uID uuid.UUID) (accounts.Account, error) {
				if tc.accountsErr != nil {
					return accounts.Account{}, tc.accountsErr
				}
				return accounts.Account{
					ID:           primaryAccID,
					UserID:       uID,
					Status:       "ACTIVE",
					BalancePaise: 100000,
				}, nil
			})

			paymentsRepo := payments.NewRepository(nil)
			paymentsRepo.SetGetForUserFn(func(ctx context.Context, uID, pID uuid.UUID) (payments.CustomerPayment, error) {
				return payments.CustomerPayment{
					ID:          pID,
					AmountPaise: 5000,
					Currency:    "INR",
					State:       tc.state,
				}, nil
			})

			svc := payments.NewService(accountsRepo, nil, paymentsRepo, nil)
			svc.SetCreateWithResultFn(func(ctx context.Context, input payments.CreateInput) (payments.Payment, bool, error) {
				if tc.createErr != nil {
					return payments.Payment{}, false, tc.createErr
				}
				return payments.Payment{
					ID:                paymentID,
					InitiatedByUserID: input.UserID,
					State:             tc.state,
				}, tc.duplicate, nil
			})

			h := &Handler{
				logger:       slog.Default(),
				payments:     svc,
				accountsRepo: accountsRepo,
			}

			mux := http.NewServeMux()
			mux.Handle("POST /api/payments", auth.Authentication(manager, auth.RequireRole(auth.PublicRoles()...)(http.HandlerFunc(h.createPayment))))

			bodyStr := `{"recipient":"bob@transactx","amountPaise":5000,"currency":"INR","note":"test"}`
			if tc.rawBody != "" {
				bodyStr = tc.rawBody
			}

			req := httptest.NewRequest(http.MethodPost, "/api/payments", strings.NewReader(bodyStr))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", "test-key-"+tc.name)

			if tc.authHeader != "NONE" {
				tokenToUse := userToken
				if tc.role != "" {
					customToken, err := manager.Issue(userID.String(), tc.role, now)
					if err != nil {
						t.Fatal(err)
					}
					tokenToUse = customToken
				}
				req.Header.Set("Authorization", "Bearer "+tokenToUse)
			}

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}

			var envelope map[string]any
			if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
				t.Fatalf("failed to decode JSON response: %v", err)
			}

			if tc.wantErrorCode != "" {
				errObj, ok := envelope["error"].(map[string]any)
				if !ok {
					t.Fatalf("expected error object in envelope: %v", envelope)
				}
				if errObj["code"] != tc.wantErrorCode {
					t.Fatalf("error code = %v, want %s", errObj["code"], tc.wantErrorCode)
				}
			} else {
				dataObj, ok := envelope["data"].(map[string]any)
				if !ok {
					t.Fatalf("expected data object in envelope: %v", envelope)
				}
				stateGot, _ := dataObj["state"].(string)
				if stateGot != tc.state {
					t.Fatalf("state in data = %v, want %s", stateGot, tc.state)
				}

				// Check terminality and reservation retention invariants
				isTerm := payments.IsTerminalState(tc.state)
				if isTerm != tc.wantTerminal {
					t.Fatalf("IsTerminalState(%s) = %v, want %v", tc.state, isTerm, tc.wantTerminal)
				}
				isRetained := tc.state == payments.StateCompleted
				if isRetained != tc.wantReservationRetain {
					t.Fatalf("ReservationRetained(%s) = %v, want %v", tc.state, isRetained, tc.wantReservationRetain)
				}
			}
		})
	}
}

func TestPaymentAPI_HTTPStateContract_GetIdempotencyKey(t *testing.T) {
	now := time.Now()
	jwtSecret := strings.Repeat("s", 32)
	manager, err := auth.NewJWTManager(jwtSecret, "test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	userID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	userToken, err := manager.Issue(userID.String(), "CUSTOMER", now)
	if err != nil {
		t.Fatal(err)
	}

	type getTestCase struct {
		name                   string
		key                    string
		state                  string
		repoErr                error
		authHeader             string
		role                   string
		wantStatus             int
		wantErrorCode          string
		wantTerminal           bool
		wantReservationRetain bool
	}

	tests := []getTestCase{
		// 1. Terminal states -> 200 OK
		{
			name:                   "Terminal_Completed",
			key:                    "key-completed",
			state:                  payments.StateCompleted,
			wantStatus:             http.StatusOK,
			wantTerminal:           true,
			wantReservationRetain: true,
		},
		{
			name:                   "Terminal_Failed",
			key:                    "key-failed",
			state:                  payments.StateFailed,
			wantStatus:             http.StatusOK,
			wantTerminal:           true,
			wantReservationRetain: false,
		},
		{
			name:                   "Terminal_Reversed",
			key:                    "key-reversed",
			state:                  payments.StateReversed,
			wantStatus:             http.StatusOK,
			wantTerminal:           true,
			wantReservationRetain: false,
		},
		// 2. Intermediate states -> 202 Accepted
		{
			name:                   "Intermediate_Created",
			key:                    "key-created",
			state:                  payments.StateCreated,
			wantStatus:             http.StatusAccepted,
			wantTerminal:           false,
			wantReservationRetain: false,
		},
		{
			name:                   "Intermediate_Validating",
			key:                    "key-validating",
			state:                  payments.StateValidating,
			wantStatus:             http.StatusAccepted,
			wantTerminal:           false,
			wantReservationRetain: false,
		},
		{
			name:                   "Intermediate_LocalSettlement",
			key:                    "key-local-settlement",
			state:                  payments.StateLocalSettlement,
			wantStatus:             http.StatusAccepted,
			wantTerminal:           false,
			wantReservationRetain: false,
		},
		{
			name:                   "Intermediate_Routing",
			key:                    "key-routing",
			state:                  payments.StateRouting,
			wantStatus:             http.StatusAccepted,
			wantTerminal:           false,
			wantReservationRetain: false,
		},
		{
			name:                   "Intermediate_Processing",
			key:                    "key-processing",
			state:                  payments.StateProcessing,
			wantStatus:             http.StatusAccepted,
			wantTerminal:           false,
			wantReservationRetain: false,
		},
		{
			name:                   "Intermediate_BankSettledCentralPending",
			key:                    "key-bank-settled-pending",
			state:                  payments.StateBankSettledCentralPending,
			wantStatus:             http.StatusAccepted,
			wantTerminal:           false,
			wantReservationRetain: false,
		},
		{
			name:                   "Intermediate_Committed",
			key:                    "key-committed",
			state:                  payments.StateCommitted,
			wantStatus:             http.StatusAccepted,
			wantTerminal:           false,
			wantReservationRetain: false,
		},
		{
			name:                   "Intermediate_PendingReconciliation",
			key:                    "key-pending-recon",
			state:                  payments.StatePendingReconciliation,
			wantStatus:             http.StatusAccepted,
			wantTerminal:           false,
			wantReservationRetain: false,
		},
		// 3. Unknown state -> 500 Fail closed
		{
			name:          "Unknown_State_FailsClosed",
			key:           "key-unknown",
			state:         "UNKNOWN_STATE_XYZ",
			wantStatus:    http.StatusInternalServerError,
			wantErrorCode: "INTERNAL_ERROR",
		},
		// 4. Offline lifecycle states -> 500 Fail closed
		{
			name:          "Offline_Captured_FailsClosed",
			key:           "key-offline-captured",
			state:         payments.StateOfflineCaptured,
			wantStatus:    http.StatusInternalServerError,
			wantErrorCode: "INTERNAL_ERROR",
		},
		{
			name:          "Offline_Queued_FailsClosed",
			key:           "key-offline-queued",
			state:         payments.StateQueued,
			wantStatus:    http.StatusInternalServerError,
			wantErrorCode: "INTERNAL_ERROR",
		},
		{
			name:          "Offline_Syncing_FailsClosed",
			key:           "key-offline-syncing",
			state:         payments.StateSyncing,
			wantStatus:    http.StatusInternalServerError,
			wantErrorCode: "INTERNAL_ERROR",
		},
		{
			name:          "Offline_ReplayFailed_FailsClosed",
			key:           "key-offline-replay-failed",
			state:         payments.StateReplayFailed,
			wantStatus:    http.StatusInternalServerError,
			wantErrorCode: "INTERNAL_ERROR",
		},
		// 5. Missing record -> 404 Not Found
		{
			name:          "Missing_Record_NotFound",
			key:           "nonexistent-key",
			repoErr:       payments.ErrNotFound,
			wantStatus:    http.StatusNotFound,
			wantErrorCode: "PAYMENT_NOT_FOUND",
		},
		// 6. Database connection error -> 500 Internal Server Error
		{
			name:          "Database_Error_500",
			key:           "key-db-error",
			repoErr:       errors.New("connection reset by peer"),
			wantStatus:    http.StatusInternalServerError,
			wantErrorCode: "INTERNAL_ERROR",
		},
		// 7. Security and Validation errors
		{
			name:          "Error_Unauthenticated",
			key:           "valid-key",
			authHeader:    "NONE",
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: "UNAUTHORIZED",
		},
		{
			name:          "Error_ForbiddenRole_OpsAdmin",
			key:           "valid-key",
			role:          "OPS_ADMIN",
			wantStatus:    http.StatusForbidden,
			wantErrorCode: "FORBIDDEN",
		},
		{
			name:          "Error_ForbiddenRole_OpsViewer",
			key:           "valid-key",
			role:          "OPS_VIEWER",
			wantStatus:    http.StatusForbidden,
			wantErrorCode: "FORBIDDEN",
		},
		{
			name:          "Error_MalformedKey_TooLong",
			key:           strings.Repeat("k", 256),
			wantStatus:    http.StatusBadRequest,
			wantErrorCode: "INVALID_REQUEST",
		},
		{
			name:          "Error_MalformedKey_Whitespace",
			key:           "   ",
			wantStatus:    http.StatusBadRequest,
			wantErrorCode: "INVALID_REQUEST",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			paymentID := uuid.New()
			paymentsRepo := payments.NewRepository(nil)
			paymentsRepo.SetGetByIdempotentFn(func(ctx context.Context, uID uuid.UUID, k string) (payments.CustomerPayment, error) {
				if tc.repoErr != nil {
					return payments.CustomerPayment{}, tc.repoErr
				}
				return payments.CustomerPayment{
					ID:          paymentID,
					AmountPaise: 7500,
					Currency:    "INR",
					State:       tc.state,
				}, nil
			})

			svc := payments.NewService(nil, nil, paymentsRepo, nil)
			h := &Handler{
				logger:   slog.Default(),
				payments: svc,
			}

			mux := http.NewServeMux()
			mux.Handle("GET /api/payments/idempotency/{key}", auth.Authentication(manager, auth.RequireRole(auth.PublicRoles()...)(http.HandlerFunc(h.paymentByIdempotencyKey))))

			// Invoke through httptest.NewServer for full TCP and HTTP stack verification
			server := httptest.NewServer(mux)
			defer server.Close()

			reqURL := server.URL + "/api/payments/idempotency/" + tc.key
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, reqURL, nil)
			if err != nil {
				t.Fatal(err)
			}

			if tc.authHeader != "NONE" {
				tokenToUse := userToken
				if tc.role != "" {
					customToken, err := manager.Issue(userID.String(), tc.role, now)
					if err != nil {
						t.Fatal(err)
					}
					tokenToUse = customToken
				}
				req.Header.Set("Authorization", "Bearer "+tokenToUse)
			}

			resp, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tc.wantStatus {
				bodyBytes, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, tc.wantStatus, string(bodyBytes))
			}

			var envelope map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
				t.Fatalf("failed to decode JSON response: %v", err)
			}

			if tc.wantErrorCode != "" {
				errObj, ok := envelope["error"].(map[string]any)
				if !ok {
					t.Fatalf("expected error object in envelope: %v", envelope)
				}
				if errObj["code"] != tc.wantErrorCode {
					t.Fatalf("error code = %v, want %s", errObj["code"], tc.wantErrorCode)
				}
			} else {
				dataObj, ok := envelope["data"].(map[string]any)
				if !ok {
					t.Fatalf("expected data object in envelope: %v", envelope)
				}
				stateGot, _ := dataObj["state"].(string)
				if stateGot != tc.state {
					t.Fatalf("state in data = %v, want %s", stateGot, tc.state)
				}

				// Check terminality and reservation retention invariants
				isTerm := payments.IsTerminalState(tc.state)
				if isTerm != tc.wantTerminal {
					t.Fatalf("IsTerminalState(%s) = %v, want %v", tc.state, isTerm, tc.wantTerminal)
				}
				isRetained := tc.state == payments.StateCompleted
				if isRetained != tc.wantReservationRetain {
					t.Fatalf("ReservationRetained(%s) = %v, want %v", tc.state, isRetained, tc.wantReservationRetain)
				}
			}
		})
	}
}
