package demohost

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type acquireFunc func(context.Context) (func(), error)

func (f acquireFunc) Acquire(ctx context.Context) (func(), error) { return f(ctx) }
func gateway(t *testing.T, fn http.HandlerFunc) (*Gateway, *atomic.Int32) {
	t.Helper()
	upstream := httptest.NewServer(fn)
	t.Cleanup(upstream.Close)
	u, _ := url.Parse(upstream.URL)
	count := &atomic.Int32{}
	g := NewGateway(u, acquireFunc(func(context.Context) (func(), error) { count.Add(1); return func() {}, nil }), []string{"https://demo.example.org"})
	return g, count
}

func TestBlockedRequestsNeverWakeServices(t *testing.T) {
	g, count := gateway(t, func(w http.ResponseWriter, r *http.Request) { t.Error("upstream reached") })
	for _, tc := range []struct {
		method, path, origin, body string
		want                       int
	}{
		{"GET", "/healthz", "", "", 200},
		{"OPTIONS", "/api/payments", "https://demo.example.org", "", 204},
		{"POST", "/api/auth/register", "", "{}", 403},
		{"POST", "/api/ops/chaos/start", "", "{}", 403},
		{"GET", "/api/me", "https://evil.example.org", "", 403},
		{"POST", "/v1/holds", "", "{}", 404},
		{"DELETE", "/api/payments", "", "", 405},
		{"POST", "/api/payments", "", strings.Repeat("x", 32769), 413},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%s %s: got %d want %d", tc.method, tc.path, w.Code, tc.want)
		}
	}
	if count.Load() != 0 {
		t.Fatal("blocked request woke PostgreSQL processes")
	}
}

func TestChunkedBodyBoundAndCORSOnErrors(t *testing.T) {
	g, count := gateway(t, func(http.ResponseWriter, *http.Request) { t.Fatal("upstream reached") })
	r := httptest.NewRequest("POST", "/api/payments", strings.NewReader(strings.Repeat("x", 32769)))
	r.ContentLength = -1
	r.Header.Set("Origin", "https://demo.example.org")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 413 || w.Header().Get("Access-Control-Allow-Origin") != "https://demo.example.org" || count.Load() != 0 {
		t.Fatalf("unsafe chunked request: %d", w.Code)
	}
}

func TestGlobalBudgetCannotBeBypassedWithForwardedIPOrToken(t *testing.T) {
	g, count := gateway(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	g.all = NewBudget(2, 0)
	for i, want := range []int{200, 200, 429} {
		r := httptest.NewRequest("GET", "/api/me", nil)
		r.Header.Set("X-Forwarded-For", strings.Repeat("1", i+1))
		r.Header.Set("Authorization", strings.Repeat("t", i+1))
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("request %d got %d want %d", i, w.Code, want)
		}
		if want == 429 && w.Header().Get("Retry-After") == "" {
			t.Fatal("retry hint missing")
		}
	}
	if count.Load() != 2 {
		t.Fatal("limited request woke services")
	}
}

func TestLoginConcurrencyProtectsPasswordHashMemory(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	g, _ := gateway(t, func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; w.WriteHeader(200) })
	done := make(chan struct{})
	go func() {
		defer close(done)
		g.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/auth/login", strings.NewReader("{}")))
	}()
	<-entered
	w := httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequest("POST", "/api/auth/login", strings.NewReader("{}")))
	close(release)
	<-done
	if w.Code != 429 {
		t.Fatalf("parallel hash admitted: %d", w.Code)
	}
}

