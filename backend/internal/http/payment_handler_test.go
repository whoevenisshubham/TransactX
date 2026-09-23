package http

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/common"
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

func TestRequestIDEndToEndPropagation(t *testing.T) {
	var capturedBankRequestID string
	bankServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBankRequestID = r.Header.Get("X-Request-ID")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"available":true}`))
	}))
	defer bankServer.Close()

	client, err := bank.NewHTTPClient(bankServer.URL, bankServer.Client())
	if err != nil {
		t.Fatal(err)
	}

	const clientReqID = "e2e-request-id-98765"
	req := httptest.NewRequest(http.MethodGet, "/test-downstream", nil)
	req.Header.Set("X-Request-ID", clientReqID)

	var apiResponseReqID string
	handler := common.RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiResponseReqID = common.GetRequestID(r)
		_, _ = client.GetHealth(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") != clientReqID {
		t.Fatalf("API response header X-Request-ID = %q, want %q", rec.Header().Get("X-Request-ID"), clientReqID)
	}
	if apiResponseReqID != clientReqID {
		t.Fatalf("API internal context request ID = %q, want %q", apiResponseReqID, clientReqID)
	}
	if capturedBankRequestID != clientReqID {
		t.Fatalf("Downstream bank service received X-Request-ID = %q, want %q", capturedBankRequestID, clientReqID)
	}
}
