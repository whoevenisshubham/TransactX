-- Migration 000024: Transactional outbox events and delivery state
-- Adds payment event versioning, outbox storage, delivery status, and atomic triggers.

-- 1. Payment event version
ALTER TABLE payments
    ADD COLUMN event_version BIGINT NOT NULL DEFAULT 0 CHECK (event_version >= 0);

-- 2. Outbox events table
CREATE TABLE outbox_events (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type        VARCHAR(120) NOT NULL,
    schema_version    INT NOT NULL DEFAULT 1 CHECK (schema_version > 0),
    aggregate_type    VARCHAR(64) NOT NULL,
    aggregate_id      UUID NOT NULL,
    aggregate_version BIGINT NOT NULL CHECK (aggregate_version >= 0),
    occurred_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    payload           JSONB NOT NULL,
    traceparent       VARCHAR(120) NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Unique aggregate version constraint for payments
CREATE UNIQUE INDEX outbox_events_payment_version_idx
    ON outbox_events (aggregate_type, aggregate_id, aggregate_version)
    WHERE aggregate_type = 'payment';

-- Query and retention indexes
CREATE INDEX outbox_events_created_at_idx
    ON outbox_events (created_at);

CREATE INDEX outbox_events_aggregate_idx
    ON outbox_events (aggregate_type, aggregate_id, created_at);

-- Immutability guard: deny updates to event records
CREATE OR REPLACE FUNCTION deny_outbox_event_update()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'outbox_events records are immutable and cannot be updated';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_deny_outbox_event_update
    BEFORE UPDATE ON outbox_events
    FOR EACH ROW
    EXECUTE FUNCTION deny_outbox_event_update();

-- 3. Outbox deliveries table
CREATE TABLE outbox_deliveries (
    consumer_name   VARCHAR(120) NOT NULL,
    event_id        UUID NOT NULL REFERENCES outbox_events(id) ON DELETE CASCADE,
    status          VARCHAR(32) NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'processing', 'delivered', 'quarantined')),
    attempts        INT NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_owner     VARCHAR(120) NULL,
    lease_until     TIMESTAMPTZ NULL,
    lease_epoch     BIGINT NOT NULL DEFAULT 0 CHECK (lease_epoch >= 0),
    delivered_at    TIMESTAMPTZ NULL,
    last_error_code VARCHAR(64) NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (consumer_name, event_id)
);

CREATE INDEX outbox_deliveries_pending_idx
    ON outbox_deliveries (consumer_name, status, next_attempt_at)
    WHERE status IN ('pending', 'processing');

CREATE INDEX outbox_deliveries_event_id_idx
    ON outbox_deliveries (event_id);

-- 4. Atomic producer trigger for payments
CREATE OR REPLACE FUNCTION trg_payments_outbox_event()
RETURNS TRIGGER AS $$
DECLARE
    v_event_id UUID;
    v_event_type VARCHAR(120);
    v_prev_state VARCHAR(64);
    v_new_version BIGINT;
    v_payload JSONB;
    v_occurred_at TIMESTAMPTZ;
BEGIN
    IF TG_OP = 'INSERT' THEN
        v_event_type := 'payment.created.v1';
        v_prev_state := NULL;
        v_new_version := 1;
        NEW.event_version := 1;
        v_occurred_at := COALESCE(NEW.created_at, NOW());
    ELSIF TG_OP = 'UPDATE' THEN
        -- If state did not change, emit nothing
        IF OLD.state IS NOT DISTINCT FROM NEW.state THEN
            RETURN NEW;
        END IF;

        v_event_type := 'payment.state_changed.v1';
        v_prev_state := OLD.state;
        v_new_version := COALESCE(OLD.event_version, 0) + 1;
        NEW.event_version := v_new_version;
        v_occurred_at := COALESCE(NEW.updated_at, NOW());
    ELSE
        RETURN NEW;
    END IF;

    v_event_id := gen_random_uuid();
    v_payload := jsonb_build_object(
        'paymentId', NEW.id,
        'previousState', v_prev_state,
        'newState', NEW.state,
        'eventVersion', v_new_version,
        'sourceBankId', NEW.source_bank_id,
        'destinationBankId', NEW.destination_bank_id,
        'routeBankId', NEW.route_bank_id,
        'amountPaise', NEW.amount_paise,
        'currency', NEW.currency,
        'failureReason', NEW.failure_reason,
        'createdAt', NEW.created_at,
        'updatedAt', NEW.updated_at
    );

    INSERT INTO outbox_events (
        id,
        event_type,
        schema_version,
        aggregate_type,
        aggregate_id,
        aggregate_version,
        occurred_at,
        payload,
        created_at
    ) VALUES (
        v_event_id,
        v_event_type,
        1,
        'payment',
        NEW.id,
        v_new_version,
        v_occurred_at,
        v_payload,
        NOW()
    );

    INSERT INTO outbox_deliveries (
        consumer_name,
        event_id,
        status,
        next_attempt_at,
        created_at
    ) VALUES (
        'ops-worklist-v1',
        v_event_id,
        'pending',
        NOW(),
        NOW()
    );

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_payments_outbox
    BEFORE INSERT OR UPDATE ON payments
    FOR EACH ROW
    EXECUTE FUNCTION trg_payments_outbox_event();

