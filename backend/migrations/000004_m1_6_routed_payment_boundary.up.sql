CREATE SCHEMA bank_a;

ALTER TABLE payments
    ADD COLUMN source_bank_id UUID REFERENCES banks (id),
    ADD COLUMN destination_bank_id UUID REFERENCES banks (id),
    ADD COLUMN routing_reason TEXT,
    ADD COLUMN bank_settled_at TIMESTAMPTZ;

ALTER TABLE payments
    DROP CONSTRAINT payments_state_check,
    ADD CONSTRAINT payments_state_check CHECK (state IN (
        'CREATED',
        'VALIDATING',
        'LOCAL_SETTLEMENT',
        'ROUTING',
        'PROCESSING',
        'BANK_SETTLED_CENTRAL_PENDING',
        'COMMITTED',
        'COMPLETED',
        'FAILED',
        'PENDING_RECONCILIATION',
        'REVERSED',
        'OFFLINE_CAPTURED',
        'QUEUED',
        'SYNCING',
        'REPLAY_FAILED'
    ));

CREATE TABLE payment_bank_operations (
    id UUID PRIMARY KEY,
    payment_id UUID NOT NULL REFERENCES payments (id),
    bank_id UUID NOT NULL REFERENCES banks (id),
    operation_id UUID NOT NULL,
    operation_type VARCHAR(32) NOT NULL,
    status VARCHAR(32) NOT NULL,
    bank_reference TEXT,
    hold_id UUID,
    original_operation_id UUID,
    amount_paise BIGINT NOT NULL CHECK (amount_paise > 0),
    currency VARCHAR(3) NOT NULL CHECK (currency = 'INR'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (bank_id, operation_id),
    UNIQUE (payment_id, operation_type)
);

CREATE TABLE bank_a.accounts (
    id UUID PRIMARY KEY,
    account_number VARCHAR(64) NOT NULL UNIQUE,
    balance_paise BIGINT NOT NULL DEFAULT 0 CHECK (balance_paise >= 0),
    version BIGINT NOT NULL DEFAULT 0 CHECK (version >= 0),
    status VARCHAR(32) NOT NULL CHECK (status IN ('ACTIVE', 'INACTIVE', 'CLOSED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE bank_a.operations (
    id UUID PRIMARY KEY,
    payment_id UUID NOT NULL,
    operation_id UUID NOT NULL UNIQUE,
    idempotency_key VARCHAR(255) NOT NULL,
    operation_type VARCHAR(32) NOT NULL CHECK (operation_type IN (
        'HOLD',
        'PROVISIONAL_CREDIT',
        'CONFIRM_HOLD',
        'RELEASE_HOLD',
        'REVERSE_PROVISIONAL_CREDIT'
    )),
    account_id UUID,
    hold_id UUID,
    original_operation_id UUID,
    amount_paise BIGINT CHECK (amount_paise > 0),
    currency VARCHAR(3) CHECK (currency = 'INR'),
    status VARCHAR(32) NOT NULL CHECK (status IN (
        'ACTIVE',
        'PROVISIONAL',
        'CONFIRMED',
        'FINAL',
        'RELEASED',
        'REVERSED',
        'FAILED',
        'UNKNOWN'
    )),
    bank_reference TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (idempotency_key)
);

CREATE TABLE bank_a.ledger_entries (
    id UUID PRIMARY KEY,
    operation_id UUID NOT NULL REFERENCES bank_a.operations (operation_id),
    payment_id UUID NOT NULL,
    account_id UUID NOT NULL REFERENCES bank_a.accounts (id),
    entry_type VARCHAR(32) NOT NULL CHECK (entry_type IN (
        'HOLD',
        'PROVISIONAL_CREDIT',
        'FINAL_CREDIT',
        'RELEASE',
        'REVERSE_CREDIT'
    )),
    amount_paise BIGINT NOT NULL CHECK (amount_paise > 0),
    currency VARCHAR(3) NOT NULL CHECK (currency = 'INR'),
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX payment_bank_operations_payment_idx ON payment_bank_operations (payment_id);
CREATE INDEX payment_bank_operations_status_idx ON payment_bank_operations (status);
CREATE INDEX bank_a_operations_payment_idx ON bank_a.operations (payment_id);
CREATE INDEX bank_a_operations_status_idx ON bank_a.operations (status);
CREATE INDEX bank_a_ledger_entries_payment_idx ON bank_a.ledger_entries (payment_id);
CREATE INDEX bank_a_ledger_entries_account_time_idx ON bank_a.ledger_entries (account_id, occurred_at, id);
