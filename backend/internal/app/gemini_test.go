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
func TestGeminiColdCompositionWithOwnerReview(t *testing.T) {
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
	digest := sha256.Sum256([]byte("fixture-key"))
	raw, err := json.Marshal(investigator.GeminiReview{Provider: "google", ProjectID: "fixture", KeySHA256: hex.EncodeToString(digest[:]), Model: "gemini-3.5-flash-lite", BillingDisabled: true, FreeTierAvailable: true, ReviewedAt: time.Now().Add(-210 * 24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name, review, key string
		enabled, ready    bool
	}{
		{"missing-review", "", "fixture-key", true, false},
		{"missing-key", string(raw), "", true, false},
		{"changed-key", string(raw), "rotated-key", true, false},
		{"seven-month-review-cold-start", string(raw), "fixture-key", true, true},
		{"seven-month-review-restart", string(raw), "fixture-key", true, true},
		{"disabled", string(raw), "fixture-key", false, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Setenv("TX_GEMINI_OWNER_REVIEW", scenario.review)
			t.Setenv("GEMINI_PROVIDER", "google")
			t.Setenv("GEMINI_PROJECT_ID", "fixture")
			if !scenario.enabled {
				t.Setenv("TX_GEMINI_ENABLED", "false")
			}
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
			identifier := "review-" + uuid.NewString() + "@fixture"
			user, err := env.AuthService.Register(context.Background(), auth.RegisterInput{Name: "Review fixture", Phone: uuid.NewString()[:24], PaymentIdentifier: identifier, Password: "fixture-password-123"})
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
			if scenario.ready {
				if fallback.Provider == nil || fallback.UnavailableReason != "" {
					t.Fatal("old unchanged review blocked provider composition")
				}
			} else if scenario.enabled {
				if fallback.UnavailableReason != "provider_free_tier_unverified" || fallback.Provider != nil {
					t.Fatal("unreviewed configuration did not fail closed")
				}
			} else if fallback.Provider != nil || fallback.Mode != investigator.ModeEvidenceOnly {
				t.Fatal("disable flag did not select evidence-only mode")
			}
			var attempts int
			if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM gemini_free_attempts`).Scan(&attempts); err != nil || attempts != 0 {
				t.Fatal("cold composition made a provider attempt")
			}

		})
	}
}
