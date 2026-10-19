package investigator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type geminiTransport func(*http.Request) (*http.Response, error)

func (f geminiTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixtureGeminiReview(key string) GeminiReview {
	sum := sha256.Sum256([]byte(key))
	return GeminiReview{Provider: "google", ProjectID: "fixture-project", Model: "gemini-3.5-flash-lite", KeySHA256: hex.EncodeToString(sum[:]), BillingDisabled: true, FreeTierAvailable: true, ReviewedAt: time.Now().Add(-time.Minute)}
}

func TestGeminiReviewAndDecoder(t *testing.T) {
	approval := fixtureGeminiReview("fixture-key")
	if !approval.valid("fixture-key", "google", approval.ProjectID, approval.Model, time.Now()) {
		t.Fatal("fixture approval rejected")
	}
	if approval.valid("different-key", "google", approval.ProjectID, approval.Model, time.Now()) {
		t.Fatal("approval not bound to key")
	}
	for _, mutate := range []func(*GeminiReview){func(a *GeminiReview) { a.BillingDisabled = false }, func(a *GeminiReview) { a.FreeTierAvailable = false }, func(a *GeminiReview) { a.Provider = "other" }, func(a *GeminiReview) { a.ProjectID = "other-project" }, func(a *GeminiReview) { a.ReviewedAt = time.Now().Add(time.Hour) }, func(a *GeminiReview) { a.Model = "paid-or-unknown-model" }} {
		a := approval
		mutate(&a)
		if a.valid("fixture-key", "google", approval.ProjectID, approval.Model, time.Now()) {
			t.Fatal("unreviewed policy accepted")
		}
	}
	raw := []byte(`{"responseId":"actual-fixture-id","modelVersion":"gemini-3.5-flash-lite","candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"{}"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":20,"thoughtsTokenCount":5}}`)
	result, err := decodeGeminiResponse(raw, approval.Model, 8000, 1200)
	if err != nil || *result.Usage.OutputTokens != 25 || result.Usage.CostMicrousd != nil {
		t.Fatal("thinking usage/cost provenance lost")
	}
	if _, err := decodeGeminiResponse(raw, approval.Model, 8000, 24); !errors.Is(err, ErrProviderMalformed) {
		t.Fatal("output including thought tokens exceeded cap")
	}
	for _, replace := range [][2]string{{`"STOP"`, `"MAX_TOKENS"`}, {`actual-fixture-id`, ``}, {`gemini-3.5-flash-lite`, `wrong-model`}, {`"{}"`, `"{\"unexpected\":true}"`}} {
		bad := strings.Replace(string(raw), replace[0], replace[1], 1)
		if _, err := decodeGeminiResponse([]byte(bad), approval.Model, 8000, 1200); err == nil {
			t.Fatal("malformed provider result accepted")
		}
	}
}

func TestGeminiReviewConfigurationBounds(t *testing.T) {
	review := fixtureGeminiReview("fixture-key")
	review.ReviewedAt = time.Now().Add(-210 * 24 * time.Hour)
	raw, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	for _, now := range []time.Time{time.Now(), time.Now().Add(365 * 24 * time.Hour)} {
		if !review.valid("fixture-key", "google", review.ProjectID, review.Model, now) {
			t.Fatal("elapsed time invalidated unchanged reviewed configuration")
		}
	}
	if _, err := ReadGeminiReview(string(raw), "fixture-key", "google", review.ProjectID, review.Model); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", string(raw) + "{}", string(raw) + strings.Repeat(" ", 4096), strings.TrimSuffix(string(raw), "}") + `,"validUntil":"2026-01-01T00:00:00Z"}`} {
		if _, err := ReadGeminiReview(bad, "fixture-key", "google", review.ProjectID, review.Model); !errors.Is(err, ErrFreeTierUnverified) {
			t.Fatal("invalid or legacy expiring configuration accepted")
		}
	}
}

