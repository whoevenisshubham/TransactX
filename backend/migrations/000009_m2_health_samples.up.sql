CREATE TABLE health_samples (
    id BIGSERIAL PRIMARY KEY,
    target_id VARCHAR(120) NOT NULL,
    sampled_at TIMESTAMPTZ NOT NULL,
    available BOOLEAN NOT NULL,
    latency_ms BIGINT NOT NULL CHECK (latency_ms >= 0),
    outcome VARCHAR(16) NOT NULL CHECK (outcome IN ('SUCCESS', 'FAILURE', 'TIMEOUT')),
    correlation_id VARCHAR(128),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX health_samples_target_sampled_at_idx ON health_samples (target_id, sampled_at DESC, id DESC);
CREATE INDEX health_samples_sampled_at_idx ON health_samples (sampled_at DESC);