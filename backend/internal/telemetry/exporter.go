package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// RawSpanRecord contains sanitized span fields ready for PostgreSQL insertion.
type RawSpanRecord struct {
	SpanID        string
	TraceID       string
	ParentSpanID  *string
	ServiceName   string
	SpanName      string
	SpanKind      string
	StartTime     time.Time
	EndTime       time.Time
	DurationMs    int64
	StatusCode    string
	StatusMessage *string
	Attributes    map[string]any
	Events        []any
	Links         []any
	PaymentID     *uuid.UUID
}

// BoundedPostgresExporter implements a safe, asynchronous, non-blocking span exporter.
type BoundedPostgresExporter struct {
	pool        *pgxpool.Pool
	cfg         Config
	metrics     *Metrics
	logger      *slog.Logger
	queue       chan RawSpanRecord
	flushReq    chan chan struct{}
	stopCh      chan struct{}
	wg          sync.WaitGroup
	stopped     bool
	stoppedMu   sync.Mutex
}

// NewBoundedPostgresExporter creates and starts the asynchronous bounded exporter worker.
func NewBoundedPostgresExporter(pool *pgxpool.Pool, cfg Config, metrics *Metrics, logger *slog.Logger) *BoundedPostgresExporter {
	if cfg.QueueCapacity <= 0 {
		cfg.QueueCapacity = 1000
	}
	if cfg.MaxBatchSize <= 0 {
		cfg.MaxBatchSize = 100
	}
	if cfg.BatchTimeout <= 0 {
		cfg.BatchTimeout = 250 * time.Millisecond
	}
	if logger == nil {
		logger = slog.Default()
	}
	if metrics == nil {
		metrics = NewMetrics()
	}

	exp := &BoundedPostgresExporter{
		pool:     pool,
		cfg:      cfg,
		metrics:  metrics,
		logger:   logger,
		queue:    make(chan RawSpanRecord, cfg.QueueCapacity),
		flushReq: make(chan chan struct{}),
		stopCh:   make(chan struct{}),
	}

	exp.wg.Add(1)
	go exp.workerLoop()

	return exp
}

// ExportSpans implements sdktrace.SpanExporter.
// Crucially, this method NEVER blocks and NEVER returns an error to callers.
// If the queue is full, the span is dropped and metrics.DroppedSpans is incremented.
func (e *BoundedPostgresExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.stoppedMu.Lock()
	if e.stopped {
		e.stoppedMu.Unlock()
		return nil
	}
	e.stoppedMu.Unlock()

	for _, span := range spans {
		record := e.sanitizeSpan(span)

		select {
		case e.queue <- record:
			e.metrics.ExportedSpans.Add(1)
		default:
			// Queue is full: immediately drop without blocking or throwing error
			e.metrics.DroppedSpans.Add(1)
		}
	}
	return nil
}

// Flush blocks until all currently queued spans are persisted to PostgreSQL.
func (e *BoundedPostgresExporter) Flush(ctx context.Context) error {
	e.stoppedMu.Lock()
	if e.stopped {
		e.stoppedMu.Unlock()
		return nil
	}
	e.stoppedMu.Unlock()

	done := make(chan struct{})
	select {
	case e.flushReq <- done:
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-e.stopCh:
			return nil
		}
	case <-ctx.Done():
		return ctx.Err()
	case <-e.stopCh:
		return nil
	}
}

