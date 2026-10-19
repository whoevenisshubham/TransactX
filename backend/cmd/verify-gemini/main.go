// verify-gemini runs one explicitly acknowledged synthetic investigation without
// enabling the production provider. It uses the canonical store, reservations,
// grounding and Gemini admission, and never starts an API or background service.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/investigator"
)

// Only transport metadata enters the private receipt: never headers, prompts,
// provider bodies, URLs/query strings or arbitrary provider error messages.
type auditTransport struct {
	next    http.RoundTripper
	receipt *os.File
}

func (a *auditTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if a.receipt == nil {
		return nil, errors.New("private transport audit unavailable")
	}
	res, err := a.next.RoundTrip(req)
	event := map[string]any{"stage": "provider_http", "operation": "unknown"}
	for _, kind := range []string{"countTokens", "generateContent"} {
		if strings.HasSuffix(req.URL.Path, ":"+kind) {
			event["operation"] = kind
		}
	}
	if err != nil {
		event["failure"] = "network"
	} else {
		event["httpStatus"] = res.StatusCode
		if res.StatusCode >= 400 {
			raw, readErr := io.ReadAll(io.LimitReader(res.Body, 4096))
			res.Body = struct {
				io.Reader
				io.Closer
			}{io.MultiReader(bytes.NewReader(raw), res.Body), res.Body}
			var body struct {
				Error struct {
					Status string `json:"status"`
				} `json:"error"`
			}
			if readErr == nil && json.Unmarshal(raw, &body) == nil {
				switch body.Error.Status {
				case "INVALID_ARGUMENT", "UNAUTHENTICATED", "PERMISSION_DENIED", "RESOURCE_EXHAUSTED", "NOT_FOUND", "UNAVAILABLE", "INTERNAL":
					event["providerStatus"] = body.Error.Status
				}
			}
		}
	}
	if writeErr := json.NewEncoder(a.receipt).Encode(event); writeErr != nil {
		if res != nil {
			res.Body.Close()
		}
		return nil, errors.New("private transport audit failed")
	}
	if syncErr := a.receipt.Sync(); syncErr != nil {
		if res != nil {
			res.Body.Close()
		}
		return nil, errors.New("private transport audit sync failed")
	}
	return res, err
}

type apiEvidence struct {
	origin, token string
	client        *http.Client
}

