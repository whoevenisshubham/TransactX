package investigator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const EvaluationSchemaVersion = "tx.investigator.eval.v1"

type EvaluationAssertion struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

type EvaluationExpectation struct {
	RequiredAssertions      []EvaluationAssertion `json:"requiredAssertions"`
	ForbiddenAssertions     []EvaluationAssertion `json:"forbiddenAssertions"`
	MissingEvidenceContains []string              `json:"missingEvidenceContains"`
	RejectedError           string                `json:"rejectedError,omitempty"`
	Coverage                string                `json:"coverage,omitempty"`
	RetrievalPath           string                `json:"retrievalPath,omitempty"`
	RetrievalHeading        string                `json:"retrievalHeading,omitempty"`
}

type EvaluationAttack struct {
	ToolName            string          `json:"toolName,omitempty"`
	Arguments           json.RawMessage `json:"arguments,omitempty"`
	InjectedRunbookText string          `json:"injectedRunbookText,omitempty"`
	ExpectedError       string          `json:"expectedError"`
}

type EvaluationCase struct {
	ID       string                `json:"id"`
	Category string                `json:"category"`
	Fixture  string                `json:"fixture"`
	Question string                `json:"question"`
	Tags     []string              `json:"tags"`
	Expected EvaluationExpectation `json:"expected"`
	Attack   *EvaluationAttack     `json:"attack,omitempty"`
}

type EvaluationDataset struct {
	SchemaVersion string           `json:"schemaVersion"`
	Version       string           `json:"version"`
	Cases         []EvaluationCase `json:"cases"`
}

type EvaluationCaseResult struct {
	ID              string              `json:"id"`
	Category        string              `json:"category"`
	Passed          bool                `json:"passed"`
	Failures        []string            `json:"failures"`
	RetrievedChunks []string            `json:"retrievedChunks"`
	RetrievalMicros int64               `json:"retrievalMicros"`
	SecurityBlocked *bool               `json:"securityBlocked,omitempty"`
	Report          InvestigationReport `json:"safeReport"`
}

type EvaluationMetrics struct {
	TotalCases              int            `json:"totalCases"`
	PassedCases             int            `json:"passedCases"`
	CategoryCounts          map[string]int `json:"categoryCounts"`
	PublishedReportCases    int            `json:"publishedReportCases"`
	CitationValidCases      int            `json:"citationValidCases"`
	TypedAgreementCases     int            `json:"typedAgreementCases"`
	RetrievalCases          int            `json:"retrievalCases"`
	RetrievalHitsAt5        int            `json:"retrievalHitsAt5"`
	RetrievalRecallAt5      float64        `json:"retrievalRecallAt5"`
	SecurityCases           int            `json:"securityCases"`
	SecurityBlocked         int            `json:"securityBlocked"`
	MissingEvidenceCases    int            `json:"missingEvidenceCases"`
	MissingEvidenceAbstains int            `json:"missingEvidenceAbstains"`
	RetrievalP50Micros      int64          `json:"retrievalP50Micros"`
	RetrievalP95Micros      int64          `json:"retrievalP95Micros"`
	ProviderCalls           int            `json:"providerCalls"`
	ActualProviderUsage     Usage          `json:"actualProviderUsage"`
	PaidCostUSD             string         `json:"paidCostUsd"`
}

type LiveProviderEvaluation struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type EvaluationArtifact struct {
	SchemaVersion     string                           `json:"schemaVersion"`
	DatasetVersion    string                           `json:"datasetVersion"`
	DatasetSHA256     string                           `json:"datasetSha256"`
	CorpusVersion     string                           `json:"corpusVersion"`
	CorpusManifestSHA string                           `json:"corpusManifestSha256"`
	PromptVersion     string                           `json:"promptVersion"`
	PromptSHA256      string                           `json:"promptSha256"`
	SourceCommit      string                           `json:"sourceCommit"`
	DirtyTree         bool                             `json:"dirtyTree"`
	EvaluatedAt       string                           `json:"evaluatedAt"`
	Environment       string                           `json:"environment"`
	Command           string                           `json:"command"`
	Metrics           EvaluationMetrics                `json:"metrics"`
	Cases             []EvaluationCaseResult           `json:"cases"`
	UIFixtures        map[string]InvestigationResponse `json:"uiFixtures"`
	LiveProvider      LiveProviderEvaluation           `json:"liveProvider"`
}

type EvaluationOptions struct {
	RepositoryRoot, DatasetPath, SourceCommit, EvaluatedAt, Command string
	DirtyTree                                                       bool
}

