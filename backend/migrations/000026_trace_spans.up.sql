-- Migration: 000026_trace_spans.up.sql
-- Description: OpenTelemetry bounded trace span storage for operational telemetry

CREATE TABLE IF NOT EXISTS trace_spans (
    span_id VARCHAR(16) NOT NULL,
    trace_id VARCHAR(32) NOT NULL,
    parent_span_id VARCHAR(16),
    service_name VARCHAR(64) NOT NULL,
    span_name VARCHAR(128) NOT NULL,
    span_kind VARCHAR(32) NOT NULL DEFAULT 'INTERNAL',
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ NOT NULL,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    status_code VARCHAR(32) NOT NULL DEFAULT 'UNSET',
    status_message TEXT,
    attributes JSONB NOT NULL DEFAULT '{}'::jsonb,
    events JSONB NOT NULL DEFAULT '[]'::jsonb,
    links JSONB NOT NULL DEFAULT '[]'::jsonb,
    payment_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (trace_id, span_id)
);

CREATE INDEX IF NOT EXISTS idx_trace_spans_trace_id ON trace_spans (trace_id);
CREATE INDEX IF NOT EXISTS idx_trace_spans_payment_id ON trace_spans (payment_id) WHERE payment_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_trace_spans_created_at ON trace_spans (created_at);
