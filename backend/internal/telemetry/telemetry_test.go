package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/transactx/backend/internal/database"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestTraceIDValidation(t *testing.T) {
	valid := "4bf92f3577b34da6a3ce929d0e0e4736"
	if err := ValidateTraceID(valid); err != nil {
		t.Fatalf("expected valid trace ID to pass, got: %v", err)
	}

	invalidCases := []string{
		"",
		"4BF92F3577B34DA6A3CE929D0E0E4736",          // uppercase rejected
		"4bf92f3577b34da6a3ce929d0e0e473",            // 31 chars
		"4bf92f3577b34da6a3ce929d0e0e47366",          // 33 chars
		"4bf92f3577b34da6a3ce929d0e0e473g",          // invalid hex char
		"invalid-non-hex-characters-here!",           // non-hex string
	}

	for _, tc := range invalidCases {
		if err := ValidateTraceID(tc); err == nil {
			t.Errorf("expected trace ID %q to fail validation, but it passed", tc)
		}
	}
}

func TestSanitizeSpan_RedactionAndBounds(t *testing.T) {
	cfg := DefaultConfig("test-svc")
	exp := NewBoundedPostgresExporter(nil, cfg, NewMetrics(), nil)
	defer exp.Shutdown(context.Background())

	paymentID := uuid.New()
	tp := sdktrace.NewTracerProvider()
	tracer := tp.Tracer("test")

	ctx := context.Background()
	_, span := tracer.Start(ctx, "test-span")

	// Add various attributes: safe, sensitive, excessive, oversized
	span.SetAttributes(
		attribute.String("payment_id", paymentID.String()),
		attribute.String("bank_code", "BANK-A"),
		attribute.String("auth_token", "secret-token-123"),
		attribute.String("jwt", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"),
		attribute.String("password", "super-secret"),
		attribute.String("customer_name", "Alice Example"),
		attribute.String("oversized", strings.Repeat("x", 500)),
	)
	// Add 30 filler attributes to test the 20-attribute cap
	for i := 0; i < 30; i++ {
		span.SetAttributes(attribute.Int(fmt.Sprintf("safe_key_%d", i), i))
	}
	span.End()

	roSpan, ok := span.(sdktrace.ReadOnlySpan)
	if !ok {
		t.Fatalf("expected span to implement sdktrace.ReadOnlySpan")
	}

	record := exp.sanitizeSpan(roSpan)

	// Verify PaymentID was extracted
	if record.PaymentID == nil || *record.PaymentID != paymentID {
		t.Fatalf("expected payment ID %s, got %+v", paymentID, record.PaymentID)
	}

	// Verify sensitive attributes were redacted
	for k := range record.Attributes {
		lower := strings.ToLower(k)
		if strings.Contains(lower, "auth") || strings.Contains(lower, "jwt") ||
			strings.Contains(lower, "password") || strings.Contains(lower, "customer_name") {
			t.Errorf("found forbidden sensitive attribute in sanitized span: %s", k)
		}
	}

	// Verify maximum 20 attributes limit
	if len(record.Attributes) > 20 {
		t.Errorf("expected max 20 attributes, got %d", len(record.Attributes))
	}

	// Verify oversized string was truncated to 256
	if s, ok := record.Attributes["oversized"].(string); ok {
		if len(s) > 256 {
			t.Errorf("expected oversized string to be truncated to <= 256, got length %d", len(s))
		}
	}
}

func TestBoundedExporter_QueueOverloadNeverBlocks(t *testing.T) {
	// Tiny queue of capacity 2
	cfg := Config{
		ServiceName:   "test-svc",
		QueueCapacity: 2,
		MaxBatchSize:  10,
		BatchTimeout:  time.Hour, // don't flush automatically
	}
	metrics := NewMetrics()
	exp := NewBoundedPostgresExporter(nil, cfg, metrics, nil)
	defer exp.Shutdown(context.Background())

	tp := sdktrace.NewTracerProvider()
	tracer := tp.Tracer("test")

	var spans []sdktrace.ReadOnlySpan
	for i := 0; i < 10; i++ {
		_, span := tracer.Start(context.Background(), fmt.Sprintf("span-%d", i))
		span.End()
		spans = append(spans, span.(sdktrace.ReadOnlySpan))
	}

	// Exporting 10 spans when queue capacity is 2 must succeed immediately and record drops
	done := make(chan error, 1)
	go func() {
		done <- exp.ExportSpans(context.Background(), spans)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ExportSpans returned error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("ExportSpans blocked on full queue!")
	}

	if metrics.DroppedSpans.Load() == 0 {
		t.Errorf("expected dropped spans count > 0 due to overload, got 0")
	}
	if metrics.ExportedSpans.Load() == 0 {
		t.Errorf("expected at least some spans to be exported, got 0")
	}
	if metrics.ExportedSpans.Load()+metrics.DroppedSpans.Load() != 10 {
		t.Errorf("expected total exported + dropped == 10, got %d + %d", metrics.ExportedSpans.Load(), metrics.DroppedSpans.Load())
	}
}

func TestSpanLinks_RecoveryLinksToOriginalTrace(t *testing.T) {
	origTraceID := "4bf92f3577b34da6a3ce929d0e0e4736"
	origSpanID := "00f067aa0ba902b7"

	tp := sdktrace.NewTracerProvider()
	tracer := tp.Tracer("recovery-worker")

	ctx, recoverySpan := StartRecoverySpan(
		context.Background(),
		tracer,
		"recovery.attempt",
		origTraceID,
		origSpanID,
		attribute.String("reason", "SCHEDULED_RETRY"),
	)
	defer recoverySpan.End()

	roSpan, ok := recoverySpan.(sdktrace.ReadOnlySpan)
	if !ok {
		t.Fatalf("span must implement ReadOnlySpan")
	}

	// 1. Recovery trace must have its OWN new trace ID, not the original trace ID
	newTraceID := roSpan.SpanContext().TraceID().String()
	if newTraceID == origTraceID {
		t.Fatalf("recovery trace must have a new independent trace ID, not reuse original")
	}

	// 2. Recovery span must NOT have a parent span ID (it's a new root)
	if roSpan.Parent().IsValid() {
		t.Fatalf("recovery span must not manufacture a parent across restarts")
	}

	// 3. Recovery span must have a span link pointing to the original trace ID and span ID
	links := roSpan.Links()
	if len(links) != 1 {
		t.Fatalf("expected exactly 1 span link, got %d", len(links))
	}
	linkedSC := links[0].SpanContext
	if linkedSC.TraceID().String() != origTraceID {
		t.Errorf("expected linked trace ID %s, got %s", origTraceID, linkedSC.TraceID().String())
	}
	if linkedSC.SpanID().String() != origSpanID {
		t.Errorf("expected linked span ID %s, got %s", origSpanID, linkedSC.SpanID().String())
	}
	_ = ctx
}

func TestW3CContextPropagation(t *testing.T) {
	cfg := TestConfig("test-app")
	provider, shutdown, err := InitProvider(context.Background(), cfg, nil, nil)
	if err != nil {
		t.Fatalf("failed to init provider: %v", err)
	}
	defer shutdown(context.Background())

	tracer := provider.Tracer()

	// Backend server handler wrapped with HTTPMiddleware
	targetHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		if !span.SpanContext().IsValid() {
			t.Errorf("server request had no valid span context")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	serverMiddleware := HTTPMiddleware(tracer, provider.Metrics())
	ts := httptest.NewServer(serverMiddleware(targetHandler))
	defer ts.Close()

	// Client making outbound call wrapped with ClientTransport
	client := &http.Client{
		Transport: NewClientTransport(http.DefaultTransport, tracer),
	}

	// Start root client span
	ctx, clientSpan := tracer.Start(context.Background(), "payment.client_call")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/test", nil)

	resp, err := client.Do(req)
	clientSpan.End()
	if err != nil {
		t.Fatalf("client request failed: %v", err)
	}
	defer resp.Body.Close()

	traceHeader := resp.Header.Get("X-Trace-ID")
	if traceHeader == "" {
		t.Fatalf("expected X-Trace-ID in response header")
	}
	if traceHeader != clientSpan.SpanContext().TraceID().String() {
		t.Errorf("expected server to correlate with client trace ID %s, got %s", clientSpan.SpanContext().TraceID().String(), traceHeader)
	}
}

func TestTraceStore_PostgresPersistenceAndRetrieval(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}

	pool, err := database.NewPool(context.Background(), dbURL)
	if err != nil {
		t.Skip("PostgreSQL not available")
	}
	defer pool.Close()

	cfg := TestConfig("transactx-api")
	provider, shutdown, err := InitProvider(context.Background(), cfg, pool, nil)
	if err != nil {
		t.Fatalf("failed to init provider: %v", err)
	}
	defer shutdown(context.Background())

	tracer := provider.Tracer()
	store := provider.Store()

	paymentID := uuid.New()
	origTraceID := "0123456789abcdef0123456789abcdef"
	origSpanID := "abcdef0123456789"

	// 1. Create a root span
	ctx, rootSpan := tracer.Start(context.Background(), "payment.intent",
		trace.WithAttributes(
			attribute.String("payment_id", paymentID.String()),
			attribute.String("bank_code", "BANK-A"),
		),
	)
	traceID := rootSpan.SpanContext().TraceID().String()
	spanID := rootSpan.SpanContext().SpanID().String()

	// 2. Create a child span
	_, childSpan := tracer.Start(ctx, "bank.authorize",
		trace.WithAttributes(
			attribute.String("payment_id", paymentID.String()),
			attribute.String("target_id", "RAIL-A"),
		),
	)
	childSpan.End()
	rootSpan.End()

	// 3. Create a recovery span linked to this trace
	_, recSpan := StartRecoverySpan(
		context.Background(),
		tracer,
		"recovery.retry",
		origTraceID,
		origSpanID,
		attribute.String("payment_id", paymentID.String()),
	)
	recTraceID := recSpan.SpanContext().TraceID().String()
	recSpan.End()

	// Force flush all spans to PostgreSQL
	if err := provider.ForceFlush(context.Background()); err != nil {
		t.Fatalf("failed to flush spans to postgres: %v", err)
	}

	// Query trace from store
	traceResp, err := store.GetTrace(context.Background(), traceID)
	if err != nil {
		t.Fatalf("GetTrace failed: %v", err)
	}

	if traceResp.SourceStatus != "available" {
		t.Fatalf("expected sourceStatus available, got %s", traceResp.SourceStatus)
	}
	if len(traceResp.Spans) != 2 {
		t.Fatalf("expected 2 spans for trace %s, got %d", traceID, len(traceResp.Spans))
	}

	foundRoot := false
	foundChild := false
	for _, s := range traceResp.Spans {
		if s.SpanID == spanID {
			foundRoot = true
			if s.Name != "payment.intent" {
				t.Errorf("root span name mismatch: %s", s.Name)
			}
			if s.ParentSpanID != nil {
				t.Errorf("root span should have null parentSpanId, got %v", *s.ParentSpanID)
			}
		} else {
			foundChild = true
			if s.ParentSpanID == nil || *s.ParentSpanID != spanID {
				t.Errorf("child span parent ID should be %s, got %v", spanID, s.ParentSpanID)
			}
		}
	}
	if !foundRoot || !foundChild {
		t.Errorf("spans missing: foundRoot=%v, foundChild=%v", foundRoot, foundChild)
	}

	// Query recovery trace to verify links
	recResp, err := store.GetTrace(context.Background(), recTraceID)
	if err != nil {
		t.Fatalf("GetTrace for recovery failed: %v", err)
	}
	if len(recResp.Spans) != 1 {
		t.Fatalf("expected 1 recovery span, got %d", len(recResp.Spans))
	}
	if len(recResp.Spans[0].Links) != 1 {
		t.Fatalf("expected 1 link on recovery span, got %d", len(recResp.Spans[0].Links))
	}
	if recResp.Spans[0].Links[0].TraceID != origTraceID {
		t.Errorf("expected link trace ID %s, got %s", origTraceID, recResp.Spans[0].Links[0].TraceID)
	}

	// Query payment trace IDs
	pTraceIDs, err := store.GetTraceIDsForPayment(context.Background(), paymentID)
	if err != nil {
		t.Fatalf("GetTraceIDsForPayment failed: %v", err)
	}
	if len(pTraceIDs) < 2 {
		t.Errorf("expected at least 2 distinct trace IDs for payment, got %d: %v", len(pTraceIDs), pTraceIDs)
	}

	// Test missing trace returns sourceStatus: "not_collected"
	missingTraceID := "99999999999999999999999999999999"
	missingResp, err := store.GetTrace(context.Background(), missingTraceID)
	if err != nil {
		t.Fatalf("GetTrace for missing trace returned unexpected error: %v", err)
	}
	if missingResp.SourceStatus != "not_collected" {
		t.Errorf("expected missing trace to return sourceStatus not_collected, got %s", missingResp.SourceStatus)
	}
	if len(missingResp.Spans) != 0 {
		t.Errorf("expected empty spans array for missing trace, got %d", len(missingResp.Spans))
	}

	// Test cleanup function executes without error
	_, err = provider.Exporter().CleanupExpired(context.Background())
	if err != nil {
		t.Fatalf("CleanupExpired failed: %v", err)
	}
}

type flusherSpyResponseWriter struct {
	header http.Header
	status int
	flushed bool
}

func (f *flusherSpyResponseWriter) Header() http.Header {
	if f.header == nil {
		f.header = make(http.Header)
	}
	return f.header
}
func (f *flusherSpyResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (f *flusherSpyResponseWriter) WriteHeader(status int)      { f.status = status }
func (f *flusherSpyResponseWriter) Flush()                      { f.flushed = true }

type nonFlusherResponseWriter struct {
	header http.Header
	status int
}

func (n *nonFlusherResponseWriter) Header() http.Header {
	if n.header == nil {
		n.header = make(http.Header)
	}
	return n.header
}
func (n *nonFlusherResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (n *nonFlusherResponseWriter) WriteHeader(status int)      { n.status = status }

func TestHTTPMiddleware_FlusherSupport(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	tracer := tp.Tracer("test")
	middleware := HTTPMiddleware(tracer, NewMetrics())

	// Case 1: Underlying writer implements http.Flusher
	spy := &flusherSpyResponseWriter{}
	flushedCalled := false
	handlerFlusher := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatalf("expected wrapped writer to implement http.Flusher")
		}
		flusher.Flush()
		flushedCalled = true
	})

	req := httptest.NewRequest(http.MethodGet, "/test/stream", nil)
	middleware(handlerFlusher).ServeHTTP(spy, req)

	if !flushedCalled {
		t.Fatalf("expected handler to call Flush")
	}
	if !spy.flushed {
		t.Fatalf("expected underlying flusher to be called")
	}

	// Case 2: Underlying writer does NOT implement http.Flusher
	nonFlusher := &nonFlusherResponseWriter{}
	nonFlusherChecked := false
	handlerNonFlusher := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ok := w.(http.Flusher)
		if ok {
			t.Fatalf("expected non-flusher writer NOT to implement http.Flusher")
		}
		nonFlusherChecked = true
	})

	middleware(handlerNonFlusher).ServeHTTP(nonFlusher, req)
	if !nonFlusherChecked {
		t.Fatalf("expected handler to check flusher")
	}
}

