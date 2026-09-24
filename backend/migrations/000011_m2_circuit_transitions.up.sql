CREATE TABLE circuit_transition_events (
    id BIGSERIAL PRIMARY KEY,
    execution_target_id VARCHAR(120) NOT NULL,
    previous_state VARCHAR(32) NOT NULL CHECK (previous_state IN ('CLOSED', 'OPEN', 'HALF_OPEN')),
    new_state VARCHAR(32) NOT NULL CHECK (new_state IN ('CLOSED', 'OPEN', 'HALF_OPEN')),
    reason VARCHAR(128) NOT NULL,
    failure_count INT NOT NULL DEFAULT 0,
    timeout_count INT NOT NULL DEFAULT 0,
    consecutive_successes INT NOT NULL DEFAULT 0,
    active_probes INT NOT NULL DEFAULT 0,
    successful_probes INT NOT NULL DEFAULT 0,
    restoration_step INT NOT NULL DEFAULT 0,
    cooldown_duration_ms BIGINT NOT NULL DEFAULT 0,
    rolling_window_ms BIGINT NOT NULL DEFAULT 0,
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    transitioned_at TIMESTAMPTZ NOT NULL,
    event_type VARCHAR(64) NOT NULL CHECK (event_type = 'CIRCUIT_STATE_TRANSITION')
);

CREATE INDEX circuit_transition_events_target_idx ON circuit_transition_events (execution_target_id, transitioned_at DESC, id DESC);
