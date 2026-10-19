package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/transactx/backend/internal/investigator"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func TestAcceptanceRequiresPersistedGroundedCitation(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "investigator", "testfixtures", "healthy.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Evidence investigator.EvidenceBundle }
	if json.Unmarshal(raw, &fixture) != nil {
		t.Fatal("fixture")
	}
	for _, kind := range []string{"valid", "empty_facts", "foreign_citation", "fallback", "failed"} {
		t.Run(kind, func(t *testing.T) {
			report, err := investigator.BuildEvidenceOnlyReport(fixture.Evidence, nil, "state")
			if err != nil {
				t.Fatal(err)
			}
			report.Mode = investigator.ModeLLM
			job := investigator.Job{Status: investigator.StatusCompleted, Snapshot: &fixture.Evidence, Report: &report}
			switch kind {
			case "empty_facts":
				report.Facts = nil
			case "foreign_citation":
				report.Facts[0].EvidenceIds = []string{"foreign"}
			case "fallback":
				report.Mode = investigator.ModeEvidenceOnly
			case "failed":
				job.Status = investigator.StatusFailed
			}
			if err = validateGeneratedAcceptance(job); (err == nil) != (kind == "valid") {
				t.Fatalf("accepted=%t case=%s", err == nil, kind)
			}
		})
	}
}

func TestGenerationAuditRecordsStructureWithoutModelText(t *testing.T) {
	const private = "secret-prompt-or-response-never-record"
	for _, finish := range []string{"STOP", "MAX_TOKENS", private} {
		t.Run(finish, func(t *testing.T) {
			f, err := os.Create(filepath.Join(t.TempDir(), "receipt.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			body, err := json.Marshal(map[string]any{"responseId": private, "modelVersion": "gemini-3.5-flash-lite", "candidates": []any{map[string]any{"finishReason": finish, "content": map[string]any{"parts": []any{map[string]any{"text": `{"unexpected":"` + private + `"}`}}}}}})
			if err != nil {
				t.Fatal(err)
			}
			var captured []byte
			a := auditTransport{receipt: f, model: "gemini-3.5-flash-lite", capture: func(raw []byte) error { captured = append([]byte(nil), raw...); return nil }, next: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
			})}
			req, _ := http.NewRequest("POST", "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.5-flash-lite:generateContent", nil)
			res, err := a.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			got, err := io.ReadAll(res.Body)
			if err != nil || string(got) != string(body) {
				t.Fatal("observer changed response")
			}
			if string(captured) != string(body) {
				t.Fatal("capture cannot reproduce exact provider response offline")
			}
			raw, err := os.ReadFile(f.Name())
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), private) {
				t.Fatal("provider text escaped structural diagnostic")
			}
			var event struct {
				Envelope map[string]any `json:"envelope"`
			}
			if json.Unmarshal(raw, &event) != nil {
				t.Fatal("invalid receipt")
			}
			expected := finish
			if finish == private {
				expected = "OTHER"
			}
			if event.Envelope["finishReason"] != expected || event.Envelope["modelMatchesConfigured"] != true || event.Envelope["reportJSONValid"] != true || event.Envelope["reportGoJSONDecodes"] != false {
				t.Fatal("missing structural rejection diagnostics")
			}
			if event.Envelope["canonicalDecodeReason"] == nil {
				t.Fatal("canonical decoder rejection boundary missing")
			}
		})
	}
}

func TestGenerationSummaryRejectsUncertainOrOversizedEnvelope(t *testing.T) {
	if generationSummary([]byte(`{"candidates":`), "gemini-3.5-flash-lite", nil)["envelopeJSONValid"] != false {
		t.Fatal("malformed JSON reported valid")
	}
	if generationSummary(make([]byte, (1<<20)+1), "gemini-3.5-flash-lite", nil)["completeWithinBound"] != false {
		t.Fatal("oversized response summarized")
	}
}

