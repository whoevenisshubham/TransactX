package http

import (
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
	request := httptest.NewRequest(http.MethodPost, "/api/payments", strings.NewReader(`{"sourceAccountId":"bad","recipient":"bob@transactx","amountPaise":100,"currency":"INR"}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	body, _ := io.ReadAll(recorder.Result().Body)
	if strings.Contains(string(body), "database") || strings.Contains(string(body), "password") {
		t.Fatalf("unsafe error response: %s", body)
	}
}

func TestCreatePaymentRejectsMalformedSourceAccountID(t *testing.T) {
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
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}
