-- Migration 000024 down: Revert transactional outbox events

DROP TRIGGER IF EXISTS trg_recovery_jobs_outbox ON recovery_jobs;
DROP FUNCTION IF EXISTS trg_recovery_jobs_outbox_event;

DROP TRIGGER IF EXISTS trg_recovery_attempts_outbox ON recovery_attempts;
DROP FUNCTION IF EXISTS trg_recovery_attempts_outbox_event;

DROP TRIGGER IF EXISTS trg_payments_outbox ON payments;
DROP FUNCTION IF EXISTS trg_payments_outbox_event;

DROP TRIGGER IF EXISTS trg_deny_outbox_event_update ON outbox_events;
DROP FUNCTION IF EXISTS deny_outbox_event_update;

DROP TABLE IF EXISTS outbox_deliveries CASCADE;
DROP TABLE IF EXISTS outbox_events CASCADE;

ALTER TABLE payments DROP COLUMN IF EXISTS event_version;
