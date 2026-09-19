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
