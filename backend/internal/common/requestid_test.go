package common_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/transactx/backend/internal/common"
)

func TestRequestIDMiddlewarePreservesHeader(t *testing.T) {
	var capturedContextID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedContextID = common.GetRequestID(r)
		w.WriteHeader(http.StatusOK)
	})
	middleware := common.RequestIDMiddleware(inner)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "client-request-id-12345")
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") != "client-request-id-12345" {
		t.Fatalf("Response header X-Request-ID = %q, want client-request-id-12345", rec.Header().Get("X-Request-ID"))
	}
	if capturedContextID != "client-request-id-12345" {
		t.Fatalf("Context request ID = %q, want client-request-id-12345", capturedContextID)
	}
}

func TestRequestIDMiddlewareGeneratesIDWhenMissing(t *testing.T) {
	var capturedContextID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedContextID = common.GetRequestID(r)
		w.WriteHeader(http.StatusOK)
	})
	middleware := common.RequestIDMiddleware(inner)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	resHeader := rec.Header().Get("X-Request-ID")
	if resHeader == "" {
		t.Fatal("Response header X-Request-ID should be generated when missing")
	}
	if capturedContextID != resHeader {
		t.Fatalf("Context request ID %q != Response header %q", capturedContextID, resHeader)
	}
}
