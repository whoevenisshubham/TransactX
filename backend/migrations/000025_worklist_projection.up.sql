-- Migration 000025: Worklist projection and consumer inbox
-- Adds consumer_inbox for idempotent event processing and ops_payment_worklist derived projection.

-- 1. Consumer Inbox table
CREATE TABLE consumer_inbox (
    consumer_name   VARCHAR(120) NOT NULL,
    event_id        UUID NOT NULL,
    processed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    handler_version INT NOT NULL DEFAULT 1,
    PRIMARY KEY (consumer_name, event_id)
);

CREATE INDEX consumer_inbox_processed_at_idx
    ON consumer_inbox (consumer_name, processed_at);

-- 2. Operational Payment Worklist (Derived projection)
CREATE TABLE ops_payment_worklist (
    payment_id           UUID PRIMARY KEY,
    current_state        VARCHAR(64) NOT NULL,
    event_version        BIGINT NOT NULL CHECK (event_version >= 0),
    amount_paise         BIGINT NOT NULL,
    currency             VARCHAR(16) NOT NULL,
    source_bank_id       UUID NULL,
    destination_bank_id  UUID NULL,
    route_bank_id        UUID NULL,
    created_at           TIMESTAMPTZ NOT NULL,
    updated_at           TIMESTAMPTZ NOT NULL,
    recovery_status      VARCHAR(64) NULL,
    recovery_attempts    INT NOT NULL DEFAULT 0 CHECK (recovery_attempts >= 0),
    last_recovery_reason VARCHAR(64) NULL,
    last_recovery_at     TIMESTAMPTZ NULL,
    projected_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX ops_payment_worklist_state_updated_idx
    ON ops_payment_worklist (current_state, updated_at DESC);

CREATE INDEX ops_payment_worklist_recovery_status_idx
    ON ops_payment_worklist (recovery_status, updated_at DESC);

CREATE INDEX ops_payment_worklist_event_version_idx
    ON ops_payment_worklist (event_version);
