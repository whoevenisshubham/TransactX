DROP TRIGGER IF EXISTS trg_immutable_payment_state_transitions ON payment_state_transitions;
DROP FUNCTION IF EXISTS prevent_payment_state_transitions_mutation();
DROP TABLE IF EXISTS payment_state_transitions;
