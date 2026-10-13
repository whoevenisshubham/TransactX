package telemetry

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/sdk/resource"
)

// Provider bundles the OpenTelemetry tracer, metrics, exporter, and store.
type Provider struct {
	cfg        Config
	tracer     trace.Tracer
	tp         *sdktrace.TracerProvider
	exporter   *BoundedPostgresExporter
	store      *TraceStore
	metrics    *Metrics
	logger     *slog.Logger
}

// InitProvider sets up OpenTelemetry globally, including W3C propagation and bounded PostgreSQL export.
func InitProvider(ctx context.Context, cfg Config, pool *pgxpool.Pool, logger *slog.Logger) (*Provider, func(context.Context) error, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// 1. Configure W3C trace context propagator globally
	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	otel.SetTextMapPropagator(propagator)

	// 2. Build OpenTelemetry resource
	serviceName := cfg.ServiceName
	if serviceName == "" {
		serviceName = "transactx"
	}
	buildSHA := cfg.BuildSHA
	if buildSHA == "" {
		buildSHA = "dev"
	}
	env := cfg.Environment
	if env == "" {
		env = "development"
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String(buildSHA),
			semconv.DeploymentEnvironmentKey.String(env),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create otel resource: %w", err)
	}

	// 3. Configure sampling
	var sampler sdktrace.Sampler
	if cfg.SampleRatio >= 1.0 {
		sampler = sdktrace.AlwaysSample()
	} else if cfg.SampleRatio <= 0.0 {
		sampler = sdktrace.NeverSample()
	} else {
		sampler = sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))
	}

	metrics := NewMetrics()

	// 4. Initialize bounded PostgreSQL exporter if enabled and pool is available
	var exporter *BoundedPostgresExporter
	var tpOpts []sdktrace.TracerProviderOption
	tpOpts = append(tpOpts, sdktrace.WithResource(res), sdktrace.WithSampler(sampler))

	if cfg.StorageEnabled && pool != nil {
		exporter = NewBoundedPostgresExporter(pool, cfg, metrics, logger)
		// Register with SimpleSpanProcessor so spans flow to our non-blocking queue immediately
		tpOpts = append(tpOpts, sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter)))
	}

	tp := sdktrace.NewTracerProvider(tpOpts...)
	otel.SetTracerProvider(tp)

	tracer := tp.Tracer(serviceName)
	store := NewTraceStore(pool, logger)

	provider := &Provider{
		cfg:      cfg,
		tracer:   tracer,
		tp:       tp,
		exporter: exporter,
		store:    store,
		metrics:  metrics,
		logger:   logger,
	}

	shutdown := func(shutdownCtx context.Context) error {
		var firstErr error
		if exporter != nil {
			if err := exporter.Shutdown(shutdownCtx); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if err := tp.Shutdown(shutdownCtx); err != nil && firstErr == nil {
			firstErr = err
		}
		return firstErr
	}

	return provider, shutdown, nil
}

// Tracer returns the named OpenTelemetry tracer.
func (p *Provider) Tracer() trace.Tracer {
	return p.tracer
}

// Exporter returns the bounded PostgreSQL exporter.
func (p *Provider) Exporter() *BoundedPostgresExporter {
	return p.exporter
}

// Store returns the operational trace query store.
func (p *Provider) Store() *TraceStore {
	return p.store
}

// Metrics returns the in-memory operational metrics collector.
func (p *Provider) Metrics() *Metrics {
	return p.metrics
}

// ForceFlush flushes all pending spans to PostgreSQL synchronously.
func (p *Provider) ForceFlush(ctx context.Context) error {
	if p.exporter != nil {
		return p.exporter.Flush(ctx)
	}
	return nil
}
