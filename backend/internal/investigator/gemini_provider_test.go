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
	"path/filepath"
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
func fixtureGeminiApproval(key string) GeminiApproval {
	sum := sha256.Sum256([]byte(key))
	return GeminiApproval{ProjectID: "fixture-project", Model: "gemini-3.5-flash-lite", KeySHA256: hex.EncodeToString(sum[:]), BillingDisabled: true, FreeTierAvailable: true, ReviewedAt: time.Now().Add(-time.Minute), ValidUntil: time.Now().Add(time.Hour)}
}

func TestGeminiApprovalAndDecoder(t *testing.T) {
	approval := fixtureGeminiApproval("fixture-key")
	if !approval.valid("fixture-key", approval.Model, time.Now()) {
		t.Fatal("fixture approval rejected")
	}
	if approval.valid("different-key", approval.Model, time.Now()) {
		t.Fatal("approval not bound to key")
	}
	for _, mutate := range []func(*GeminiApproval){func(a *GeminiApproval) { a.BillingDisabled = false }, func(a *GeminiApproval) { a.FreeTierAvailable = false }, func(a *GeminiApproval) { a.ValidUntil = time.Now().Add(-time.Minute) }, func(a *GeminiApproval) { a.ValidUntil = time.Now().Add(8 * 24 * time.Hour) }, func(a *GeminiApproval) { a.Model = "paid-or-unknown-model" }} {
		a := approval
		mutate(&a)
		if a.valid("fixture-key", a.Model, time.Now()) {
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

func TestGeminiApprovalFileBounds(t *testing.T) {
	approval := fixtureGeminiApproval("fixture-key")
	raw, err := json.Marshal(approval)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "approval.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadGeminiApproval(path, "fixture-key", approval.Model); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, []byte(strings.Repeat(" ", 5000)+"ignored trailing data")...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadGeminiApproval(path, "fixture-key", approval.Model); !errors.Is(err, ErrFreeTierUnverified) {
		t.Fatal("oversized approval/trailing data accepted")
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
	transport := geminiTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Host != "generativelanguage.googleapis.com" || r.URL.Scheme != "https" || r.URL.RawQuery != "" || r.Header.Get("x-goog-api-key") != "fixture-key" {
			t.Error("unsafe endpoint/key transport")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		raw := responseBody
		if strings.HasSuffix(r.URL.Path, ":countTokens") {
			if _, ok := body["generateContentRequest"]; !ok {
				t.Error("incomplete token preflight")
			}
			raw = []byte(`{"totalTokens":100}`)
		} else {
			config := body["generationConfig"].(map[string]any)
			if config["maxOutputTokens"] != float64(1200) || config["candidateCount"] != float64(1) || config["responseJsonSchema"] == nil || body["tools"] != nil {
				t.Error("generation bounds/schema/read-only boundary missing")
			}
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	})
	provider, err := NewGeminiProvider(GeminiProviderConfig{APIKey: "fixture-key", Model: "gemini-3.5-flash-lite", Approval: fixtureGeminiApproval("fixture-key"), DB: pool, Client: &http.Client{Transport: transport}})
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
	t.Run("expired policy makes no provider request", func(t *testing.T) {
		before := calls.Load()
		provider.approval.ValidUntil = time.Now().Add(-time.Second)
		if _, err := provider.Generate(ctx, request); !errors.Is(err, ErrFreeTierUnverified) || calls.Load() != before {
			t.Fatal("expired approval called provider")
		}
	})
}
