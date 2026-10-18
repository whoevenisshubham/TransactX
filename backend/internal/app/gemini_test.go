package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/config"
	"github.com/transactx/backend/internal/investigator"
)

// Fresh composition models a cold boot, not a native process kill/restart proof.
func TestGeminiColdCompositionAfterExpiredApproval(t *testing.T) {
	dsn := os.Getenv("APP_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("APP_TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Hostname() != "127.0.0.1" || !strings.HasPrefix(parsed.Path, "/txv3_oidc_") {
		t.Fatal("requires explicit isolated loopback database")
	}
	t.Setenv("APP_DEVELOPMENT", "true")
	t.Setenv("TX_SIMULATION_MODE", "true")
	t.Setenv("TX_PARTICIPANT_PROVISIONING", "true")
	t.Setenv("GOOGLE_OAUTH_ENABLED", "false")
	t.Setenv("JWT_SECRET", strings.Repeat("f", 32))
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("TX_GEMINI_ENABLED", "true")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	path := filepath.Join(t.TempDir(), "expired.json")
	digest := sha256.Sum256([]byte("fixture-key"))
	raw, _ := json.Marshal(investigator.GeminiApproval{ProjectID: "fixture", KeySHA256: hex.EncodeToString(digest[:]), Model: "gemini-3.5-flash-lite", BillingDisabled: true, FreeTierAvailable: true, ReviewedAt: time.Now().Add(-48 * time.Hour), ValidUntil: time.Now().Add(-24 * time.Hour)})
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct{ name, review, key string }{{"missing-file", "", "fixture-key"}, {"expired-file", path, "fixture-key"}, {"missing-credentials", path, ""}} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Setenv("TX_GEMINI_APPROVAL_FILE", scenario.review)
			t.Setenv("GEMINI_API_KEY", scenario.key)
			t.Setenv("GEMINI_MODEL", "gemini-3.5-flash-lite")
			cfg, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			pool, err := pgxpool.New(context.Background(), dsn)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(pool.Close)
			env, err := BuildEnvironmentWithPool(context.Background(), cfg, pool, logger)
			if err != nil {
				t.Fatalf("cold composition blocked by optional review: %v", err)
			}
			t.Cleanup(env.Close)
			recorder := httptest.NewRecorder()
			env.NewHTTPHandler().ServeHTTP(recorder, httptest.NewRequest("GET", "/api/auth/options", nil))
			if recorder.Code != 200 || env.AuthService == nil || env.PaymentsService == nil || NewWorker(env) == nil {
				t.Fatal("core availability blocked by provider review")
			}
			// A real stored password user can log in and reach authenticated payment reads.
			identifier := "expiry-" + uuid.NewString() + "@fixture"
			user, err := env.AuthService.Register(context.Background(), auth.RegisterInput{Name: "Expiry fixture", Phone: uuid.NewString()[:24], PaymentIdentifier: identifier, Password: "fixture-password-123"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				// Cleanup only this newly generated zero-balance user's rows, with checked errors.
				for _, query := range []string{`DELETE FROM bank_a.accounts WHERE id IN (SELECT bank_account_id FROM accounts WHERE user_id=$1)`, `DELETE FROM accounts WHERE user_id=$1`, `DELETE FROM users WHERE id=$1`} {
					if _, err := pool.Exec(context.Background(), query, user.ID); err != nil {
						t.Error(err)
					}
				}
			})
			token, _, err := env.AuthService.Login(context.Background(), identifier, "fixture-password-123", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			for _, route := range []string{"/api/accounts", "/api/payments"} {
				req := httptest.NewRequest("GET", route, nil)
				req.Header.Set("Authorization", "Bearer "+token)
				response := httptest.NewRecorder()
				env.NewHTTPHandler().ServeHTTP(response, req)
				if response.Code != 200 {
					t.Fatalf("core route %s unavailable: %d", route, response.Code)
				}
			}
			store, err := investigator.NewPostgresInvestigationStore(pool, time.Now)
			if err != nil {
				t.Fatal(err)
			}
			fallback := optionalGemini(context.Background(), cfg, pool, store, logger)
			if fallback.UnavailableReason != "provider_free_tier_unverified" || fallback.Provider != nil {
				t.Fatal("expired/missing approval did not fail closed")
			}
		})
	}
}
