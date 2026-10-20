package http

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/auth"
)

func TestGoogleHTTPDisabled(t *testing.T) {
	manager, _ := auth.NewJWTManager(strings.Repeat("x", 32), "fixture", time.Hour)
	handler := NewHandler(nil, slog.Default(), auth.NewService(nil, manager, "BANK-A"), manager)
	for _, path := range []string{"/api/auth/oidc/google/start", "/api/auth/oidc/google/callback"} {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Code != 503 || r.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("disabled Google route did not fail closed")
		}
	}
}
func TestGoogleHTTPBindingDenialAndOrigins(t *testing.T) {
	dsn := os.Getenv("OIDC_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("OIDC_TEST_DATABASE_URL is not set")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Hostname() != "127.0.0.1" || !strings.HasPrefix(u.Path, "/txv3_oidc_") {
		t.Fatal("requires isolated loopback OIDC database")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	manager, _ := auth.NewJWTManager(strings.Repeat("x", 32), "fixture", time.Hour)
	service := auth.NewService(pool, manager, "BANK-A").WithParticipantProvisioner(auth.NewSimulationParticipantAccountProvisioner())
	if err := service.EnableGoogle(auth.GoogleConfig{ClientID: "fixture", ClientSecret: "fixture-only", RedirectURI: "https://api.example.test/api/auth/oidc/google/callback", FrontendOrigin: "https://front.example.test"}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(pool, slog.Default(), service, manager)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`DELETE FROM auth_oidc_admission`)
	t.Cleanup(func() {
		for _, sql := range []string{`DELETE FROM auth_login_flows`, `DELETE FROM auth_oidc_admission`} {
			if _, err := pool.Exec(context.Background(), sql); err != nil {
				t.Error(err)
			}
		}
	})
	for _, origin := range []string{"", "https://evil.example.test"} {
		req := httptest.NewRequest("POST", "/api/auth/oidc/exchange", strings.NewReader(`{"code":"fixture","verifier":"fixture"}`))
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, req)
		if r.Code != 403 {
			t.Fatalf("exchange origin accepted: %d", r.Code)
		}
	}
	req := httptest.NewRequest("GET", "/api/auth/oidc/google/start?app_challenge=invalid&return=https://evil.test", nil)
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, req)
	if r.Code != 400 || r.Header().Get("Location") != "" {
		t.Fatal("redirect injection accepted")
	}
	state := strings.Repeat("s", 43)
	binding := strings.Repeat("b", 43)
	hash := func(s string) []byte { v := sha256.Sum256([]byte(s)); return v[:] }
	exec(`INSERT INTO auth_login_flows(state_hash,binding_hash,nonce_hash,provider_verifier,app_challenge,expires_at) VALUES($1,$2,$3,$4,$5,CURRENT_TIMESTAMP+INTERVAL '10 minutes')`, hash(state), hash(binding), hash("nonce"), strings.Repeat("v", 43), base64.RawURLEncoding.EncodeToString(hash("app")))
	callback := func(cookie string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/auth/oidc/google/callback?state="+state+"&error=access_denied", nil)
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: oidcCookie, Value: cookie})
		}
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, req)
		return r
	}
	for _, cookie := range []string{"", strings.Repeat("z", 43)} {
		r := callback(cookie)
		if r.Header().Get("Location") != "https://front.example.test/auth/callback?error=invalid_flow" || len(r.Result().Cookies()) != 0 {
			t.Fatal("missing/wrong cookie accepted or destroyed valid flow")
		}
	}
	r = callback(binding)
	if r.Code != 303 || r.Header().Get("Location") != "https://front.example.test/auth/callback?error=denied" {
		t.Fatal("denied consent handling failed")
	}
	cookies := r.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].Domain != "" || cookies[0].Path != "/" || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("unsafe binding cookie attributes")
	}
	if r.Header().Get("Referrer-Policy") != "no-referrer" || r.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("callback could leak/cache a code")
	}
	if callback(binding).Header().Get("Location") != "https://front.example.test/auth/callback?error=invalid_flow" {
		t.Fatal("denied state replay accepted")
	}
}
