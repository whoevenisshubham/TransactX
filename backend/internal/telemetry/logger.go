package telemetry

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// TraceCorrelatingHandler decorates any slog.Handler by injecting trace_id and span_id
// from the context into structured log entries.
type TraceCorrelatingHandler struct {
	inner slog.Handler
}

// NewTraceCorrelatingHandler wraps a slog.Handler to correlate logs with active OpenTelemetry traces.
func NewTraceCorrelatingHandler(inner slog.Handler) slog.Handler {
	return &TraceCorrelatingHandler{inner: inner}
}

func (h *TraceCorrelatingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *TraceCorrelatingHandler) Handle(ctx context.Context, r slog.Record) error {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		sc := span.SpanContext()
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.inner.Handle(ctx, r)
}

func (h *TraceCorrelatingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TraceCorrelatingHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *TraceCorrelatingHandler) WithGroup(name string) slog.Handler {
	return &TraceCorrelatingHandler{inner: h.inner.WithGroup(name)}
}
