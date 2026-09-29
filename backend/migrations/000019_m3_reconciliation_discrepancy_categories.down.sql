BEGIN;

ALTER TABLE recon_discrepancies
    DROP CONSTRAINT recon_discrepancies_mismatch_category_check;

-- This intentionally fails and rolls back without data loss if record-level
-- discrepancy rows exist that the older schema cannot represent.
ALTER TABLE recon_discrepancies
    ADD CONSTRAINT recon_discrepancies_mismatch_category_check
    CHECK (mismatch_category IN (
        'BUCKET_ROOT_MISMATCH',
        'CANONICAL_ROOT_MISMATCH',
        'PARTICIPANT_UNAVAILABLE',
        'SCOPE_MISMATCH',
        'VERSION_INCOMPATIBLE'
    ));

COMMIT;
