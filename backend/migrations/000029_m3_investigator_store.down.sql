-- Migration 000029 down: Rollback M3 investigator store additions

DROP TABLE IF EXISTS runbook_chunks CASCADE;
DROP TABLE IF EXISTS corpus_versions CASCADE;
DROP TABLE IF EXISTS investigation_usage_limits CASCADE;

DROP INDEX IF EXISTS idx_investigation_jobs_active;

ALTER TABLE investigation_jobs DROP COLUMN IF EXISTS role;
ALTER TABLE investigation_jobs DROP COLUMN IF EXISTS request_hash;
ALTER TABLE investigation_jobs DROP COLUMN IF EXISTS deadline_at;
ALTER TABLE investigation_jobs DROP COLUMN IF EXISTS job_payload;

ALTER TABLE investigation_usage_reservations DROP COLUMN IF EXISTS reserved_input;
ALTER TABLE investigation_usage_reservations DROP COLUMN IF EXISTS reserved_output;
ALTER TABLE investigation_usage_reservations DROP COLUMN IF EXISTS actual_input;
ALTER TABLE investigation_usage_reservations DROP COLUMN IF EXISTS actual_output;
