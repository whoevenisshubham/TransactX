package investigator

import (
	"path/filepath"
	"testing"
)

func TestFixedInvestigatorEvaluationPassesAllGates(t *testing.T) {
	repositoryRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	artifact, err := EvaluateInvestigator(EvaluationOptions{
		RepositoryRoot: repositoryRoot,
		DatasetPath:    filepath.Join("evaluation", "cases.json"),
		SourceCommit:   "test-source",
		EvaluatedAt:    "2026-10-12T00:00:00Z",
		Command:        "go test",
		DirtyTree:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	metrics := artifact.Metrics
	if metrics.TotalCases != 40 || metrics.PassedCases != 40 {
		t.Fatalf("evaluation cases = %d/%d, want 40/40", metrics.PassedCases, metrics.TotalCases)
	}
	wantCategories := map[string]int{"factual_recovery": 16, "retrieval": 8, "adversarial": 8, "missing_or_contradictory": 8}
	for category, want := range wantCategories {
		if metrics.CategoryCounts[category] != want {
			t.Errorf("category %s = %d, want %d", category, metrics.CategoryCounts[category], want)
		}
	}
	if metrics.PublishedReportCases != 39 || metrics.CitationValidCases != 39 || metrics.TypedAgreementCases != 39 {
		t.Errorf("published/citation/agreement = %d/%d/%d, want 39/39/39", metrics.PublishedReportCases, metrics.CitationValidCases, metrics.TypedAgreementCases)
	}
	if metrics.RetrievalRecallAt5 < 0.85 || metrics.RetrievalHitsAt5 != 8 {
		t.Errorf("retrieval recall@5 = %f (%d hits), want 1.0 (8 hits)", metrics.RetrievalRecallAt5, metrics.RetrievalHitsAt5)
	}
	if metrics.SecurityBlocked != metrics.SecurityCases || metrics.SecurityCases != 8 {
		t.Errorf("security blocked = %d/%d, want 8/8", metrics.SecurityBlocked, metrics.SecurityCases)
	}
	if metrics.MissingEvidenceAbstains != metrics.MissingEvidenceCases || metrics.MissingEvidenceCases != 8 {
		t.Errorf("missing evidence abstentions = %d/%d, want 8/8", metrics.MissingEvidenceAbstains, metrics.MissingEvidenceCases)
	}
	if artifact.LiveProvider.Status != "pending" {
		t.Errorf("live provider status = %q, want pending", artifact.LiveProvider.Status)
	}
	if metrics.ProviderCalls != 0 || metrics.ActualProviderUsage.InputTokens != nil || metrics.ActualProviderUsage.OutputTokens != nil || metrics.PaidCostUSD != "0.00" {
		t.Errorf("offline evaluation spent provider resources: calls=%d usage=%+v cost=%s", metrics.ProviderCalls, metrics.ActualProviderUsage, metrics.PaidCostUSD)
	}
	for _, name := range []string{"completedEvidenceOnly", "partialEvidenceOnly", "providerFailedFallback", "validatedLLMFixtureOnly"} {
		if _, exists := artifact.UIFixtures[name]; !exists {
			t.Errorf("missing UI fixture %q", name)
		}
	}
	if artifact.UIFixtures["validatedLLMFixtureOnly"].Mode != ModeLLM || artifact.UIFixtures["providerFailedFallback"].Error == nil {
		t.Error("UI fixture modes/failure do not exercise the integration contract")
	}
}

func TestEvaluationIncludesRequiredSecurityScenarios(t *testing.T) {
	repositoryRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	artifact, err := EvaluateInvestigator(EvaluationOptions{
		RepositoryRoot: repositoryRoot,
		DatasetPath:    filepath.Join("evaluation", "cases.json"),
		SourceCommit:   "test-source",
		EvaluatedAt:    "2026-10-12T00:00:00Z",
		Command:        "go test",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"attack-injected-runbook":                false,
		"attack-status-identity-mismatch":        false,
		"attack-foreign-payment":                 false,
		"missing-no-reconciliation-relationship": false,
	}
	for _, result := range artifact.Cases {
		if _, exists := want[result.ID]; exists {
			want[result.ID] = result.Passed
		}
	}
	for id, passed := range want {
		if !passed {
			t.Errorf("required scenario %q did not pass", id)
		}
	}
}
