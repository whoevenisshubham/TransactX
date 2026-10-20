package config

import "testing"

func TestGoogleSignInFailsClosed(t *testing.T) {
	t.Setenv("APP_DEVELOPMENT", "true")
	t.Setenv("TX_ALLOW_REGISTRATION", "false")
	t.Setenv("TX_SIMULATION_MODE", "true")
	t.Setenv("TX_PARTICIPANT_PROVISIONING", "true")
	t.Setenv("GOOGLE_OAUTH_ENABLED", "true")
	t.Setenv("GOOGLE_OAUTH_CLIENT_ID", "fixture-client")
	t.Setenv("GOOGLE_OAUTH_CLIENT_SECRET", "fixture-only")
	t.Setenv("GOOGLE_OAUTH_REDIRECT_URI", "https://api.example.test/api/auth/oidc/google/callback")
	t.Setenv("OAUTH_FRONTEND_ORIGIN", "https://front.example.test")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://front.example.test")
	if cfg, err := Load(); err != nil || !cfg.GoogleOAuthEnabled {
		t.Fatalf("safe config: %v", err)
	}
	for _, item := range []struct{ key, value string }{{"TX_PARTICIPANT_PROVISIONING", "false"}, {"TX_ALLOW_REGISTRATION", "true"}, {"TX_SIMULATION_MODE", "false"}, {"CORS_ALLOWED_ORIGINS", "https://other.example.test"}, {"GOOGLE_OAUTH_CLIENT_SECRET", ""}, {"GOOGLE_OAUTH_REDIRECT_URI", "https://evil.test/"}, {"GOOGLE_OAUTH_ENABLED", "invalid"}} {
		t.Run(item.key, func(t *testing.T) {
			t.Setenv(item.key, item.value)
			if _, err := Load(); err == nil {
				t.Fatal("unsafe Google configuration accepted")
			}
		})
	}
	t.Setenv("GOOGLE_OAUTH_ENABLED", "false")
	t.Setenv("GOOGLE_OAUTH_CLIENT_SECRET", "")
	if cfg, err := Load(); err != nil || cfg.GoogleOAuthEnabled {
		t.Fatal("disabled Google config should not block password/demo access")
	}
}
