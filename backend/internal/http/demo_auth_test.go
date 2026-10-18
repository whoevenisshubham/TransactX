package http

import (
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicDemoAuthCompositionFailsClosed(t *testing.T) {
	for _, options := range []RuntimeOptions{
		{}, {PublicDemoAuth: true}, {PublicDemoAuth: true, SimulationMode: true, AllowRegistration: true},
	} {
		h := NewHandlerWithReconciliationAndRuntimeOptions(nil, slog.Default(), nil, nil, nil, nil, options)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/auth/demo", strings.NewReader(`{"persona":"operator"}`)))
		if w.Code != 403 || !strings.Contains(w.Body.String(), "DEMO_DISABLED") || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("unsafe composition admitted public demo")
		}
	}
}

func TestPublicDemoAuthRejectsAdditionalClaims(t *testing.T) {
	h := &Handler{publicDemoAuth: true}
	for _, body := range []string{`{"persona":"operator","role":"OPS_ADMIN"}`, `{"persona":"customer","userId":"arbitrary"}`, `{"persona":"customer"} {}`, `{`} {
		w := httptest.NewRecorder()
		h.demoLogin(w, httptest.NewRequest("POST", "/api/auth/demo", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("additional claims accepted: status %d", w.Code)
		}
	}
}
