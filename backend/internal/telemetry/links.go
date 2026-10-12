package telemetry

import (
	"context"
	"encoding/hex"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// LinkFromTraceAndSpanID creates a trace.Link from hex trace and span ID strings.
// If the IDs are malformed or invalid, an empty link is returned with ok=false.
func LinkFromTraceAndSpanID(traceIDHex, spanIDHex string) (trace.Link, bool) {
	if len(traceIDHex) != 32 || len(spanIDHex) != 16 {
		return trace.Link{}, false
	}

	var traceID trace.TraceID
	tBytes, err := hex.DecodeString(traceIDHex)
	if err != nil || len(tBytes) != 16 {
		return trace.Link{}, false
	}
	copy(traceID[:], tBytes)

	var spanID trace.SpanID
	sBytes, err := hex.DecodeString(spanIDHex)
	if err != nil || len(sBytes) != 8 {
		return trace.Link{}, false
	}
	copy(spanID[:], sBytes)

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})

	return trace.Link{SpanContext: sc}, true
}

// StartRecoverySpan starts a new root span for an asynchronous recovery attempt,
// linking to the original trace ID and span ID if provided.
// This ensures that the recovery attempt has its own independent trace while preserving
// causal correlation via OpenTelemetry span links without manufacturing a synthetic parent across restarts.
func StartRecoverySpan(
	ctx context.Context,
	tracer trace.Tracer,
	spanName string,
	origTraceIDHex, origSpanIDHex string,
	attrs ...attribute.KeyValue,
) (context.Context, trace.Span) {
	opts := []trace.SpanStartOption{
		trace.WithNewRoot(),
		trace.WithAttributes(attrs...),
		trace.WithSpanKind(trace.SpanKindConsumer),
	}

	if origTraceIDHex != "" && origSpanIDHex != "" {
		if link, ok := LinkFromTraceAndSpanID(origTraceIDHex, origSpanIDHex); ok {
			opts = append(opts, trace.WithLinks(link))
		}
	}

	return tracer.Start(ctx, spanName, opts...)
}