-- 5. Atomic producer trigger for recovery attempts
CREATE OR REPLACE FUNCTION trg_recovery_attempts_outbox_event()
RETURNS TRIGGER AS $$
DECLARE
    v_event_id UUID;
    v_payload JSONB;
BEGIN
    IF TG_OP = 'INSERT' THEN
        v_event_id := gen_random_uuid();
        v_payload := jsonb_build_object(
            'attemptId', NEW.id,
            'paymentId', NEW.payment_id,
            'leaseEpoch', NEW.lease_epoch,
            'workerIdentity', NEW.worker_identity,
            'stateBefore', NEW.state_before,
            'startedAt', NEW.started_at
        );

        INSERT INTO outbox_events (
            id,
            event_type,
            schema_version,
            aggregate_type,
            aggregate_id,
            aggregate_version,
            occurred_at,
            payload,
            created_at
        ) VALUES (
            v_event_id,
            'recovery.attempt_started.v1',
            1,
            'recovery',
            NEW.payment_id,
            NEW.lease_epoch,
            COALESCE(NEW.started_at, NOW()),
            v_payload,
            NOW()
        );

        INSERT INTO outbox_deliveries (
            consumer_name,
            event_id,
            status,
            next_attempt_at,
            created_at
        ) VALUES (
            'ops-worklist-v1',
            v_event_id,
            'pending',
            NOW(),
            NOW()
        );

        RETURN NEW;
    ELSIF TG_OP = 'UPDATE' THEN
        IF OLD.finished_at IS NULL AND NEW.finished_at IS NOT NULL THEN
            v_event_id := gen_random_uuid();
            v_payload := jsonb_build_object(
                'attemptId', NEW.id,
                'paymentId', NEW.payment_id,
                'leaseEpoch', NEW.lease_epoch,
                'workerIdentity', NEW.worker_identity,
                'stateBefore', NEW.state_before,
                'stateAfter', NEW.state_after,
                'result', NEW.result,
                'reasonCode', NEW.reason_code,
                'finishedAt', NEW.finished_at
            );

            INSERT INTO outbox_events (
                id,
                event_type,
                schema_version,
                aggregate_type,
                aggregate_id,
                aggregate_version,
                occurred_at,
                payload,
                created_at
            ) VALUES (
                v_event_id,
                'recovery.attempt_finished.v1',
                1,
                'recovery',
                NEW.payment_id,
                NEW.lease_epoch,
                NEW.finished_at,
                v_payload,
                NOW()
            );

            INSERT INTO outbox_deliveries (
                consumer_name,
                event_id,
                status,
                next_attempt_at,
                created_at
            ) VALUES (
                'ops-worklist-v1',
                v_event_id,
                'pending',
                NOW(),
                NOW()
            );
        END IF;

        RETURN NEW;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_recovery_attempts_outbox
    AFTER INSERT OR UPDATE ON recovery_attempts
    FOR EACH ROW
    EXECUTE FUNCTION trg_recovery_attempts_outbox_event();

-- 6. Atomic producer trigger for recovery jobs (attention required)
CREATE OR REPLACE FUNCTION trg_recovery_jobs_outbox_event()
RETURNS TRIGGER AS $$
DECLARE
    v_event_id UUID;
    v_payload JSONB;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF OLD.status IS DISTINCT FROM NEW.status AND NEW.status = 'needs_attention' THEN
            v_event_id := gen_random_uuid();
            v_payload := jsonb_build_object(
                'paymentId', NEW.payment_id,
                'status', NEW.status,
                'attemptCount', NEW.attempt_count,
                'lastReasonCode', NEW.last_reason_code,
                'lastAttemptId', NEW.last_attempt_id,
                'updatedAt', NEW.updated_at
            );

            INSERT INTO outbox_events (
                id,
                event_type,
                schema_version,
                aggregate_type,
                aggregate_id,
                aggregate_version,
                occurred_at,
                payload,
                created_at
            ) VALUES (
                v_event_id,
                'recovery.attention_required.v1',
                1,
                'recovery',
                NEW.payment_id,
                NEW.lease_epoch,
                COALESCE(NEW.updated_at, NOW()),
                v_payload,
                NOW()
            );

            INSERT INTO outbox_deliveries (
                consumer_name,
                event_id,
                status,
                next_attempt_at,
                created_at
            ) VALUES (
                'ops-worklist-v1',
                v_event_id,
                'pending',
                NOW(),
                NOW()
            );
        END IF;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_recovery_jobs_outbox
    AFTER UPDATE ON recovery_jobs
    FOR EACH ROW
    EXECUTE FUNCTION trg_recovery_jobs_outbox_event();