func TestGeminiDurableAdmissionAndGrounding(t *testing.T) {
	dsn := os.Getenv("GEMINI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GEMINI_TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Hostname() != "127.0.0.1" || !strings.HasPrefix(parsed.Path, "/txv3_oidc_") {
		t.Fatal("requires explicit disposable loopback Gemini fixture database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	var prior int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM gemini_free_attempts`).Scan(&prior); err != nil || prior != 0 {
		t.Fatal("requires unused disposable provider-attempt namespace")
	}
	t.Cleanup(func() {
		for _, sql := range []string{`DELETE FROM gemini_free_attempts`, `DELETE FROM gemini_free_daily`, `UPDATE gemini_free_admission SET lease_owner=NULL,busy_until='epoch'`} {
			if _, err := pool.Exec(ctx, sql); err != nil {
				t.Error(err)
			}
		}
	})
	fixture := loadFixture(t, "healthy.json")
	report, err := BuildEvidenceOnlyReport(fixture.Evidence, nil, "state")
	if err != nil {
		t.Fatal(err)
	}
	report.Mode = ModeLLM
	// Deliberately untrusted model prose must be canonicalized by existing grounding.
	report.Summary = "Untrusted model says every bank is healthy."
	reportJSON, _ := json.Marshal(report)
	responseBody, _ := json.Marshal(map[string]any{"responseId": "fixture-google-response", "modelVersion": "gemini-3.5-flash-lite", "candidates": []any{map[string]any{"finishReason": "STOP", "content": map[string]any{"parts": []any{map[string]any{"text": string(reportJSON)}}}}}, "usageMetadata": map[string]any{"promptTokenCount": 100, "candidatesTokenCount": 200, "thoughtsTokenCount": 0}})
	var calls atomic.Int32
	var countedRequest map[string]any
	transport := geminiTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Host != "generativelanguage.googleapis.com" || r.URL.Scheme != "https" || r.URL.RawQuery != "" || r.Header.Get("x-goog-api-key") != "fixture-key" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("unsafe endpoint/key transport")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		raw := responseBody
		if strings.HasSuffix(r.URL.Path, ":countTokens") {
			countedRequest, _ = body["generateContentRequest"].(map[string]any)
			if r.URL.Path != "/v1beta/models/gemini-3.5-flash-lite:countTokens" || len(body) != 1 || countedRequest == nil || countedRequest["model"] != "models/gemini-3.5-flash-lite" || countedRequest["systemInstruction"] == nil || countedRequest["contents"] == nil || countedRequest["generationConfig"] == nil {
				t.Error("incomplete token preflight")
			}
			raw = []byte(`{"totalTokens":100}`)
		} else {
			if r.URL.Path != "/v1beta/models/gemini-3.5-flash-lite:generateContent" || !reflect.DeepEqual(countedRequest, body) {
				t.Error("generation differs from the complete request admitted by token counting")
			}
			config := body["generationConfig"].(map[string]any)
			if config["maxOutputTokens"] != float64(1200) || config["candidateCount"] != float64(1) || config["responseJsonSchema"] == nil || body["tools"] != nil {
				t.Error("generation bounds/schema/read-only boundary missing")
			}
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	})
	provider, err := NewGeminiProvider(GeminiProviderConfig{APIKey: "fixture-key", Model: "gemini-3.5-flash-lite", Provider: "google", ProjectID: "fixture-project", Review: fixtureGeminiReview("fixture-key"), DB: pool, Client: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	request := GenerationRequest{Question: "current state", Evidence: fixture.Evidence, PromptVersion: InvestigatorPromptVersion, InputTokenLimit: 8000, OutputTokenLimit: 1200}
	result, err := provider.Generate(ctx, request)
	if err != nil || result.Report == nil || calls.Load() != 2 {
		t.Fatalf("count/generate fixture: %v", err)
	}
	grounded, err := GroundGeneratedReport(*result.Report, fixture.Evidence, nil, "state")
	if err != nil {
		t.Fatal(err)
	}
	if grounded.Summary == report.Summary {
		t.Fatal("untrusted provider financial prose escaped grounding")
	}
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT attempts FROM gemini_free_daily WHERE day=(CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date`).Scan(&attempts); err != nil || attempts != 2 {
		t.Fatal("token counting was not durably charged")
	}
	t.Run("canonical service persistence and daily reservation window", func(t *testing.T) {
		store, err := NewPostgresInvestigationStore(pool, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		config := DefaultServiceConfig()
		config.Mode = ModeLLM
		config.Provider = provider
		config.MaxProviderRounds = 1
		deps := ToolDependencies{Evidence: &countingEvidenceReader{bundle: fixture.Evidence}, Runbooks: activeTestCorpus(t)}
		if _, err := NewService(config, store, deps); err == nil {
			t.Fatal("Gemini accepted without explicit durable daily window")
		}
		store.UseUTCDailyUsageWindow()
		if err := store.SetUsageLimits(ctx, UsageAllowance{InputTokens: 8000, OutputTokens: 1200}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			for _, sql := range []string{`DELETE FROM investigation_usage_reservations WHERE job_id IN(SELECT id FROM investigation_jobs WHERE actor_id='gemini-fixture')`, `DELETE FROM investigation_jobs WHERE actor_id='gemini-fixture'`, `UPDATE investigation_usage_limits SET input_limit=0,output_limit=0,cost_limit_microusd=0`} {
				if _, err := pool.Exec(ctx, sql); err != nil {
					t.Error(err)
				}
			}
		})
		service, err := NewService(config, store, deps)
		if err != nil {
			t.Fatal(err)
		}
		scope := TrustedScope{ActorID: "gemini-fixture", Role: "OPS_VIEWER", SelectedPaymentID: fixture.TrustedPaymentId}
		for index, key := range []string{"gemini-first", "gemini-next-day"} {
			if index == 1 {
				exec(`UPDATE investigation_usage_reservations SET created_at=CURRENT_TIMESTAMP-INTERVAL '2 days' WHERE job_id IN(SELECT id FROM investigation_jobs WHERE actor_id='gemini-fixture')`)
			}
			job, _, err := service.Start(ctx, scope, fixture.TrustedPaymentId, "state", key)
			if err != nil {
				t.Fatal(err)
			}
			completed, err := service.Run(ctx, job.ID, "fixture-worker")
			if err != nil || completed.Status != StatusCompleted || completed.Report == nil || completed.Report.Mode != ModeLLM {
				t.Fatalf("canonical Gemini service: status=%s error=%s err=%v", completed.Status, completed.ErrorCode, err)
			}
			var persisted []byte
			if err := pool.QueryRow(ctx, `SELECT job_payload FROM investigation_jobs WHERE id=$1`, job.ID).Scan(&persisted); err != nil {
				t.Fatal(err)
			}
			var saved Job
			if json.Unmarshal(persisted, &saved) != nil || saved.Report == nil || saved.ProviderModel != provider.model || saved.Report.Summary == report.Summary {
				t.Fatal("grounded report/provider provenance not durably saved")
			}
		}
	})
	t.Run("global concurrency and restart quotas", func(t *testing.T) {
		a := geminiAdmission{pool}
		owner := uuid.New()
		if _, err := a.reserve(ctx, owner, "countTokens", provider.model); err != nil {
			t.Fatal(err)
		}
		another := geminiAdmission{pool}
		if _, err := another.reserve(ctx, uuid.New(), "countTokens", provider.model); !errors.Is(err, ErrProviderRateLimited) {
			t.Fatal("cross-instance global concurrency bypassed")
		}
		if err := a.release(owner); err != nil {
			t.Fatal(err)
		}
		exec(`UPDATE gemini_free_daily SET attempts=9`)
		var wg sync.WaitGroup
		var accepted atomic.Int32
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				lease := uuid.New()
				if _, err := another.reserve(ctx, lease, "generateContent", provider.model); err == nil {
					accepted.Add(1)
					if err := another.release(lease); err != nil {
						t.Error(err)
					}
				} else if !errors.Is(err, ErrProviderRateLimited) {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		if accepted.Load() != 1 {
			t.Fatalf("daily remaining admission=%d", accepted.Load())
		}
	})
	t.Run("uncertain network attempt stays charged and holds global lease", func(t *testing.T) {
		exec(`UPDATE gemini_free_daily SET attempts=0`)
		before := calls.Load()
		provider.client.Transport = geminiTransport(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return nil, errors.New("controlled network uncertainty")
		})
		if _, err := provider.Generate(ctx, request); err == nil {
			t.Fatal("network failure accepted")
		}
		if _, err := provider.Generate(ctx, request); !errors.Is(err, ErrProviderRateLimited) {
			t.Fatal("uncertain global lease was released early")
		}
		if calls.Load() != before+1 {
			t.Fatal("network error retried provider")
		}
		var charged int
		if err := pool.QueryRow(ctx, `SELECT attempts FROM gemini_free_daily WHERE day=(CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date`).Scan(&charged); err != nil || charged != 1 {
			t.Fatal("uncertain attempt refunded")
		}
	})
	t.Run("configuration change requires owner review and makes no provider request", func(t *testing.T) {
		before := calls.Load()
		for _, mutate := range []func(*GeminiProvider){func(p *GeminiProvider) { p.key = "rotated-key" }, func(p *GeminiProvider) { p.projectID = "changed-project" }, func(p *GeminiProvider) { p.model = "retired-model" }, func(p *GeminiProvider) { p.provider = "other" }, func(p *GeminiProvider) { p.review.BillingDisabled = false }} {
			changed := *provider
			mutate(&changed)
			if _, err := changed.Generate(ctx, request); !errors.Is(err, ErrFreeTierUnverified) || calls.Load() != before {
				t.Fatal("unreviewed configuration called provider")
			}
		}
	})
}

// Transport is synthetic; canonical storage/admission is real disposable PostgreSQL.
func TestGeminiFailuresAndDisableRetainEvidence(t *testing.T) {
	dsn := os.Getenv("GEMINI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GEMINI_TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Hostname() != "127.0.0.1" || !strings.HasPrefix(parsed.Path, "/txv3_oidc_") {
		t.Fatal("requires isolated loopback Gemini fixture database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var prior int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM gemini_free_attempts`).Scan(&prior); err != nil || prior != 0 {
		t.Fatal("requires unused disposable provider attempt namespace")
	}
	t.Cleanup(func() {
		for _, sql := range []string{`DELETE FROM investigation_usage_reservations WHERE job_id IN(SELECT id FROM investigation_jobs WHERE actor_id='gemini-failure-fixture')`, `DELETE FROM investigation_jobs WHERE actor_id='gemini-failure-fixture'`, `UPDATE investigation_usage_limits SET input_limit=0,output_limit=0,cost_limit_microusd=0`, `DELETE FROM gemini_free_attempts`, `DELETE FROM gemini_free_daily`, `UPDATE gemini_free_admission SET disabled=false,lease_owner=NULL,busy_until='epoch'`} {
			if _, err := pool.Exec(ctx, sql); err != nil {
				t.Error(err)
			}
		}
	})
	fixture := loadFixture(t, "healthy.json")
	store, err := NewPostgresInvestigationStore(pool, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	store.UseUTCDailyUsageWindow()
	if err := store.SetUsageLimits(ctx, UsageAllowance{InputTokens: 80000, OutputTokens: 12000}); err != nil {
		t.Fatal(err)
	}
	var calls int
	status := 401
	transport := geminiTransport(func(*http.Request) (*http.Response, error) {
		calls++
		if status == 0 {
			return nil, context.DeadlineExceeded
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"fixture provider failure"}`))}, nil
	})
	providerConfig := GeminiProviderConfig{APIKey: "fixture-key", Provider: "google", ProjectID: "fixture-project", Model: "gemini-3.5-flash-lite", Review: fixtureGeminiReview("fixture-key"), DB: pool, Client: &http.Client{Transport: transport}}
	providerConfig.Review.ReviewedAt = time.Now().Add(-210 * 24 * time.Hour)
	provider, err := NewGeminiProvider(providerConfig)
	if err != nil {
		t.Fatal(err)
	}
	if provider.client.Timeout != 10*time.Second || provider.client.CheckRedirect == nil {
		t.Fatal("bounded transport policy lost")
	}
	cfg := DefaultServiceConfig()
	cfg.Mode = ModeLLM
	cfg.MaxProviderRounds = 1
	cfg.Provider = provider
	service, err := NewService(cfg, store, ToolDependencies{Evidence: &countingEvidenceReader{bundle: fixture.Evidence}, Runbooks: activeTestCorpus(t)})
	if err != nil {
		t.Fatal(err)
	}
	scope := TrustedScope{ActorID: "gemini-failure-fixture", Role: "OPS_VIEWER", SelectedPaymentID: fixture.TrustedPaymentId}
	// Disable after an admitted preflight: the generation attempt must not start.
	provider.client.Transport = geminiTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if !strings.HasSuffix(r.URL.Path, ":countTokens") {
			t.Fatal("generation escaped mid-sequence disable")
		}
		if _, err := pool.Exec(ctx, `UPDATE gemini_free_admission SET disabled=true WHERE singleton`); err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"totalTokens":100}`))}, nil
	})
	if _, err := provider.Generate(ctx, GenerationRequest{Question: "state", Evidence: fixture.Evidence, PromptVersion: InvestigatorPromptVersion, InputTokenLimit: 8000, OutputTokenLimit: 1200}); !errors.Is(err, ErrGeminiDisabled) || calls != 1 {
		t.Fatalf("count-to-generation disable failed: %v", err)
	}
	// Explicit owner-style resume in this isolated test does not reset the counter.
	if _, err := pool.Exec(ctx, `UPDATE gemini_free_admission SET disabled=false WHERE singleton`); err != nil {
		t.Fatal(err)
	}
	provider.client.Transport = transport
	for _, scenario := range []struct {
		name   string
		status int
		code   string
	}{{"expired-key", 401, "provider_unavailable"}, {"revoked-key", 403, "provider_unavailable"}, {"retired-model", 404, "provider_unavailable"}, {"quota", 429, "provider_rate_limited"}, {"provider-down", 503, "provider_unavailable"}, {"timeout", 0, "provider_timeout"}} {
		t.Run(scenario.name, func(t *testing.T) {
			status = scenario.status
			before := calls
			job, _, err := service.Start(ctx, scope, fixture.TrustedPaymentId, "current state", scenario.name)
			if err != nil {
				t.Fatal(err)
			}
			result, runErr := service.Run(ctx, job.ID, "fixture-worker")
			if runErr == nil || result.ErrorCode != scenario.code || result.Report == nil || result.Report.Mode != ModeEvidenceOnly || calls != before+1 {
				t.Fatalf("failure fallback: code=%s calls=%d err=%v", result.ErrorCode, calls-before, runErr)
			}
			saved, err := store.Get(ctx, scope, job.ID)
			if err != nil || saved.Report == nil || saved.Report.Mode != ModeEvidenceOnly || saved.ErrorCode != scenario.code {
				t.Fatal("explicit evidence fallback not persisted")
			}
		})
	}
	if _, err := pool.Exec(ctx, `UPDATE gemini_free_admission SET disabled=true WHERE singleton`); err != nil {
		t.Fatal(err)
	}
	// A fresh pool/provider sees the persisted stop; no restart or config reload needed.
	another, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer another.Close()
	providerConfig.DB = another
	stopped, err := NewGeminiProvider(providerConfig)
	if err != nil {
		t.Fatal(err)
	}
	before := calls
	if _, err := stopped.Generate(ctx, GenerationRequest{Question: "state", Evidence: fixture.Evidence, PromptVersion: InvestigatorPromptVersion, InputTokenLimit: 8000, OutputTokenLimit: 1200}); !errors.Is(err, ErrGeminiDisabled) || calls != before {
		t.Fatalf("durable disable did not stop fresh provider: %v", err)
	}
	cfg.Provider = stopped
	disabledService, err := NewService(cfg, store, ToolDependencies{Evidence: &countingEvidenceReader{bundle: fixture.Evidence}, Runbooks: activeTestCorpus(t)})
	if err != nil {
		t.Fatal(err)
	}
	job, _, err := disabledService.Start(ctx, scope, fixture.TrustedPaymentId, "state", "disabled")
	if err != nil {
		t.Fatal(err)
	}
	stoppedJob, _ := disabledService.Run(ctx, job.ID, "fixture-worker")
	if stoppedJob.ErrorCode != "provider_disabled" || stoppedJob.Report == nil || stoppedJob.Report.Mode != ModeEvidenceOnly || calls != before {
		t.Fatal("disable lost evidence fallback")
	}
	var attempts int
	if err := another.QueryRow(ctx, `SELECT attempts FROM gemini_free_daily WHERE day=(CURRENT_TIMESTAMP AT TIME ZONE 'UTC')::date`).Scan(&attempts); err != nil || attempts != 7 {
		t.Fatal("disable/reset/restart refunded or added provider attempts")
	}
}
