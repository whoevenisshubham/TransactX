DROP INDEX merkle_commitments_lookup_idx;

ALTER TABLE merkle_commitments
    DROP CONSTRAINT merkle_commitments_owner_partition_width_scope_key;

ALTER TABLE merkle_commitments
    DROP COLUMN owner_id;

ALTER TABLE merkle_commitments
    ADD CONSTRAINT merkle_commitments_partition_width_scope_key
    UNIQUE (partition, bucket_width_ns, scope_from, scope_to);

CREATE INDEX merkle_commitments_lookup_idx
    ON merkle_commitments (partition, scope_from, scope_to);

