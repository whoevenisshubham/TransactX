ALTER TABLE recon_runs
    ADD COLUMN elapsed_ns BIGINT NOT NULL DEFAULT 0 CHECK (elapsed_ns >= 0),
    ADD COLUMN nodes_visited BIGINT NOT NULL DEFAULT 0 CHECK (nodes_visited >= 0),
    ADD COLUMN records_inspected BIGINT NOT NULL DEFAULT 0 CHECK (records_inspected >= 0),
    ADD COLUMN bytes_examined BIGINT NOT NULL DEFAULT 0 CHECK (bytes_examined >= 0),
    ADD COLUMN divergent_buckets BIGINT NOT NULL DEFAULT 0 CHECK (divergent_buckets >= 0),
    ADD COLUMN divergent_records BIGINT NOT NULL DEFAULT 0 CHECK (divergent_records >= 0);

COMMENT ON COLUMN recon_runs.elapsed_ns IS
    'Production engine elapsed time after the durable RUNNING record, in nanoseconds.';
COMMENT ON COLUMN recon_runs.nodes_visited IS
    'Merkle node hashes inspected across canonical and participant trees, including both roots.';
COMMENT ON COLUMN recon_runs.records_inspected IS
    'Canonical and participant ledger records loaded from divergent buckets.';
COMMENT ON COLUMN recon_runs.bytes_examined IS
    'Exact commitment-hash bytes and canonical record bytes inspected in process; not network bytes.';
COMMENT ON COLUMN recon_runs.divergent_buckets IS
    'Distinct logical buckets whose commitments or presence differ.';
COMMENT ON COLUMN recon_runs.divergent_records IS
    'Distinct logical ledger records found missing, extra, or field-divergent.';
