package telemetry

import (
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

type statusCapturingResponseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (w *statusCapturingResponseWriter) WriteHeader(code int) {
	if !w.written {
		w.statusCode = code
		w.written = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusCapturingResponseWriter) Write(b []byte) (int, error) {
	if !w.written {
		w.statusCode = http.StatusOK
		w.written = true
	}
	return w.ResponseWriter.Write(b)
}

// HTTPMiddleware instruments incoming HTTP server requests with W3C propagation,
// span tracking, response header correlation, and route metrics.
func HTTPMiddleware(tracer trace.Tracer, metrics *Metrics) func(http.Handler) http.Handler {
	propagator := otel.GetTextMapPropagator()
	if propagator == nil {
		propagator = propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Extract W3C trace context from request headers
			ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			spanName := fmt.Sprintf("%s %s", r.Method, r.URL.Path)
			ctx, span := tracer.Start(ctx, spanName,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					semconv.HTTPRequestMethodKey.String(r.Method),
					semconv.URLPathKey.String(r.URL.Path),
					semconv.UserAgentOriginalKey.String(r.UserAgent()),
				),
			)
			defer span.End()

			sc := span.SpanContext()
			if sc.IsValid() {
				w.Header().Set("X-Trace-ID", sc.TraceID().String())
			}

			sw := &statusCapturingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(sw, r.WithContext(ctx))

			duration := time.Since(start)

			span.SetAttributes(semconv.HTTPResponseStatusCodeKey.Int(sw.statusCode))
			if sw.statusCode >= 500 {
				span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", sw.statusCode))
			} else {
				span.SetStatus(codes.Ok, "")
			}

			if metrics != nil {
				// Use path as route template baseline
				metrics.RecordHTTPRequest(r.Method, r.URL.Path, sw.statusCode, duration)
			}
		})
	}
}

// ClientTransport wraps an http.RoundTripper with W3C trace context injection and CLIENT spans.
type ClientTransport struct {
	base   http.RoundTripper
	tracer trace.Tracer
}

// NewClientTransport creates a transport that injects W3C headers into outbound HTTP calls.
func NewClientTransport(base http.RoundTripper, tracer trace.Tracer) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &ClientTransport{
		base:   base,
		tracer: tracer,
	}
}

func (t *ClientTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	propagator := otel.GetTextMapPropagator()
	if propagator == nil {
		propagator = propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	}

	spanName := fmt.Sprintf("HTTP %s %s", req.Method, req.URL.Host)
	ctx, span := t.tracer.Start(req.Context(), spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			semconv.HTTPRequestMethodKey.String(req.Method),
			semconv.ServerAddressKey.String(req.URL.Host),
			semconv.URLPathKey.String(req.URL.Path),
		),
	)
	defer span.End()

	// Clone request to avoid mutating original headers concurrently
	clonedReq := req.Clone(ctx)
	propagator.Inject(ctx, propagation.HeaderCarrier(clonedReq.Header))

	resp, err := t.base.RoundTrip(clonedReq)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(semconv.HTTPResponseStatusCodeKey.Int(resp.StatusCode))
	if resp.StatusCode >= 400 {
		span.SetStatus(codes.Error, resp.Status)
	} else {
		span.SetStatus(codes.Ok, "")
	}

	return resp, nil
}