func TestSpanDTO_JSONSerialization_Gate4Contracts(t *testing.T) {
	now := time.Date(2026, 10, 11, 14, 30, 0, 123456789, time.UTC)
	fin := now.Add(45 * time.Millisecond)
	finStr := fin.Format(time.RFC3339Nano)

	span := SpanDTO{
		SpanID:     "span-1",
		Service:    "transactx-api",
		Name:       "POST /payments",
		Kind:       "SERVER",
		StartedAt:  now.Format(time.RFC3339Nano),
		FinishedAt: &finStr,
		StartTime:  now,
		EndTime:    fin,
		DurationMs: 45,
		Status:     "OK",
	}

	data, err := json.Marshal(span)
	if err != nil {
		t.Fatalf("failed to marshal span: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to unmarshal span json: %v", err)
	}

	// Verify both startedAt/finishedAt and startTime/endTime exist
	startedAt, ok := m["startedAt"].(string)
	if !ok || startedAt == "" {
		t.Fatalf("expected non-empty startedAt string, got %v", m["startedAt"])
	}
	finishedAt, ok := m["finishedAt"].(string)
	if !ok || finishedAt == "" {
		t.Fatalf("expected non-empty finishedAt string, got %v", m["finishedAt"])
	}
	if _, ok := m["startTime"].(string); !ok {
		t.Fatalf("expected startTime string for backwards compatibility")
	}
	if _, ok := m["endTime"].(string); !ok {
		t.Fatalf("expected endTime string for backwards compatibility")
	}
}

