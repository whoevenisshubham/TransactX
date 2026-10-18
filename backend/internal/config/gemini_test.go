package config

import "testing"

func TestGeminiRequiresSeparatePrivateProjectReview(t *testing.T) {
	t.Setenv("APP_DEVELOPMENT", "true")
	t.Setenv("TX_GEMINI_ENABLED", "false")
	if cfg, err := Load(); err != nil || cfg.GeminiEnabled {
		t.Fatal("default-disabled provider blocked normal app")
	}
	t.Setenv("TX_GEMINI_ENABLED", "true")
	if _, err := Load(); err == nil {
		t.Fatal("missing provider configuration accepted")
	}
	t.Setenv("GEMINI_API_KEY", "fixture-only-key")
	t.Setenv("GEMINI_MODEL", "gemini-3.5-flash-lite")
	if _, err := Load(); err == nil {
		t.Fatal("key/model alone bypassed private project review")
	}
	t.Setenv("TX_GEMINI_APPROVAL_FILE", "/private/review.json")
	if cfg, err := Load(); err != nil || !cfg.GeminiEnabled {
		t.Fatal("complete configuration rejected before runtime policy validation")
	}
}
