DROP INDEX IF EXISTS payment_bank_operations_account_idx;

ALTER TABLE payment_bank_operations
    DROP COLUMN IF EXISTS idempotency_key,
    DROP COLUMN IF EXISTS account_id;

ALTER TABLE payments
    DROP COLUMN IF EXISTS source_bank_account_id,
    DROP COLUMN IF EXISTS destination_bank_account_id;

DROP INDEX IF EXISTS accounts_bank_account_lookup_idx;
ALTER TABLE accounts DROP COLUMN IF EXISTS bank_account_id;