func TestDiagnosticIdentityRequiresExplicitAuthorization(t *testing.T) {
	original, err := verificationKey("", "gemini-2.5-flash-lite")
	if err != nil {
		t.Fatal(err)
	}
	diagnostic, err := verificationKey("OWNER AUTHORIZED ONE ADDITIONAL DIAGNOSTIC PROBE", "gemini-2.5-flash-lite")
	if err != nil || original == diagnostic {
		t.Fatal("distinct authorized identity required")
	}
	if _, err := verificationKey("retry", "gemini-2.5-flash-lite"); err == nil {
		t.Fatal("arbitrary retries accepted")
	}
	model35, err := verificationKey("OWNER AUTHORIZED ONE GEMINI 3.5 VERIFICATION", "gemini-3.5-flash-lite")
	if err != nil || model35 == diagnostic || model35 == original {
		t.Fatal("model-change verification must preserve both prior identities")
	}
	if _, err := verificationKey("OWNER AUTHORIZED ONE GEMINI 3.5 VERIFICATION", "gemini-2.5-flash-lite"); err == nil {
		t.Fatal("model-change acknowledgement cannot repeat the failed legacy request")
	}
	instrumented, err := verificationKey("OWNER AUTHORIZED ONE ENCRYPTED GEMINI 3.5 VERIFICATION", "gemini-3.5-flash-lite")
	if err != nil || instrumented == model35 || instrumented == diagnostic || instrumented == original {
		t.Fatal("instrumented identity must preserve all prior jobs")
	}
	if _, err := verificationKey("OWNER AUTHORIZED ONE ENCRYPTED GEMINI 3.5 VERIFICATION", "gemini-2.5-flash-lite"); err == nil {
		t.Fatal("instrumented acknowledgement accepted wrong model")
	}
	corrected, err := verificationKey("OWNER AUTHORIZED ONE CORRECTED GEMINI 3.5 VERIFICATION", "gemini-3.5-flash-lite")
	if err != nil || corrected == instrumented || corrected == model35 || corrected == diagnostic || corrected == original {
		t.Fatal("corrected request identity must preserve all prior jobs")
	}
	if _, err := verificationKey("OWNER AUTHORIZED ONE CORRECTED GEMINI 3.5 VERIFICATION", "gemini-2.5-flash-lite"); err == nil {
		t.Fatal("corrected request acknowledgement accepted wrong model")
	}
}

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTransportAuditRetainsStatusWithoutCredentialsOrBodies(t *testing.T) {
	p := filepath.Join(t.TempDir(), "receipt.jsonl")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	const private = "fixture-secret-never-record"
	body := `{"error":{"status":"INVALID_ARGUMENT","message":"` + private + `"}}`
	a := auditTransport{receipt: f, next: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	req, _ := http.NewRequest("POST", "https://generativelanguage.googleapis.com/v1beta/models/fixture:countTokens?private="+private, strings.NewReader(private))
	req.Header.Set("x-goog-api-key", private)
	res, err := a.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	got, err := io.ReadAll(res.Body)
	if err != nil || string(got) != body {
		t.Fatal("observer changed provider response")
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, private) || !strings.Contains(text, `"httpStatus":400`) || !strings.Contains(text, `"providerStatus":"INVALID_ARGUMENT"`) {
		t.Fatal("unsafe or missing diagnostic")
	}
}

func TestVerificationRejectsForeignOrUnacknowledgedTargets(t *testing.T) {
	const host = "ep-confirmed.neon.tech"
	const dsn = "postgres://fixture:fixture@" + host + "/neondb?sslmode=require"
	const origin = "https://transactx-demo-api.onrender.com"
	const ack = "VERIFY ONE SYNTHETIC GEMINI INVESTIGATION"
	if err := validateTarget(dsn, host, "neondb", origin, ack); err != nil {
		t.Fatal(err)
	}
	for _, c := range [][5]string{
		{dsn, "ep-other.neon.tech", "neondb", origin, ack},
		{dsn, host, "transactx", origin, ack},
		{dsn, host, "neondb", "https://unrelated.invalid", ack},
		{dsn, host, "neondb", origin, ""},
		{"postgres://fixture:fixture@" + host + "/neondb?sslmode=disable", host, "neondb", origin, ack},
	} {
		if validateTarget(c[0], c[1], c[2], c[3], c[4]) == nil {
			t.Fatal("unsafe target accepted")
		}
	}
}
