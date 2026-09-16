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