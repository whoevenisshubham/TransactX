-- M3-8: Expand recon_discrepancies.mismatch_category CHECK constraint
-- to support record-level mismatch categories (RECORD_MISMATCH, MISSING_PARTICIPANT_RECORD, EXTRA_PARTICIPANT_RECORD).

ALTER TABLE recon_discrepancies
    DROP CONSTRAINT IF EXISTS recon_discrepancies_mismatch_category_check;

ALTER TABLE recon_discrepancies
    ADD CONSTRAINT recon_discrepancies_mismatch_category_check
    CHECK (mismatch_category IN (
        'BUCKET_ROOT_MISMATCH',
        'CANONICAL_ROOT_MISMATCH',
        'RECORD_MISMATCH',
        'MISSING_PARTICIPANT_RECORD',
        'EXTRA_PARTICIPANT_RECORD',
        'PARTICIPANT_UNAVAILABLE',
        'SCOPE_MISMATCH',
        'VERSION_INCOMPATIBLE'
    ));
