-- Migration 000029: Reconcile M3 investigator store, immutable corpus versions, and usage limits

-- 1. Immutable corpus versions and role-filtered chunks
CREATE TABLE IF NOT EXISTS corpus_versions (
    version TEXT PRIMARY KEY,
    manifest_sha256 TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('staging', 'active', 'retired')),
    chunk_count INTEGER NOT NULL CHECK (chunk_count >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    activated_at TIMESTAMPTZ,
    deactivated_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS corpus_versions_one_active_idx ON corpus_versions ((status)) WHERE status = 'active';

CREATE TABLE IF NOT EXISTS runbook_chunks (
    corpus_version TEXT NOT NULL REFERENCES corpus_versions(version),
    chunk_id TEXT NOT NULL,
    document_path TEXT NOT NULL,
    heading TEXT NOT NULL,
    start_line INTEGER NOT NULL CHECK (start_line > 0),
    end_line INTEGER NOT NULL CHECK (end_line >= start_line),
    source_sha256 TEXT NOT NULL,
    content_sha256 TEXT NOT NULL,
    implementation_status TEXT NOT NULL CHECK (implementation_status IN ('existing', 'planned')),
    tags TEXT[] NOT NULL,
    allowed_roles TEXT[] NOT NULL,
    content TEXT NOT NULL,
    search_vector TSVECTOR GENERATED ALWAYS AS (
        to_tsvector('simple', coalesce(heading, '') || ' ' || coalesce(content, ''))
    ) STORED,
    PRIMARY KEY (corpus_version, chunk_id)
);

CREATE INDEX IF NOT EXISTS runbook_chunks_search_idx ON runbook_chunks USING GIN (search_vector);
CREATE INDEX IF NOT EXISTS runbook_chunks_tags_idx ON runbook_chunks USING GIN (tags);

-- 2. Usage limits table for global token budget serialization
CREATE TABLE IF NOT EXISTS investigation_usage_limits (
    id INT PRIMARY KEY DEFAULT 1,
    input_limit BIGINT NOT NULL,
    output_limit BIGINT NOT NULL
);

INSERT INTO investigation_usage_limits (id, input_limit, output_limit)
VALUES (1, 8000, 1200)
ON CONFLICT (id) DO NOTHING;

-- 3. Upgrade investigation_jobs for durable checkpointing and lease protocol
ALTER TABLE investigation_usage_reservations DROP CONSTRAINT IF EXISTS investigation_usage_reservations_job_id_fkey;
ALTER TABLE investigation_jobs DROP CONSTRAINT IF EXISTS investigation_jobs_payment_id_fkey;

ALTER TABLE investigation_jobs ALTER COLUMN id TYPE TEXT USING id::text;
ALTER TABLE investigation_jobs ALTER COLUMN payment_id TYPE TEXT USING payment_id::text;
ALTER TABLE investigation_jobs ALTER COLUMN actor_id TYPE TEXT USING actor_id::text;
ALTER TABLE investigation_jobs ALTER COLUMN status TYPE TEXT USING status::text;
ALTER TABLE investigation_jobs ALTER COLUMN mode TYPE TEXT USING mode::text;
ALTER TABLE investigation_jobs ALTER COLUMN idempotency_key TYPE TEXT USING idempotency_key::text;

ALTER TABLE investigation_jobs ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'OPS_VIEWER';
ALTER TABLE investigation_jobs ADD COLUMN IF NOT EXISTS request_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE investigation_jobs ADD COLUMN IF NOT EXISTS deadline_at TIMESTAMPTZ;
ALTER TABLE investigation_jobs ADD COLUMN IF NOT EXISTS job_payload JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE INDEX IF NOT EXISTS idx_investigation_jobs_active ON investigation_jobs (status) WHERE status IN ('queued', 'running');

-- 4. Upgrade investigation_usage_reservations
ALTER TABLE investigation_usage_reservations ALTER COLUMN id TYPE TEXT USING id::text;
ALTER TABLE investigation_usage_reservations ALTER COLUMN job_id TYPE TEXT USING job_id::text;
ALTER TABLE investigation_usage_reservations ALTER COLUMN actor_id TYPE TEXT USING actor_id::text;
ALTER TABLE investigation_usage_reservations ALTER COLUMN status TYPE TEXT USING status::text;

ALTER TABLE investigation_usage_reservations ADD COLUMN IF NOT EXISTS reserved_input BIGINT NOT NULL DEFAULT 0;
ALTER TABLE investigation_usage_reservations ADD COLUMN IF NOT EXISTS reserved_output BIGINT NOT NULL DEFAULT 0;
ALTER TABLE investigation_usage_reservations ADD COLUMN IF NOT EXISTS actual_input BIGINT;
ALTER TABLE investigation_usage_reservations ADD COLUMN IF NOT EXISTS actual_output BIGINT;

ALTER TABLE investigation_usage_reservations DROP CONSTRAINT IF EXISTS investigation_usage_reservations_status_check;
ALTER TABLE investigation_usage_reservations ADD CONSTRAINT investigation_usage_reservations_status_check
    CHECK (status IN ('reserved', 'committed', 'released', 'completed', 'uncertain'));

ALTER TABLE investigation_usage_reservations ADD CONSTRAINT investigation_usage_reservations_job_id_fkey
    FOREIGN KEY (job_id) REFERENCES investigation_jobs(id) ON DELETE CASCADE;
