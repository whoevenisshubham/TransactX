DROP INDEX payment_state_transitions_payment_idx;
DROP INDEX payment_state_transitions_sequence_key;

ALTER TABLE payment_state_transitions
    DROP COLUMN sequence_number;

CREATE INDEX payment_state_transitions_payment_idx
    ON payment_state_transitions (payment_id, transitioned_at ASC);

