DROP INDEX IF EXISTS payments_sender_receiver_created_at_idx;

ALTER TABLE payments
    DROP CONSTRAINT IF EXISTS payments_origin_check,
    DROP COLUMN IF EXISTS origin,
    DROP COLUMN IF EXISTS note;
