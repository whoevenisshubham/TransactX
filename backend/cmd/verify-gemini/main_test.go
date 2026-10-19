package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func TestDiagnosticIdentityRequiresExplicitAuthorization(t *testing.T) {
	original, err := verificationKey("")
	if err != nil {
		t.Fatal(err)
	}
	diagnostic, err := verificationKey("OWNER AUTHORIZED ONE ADDITIONAL DIAGNOSTIC PROBE")
	if err != nil || original == diagnostic {
		t.Fatal("distinct authorized identity required")
	}
	if _, err := verificationKey("retry"); err == nil {
		t.Fatal("arbitrary retries accepted")
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
