CREATE TABLE users (
    id UUID PRIMARY KEY,
    name VARCHAR(120) NOT NULL,
    phone VARCHAR(32) NOT NULL,
    upi_id VARCHAR(128) NOT NULL,
    password_hash TEXT NOT NULL,
    role VARCHAR(32) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT users_phone_key UNIQUE (phone),
    CONSTRAINT users_upi_id_key UNIQUE (upi_id),
    CONSTRAINT users_role_check CHECK (role IN ('CUSTOMER', 'MERCHANT', 'OPS_ADMIN'))
);

CREATE TABLE banks (
    id UUID PRIMARY KEY,
    code VARCHAR(32) NOT NULL,
    name VARCHAR(120) NOT NULL,
    status VARCHAR(32) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT banks_code_key UNIQUE (code),
    CONSTRAINT banks_status_check CHECK (status IN ('ACTIVE', 'INACTIVE'))
);

CREATE TABLE accounts (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    bank_id UUID NOT NULL,
    account_number VARCHAR(64) NOT NULL,
    balance_paise BIGINT NOT NULL DEFAULT 0,
    version BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT accounts_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id),
    CONSTRAINT accounts_bank_id_fkey FOREIGN KEY (bank_id) REFERENCES banks (id),
    CONSTRAINT accounts_account_number_key UNIQUE (account_number),
    CONSTRAINT accounts_balance_paise_check CHECK (balance_paise >= 0),
    CONSTRAINT accounts_version_check CHECK (version >= 0),
    CONSTRAINT accounts_status_check CHECK (status IN ('ACTIVE', 'BLOCKED', 'CLOSED'))
);

CREATE TABLE payments (
    id UUID PRIMARY KEY,
    initiated_by_user_id UUID NOT NULL,
    sender_account_id UUID NOT NULL,
    receiver_account_id UUID NOT NULL,
    amount_paise BIGINT NOT NULL,
    currency VARCHAR(3) NOT NULL,
    state VARCHAR(32) NOT NULL,
    route_bank_id UUID,
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMPTZ,
    CONSTRAINT payments_initiated_by_user_id_fkey FOREIGN KEY (initiated_by_user_id) REFERENCES users (id),
    CONSTRAINT payments_sender_account_id_fkey FOREIGN KEY (sender_account_id) REFERENCES accounts (id),
    CONSTRAINT payments_receiver_account_id_fkey FOREIGN KEY (receiver_account_id) REFERENCES accounts (id),
    CONSTRAINT payments_route_bank_id_fkey FOREIGN KEY (route_bank_id) REFERENCES banks (id),
    CONSTRAINT payments_amount_paise_check CHECK (amount_paise > 0),
    CONSTRAINT payments_currency_check CHECK (currency IN ('INR')),
    CONSTRAINT payments_state_check CHECK (state IN (
        'CREATED',
        'VALIDATING',
        'ROUTING',
        'PROCESSING',
        'COMMITTED',
        'COMPLETED',
        'FAILED',
        'PENDING_RECONCILIATION',
        'REVERSED',
        'OFFLINE_CAPTURED',
        'QUEUED',
        'SYNCING',
        'REPLAY_FAILED'
    ))
);

CREATE TABLE ledger_transactions (
    id UUID PRIMARY KEY,
    payment_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT ledger_transactions_payment_id_fkey FOREIGN KEY (payment_id) REFERENCES payments (id),
    CONSTRAINT ledger_transactions_payment_id_key UNIQUE (payment_id)
);

CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY,
    ledger_transaction_id UUID NOT NULL,
    account_id UUID NOT NULL,
    entry_type VARCHAR(16) NOT NULL,
    amount_paise BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT ledger_entries_ledger_transaction_id_fkey FOREIGN KEY (ledger_transaction_id) REFERENCES ledger_transactions (id),
    CONSTRAINT ledger_entries_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts (id),
    CONSTRAINT ledger_entries_entry_type_check CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    CONSTRAINT ledger_entries_amount_paise_check CHECK (amount_paise > 0)
);

CREATE TABLE idempotency_records (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    key VARCHAR(255) NOT NULL,
    request_hash VARCHAR(128) NOT NULL,
    payment_id UUID,
    response_snapshot JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ,
    CONSTRAINT idempotency_records_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id),
    CONSTRAINT idempotency_records_payment_id_fkey FOREIGN KEY (payment_id) REFERENCES payments (id),
    CONSTRAINT idempotency_records_user_id_key_key UNIQUE (user_id, key),
    CONSTRAINT idempotency_records_key_check CHECK (length(trim(key)) > 0)
);

CREATE INDEX accounts_user_id_idx ON accounts (user_id);
CREATE INDEX accounts_bank_id_idx ON accounts (bank_id);
CREATE INDEX payments_initiated_by_user_id_idx ON payments (initiated_by_user_id);
CREATE INDEX payments_sender_account_id_idx ON payments (sender_account_id);
CREATE INDEX payments_receiver_account_id_idx ON payments (receiver_account_id);
CREATE INDEX payments_created_at_idx ON payments (created_at DESC);
CREATE INDEX payments_state_idx ON payments (state);
CREATE INDEX ledger_entries_ledger_transaction_id_idx ON ledger_entries (ledger_transaction_id);
CREATE INDEX ledger_entries_account_id_created_at_idx ON ledger_entries (account_id, created_at);
CREATE INDEX idempotency_records_payment_id_idx ON idempotency_records (payment_id);
CREATE INDEX idempotency_records_expires_at_idx ON idempotency_records (expires_at);