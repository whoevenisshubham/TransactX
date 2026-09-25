-- M3-7: Durable participant Merkle commitments for runtime financial integrity.
-- Persists derived commitment state (partition, bucket width, scope, generation,
-- root, record count, rebuild count, tree levels, bucket records) independently
-- from authoritative ledger transactions.

CREATE TABLE merkle_commitments (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    partition         VARCHAR(120) NOT NULL,
    bucket_width_ns   BIGINT NOT NULL,
    scope_from        TIMESTAMPTZ NOT NULL,
    scope_to          TIMESTAMPTZ NOT NULL,
    generation        VARCHAR(120) NOT NULL,
    canonical_version VARCHAR(32) NOT NULL,
    algorithm_version VARCHAR(32) NOT NULL,
    root              BYTEA NOT NULL,
    record_count      BIGINT NOT NULL,
    rebuild_count     BIGINT NOT NULL DEFAULT 0,
    captured_at       TIMESTAMPTZ NOT NULL,
    buckets_data      JSONB NOT NULL DEFAULT '[]'::jsonb,
    records_data      JSONB NOT NULL DEFAULT '[]'::jsonb,
    levels_data       JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT merkle_commitments_partition_width_scope_key UNIQUE (partition, bucket_width_ns, scope_from, scope_to)
);

CREATE INDEX merkle_commitments_lookup_idx
    ON merkle_commitments (partition, scope_from, scope_to);

CREATE INDEX merkle_commitments_gen_idx
    ON merkle_commitments (generation);
