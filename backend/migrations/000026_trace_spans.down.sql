-- Migration: 000026_trace_spans.down.sql
-- Description: Rollback OpenTelemetry bounded trace span storage

DROP TABLE IF EXISTS trace_spans;
