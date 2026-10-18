// Package demohost packages the native services for a single sleeping demo host.
// It is deliberately separate from the production payment/recovery kernel.
package demohost

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Budget is a bounded, process-local token bucket. Global budgets deliberately
// do not trust X-Forwarded-For or user-supplied identity; a single demo replica
// shares one allowance even when an attacker rotates IPs/tokens.
type Budget struct {
	mu                          sync.Mutex
	tokens, capacity, perSecond float64
	last                        time.Time
}

func NewBudget(capacity int, perSecond float64) *Budget {
	return &Budget{tokens: float64(capacity), capacity: float64(capacity), perSecond: perSecond, last: time.Now()}
}

func (b *Budget) Allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if now.After(b.last) {
		b.tokens += now.Sub(b.last).Seconds() * b.perSecond
		if b.tokens > b.capacity {
			b.tokens = b.capacity
		}
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

type Acquirer interface {
	Acquire(context.Context) (release func(), err error)
}

type Gateway struct {
	upstream           http.Handler
	services           Acquirer
	origins            map[string]bool
	all, writes, login *Budget
	slots, loginSlots  chan struct{}
	streamLifetime     time.Duration
}

func NewGateway(target *url.URL, services Acquirer, origins []string) *Gateway {
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1 // Flush SSE frames immediately.
	proxy.ModifyResponse = func(response *http.Response) error {
		// The gateway is the single CORS authority, including proxy failures.
		// Otherwise ReverseProxy adds the backend header to ours and browsers
		// reject two Access-Control-Allow-Origin values.
		response.Header.Del("Access-Control-Allow-Origin")
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, _ error) {
		failure(w, http.StatusServiceUnavailable, "DEMO_STARTING", "Demo backend is starting. Retry shortly.")
	}
	g := &Gateway{upstream: proxy, services: services, origins: map[string]bool{},
		all: NewBudget(60, 4), writes: NewBudget(6, 20.0/60), login: NewBudget(3, 6.0/60),
		slots: make(chan struct{}, 12), loginSlots: make(chan struct{}, 1), streamLifetime: 2 * time.Minute}
	for _, origin := range origins {
		g.origins[strings.TrimSpace(origin)] = true
	}
	return g
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path == "/healthz" && r.Method == http.MethodGet {
		// Gateway liveness only. Never connects to PostgreSQL or starts services.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"gateway":"alive","databaseReadiness":"not-probed"}`))
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		if !g.origins[origin] {
			failure(w, 403, "ORIGIN_DENIED", "Origin is not allowed.")
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Add("Vary", "Origin")
		w.Header().Set("Access-Control-Expose-Headers", "Retry-After")
	}
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Request-ID, Last-Event-ID")
		w.Header().Set("Access-Control-Max-Age", "600")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		failure(w, 405, "METHOD_NOT_ALLOWED", "Method unavailable in public demo.")
		return
	}
	// These remain blocked even if somebody accidentally enables backend flags.
	if r.URL.Path == "/api/auth/register" || strings.HasPrefix(r.URL.Path, "/api/ops/chaos") {
		failure(w, 403, "DEMO_RESTRICTED", "Public registration and chaos controls are disabled.")
		return
	}
	now := time.Now()
	isLogin := r.URL.Path == "/api/auth/login" || r.URL.Path == "/api/auth/demo"
	if !g.all.Allow(now) || (r.Method == http.MethodPost && !g.writes.Allow(now)) ||
		(isLogin && !g.login.Allow(now)) {
		failure(w, 429, "RATE_LIMITED", "Demo request limit reached. Retry later.")
		return
	}
	select {
	case g.slots <- struct{}{}:
		defer func() { <-g.slots }()
	default:
		failure(w, 429, "RATE_LIMITED", "Demo is busy. Retry later.")
		return
	}
	if isLogin {
		select {
		case g.loginSlots <- struct{}{}:
			defer func() { <-g.loginSlots }()
		default:
			failure(w, 429, "RATE_LIMITED", "A sign-in is already being processed.")
			return
		}
	}
	const maxBody = 32 << 10
	if r.ContentLength > maxBody {
		failure(w, 413, "BODY_TOO_LARGE", "Request body exceeds demo limit.")
		return
	}
	// Buffer bounded bodies so chunked requests also fail with 413 before they
	// reach authentication, JSON decoding, or wake a database-connected process.
	body := http.MaxBytesReader(w, r.Body, maxBody)
	defer body.Close()
	bytes, err := io.ReadAll(body)
	if err != nil {
		failure(w, 413, "BODY_TOO_LARGE", "Request body exceeds demo limit.")
		return
	}
	r.Body = http.NoBody
	if len(bytes) > 0 {
		r.Body = io.NopCloser(strings.NewReader(string(bytes)))
	}
	r.ContentLength = int64(len(bytes))
	ctx, cancel := context.WithTimeout(r.Context(), g.streamLifetime)
	defer cancel()
	release, err := g.services.Acquire(ctx)
	if err != nil {
		failure(w, 503, "DEMO_STARTING", "Demo backend is starting. Retry shortly.")
		return
	}
	defer release()
	// Gateway owns CORS on successful, rejected and failed proxy responses.
	r.Header.Del("Forwarded")
	r.Header.Del("X-Forwarded-For")
	g.upstream.ServeHTTP(w, r.WithContext(ctx))
}

func failure(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	if status == 429 || status == 503 {
		w.Header().Set("Retry-After", "10")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}
