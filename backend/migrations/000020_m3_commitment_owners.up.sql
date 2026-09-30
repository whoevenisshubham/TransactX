-- Keep independently maintained canonical and participant commitments for the
-- same logical bank partition and closed reconciliation scope.
ALTER TABLE merkle_commitments
    ADD COLUMN owner_id VARCHAR(180) NOT NULL DEFAULT 'legacy';

ALTER TABLE merkle_commitments
    DROP CONSTRAINT merkle_commitments_partition_width_scope_key;

ALTER TABLE merkle_commitments
    ADD CONSTRAINT merkle_commitments_owner_partition_width_scope_key
    UNIQUE (owner_id, partition, bucket_width_ns, scope_from, scope_to);

DROP INDEX merkle_commitments_lookup_idx;
CREATE INDEX merkle_commitments_lookup_idx
    ON merkle_commitments (owner_id, partition, scope_from, scope_to);

