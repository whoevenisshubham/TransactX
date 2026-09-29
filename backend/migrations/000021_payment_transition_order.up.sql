-- CURRENT_TIMESTAMP is transaction-stable, so transitions written by one
-- settlement need an explicit insertion order for deterministic replay.
ALTER TABLE payment_state_transitions
    ADD COLUMN sequence_number BIGSERIAL NOT NULL;

CREATE UNIQUE INDEX payment_state_transitions_sequence_key
    ON payment_state_transitions (sequence_number);

DROP INDEX payment_state_transitions_payment_idx;
CREATE INDEX payment_state_transitions_payment_idx
    ON payment_state_transitions (payment_id, sequence_number ASC);

