CREATE SCHEMA bank_b;

CREATE TABLE bank_b.accounts (
    id UUID PRIMARY KEY,
    account_number VARCHAR(64) NOT NULL UNIQUE,
    balance_paise BIGINT NOT NULL DEFAULT 0 CHECK (balance_paise >= 0),
    version BIGINT NOT NULL DEFAULT 0 CHECK (version >= 0),
    status VARCHAR(32) NOT NULL CHECK (status IN ('ACTIVE', 'INACTIVE', 'CLOSED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE bank_b.operations (LIKE bank_a.operations INCLUDING ALL);
ALTER TABLE bank_b.operations ALTER COLUMN bank_id DROP DEFAULT;

CREATE TABLE bank_b.ledger_entries (LIKE bank_a.ledger_entries INCLUDING ALL);

CREATE INDEX bank_b_operations_payment_idx ON bank_b.operations (payment_id);
CREATE INDEX bank_b_operations_status_idx ON bank_b.operations (status);
CREATE INDEX bank_b_operations_bank_account_idx ON bank_b.operations (bank_id, account_id);
CREATE INDEX bank_b_ledger_entries_payment_idx ON bank_b.ledger_entries (payment_id);
CREATE INDEX bank_b_ledger_entries_account_time_idx ON bank_b.ledger_entries (account_id, occurred_at, id);
