-- Central account IDs and participant account IDs are distinct identities. Existing
-- development data used the same UUID for both, so the backfill preserves that
-- compatibility while making the mapping explicit for new accounts.
ALTER TABLE accounts
    ADD COLUMN bank_account_id UUID;

UPDATE accounts
SET bank_account_id = id
WHERE bank_account_id IS NULL;

CREATE INDEX accounts_bank_account_lookup_idx ON accounts (bank_id, bank_account_id);

ALTER TABLE payments
    ADD COLUMN source_bank_account_id UUID,
    ADD COLUMN destination_bank_account_id UUID;

UPDATE payments
SET source_bank_account_id = sender_account_id,
    destination_bank_account_id = receiver_account_id
WHERE source_bank_account_id IS NULL OR destination_bank_account_id IS NULL;

ALTER TABLE payment_bank_operations
    ADD COLUMN account_id UUID,
    ADD COLUMN idempotency_key VARCHAR(255);

UPDATE payment_bank_operations
SET idempotency_key = operation_id::text
WHERE idempotency_key IS NULL;

CREATE INDEX payment_bank_operations_account_idx ON payment_bank_operations (bank_id, account_id);
