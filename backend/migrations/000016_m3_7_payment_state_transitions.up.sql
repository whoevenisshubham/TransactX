-- M3-7-C4: Immutable state transition history for payments.
-- Existing historical payments before this migration have no reconstructed transition history.
-- Their current state may be checked, but historical transition validity is not retroactively claimed.

CREATE TABLE payment_state_transitions (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id      UUID         NOT NULL REFERENCES payments(id),
    from_state      VARCHAR(32)  NOT NULL,
    to_state        VARCHAR(32)  NOT NULL,
    transitioned_at TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX payment_state_transitions_payment_idx
    ON payment_state_transitions (payment_id, transitioned_at ASC);

-- Immutability enforcement: prevent update and delete application paths.
CREATE OR REPLACE FUNCTION prevent_payment_state_transitions_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'payment_state_transitions is an immutable append-only table';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_immutable_payment_state_transitions
BEFORE UPDATE OR DELETE ON payment_state_transitions
FOR EACH ROW
EXECUTE FUNCTION prevent_payment_state_transitions_mutation();
