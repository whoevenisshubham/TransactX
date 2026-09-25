-- M3-7: Runtime financial integrity engine runs and structured check results.

CREATE TABLE integrity_runs (
    id                UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    scope_from        TIMESTAMPTZ  NULL,
    scope_to          TIMESTAMPTZ  NULL,
    participant_id    VARCHAR(120) NULL,
    status            VARCHAR(32)  NOT NULL DEFAULT 'RUNNING'
                          CHECK (status IN ('RUNNING', 'COMPLETED', 'FAILED')),
    total_checks      INT          NOT NULL DEFAULT 0,
    passed_checks     INT          NOT NULL DEFAULT 0,
    failed_checks     INT          NOT NULL DEFAULT 0,
    error_checks      INT          NOT NULL DEFAULT 0,
    na_checks         INT          NOT NULL DEFAULT 0,
    error_message     TEXT         NULL,
    started_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    completed_at      TIMESTAMPTZ  NULL
);

CREATE INDEX integrity_runs_started_idx
    ON integrity_runs (started_at DESC, id DESC);

CREATE TABLE integrity_check_results (
    id                UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id            UUID         NOT NULL REFERENCES integrity_runs(id) ON DELETE CASCADE,
    check_code        VARCHAR(64)  NOT NULL,
    severity          VARCHAR(32)  NOT NULL,
    status            VARCHAR(32)  NOT NULL
                          CHECK (status IN ('PASS', 'FAIL', 'ERROR', 'NOT_APPLICABLE')),
    message           TEXT         NOT NULL,
    observed          TEXT         NULL,
    error_text        TEXT         NULL,
    started_at        TIMESTAMPTZ  NOT NULL,
    completed_at      TIMESTAMPTZ  NOT NULL
);

CREATE INDEX integrity_check_results_run_id_idx
    ON integrity_check_results (run_id, started_at ASC);
