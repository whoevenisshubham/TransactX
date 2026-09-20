ALTER TABLE payments
    ADD COLUMN note VARCHAR(280),
    ADD COLUMN origin VARCHAR(32) NOT NULL DEFAULT 'ONLINE';

ALTER TABLE payments
    ADD CONSTRAINT payments_origin_check CHECK (origin IN ('ONLINE'));

CREATE INDEX payments_sender_receiver_created_at_idx
    ON payments (sender_account_id, receiver_account_id, created_at DESC);
