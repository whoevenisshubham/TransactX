DROP INDEX IF EXISTS bank_a_operations_bank_account_idx;
ALTER TABLE bank_a.operations DROP COLUMN IF EXISTS bank_id;
