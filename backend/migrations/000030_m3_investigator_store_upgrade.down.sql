-- Migration 000030 down: Rollback M3 investigator store upgrade

ALTER TABLE investigation_jobs DROP COLUMN IF EXISTS provider_call_pending;
ALTER TABLE investigation_usage_limits DROP COLUMN IF EXISTS cost_limit_microusd;
ALTER TABLE investigation_usage_reservations DROP COLUMN IF EXISTS reserved_cost_microusd;
ALTER TABLE investigation_usage_reservations DROP COLUMN IF EXISTS actual_cost_microusd;
DROP INDEX IF EXISTS idx_investigation_usage_job_uniq;