func EvaluateInvestigator(options EvaluationOptions) (EvaluationArtifact, error) {
	if strings.TrimSpace(options.RepositoryRoot) == "" || strings.TrimSpace(options.DatasetPath) == "" || strings.TrimSpace(options.SourceCommit) == "" || strings.TrimSpace(options.Command) == "" {
		return EvaluationArtifact{}, errors.New("evaluation provenance fields are required")
	}
	if _, err := time.Parse(time.RFC3339Nano, options.EvaluatedAt); err != nil {
		return EvaluationArtifact{}, errors.New("evaluated-at must be RFC3339")
	}
	raw, err := os.ReadFile(options.DatasetPath)
	if err != nil {
		return EvaluationArtifact{}, err
	}
	raw = canonicalLF(raw)
	var dataset EvaluationDataset
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&dataset); err != nil {
		return EvaluationArtifact{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return EvaluationArtifact{}, errors.New("evaluation dataset contains trailing JSON")
	}
	if err := validateEvaluationDataset(dataset); err != nil {
		return EvaluationArtifact{}, err
	}
	corpusBuild, err := BuildCorpus(options.RepositoryRoot, filepath.Join(options.RepositoryRoot, "docs", "runbooks", "corpus-manifest.json"))
	if err != nil {
		return EvaluationArtifact{}, err
	}
	corpus := NewMemoryCorpus()
	if err := corpus.Stage(corpusBuild); err != nil {
		return EvaluationArtifact{}, err
	}
	if err := corpus.Activate(corpusBuild.Version, corpusBuild.ManifestSHA256); err != nil {
		return EvaluationArtifact{}, err
	}
	prompt, err := LoadPromptAssets()
	if err != nil {
		return EvaluationArtifact{}, err
	}
	datasetDigest := sha256.Sum256(raw)
	artifact := EvaluationArtifact{
		SchemaVersion: EvaluationSchemaVersion, DatasetVersion: dataset.Version,
		DatasetSHA256: hex.EncodeToString(datasetDigest[:]), CorpusVersion: corpusBuild.Version,
		CorpusManifestSHA: corpusBuild.ManifestSHA256, PromptVersion: prompt.Version, PromptSHA256: prompt.PromptSHA256,
		SourceCommit: options.SourceCommit, DirtyTree: options.DirtyTree, EvaluatedAt: options.EvaluatedAt,
		Environment: runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH, Command: options.Command,
		Metrics:      EvaluationMetrics{CategoryCounts: make(map[string]int), PaidCostUSD: "0.00"},
		LiveProvider: LiveProviderEvaluation{Status: "pending", Reason: "No authorized provider key/local model and nonzero budget were configured; no live call was made."},
	}

	latencies := make([]int64, 0, len(dataset.Cases))
	reportsByFixture := make(map[string]InvestigationReport)
	for _, testCase := range dataset.Cases {
		result := evaluateCase(options.RepositoryRoot, corpus, testCase)
		artifact.Cases = append(artifact.Cases, result)
		artifact.Metrics.TotalCases++
		artifact.Metrics.CategoryCounts[testCase.Category]++
		latencies = append(latencies, result.RetrievalMicros)
		if result.Passed {
			artifact.Metrics.PassedCases++
			if result.Report.SchemaVersion != "" {
				artifact.Metrics.PublishedReportCases++
				artifact.Metrics.CitationValidCases++
				artifact.Metrics.TypedAgreementCases++
			}
		}
		if testCase.Category == "retrieval" {
			artifact.Metrics.RetrievalCases++
			if result.Passed {
				artifact.Metrics.RetrievalHitsAt5++
			}
		}
		if testCase.Category == "adversarial" {
			artifact.Metrics.SecurityCases++
			if result.SecurityBlocked != nil && *result.SecurityBlocked {
				artifact.Metrics.SecurityBlocked++
			}
		}
		if testCase.Category == "missing_or_contradictory" {
			artifact.Metrics.MissingEvidenceCases++
			if result.Report.EvidenceCoverage != "complete_for_question" {
				artifact.Metrics.MissingEvidenceAbstains++
			}
		}
		if _, exists := reportsByFixture[testCase.Fixture]; !exists && result.Passed {
			reportsByFixture[testCase.Fixture] = result.Report
		}
	}
	if artifact.Metrics.RetrievalCases > 0 {
		artifact.Metrics.RetrievalRecallAt5 = float64(artifact.Metrics.RetrievalHitsAt5) / float64(artifact.Metrics.RetrievalCases)
	}
	sort.Slice(latencies, func(left, right int) bool { return latencies[left] < latencies[right] })
	artifact.Metrics.RetrievalP50Micros = percentileInt64(latencies, 0.50)
	artifact.Metrics.RetrievalP95Micros = percentileInt64(latencies, 0.95)
	artifact.UIFixtures = evaluationUIFixtures(reportsByFixture, options.EvaluatedAt)
	if artifact.Metrics.PassedCases != artifact.Metrics.TotalCases || artifact.Metrics.CitationValidCases != artifact.Metrics.PublishedReportCases || artifact.Metrics.TypedAgreementCases != artifact.Metrics.PublishedReportCases || artifact.Metrics.RetrievalRecallAt5 < 0.85 || artifact.Metrics.SecurityBlocked != artifact.Metrics.SecurityCases || artifact.Metrics.MissingEvidenceAbstains != artifact.Metrics.MissingEvidenceCases {
		return artifact, errors.New("investigator evaluation gates failed")
	}
	return artifact, nil
}

