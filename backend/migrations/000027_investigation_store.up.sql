-- Migration 000027: Approved corpus versions/chunks, investigation jobs/reports/usage reservations

CREATE TABLE IF NOT EXISTS runbook_corpus_chunks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    corpus_version VARCHAR(64) NOT NULL DEFAULT 'v1',
    document_path VARCHAR(255) NOT NULL,
    heading VARCHAR(255) NOT NULL,
    start_line INT NOT NULL,
    end_line INT NOT NULL,
    content TEXT NOT NULL,
    sha256 VARCHAR(64) NOT NULL,
    approved_tags TEXT[] NOT NULL DEFAULT '{}',
    allowed_roles TEXT[] NOT NULL DEFAULT '{"OPS_ADMIN", "OPS_VIEWER"}',
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_runbook_corpus_tsv ON runbook_corpus_chunks USING gin(to_tsvector('english', heading || ' ' || content));
CREATE UNIQUE INDEX IF NOT EXISTS idx_runbook_corpus_chunk_uniq ON runbook_corpus_chunks (corpus_version, document_path, heading, start_line);

CREATE TABLE IF NOT EXISTS investigation_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id UUID NOT NULL REFERENCES payments(id),
    actor_id VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'completed', 'failed', 'cancelled')),
    mode VARCHAR(32) NOT NULL DEFAULT 'evidence_only' CHECK (mode IN ('evidence_only', 'llm')),
    question TEXT NOT NULL,
    report JSONB,
    tool_timeline JSONB NOT NULL DEFAULT '[]'::jsonb,
    usage JSONB,
    error_code VARCHAR(64),
    error_message TEXT,
    lease_owner VARCHAR(255),
    lease_epoch BIGINT NOT NULL DEFAULT 0,
    lease_expires_at TIMESTAMPTZ,
    idempotency_key VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_investigation_jobs_actor_idemp ON investigation_jobs (actor_id, idempotency_key);
CREATE INDEX IF NOT EXISTS idx_investigation_jobs_payment ON investigation_jobs (payment_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_investigation_jobs_status_claim ON investigation_jobs (status, created_at ASC) WHERE status IN ('queued', 'running');

CREATE TABLE IF NOT EXISTS investigation_usage_reservations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES investigation_jobs(id) ON DELETE CASCADE,
    actor_id VARCHAR(255) NOT NULL,
    provider VARCHAR(64) NOT NULL,
    model VARCHAR(64) NOT NULL,
    reserved_tokens INT NOT NULL,
    actual_tokens INT,
    status VARCHAR(32) NOT NULL DEFAULT 'reserved' CHECK (status IN ('reserved', 'committed', 'released')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_investigation_usage_job ON investigation_usage_reservations (job_id);
