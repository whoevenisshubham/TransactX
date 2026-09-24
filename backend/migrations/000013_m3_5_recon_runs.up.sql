-- M3-5: Reconciliation runs and discrepancy evidence.
-- A run compares participant-side state against the canonical central model.
-- A completed run with discrepancies is COMPLETED (not FAILED).
-- FAILED is reserved for operational execution failures.

CREATE TABLE recon_runs (
    id                UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    participant_id    VARCHAR(120) NOT NULL,
    scope_from        TIMESTAMPTZ  NOT NULL,
    scope_to          TIMESTAMPTZ  NOT NULL,
    status            VARCHAR(32)  NOT NULL DEFAULT 'RUNNING'
                          CHECK (status IN ('RUNNING', 'COMPLETED', 'FAILED')),
    canonical_root    BYTEA        NULL,
    participant_root  BYTEA        NULL,
    canonical_version VARCHAR(32)  NULL,
    algorithm_version VARCHAR(32)  NULL,
    record_count      BIGINT       NOT NULL DEFAULT 0,
    discrepancy_count BIGINT       NOT NULL DEFAULT 0,
    error_message     TEXT         NULL,
    started_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    completed_at      TIMESTAMPTZ  NULL
);

CREATE INDEX recon_runs_participant_started_idx
    ON recon_runs (participant_id, started_at DESC);

CREATE INDEX recon_runs_status_started_idx
    ON recon_runs (status, started_at DESC);

-- Discrepancy evidence: one row per mismatching bucket/region.
CREATE TABLE recon_discrepancies (
    id                UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id            UUID         NOT NULL REFERENCES recon_runs(id) ON DELETE CASCADE,
    participant_id    VARCHAR(120) NOT NULL,
    bucket_key        TEXT         NOT NULL,
    bucket_partition  VARCHAR(120) NOT NULL,
    bucket_start      TIMESTAMPTZ  NOT NULL,
    bucket_width_ns   BIGINT       NOT NULL,
    expected_root     BYTEA        NULL,
    observed_root     BYTEA        NULL,
    mismatch_category VARCHAR(64)  NOT NULL
                          CHECK (mismatch_category IN (
                              'BUCKET_ROOT_MISMATCH',
                              'CANONICAL_ROOT_MISMATCH',
                              'PARTICIPANT_UNAVAILABLE',
                              'SCOPE_MISMATCH',
                              'VERSION_INCOMPATIBLE'
                          )),
    resolved          BOOLEAN      NOT NULL DEFAULT false,
    evidence          JSONB        NOT NULL DEFAULT '{}'::jsonb,
    detected_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX recon_disc_run_id_idx
    ON recon_discrepancies (run_id, detected_at DESC);

CREATE INDEX recon_disc_participant_resolved_idx
    ON recon_discrepancies (participant_id, resolved, detected_at DESC);