// Shutdown gracefully stops the worker and drains pending queue items up to the timeout.
func (e *BoundedPostgresExporter) Shutdown(ctx context.Context) error {
	e.stoppedMu.Lock()
	if e.stopped {
		e.stoppedMu.Unlock()
		return nil
	}
	e.stopped = true
	close(e.stopCh)
	e.stoppedMu.Unlock()

	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// workerLoop drains the channel in batches and writes them to PostgreSQL.
func (e *BoundedPostgresExporter) workerLoop() {
	defer e.wg.Done()

	ticker := time.NewTicker(e.cfg.BatchTimeout)
	defer ticker.Stop()

	batch := make([]RawSpanRecord, 0, e.cfg.MaxBatchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		e.flushBatch(batch)
		batch = make([]RawSpanRecord, 0, e.cfg.MaxBatchSize)
	}

	for {
		select {
		case <-e.stopCh:
			// Drain remaining items from queue
			for {
				select {
				case rec := <-e.queue:
					batch = append(batch, rec)
					if len(batch) >= e.cfg.MaxBatchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}

		case done := <-e.flushReq:
			// Drain all currently queued items and flush synchronously
		flushLoop:
			for {
				select {
				case rec := <-e.queue:
					batch = append(batch, rec)
					if len(batch) >= e.cfg.MaxBatchSize {
						flush()
					}
				default:
					flush()
					close(done)
					break flushLoop
				}
			}

		case rec := <-e.queue:
			batch = append(batch, rec)
			if len(batch) >= e.cfg.MaxBatchSize {
				flush()
			}

		case <-ticker.C:
			flush()
		}
	}
}

func (e *BoundedPostgresExporter) flushBatch(records []RawSpanRecord) {
	if e.pool == nil || len(records) == 0 {
		return
	}

	// Use a bounded 3-second context for writing telemetry spans
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tx, err := e.pool.Begin(ctx)
	if err != nil {
		e.logger.Debug("failed to begin transaction for trace spans batch", "error", err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	query := `
		INSERT INTO trace_spans (
			span_id, trace_id, parent_span_id, service_name, span_name, span_kind,
			start_time, end_time, duration_ms, status_code, status_message,
			attributes, events, links, payment_id
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11,
			$12, $13, $14, $15
		) ON CONFLICT (trace_id, span_id) DO NOTHING
	`

	for _, rec := range records {
		attrJSON, _ := json.Marshal(rec.Attributes)
		eventsJSON, _ := json.Marshal(rec.Events)
		linksJSON, _ := json.Marshal(rec.Links)

		_, err := tx.Exec(ctx, query,
			rec.SpanID,
			rec.TraceID,
			rec.ParentSpanID,
			rec.ServiceName,
			rec.SpanName,
			rec.SpanKind,
			rec.StartTime,
			rec.EndTime,
			rec.DurationMs,
			rec.StatusCode,
			rec.StatusMessage,
			attrJSON,
			eventsJSON,
			linksJSON,
			rec.PaymentID,
		)
		if err != nil {
			// Telemetry write failure must never break anything; log as debug/warn
			e.logger.Debug("failed to persist telemetry trace span", "error", err, "trace_id", rec.TraceID, "span_id", rec.SpanID)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		e.logger.Debug("failed to commit trace spans batch", "error", err)
	}
}

// CleanupExpired deletes spans older than retention hours and enforces the maximum stored spans limit.
func (e *BoundedPostgresExporter) CleanupExpired(ctx context.Context) (int64, error) {
	if e.pool == nil {
		return 0, nil
	}

	retention := e.cfg.RetentionHours
	if retention <= 0 {
		retention = 24
	}

	// 1. Delete rows older than retention interval
	queryAge := `DELETE FROM trace_spans WHERE created_at < NOW() - make_interval(hours => $1)`
	tag, err := e.pool.Exec(ctx, queryAge, retention)
	if err != nil {
		return 0, fmt.Errorf("cleanup expired trace spans: %w", err)
	}
	deleted := tag.RowsAffected()

	// 2. Enforce maximum total stored spans
	maxSpans := e.cfg.MaxStoredSpans
	if maxSpans <= 0 {
		maxSpans = 20000
	}

	queryCap := `
		WITH overflow AS (
			SELECT trace_id, span_id
			FROM trace_spans
			ORDER BY created_at DESC
			OFFSET $1
		)
		DELETE FROM trace_spans
		WHERE (trace_id, span_id) IN (SELECT trace_id, span_id FROM overflow)
	`
	tagCap, err := e.pool.Exec(ctx, queryCap, maxSpans)
	if err == nil {
		deleted += tagCap.RowsAffected()
	}

	return deleted, nil
}

// sanitizeSpan inspects and bounds attributes, stripping forbidden credentials/names.
func (e *BoundedPostgresExporter) sanitizeSpan(span sdktrace.ReadOnlySpan) RawSpanRecord {
	sc := span.SpanContext()
	traceID := sc.TraceID().String()
	spanID := sc.SpanID().String()

	var parentSpanID *string
	if span.Parent().IsValid() {
		pid := span.Parent().SpanID().String()
		parentSpanID = &pid
	}

	duration := span.EndTime().Sub(span.StartTime()).Milliseconds()
	if duration < 0 {
		duration = 0
	}

	var statusMsg *string
	if span.Status().Description != "" {
		msg := truncateString(span.Status().Description, 256)
		statusMsg = &msg
	}

	var paymentID *uuid.UUID

	// Extract and sanitize attributes (max 20 attributes, max 256 byte strings, sensitive keys redacted)
	attrs := make(map[string]any)
	rawAttrs := span.Attributes()
	count := 0

	for _, kv := range rawAttrs {
		if count >= 20 {
			break
		}
		key := string(kv.Key)
		lowerKey := strings.ToLower(key)

		// Check sensitive blacklist
		if isSensitiveAttribute(lowerKey) {
			continue
		}

		val := kv.Value.AsInterface()
		if s, ok := val.(string); ok {
			s = truncateString(s, 256)
			val = s

			// Extract payment_id if found
			if (lowerKey == "payment_id" || lowerKey == "paymentid") && paymentID == nil {
				if parsed, err := uuid.Parse(s); err == nil {
					paymentID = &parsed
				}
			}
		}

		attrs[key] = val
		count++
	}

	// Sanitize links
	var links []any
	for _, l := range span.Links() {
		lsc := l.SpanContext
		if lsc.IsValid() {
			lMap := map[string]any{
				"traceId": lsc.TraceID().String(),
				"spanId":  lsc.SpanID().String(),
			}
			links = append(links, lMap)
		}
	}
	if links == nil {
		links = []any{}
	}

	// Sanitize events
	var events []any
	for _, ev := range span.Events() {
		evMap := map[string]any{
			"name":      truncateString(ev.Name, 128),
			"timestamp": ev.Time.UTC().Format(time.RFC3339Nano),
		}
		events = append(events, evMap)
	}
	if events == nil {
		events = []any{}
	}

	serviceName := e.cfg.ServiceName
	if serviceName == "" {
		serviceName = "transactx"
	}

	return RawSpanRecord{
		SpanID:        spanID,
		TraceID:       traceID,
		ParentSpanID:  parentSpanID,
		ServiceName:   serviceName,
		SpanName:      span.Name(),
		SpanKind:      span.SpanKind().String(),
		StartTime:     span.StartTime().UTC(),
		EndTime:       span.EndTime().UTC(),
		DurationMs:    duration,
		StatusCode:    span.Status().Code.String(),
		StatusMessage: statusMsg,
		Attributes:    attrs,
		Events:        events,
		Links:         links,
		PaymentID:     paymentID,
	}
}

func isSensitiveAttribute(key string) bool {
	sensitiveSubstrings := []string{
		"auth", "token", "password", "secret", "jwt",
		"cookie", "idempotency", "prompt", "url", "note",
		"customer_name", "customername", "identifier",
	}
	for _, sub := range sensitiveSubstrings {
		if strings.Contains(key, sub) {
			return true
		}
	}
	return false
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