func (a apiEvidence) read(ctx context.Context, path string) ([]byte, error) {
	r, err := http.NewRequestWithContext(ctx, "GET", a.origin+path, nil)
	if err != nil {
		return nil, errors.New("invalid API request")
	}
	r.Header.Set("Authorization", "Bearer "+a.token)
	res, err := a.client.Do(r)
	if err != nil {
		return nil, errors.New("API unavailable; no retry")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("API status %d; no retry", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, errors.New("incomplete API response")
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil || len(envelope.Data) == 0 {
		return nil, errors.New("invalid API envelope")
	}
	return envelope.Data, nil
}

func (a apiEvidence) ReadPaymentEvidenceJSON(ctx context.Context, scope investigator.TrustedScope, payment string) ([]byte, error) {
	if payment != scope.SelectedPaymentID {
		return nil, errors.New("foreign payment scope")
	}
	return a.read(ctx, "/api/ops/payments/"+url.PathEscape(payment)+"/evidence")
}

func validateTarget(dsn, host, database, origin, ack string) error {
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.User == nil || host == "" || !strings.HasSuffix(host, ".neon.tech") || strings.Contains(host, "-pooler") || u.Hostname() != host || database != "neondb" || u.Path != "/"+database || (u.Query().Get("sslmode") != "require" && u.Query().Get("sslmode") != "verify-full") || origin != "https://transactx-demo-api.onrender.com" || ack != "VERIFY ONE SYNTHETIC GEMINI INVESTIGATION" {
		return errors.New("exact acknowledged synthetic demo target required")
	}
	return nil
}

func run() error {
	host := flag.String("expected-host", "", "owner-confirmed direct Neon hostname")
	database := flag.String("expected-db", "", "owner-confirmed existing synthetic database")
	origin := flag.String("api-origin", "", "existing synthetic HTTPS API")
	payment := flag.String("payment-id", "", "existing synthetic payment UUID")
	receipt := flag.String("receipt", "", "new absolute private receipt outside Git; exclusive creation prevents reruns")
	ack := flag.String("ack", "", "VERIFY ONE SYNTHETIC GEMINI INVESTIGATION")
	diagnosticAck := flag.String("diagnostic-ack", "", "explicit additional owner authorization; never an automatic retry")
	flag.Parse()
	requestKey, err := verificationKey(*diagnosticAck)
	if err != nil {
		return err
	}
	if err := validateTarget(os.Getenv("DATABASE_URL"), *host, *database, *origin, *ack); err != nil {
		return err
	}
	if os.Getenv("TX_GEMINI_ENABLED") == "true" || *payment == "" || !filepath.IsAbs(*receipt) {
		return errors.New("production flag must stay off; payment and private receipt required")
	}
	for p := filepath.Dir(*receipt); ; p = filepath.Dir(p) {
		if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
			return errors.New("receipt must stay outside Git")
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	key, model, project := os.Getenv("GEMINI_API_KEY"), os.Getenv("GEMINI_MODEL"), os.Getenv("GEMINI_PROJECT_ID")
	review, err := investigator.ReadGeminiReview(os.Getenv("TX_GEMINI_OWNER_REVIEW"), key, "google", project, model)
	if err != nil {
		return errors.New("actual configuration-bound owner review required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	pc, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		return errors.New("invalid private database configuration")
	}
	pc.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return errors.New("database unavailable")
	}
	defer pool.Close()
	var connected string
	if err = pool.QueryRow(ctx, "SELECT current_database()").Scan(&connected); err != nil || connected != *database {
		return errors.New("connected database mismatch")
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	api := apiEvidence{*origin, os.Getenv("TX_VERIFY_OPS_TOKEN"), client}
	profile, err := api.read(ctx, "/api/me")
	if err != nil {
		return err
	}
	var actor struct{ ID, Role string }
	if json.Unmarshal(profile, &actor) != nil || actor.Role != "OPS_VIEWER" {
		return errors.New("actual read-only operator authentication required")
	}
	var matches bool
	if err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE id::text=$1 AND upi_id='viewer@transactx' AND role='OPS_VIEWER')", actor.ID).Scan(&matches); err != nil || !matches {
		return errors.New("API actor does not match confirmed database")
	}
	audit := &auditTransport{next: http.DefaultTransport}
	providerClient := *client
	providerClient.Transport = audit
	provider, err := investigator.NewGeminiProvider(investigator.GeminiProviderConfig{APIKey: key, Model: model, Provider: "google", ProjectID: project, Review: review, DB: pool, Client: &providerClient})
	if err != nil {
		return errors.New("provider review rejected")
	}
	store, err := investigator.NewPostgresInvestigationStore(pool, time.Now)
	if err != nil {
		return err
	}
	store.UseUTCDailyUsageWindow()
	corpus, err := investigator.NewPostgresCorpusRepository(pool)
	if err != nil {
		return err
	}
	cfg := investigator.DefaultServiceConfig()
	cfg.Mode = investigator.ModeLLM
	cfg.Provider = provider
	cfg.MaxProviderRounds = 1
	cfg.OutputTokenLimit = 600
	cfg.MaxCoreAttempts = 1
	service, err := investigator.NewService(cfg, store, investigator.ToolDependencies{Evidence: investigator.BridgedEvidenceReader{Source: api}, Runbooks: corpus})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(*receipt, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("receipt exists or is unavailable; do not automatically retry")
	}
	defer f.Close()
	audit.receipt = f
	enc := json.NewEncoder(f)
	if err = enc.Encode(map[string]any{"stage": "admitted", "model": model, "payment": *payment, "observedAt": time.Now().UTC()}); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = store.SetUsageLimits(ctx, investigator.UsageAllowance{InputTokens: 80000, OutputTokens: 12000, CostMicrousd: 0}); err != nil {
		return errors.New("durable token budget unavailable")
	}
	job, created, err := service.Start(ctx, investigator.TrustedScope{ActorID: actor.ID, Role: actor.Role, SelectedPaymentID: *payment}, *payment, "Explain the central state briefly with one collected evidence citation. Keep the report concise.", requestKey)
	if err != nil {
		return errors.New("canonical investigation admission failed")
	}
	if !created {
		return errors.New("original verification already exists; inspect it without resubmitting")
	}
	if err = enc.Encode(map[string]any{"stage": "created", "jobId": job.ID}); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	completed, runErr := service.Run(ctx, job.ID, "owner-gemini-verification")
	if err = enc.Encode(map[string]any{"stage": "observed", "job": completed, "runSucceeded": runErr == nil}); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if runErr != nil || completed.Status != investigator.StatusCompleted || completed.Report == nil || completed.Report.Mode != investigator.ModeLLM {
		return errors.New("verification did not produce a completed grounded generated report; production stays off; inspect private receipt, no automatic retry")
	}
	fmt.Println("One grounded generated report persisted through canonical store and durable budgets; production configuration unchanged.")
	return nil
}

func verificationKey(ack string) (string, error) {
	switch ack {
	case "":
		return "gemini-owner-free-verification-v1", nil
	case "OWNER AUTHORIZED ONE ADDITIONAL DIAGNOSTIC PROBE":
		return "gemini-owner-free-diagnostic-v2", nil
	default:
		return "", errors.New("additional probe requires explicit owner authorization")
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
