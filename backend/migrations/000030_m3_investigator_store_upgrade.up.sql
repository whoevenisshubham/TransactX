-- Migration 000030: Additive upgrade for M3 durable store, cost budgets, and legacy payload backfill

-- 1. Usage limits: cost budget limit
ALTER TABLE investigation_usage_limits ADD COLUMN IF NOT EXISTS cost_limit_microusd BIGINT NOT NULL DEFAULT 0;

-- 2. Investigation jobs: provider call pending fence
ALTER TABLE investigation_jobs ADD COLUMN IF NOT EXISTS provider_call_pending BOOLEAN NOT NULL DEFAULT FALSE;

-- 3. Usage reservations: decoupled micro-USD cost fields and nullable legacy dimensions
ALTER TABLE investigation_usage_reservations ADD COLUMN IF NOT EXISTS reserved_cost_microusd BIGINT NOT NULL DEFAULT 0;
ALTER TABLE investigation_usage_reservations ADD COLUMN IF NOT EXISTS actual_cost_microusd BIGINT;

ALTER TABLE investigation_usage_reservations ALTER COLUMN actor_id DROP NOT NULL;
ALTER TABLE investigation_usage_reservations ALTER COLUMN provider DROP NOT NULL;
ALTER TABLE investigation_usage_reservations ALTER COLUMN model DROP NOT NULL;
ALTER TABLE investigation_usage_reservations ALTER COLUMN reserved_tokens DROP NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_investigation_usage_job_uniq ON investigation_usage_reservations (job_id);

-- 4. Preserve historical usage accounting: map legacy single-token columns to decoupled input dimensions
UPDATE investigation_usage_reservations
SET reserved_input = reserved_tokens
WHERE reserved_tokens IS NOT NULL AND reserved_input = 0;

UPDATE investigation_usage_reservations
SET actual_input = actual_tokens
WHERE actual_tokens IS NOT NULL AND actual_input IS NULL;

-- 5. Safe legacy job handling:
-- Active (queued/running) legacy jobs with missing request identity or payload must NOT be replayed
UPDATE investigation_jobs
SET status = 'cancelled',
    error_code = 'MIGRATION_LEGACY_UNREPLAYABLE',
    error_message = 'Legacy job created before durable payload schema cannot be safely replayed',
    completed_at = COALESCE(completed_at, NOW())
WHERE status IN ('queued', 'running') AND (request_hash = '' OR job_payload = '{}'::jsonb);

-- Terminal legacy jobs are backfilled with complete validated JSON representations so historical reports/usage remain readable
UPDATE investigation_jobs
SET request_hash = CASE
        WHEN request_hash = '' OR request_hash IS NULL THEN encode(sha256(convert_to(coalesce(idempotency_key, id::text), 'UTF8')), 'hex')
        ELSE request_hash
    END,
    deadline_at = COALESCE(deadline_at, completed_at, created_at + interval '1 hour'),
    role = COALESCE(NULLIF(role, ''), 'OPS_VIEWER'),
    job_payload = CASE
        WHEN job_payload = '{}'::jsonb OR job_payload IS NULL THEN
            jsonb_build_object(
                'ID', id::text,
                'ActorID', actor_id::text,
                'Role', COALESCE(NULLIF(role, ''), 'OPS_VIEWER'),
                'PaymentID', payment_id::text,
                'Question', coalesce(question, ''),
                'IdempotencyKey', idempotency_key::text,
                'RequestHash', CASE
                    WHEN request_hash = '' OR request_hash IS NULL THEN encode(sha256(convert_to(coalesce(idempotency_key, id::text), 'UTF8')), 'hex')
                    ELSE request_hash
                END,
                'Status', status::text,
                'Mode', coalesce(mode, 'evidence_only')::text,
                'LeaseOwner', coalesce(lease_owner, '')::text,
                'LeaseEpoch', coalesce(lease_epoch, 0),
                'LeaseExpiresAt', lease_expires_at,
                'DeadlineAt', COALESCE(deadline_at, completed_at, created_at + interval '1 hour'),
                'CreatedAt', created_at,
                'CompletedAt', completed_at,
                'ErrorCode', coalesce(error_code, '')::text,
                'Report', report,
                'ToolTimeline', coalesce(tool_timeline, '[]'::jsonb),
                'UsageKnown', (usage IS NOT NULL),
                'Usage', coalesce(usage, '{}'::jsonb)
            )
        ELSE job_payload
    END
WHERE request_hash = '' OR job_payload = '{}'::jsonb;
