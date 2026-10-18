package app

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/config"
	"github.com/transactx/backend/internal/investigator"
	"log/slog"
)

// Optional provider readiness never becomes payment/API/worker boot readiness.
func optionalGemini(ctx context.Context, cfg config.Config, db *pgxpool.Pool, store *investigator.PostgresInvestigationStore, logger *slog.Logger) investigator.ServiceConfig {
	result := investigator.DefaultServiceConfig()
	if !cfg.GeminiEnabled {
		return result
	}
	result.Mode = investigator.ModeLLM
	result.MaxProviderRounds = 1
	result.UnavailableReason = "provider_free_tier_unverified"
	approval, err := investigator.ReadGeminiApproval(cfg.GeminiApprovalFile, cfg.GeminiAPIKey, cfg.GeminiModel)
	if err != nil {
		logger.Warn("Gemini unavailable; evidence-summary fallback active", "reason", result.UnavailableReason)
		return result
	}
	result.UnavailableReason = "provider_unavailable"
	provider, err := investigator.NewGeminiProvider(investigator.GeminiProviderConfig{APIKey: cfg.GeminiAPIKey, Model: cfg.GeminiModel, Approval: approval, DB: db})
	if err != nil {
		logger.Warn("Gemini unavailable; evidence-summary fallback active", "reason", result.UnavailableReason)
		return result
	}
	store.UseUTCDailyUsageWindow()
	if err := store.SetUsageLimits(ctx, investigator.UsageAllowance{InputTokens: 80000, OutputTokens: 12000, CostMicrousd: 0}); err != nil {
		logger.Warn("Gemini budget unavailable; evidence-summary fallback active")
		return result
	}
	result.Provider = provider
	result.UnavailableReason = ""
	return result
}