func TestDemoLoginSharesPasswordAdmissionBucket(t *testing.T) {
	g, count := gateway(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	g.login = NewBudget(1, 0)
	for index, path := range []string{"/api/auth/demo", "/api/auth/login", "/api/auth/demo", "/api/auth/oidc/google/start", "/api/auth/oidc/google/callback", "/api/auth/oidc/exchange"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", path, strings.NewReader(`{"persona":"operator"}`))
		r.Header.Set("X-Forwarded-For", "rotating-client")
		g.ServeHTTP(w, r)
		want := 429
		if index == 0 {
			want = 200
		}
		if w.Code != want {
			t.Fatalf("%s got %d, expected %d", path, w.Code, want)
		}
	}
	if count.Load() != 1 {
		t.Fatal("limited login woke services")
	}
}

func TestProxyPreservesPayloadAndStreamsBeforeCompletion(t *testing.T) {
	first := make(chan struct{})
	finish := make(chan struct{})
	g, _ := gateway(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != "{\"hello\":1}" || r.Header.Get("Idempotency-Key") != "original" {
			t.Error("request changed")
		}
		if r.Header.Get("X-Forwarded-For") == "attacker" {
			t.Error("untrusted forwarding passed through")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: ready\ndata: {}\n\n")
		w.(http.Flusher).Flush()
		close(first)
		<-finish
	})
	srv := httptest.NewServer(g)
	defer srv.Close()
	r, _ := http.NewRequest("POST", srv.URL+"/api/events", strings.NewReader("{\"hello\":1}"))
	r.Header.Set("Idempotency-Key", "original")
	r.Header.Set("X-Forwarded-For", "attacker")
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	buf := make([]byte, 6)
	_, err = io.ReadFull(response.Body, buf)
	close(finish)
	<-first
	if err != nil || string(buf) != "event:" {
		t.Fatalf("SSE did not flush: %q %v", buf, err)
	}
}

func TestBudgetRefillAndParallelAdmission(t *testing.T) {
	b := NewBudget(5, 1)
	now := time.Now()
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if b.Allow(now) {
				admitted.Add(1)
			}
		}()
	}
	wg.Wait()
	if admitted.Load() != 5 {
		t.Fatalf("racy budget: %d", admitted.Load())
	}
	if !b.Allow(now.Add(time.Second)) || b.Allow(now.Add(time.Second)) {
		t.Fatal("refill incorrect")
	}
}

func TestUnavailableGroupReturnsBoundedCORSResponse(t *testing.T) {
	u, _ := url.Parse("http://127.0.0.1:1")
	g := NewGateway(u, acquireFunc(func(context.Context) (func(), error) { return nil, errors.New("not ready") }), []string{"https://demo.example.org"})
	r := httptest.NewRequest("GET", "/api/me", nil)
	r.Header.Set("Origin", "https://demo.example.org")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 503 || w.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatal("unreadable startup response")
	}
}

func TestCORSHasSingleOriginAndSurvivesUpstreamFailure(t *testing.T) {
	g, _ := gateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "https://demo.example.org")
		w.WriteHeader(200)
	})
	r := httptest.NewRequest("GET", "/api/me", nil)
	r.Header.Set("Origin", "https://demo.example.org")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if values := w.Header().Values("Access-Control-Allow-Origin"); len(values) != 1 {
		t.Fatalf("duplicate CORS: %v", values)
	}
	u, _ := url.Parse("http://127.0.0.1:1")
	g = NewGateway(u, acquireFunc(func(context.Context) (func(), error) { return func() {}, nil }), []string{"https://demo.example.org"})
	w = httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 503 || w.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatal("proxy failure lost CORS")
	}
}

func TestProcessGroupIdlesAndCanWakeAgain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewProcesses(ctx, t.TempDir(), nil, 10*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))
	release, err := p.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	first := p.current
	p.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	p.mu.Lock()
	if p.current != first {
		t.Error("active request lost its session")
	}
	p.mu.Unlock()
	release()
	deadline := time.Now().Add(3 * time.Second)
	for {
		p.mu.Lock()
		stopped := p.current == nil
		if stopped {
			p.cooldown = time.Time{}
		}
		p.mu.Unlock()
		if stopped {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("group did not idle")
		}
		time.Sleep(10 * time.Millisecond)
	}
	release, err = p.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	release()
	cancel()
	p.Wait()
}

func TestMissingChildFailsClosedAndLeavesNoSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	p := NewProcesses(ctx, t.TempDir(), []Service{{Name: "missing"}}, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if release, err := p.Acquire(ctx); err == nil {
		release()
		t.Fatal("missing binary reported ready")
	}
	p.Wait()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.current != nil {
		t.Fatal("failed startup retained session")
	}
}
