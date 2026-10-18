package demoenrich

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestEnrichmentClientPreservesIdentityAndStopsOnAmbiguity(t *testing.T) {
	var calls int
	client := &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Idempotency-Key") != "stable-key" || r.Header.Get("Authorization") != "Bearer fixture-jwt" {
			t.Fatal("request identity lost")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil || body["amountPaise"] != float64(125) {
			t.Fatal("paise changed")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":{"id":"fixture-payment","state":"COMPLETED"}}`))}, nil
	})}
	var result struct{ ID, State string }
	if err := request(context.Background(), client, "https://fixture.example", "POST", "/api/payments", "fixture-jwt", "stable-key", map[string]any{"amountPaise": 125}, &result); err != nil || result.ID != "fixture-payment" {
		t.Fatal("valid response lost")
	}
	client.Transport = fixtureTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("fixture transport timeout with private details")
	})
	err := request(context.Background(), client, "https://fixture.example", "POST", "/api/payments", "fixture-jwt", "stable-key", nil, &result)
	if err == nil || !strings.Contains(err.Error(), "unknown") || strings.Contains(err.Error(), "private details") || calls != 2 {
		t.Fatal("ambiguous outcome retried or leaked exception")
	}
}
func TestEnrichmentClientDoesNotLogProviderBodyOrIgnoreRetryAfter(t *testing.T) {
	for _, status := range []int{401, 429, 503} {
		client := &http.Client{Transport: fixtureTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{"60"}}, Body: io.NopCloser(strings.NewReader("secret response fixture"))}, nil
		})}
		err := request(context.Background(), client, "https://fixture.example", "POST", "/api/payments", "", "", nil, &struct{}{})
		if err == nil || strings.Contains(err.Error(), "secret response") {
			t.Fatal("unsafe response handling")
		}
		if status == 429 && !strings.Contains(err.Error(), "60") {
			t.Fatal("Retry-After lost")
		}
	}
}
