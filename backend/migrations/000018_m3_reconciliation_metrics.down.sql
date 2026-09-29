ALTER TABLE recon_runs
    DROP COLUMN divergent_records,
    DROP COLUMN divergent_buckets,
    DROP COLUMN bytes_examined,
    DROP COLUMN records_inspected,
    DROP COLUMN nodes_visited,
    DROP COLUMN elapsed_ns;
