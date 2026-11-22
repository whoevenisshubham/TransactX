package http

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
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
	handler := NewHandler(nil, slog.Default(), nil, manager)
	request := httptest.NewRequest(http.MethodGet, "/api/payments/idempotency/nonexistent-key", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

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
