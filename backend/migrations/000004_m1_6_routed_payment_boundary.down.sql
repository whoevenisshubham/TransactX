DROP TABLE IF EXISTS bank_a.ledger_entries;
DROP TABLE IF EXISTS bank_a.operations;
DROP TABLE IF EXISTS bank_a.accounts;
DROP SCHEMA IF EXISTS bank_a;

DROP TABLE IF EXISTS payment_bank_operations;

ALTER TABLE payments
    DROP CONSTRAINT payments_state_check,
    ADD CONSTRAINT payments_state_check CHECK (state IN (
        'CREATED',
        'VALIDATING',
        'LOCAL_SETTLEMENT',
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
    ));

ALTER TABLE payments
    DROP COLUMN IF EXISTS source_bank_id,
    DROP COLUMN IF EXISTS destination_bank_id,
    DROP COLUMN IF EXISTS routing_reason,
    DROP COLUMN IF EXISTS bank_settled_at;
