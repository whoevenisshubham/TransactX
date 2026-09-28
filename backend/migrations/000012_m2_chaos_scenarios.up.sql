CREATE TABLE chaos_scenarios (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scenario_id VARCHAR(120) NOT NULL UNIQUE,
    scenario_type VARCHAR(64) NOT NULL CHECK (scenario_type IN ('BANK_OUTAGE', 'LATENCY', 'TRANSIENT_DROP', 'TEMPORARY_PARTITION', 'TRANSIENT', 'MESSAGE_DROP')),
    target_id VARCHAR(120) NOT NULL,
    parameters JSONB NOT NULL DEFAULT '{}'::jsonb,
    started_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    stopped_at TIMESTAMPTZ NULL,
    active BOOLEAN NOT NULL DEFAULT true,
    mode VARCHAR(32) NOT NULL DEFAULT 'SIMULATION' CHECK (mode = 'SIMULATION'),
    created_by VARCHAR(120) NOT NULL,
    stopped_by VARCHAR(120) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX chaos_scenarios_target_active_idx ON chaos_scenarios (target_id, active);
CREATE INDEX chaos_scenarios_active_expiry_idx ON chaos_scenarios (active, expires_at);

CREATE TABLE chaos_events (
    id BIGSERIAL PRIMARY KEY,
    scenario_id VARCHAR(120) NOT NULL,
    event_type VARCHAR(64) NOT NULL CHECK (event_type IN ('CHAOS_STARTED', 'CHAOS_STOPPED', 'CHAOS_RESET', 'CHAOS_EXPIRED')),
    target_id VARCHAR(120) NOT NULL,
    fault_type VARCHAR(64) NOT NULL,
    actor_id VARCHAR(120) NOT NULL,
    actor_role VARCHAR(64) NOT NULL,
    parameters JSONB NOT NULL DEFAULT '{}'::jsonb,
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX chaos_events_scenario_idx ON chaos_events (scenario_id, occurred_at DESC);
CREATE INDEX chaos_events_target_idx ON chaos_events (target_id, occurred_at DESC);
