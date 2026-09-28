CREATE TABLE payment_route_decisions (
    id BIGSERIAL PRIMARY KEY,
    payment_id UUID NOT NULL REFERENCES payments (id),
    candidate_id VARCHAR(160) NOT NULL,
    source_bank_id UUID NOT NULL REFERENCES banks (id),
    destination_bank_id UUID NOT NULL REFERENCES banks (id),
    execution_target_id VARCHAR(120) NOT NULL,
    selected_score DOUBLE PRECISION NOT NULL,
    health_snapshot JSONB NOT NULL,
    reason_code VARCHAR(64) NOT NULL,
    selection_mode VARCHAR(16) NOT NULL CHECK (selection_mode IN ('STATIC', 'ADAPTIVE')),
    selected_at TIMESTAMPTZ NOT NULL,
    event_type VARCHAR(32) NOT NULL CHECK (event_type = 'PAYMENT_ROUTED')
);

CREATE INDEX payment_route_decisions_payment_idx ON payment_route_decisions (payment_id, selected_at DESC, id DESC);
CREATE INDEX payment_route_decisions_target_idx ON payment_route_decisions (execution_target_id, selected_at DESC, id DESC);
