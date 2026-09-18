ALTER TABLE bank_a.operations
    ADD COLUMN bank_id VARCHAR(32) NOT NULL DEFAULT 'BANK-A';

ALTER TABLE bank_a.operations
    ALTER COLUMN bank_id DROP DEFAULT;

CREATE INDEX bank_a_operations_bank_account_idx ON bank_a.operations (bank_id, account_id);
