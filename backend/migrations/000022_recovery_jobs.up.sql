CREATE TABLE recovery_jobs (
    payment_id UUID PRIMARY KEY REFERENCES payments(id) ON DELETE CASCADE,
    status VARCHAR(32) NOT NULL CHECK (status IN ('scheduled', 'running', 'deferred', 'needs_attention', 'resolved')),
    due_at TIMESTAMPTZ NOT NULL,
    lease_owner VARCHAR(128),
    lease_until TIMESTAMPTZ,
    lease_epoch BIGINT NOT NULL DEFAULT 0,
    attempt_count INT NOT NULL DEFAULT 0,
    last_reason_code VARCHAR(64),
    last_attempt_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX recovery_jobs_due_idx
    ON recovery_jobs (due_at, payment_id)
    WHERE status != 'resolved';

CREATE TABLE recovery_attempts (
    id UUID PRIMARY KEY,
    payment_id UUID NOT NULL REFERENCES payments(id) ON DELETE CASCADE,
    lease_epoch BIGINT NOT NULL,
    worker_identity VARCHAR(128) NOT NULL,
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    state_before VARCHAR(64) NOT NULL,
    state_after VARCHAR(64),
    result VARCHAR(64),
    reason_code VARCHAR(64),
    operation_ids UUID[],
    trace_id VARCHAR(64),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT recovery_attempts_payment_epoch_uniq UNIQUE (payment_id, lease_epoch)
);

CREATE INDEX recovery_attempts_payment_idx
    ON recovery_attempts (payment_id, started_at DESC);

CREATE TABLE worker_heartbeats (
    worker_id UUID PRIMARY KEY,
    worker_label VARCHAR(128) NOT NULL,
    started_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    job_families TEXT[] NOT NULL DEFAULT '{}',
    build_sha VARCHAR(64) NOT NULL
);

CREATE INDEX worker_heartbeats_last_seen_idx
    ON worker_heartbeats (last_seen_at DESC);
