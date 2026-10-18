-- External-only users have no local password or phone. Existing values remain.
ALTER TABLE users ALTER COLUMN phone DROP NOT NULL;
ALTER TABLE users ALTER COLUMN password_hash DROP NOT NULL;

CREATE TABLE auth_external_identities (
    issuer TEXT NOT NULL,
    subject VARCHAR(255) NOT NULL,
    provider TEXT NOT NULL CHECK (provider = 'google'),
    user_id UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (issuer, subject),
    UNIQUE (provider, user_id)
);
CREATE TABLE auth_login_flows (
    state_hash BYTEA PRIMARY KEY CHECK (octet_length(state_hash) = 32),
    binding_hash BYTEA NOT NULL CHECK (octet_length(binding_hash) = 32),
    nonce_hash BYTEA NOT NULL CHECK (octet_length(nonce_hash) = 32),
    provider_verifier TEXT NOT NULL,
    app_challenge VARCHAR(43) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX auth_login_flows_expiry ON auth_login_flows(expires_at);
CREATE TABLE auth_exchange_codes (
    code_hash BYTEA PRIMARY KEY CHECK (octet_length(code_hash) = 32),
    user_id UUID NOT NULL REFERENCES users(id),
    app_challenge VARCHAR(43) NOT NULL,
    is_new BOOLEAN NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX auth_exchange_codes_expiry ON auth_exchange_codes(expires_at);
CREATE TABLE auth_oidc_admission (
    action TEXT PRIMARY KEY CHECK (action IN ('start','callback','exchange')),
    window_start TIMESTAMPTZ NOT NULL,
    attempts INTEGER NOT NULL CHECK (attempts >= 0)
);
CREATE TABLE auth_external_identity_budget (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    day DATE NOT NULL,
    daily INTEGER NOT NULL CHECK (daily BETWEEN 0 AND 5),
    lifetime INTEGER NOT NULL CHECK (lifetime BETWEEN 0 AND 50)
);
