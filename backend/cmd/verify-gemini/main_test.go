package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

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
			a := auditTransport{receipt: f, model: "gemini-3.5-flash-lite", next: roundTripFunc(func(*http.Request) (*http.Response, error) {
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
			if event.Envelope["finishReason"] != expected || event.Envelope["modelMatchesConfigured"] != true || event.Envelope["reportJSONValid"] != true || event.Envelope["canonicalJSONDecodes"] != false {
				t.Fatal("missing structural rejection diagnostics")
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
