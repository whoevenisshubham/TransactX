package investigator

import (
	"context"
	"testing"
	"time"
)

func TestGeminiUnavailableRetainsExplicitEvidenceFallback(t *testing.T) {
	fixture := loadFixture(t, "healthy.json")
	config := DefaultServiceConfig()
	config.Mode = ModeLLM
	config.MaxProviderRounds = 1
	config.UnavailableReason = "provider_free_tier_unverified"
	for _, invalid := range []func(*ServiceConfig){
		func(c *ServiceConfig) { c.InputTokenLimit = 8001 },
		func(c *ServiceConfig) { c.OutputTokenLimit = 1201 },
		func(c *ServiceConfig) { c.MaxProviderRounds = 2 },
		func(c *ServiceConfig) { c.CostLimitMicrousd = 1 },
	} {
		bad := config
		invalid(&bad)
		if _, err := NewService(bad, NewMemoryInvestigationStore(time.Now), ToolDependencies{}); err == nil {
			t.Fatal("unavailable provider bypassed generation bounds")
		}
	}
	store := NewMemoryInvestigationStore(time.Now)
	service, err := NewService(config, store, ToolDependencies{Evidence: &countingEvidenceReader{bundle: fixture.Evidence}, Runbooks: activeTestCorpus(t)})
	if err != nil {
		t.Fatal(err)
	}
	scope := TrustedScope{ActorID: "fixture-viewer", Role: "OPS_VIEWER", SelectedPaymentID: fixture.TrustedPaymentId}
	job, _, err := service.Start(context.Background(), scope, fixture.TrustedPaymentId, "current state", "expired-review")
	if err != nil {
		t.Fatal(err)
	}
	result, _ := service.Run(context.Background(), job.ID, "fixture-worker")
	if result.Report == nil || result.Report.Mode != ModeEvidenceOnly || result.ErrorCode != "provider_free_tier_unverified" || len(result.ProviderRequestIDs) != 0 {
		t.Fatal("unavailable optional provider lost explicit evidence summary or attempted provider")
	}
	// Model a previously reserved job without claiming a native process crash.
	store.SetUsageLimits(8000, 1200, 0)
	job, _, err = service.Start(context.Background(), scope, fixture.TrustedPaymentId, "current state", "reserved-expired-review")
	if err != nil {
		t.Fatal(err)
	}
	job, err = store.Claim(context.Background(), job.ID, "fixture-worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	job.UsageReservationID, err = store.ReserveUsage(context.Background(), job, UsageAllowance{InputTokens: 8000, OutputTokens: 1200})
	if err != nil {
		t.Fatal(err)
	}
	job.Snapshot, job.ProviderCallPending = &fixture.Evidence, true
	if err := store.SaveCheckpoint(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	result, _ = service.runGenerated(context.Background(), job)
	if result.Report == nil || result.ProviderCallPending || result.ErrorCode != "provider_free_tier_unverified" || store.reservations[job.UsageReservationID].status != usageUncertain {
		t.Fatal("expired review lost conservative reservation or evidence fallback on resume")
	}
}