func evaluateCase(repositoryRoot string, corpus *MemoryCorpus, testCase EvaluationCase) EvaluationCaseResult {
	result := EvaluationCaseResult{ID: testCase.ID, Category: testCase.Category, Passed: true, Failures: []string{}, RetrievedChunks: []string{}}
	raw, err := os.ReadFile(filepath.Join(repositoryRoot, "backend", "internal", "investigator", "testfixtures", testCase.Fixture))
	if err != nil {
		result.Passed = false
		result.Failures = append(result.Failures, "fixture_read")
		return result
	}
	var fixture struct {
		TrustedPaymentId string         `json:"trustedPaymentId"`
		Evidence         EvidenceBundle `json:"evidence"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		result.Passed = false
		result.Failures = append(result.Failures, "fixture_decode")
		return result
	}
	scope := TrustedScope{ActorID: "evaluation-operator", Role: "OPS_ADMIN", SelectedPaymentID: fixture.TrustedPaymentId}
	identityErr := validateEvidenceBundle(scope, fixture.TrustedPaymentId, fixture.Evidence)
	if testCase.Expected.RejectedError != "" {
		blocked := matchesEvaluationError(identityErr, testCase.Expected.RejectedError)
		result.SecurityBlocked = &blocked
		if !blocked {
			result.Passed = false
			result.Failures = append(result.Failures, "expected_fixture_rejection_missing")
		}
		return result
	}
	if identityErr != nil {
		result.Passed = false
		result.Failures = append(result.Failures, "fixture_identity_invalid")
		return result
	}
	started := time.Now()
	chunks, err := corpus.Retrieve(context.Background(), scope, testCase.Question, testCase.Tags, 5)
	result.RetrievalMicros = time.Since(started).Microseconds()
	if err != nil {
		result.Passed = false
		result.Failures = append(result.Failures, "retrieval_error")
		return result
	}
	for _, chunk := range chunks {
		result.RetrievedChunks = append(result.RetrievedChunks, chunk.ID)
	}
	if testCase.Attack != nil && testCase.Attack.InjectedRunbookText != "" {
		digest := sha256.Sum256([]byte(testCase.Attack.InjectedRunbookText))
		injected := RunbookChunk{
			ID: "runbook:adversarial-injection", Path: "evaluation/injected-runbook.md",
			Heading: "Adversarial evaluation content", StartLine: 1, EndLine: 1,
			Content: testCase.Attack.InjectedRunbookText, SHA256: hex.EncodeToString(digest[:]),
			SourceSHA256: hex.EncodeToString(digest[:]), Version: "tx.eval.injection.v1",
			ImplementationStatus: "untrusted_evaluation", AllowedRoles: []string{"OPS_ADMIN"},
		}
		chunks = append([]RunbookChunk{injected}, chunks...)
		result.RetrievedChunks = append([]string{injected.ID}, result.RetrievedChunks...)
	}
	report, err := BuildEvidenceOnlyReport(fixture.Evidence, chunks, testCase.Question)
	if err != nil {
		result.Passed = false
		result.Failures = append(result.Failures, "report_error")
		return result
	}
	result.Report = report
	if err := ValidateReportGrounding(report, fixture.Evidence, chunks); err != nil {
		result.Passed = false
		result.Failures = append(result.Failures, "citation_invalid")
	}
	for _, expected := range testCase.Expected.RequiredAssertions {
		if !reportHasAssertion(report, expected) {
			result.Passed = false
			result.Failures = append(result.Failures, "missing_required:"+expected.Field+"="+expected.Value)
		}
	}
	for _, forbidden := range testCase.Expected.ForbiddenAssertions {
		if reportHasAssertion(report, forbidden) {
			result.Passed = false
			result.Failures = append(result.Failures, "forbidden_assertion:"+forbidden.Field+"="+forbidden.Value)
		}
	}
	for _, missing := range testCase.Expected.MissingEvidenceContains {
		if !containsString(report.MissingEvidence, missing) {
			result.Passed = false
			result.Failures = append(result.Failures, "missing_evidence_not_reported:"+missing)
		}
	}
	if testCase.Expected.Coverage != "" && report.EvidenceCoverage != testCase.Expected.Coverage {
		result.Passed = false
		result.Failures = append(result.Failures, "wrong_coverage")
	}
	if testCase.Expected.RetrievalPath != "" && !hasEvaluationChunk(chunks, testCase.Expected.RetrievalPath, testCase.Expected.RetrievalHeading) {
		result.Passed = false
		result.Failures = append(result.Failures, "retrieval_miss")
	}
	if testCase.Category == "adversarial" {
		blocked := false
		if testCase.Attack != nil && testCase.Attack.InjectedRunbookText != "" {
			blocked = testCase.Attack.ExpectedError == "injection_ignored" && !reportContainsText(report, testCase.Attack.InjectedRunbookText)
		} else if testCase.Attack != nil {
			reader := evaluationEvidenceReader{bundle: fixture.Evidence}
			registry, registryErr := NewToolRegistry(ToolDependencies{
				Evidence: reader, Recovery: evaluationRecoveryReader{}, Trace: evaluationTraceReader{},
				Analytics: evaluationAnalyticsReader{}, Runbooks: corpus,
			})
			if registryErr == nil {
				_, attackErr := registry.Execute(context.Background(), scope, &fixture.Evidence, ToolCall{Name: testCase.Attack.ToolName, Arguments: testCase.Attack.Arguments})
				blocked = matchesEvaluationError(attackErr, testCase.Attack.ExpectedError)
			}
		}
		result.SecurityBlocked = &blocked
		if !blocked {
			result.Passed = false
			result.Failures = append(result.Failures, "security_attack_not_blocked")
		}
	}
	return result
}

func validateEvaluationDataset(dataset EvaluationDataset) error {
	if dataset.SchemaVersion != EvaluationSchemaVersion || dataset.Version == "" || len(dataset.Cases) < 40 {
		return errors.New("evaluation dataset must contain at least 40 versioned cases")
	}
	counts := make(map[string]int)
	seen := make(map[string]struct{})
	for _, testCase := range dataset.Cases {
		if testCase.ID == "" || testCase.Fixture == "" || strings.TrimSpace(testCase.Question) == "" || len(testCase.Expected.RequiredAssertions) == 0 || len(testCase.Expected.ForbiddenAssertions) == 0 {
			return fmt.Errorf("invalid evaluation case %q", testCase.ID)
		}
		if _, duplicate := seen[testCase.ID]; duplicate {
			return fmt.Errorf("duplicate evaluation case %q", testCase.ID)
		}
		seen[testCase.ID] = struct{}{}
		counts[testCase.Category]++
		switch testCase.Category {
		case "retrieval":
			if testCase.Expected.RetrievalPath == "" || testCase.Expected.RetrievalHeading == "" {
				return fmt.Errorf("retrieval case %q lacks an expected citation", testCase.ID)
			}
		case "adversarial":
			if testCase.Attack == nil && testCase.Expected.RejectedError == "" {
				return fmt.Errorf("adversarial case %q lacks an attack or rejected fixture", testCase.ID)
			}
		case "missing_or_contradictory":
			if len(testCase.Expected.MissingEvidenceContains) == 0 || testCase.Expected.Coverage == "" {
				return fmt.Errorf("missing-evidence case %q lacks an abstention expectation", testCase.ID)
			}
		case "factual_recovery":
		default:
			return fmt.Errorf("unknown evaluation category %q", testCase.Category)
		}
	}
	for category, minimum := range map[string]int{"factual_recovery": 16, "retrieval": 8, "adversarial": 8, "missing_or_contradictory": 8} {
		if counts[category] < minimum {
			return fmt.Errorf("evaluation category %s has %d cases, want at least %d", category, counts[category], minimum)
		}
	}
	return nil
}

func reportHasAssertion(report InvestigationReport, expected EvaluationAssertion) bool {
	for _, fact := range report.Facts {
		for _, assertion := range fact.Assertions {
			if assertion.Field == expected.Field && assertion.Value == expected.Value {
				return true
			}
		}
	}
	return false
}

func hasEvaluationChunk(chunks []RunbookChunk, path, heading string) bool {
	for _, chunk := range chunks {
		if chunk.Path == path && chunk.Heading == heading {
			return true
		}
	}
	return false
}

func matchesEvaluationError(err error, expected string) bool {
	switch expected {
	case "unknown_tool":
		return errors.Is(err, ErrUnknownTool)
	case "invalid_arguments":
		return errors.Is(err, ErrInvalidToolArguments)
	case "scope_violation":
		return errors.Is(err, ErrScopeViolation)
	default:
		return false
	}
}

func reportContainsText(report InvestigationReport, text string) bool {
	raw, err := json.Marshal(report)
	return err != nil || strings.Contains(strings.ToLower(string(raw)), strings.ToLower(text))
}

func percentileInt64(values []int64, quantile float64) int64 {
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values)-1) * quantile)
	return values[index]
}

func evaluationUIFixtures(reports map[string]InvestigationReport, evaluatedAt string) map[string]InvestigationResponse {
	completed := reports["healthy.json"]
	partial := reports["partial_evidence.json"]
	completedAt := evaluatedAt
	inputTokens, outputTokens := int64(10), int64(5)
	fixtureModel := "fixture-model-not-live"
	timeline := []ToolTimelineEntry{{
		ToolName:  ToolReadPaymentEvidence,
		Arguments: json.RawMessage(`{"paymentId":"11111111-1111-4111-8111-111111111111"}`),
		StartedAt: evaluatedAt, FinishedAt: evaluatedAt, Status: "completed",
		EvidenceIds: []string{"payment:11111111-1111-4111-8111-111111111111"},
	}}
	llm := completed
	llm.Mode = ModeLLM
	llm.Inferences = []Inference{{Text: "Advisory inference: Fixture-only provider inference for UI rendering.", EvidenceIds: []string{completed.Facts[0].EvidenceIds[0]}}}
	return map[string]InvestigationResponse{
		"completedEvidenceOnly":  {Id: "00000000-0000-4000-8000-000000000001", Status: StatusCompleted, Mode: ModeEvidenceOnly, Report: &completed, ToolTimeline: append([]ToolTimelineEntry(nil), timeline...), CreatedAt: evaluatedAt, CompletedAt: &completedAt},
		"partialEvidenceOnly":    {Id: "00000000-0000-4000-8000-000000000002", Status: StatusCompleted, Mode: ModeEvidenceOnly, Report: &partial, ToolTimeline: append([]ToolTimelineEntry(nil), timeline...), CreatedAt: evaluatedAt, CompletedAt: &completedAt},
		"providerFailedFallback": {Id: "00000000-0000-4000-8000-000000000003", Status: StatusFailed, Mode: ModeLLM, Report: &partial, ToolTimeline: append([]ToolTimelineEntry(nil), timeline...), Error: &InvestigationError{Code: "provider_rate_limited", Message: publicInvestigationErrorMessage("provider_rate_limited")}, CreatedAt: evaluatedAt, CompletedAt: &completedAt, Usage: InvestigationUsage{PromptVersion: InvestigatorPromptVersion}},
		"validatedLLMFixtureOnly": {
			Id: "00000000-0000-4000-8000-000000000004", Status: StatusCompleted, Mode: ModeLLM, Report: &llm,
			ToolTimeline: append([]ToolTimelineEntry(nil), timeline...), CreatedAt: evaluatedAt, CompletedAt: &completedAt,
			Usage: InvestigationUsage{InputTokens: &inputTokens, OutputTokens: &outputTokens, Model: &fixtureModel, PromptVersion: InvestigatorPromptVersion},
		},
	}
}

type evaluationEvidenceReader struct{ bundle EvidenceBundle }

func (reader evaluationEvidenceReader) ReadPaymentEvidence(_ context.Context, _ TrustedScope, _ string) (EvidenceBundle, error) {
	return reader.bundle, nil
}

type evaluationRecoveryReader struct{}

func (evaluationRecoveryReader) ReadRecoveryAttempts(context.Context, TrustedScope, string, int) ([]RecoveryAttempt, error) {
	return nil, nil
}

type evaluationTraceReader struct{}

func (evaluationTraceReader) ReadTrace(context.Context, TrustedScope, string) (Trace, error) {
	return Trace{}, nil
}

type evaluationAnalyticsReader struct{}

func (evaluationAnalyticsReader) ReadWindows(context.Context, TrustedScope, string, string, string, string, int) ([]AnalyticsWindow, error) {
	return nil, nil
}
