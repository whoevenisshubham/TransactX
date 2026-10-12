-- Migration 000028: Operational windows, anomaly versions/scores and aggregate job cursor

CREATE TABLE IF NOT EXISTS operational_aggregate_windows (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type VARCHAR(32) NOT NULL CHECK (entity_type IN ('bank', 'execution_target')),
    entity_id VARCHAR(255) NOT NULL,
    window_from TIMESTAMPTZ NOT NULL,
    window_to TIMESTAMPTZ NOT NULL,
    sample_count INT NOT NULL DEFAULT 0 CHECK (sample_count >= 0),
    feature_schema_version VARCHAR(64) NOT NULL DEFAULT 'tx.features.v1',
    features JSONB NOT NULL DEFAULT '{}'::jsonb,
    baseline_alert JSONB,
    model_version VARCHAR(255),
    model_status VARCHAR(64) NOT NULL DEFAULT 'unavailable' CHECK (model_status IN ('ready', 'insufficient_data', 'stale', 'unavailable', 'out_of_distribution')),
    model_score DOUBLE PRECISION,
    model_threshold DOUBLE PRECISION,
    advisory BOOLEAN NOT NULL DEFAULT true,
    raw_aggregates JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_ops_agg_window UNIQUE (entity_type, entity_id, window_from, feature_schema_version),
    CONSTRAINT chk_ops_agg_window_range CHECK (window_to > window_from)
);

CREATE INDEX IF NOT EXISTS idx_ops_agg_windows_entity_time ON operational_aggregate_windows (entity_type, entity_id, window_from DESC);
CREATE INDEX IF NOT EXISTS idx_ops_agg_windows_time ON operational_aggregate_windows (window_from DESC);

CREATE TABLE IF NOT EXISTS operational_aggregate_cursors (
    cursor_name VARCHAR(64) PRIMARY KEY,
    last_window_to TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
