ALTER TABLE recon_discrepancies
    DROP CONSTRAINT IF EXISTS recon_discrepancies_mismatch_category_check;

ALTER TABLE recon_discrepancies
    ADD CONSTRAINT recon_discrepancies_mismatch_category_check
    CHECK (mismatch_category IN (
        'BUCKET_ROOT_MISMATCH',
        'CANONICAL_ROOT_MISMATCH',
        'PARTICIPANT_UNAVAILABLE',
        'SCOPE_MISMATCH',
        'VERSION_INCOMPATIBLE'
    ));
