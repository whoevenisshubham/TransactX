-- Never destroy external accounts or invent credentials during downgrade.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM users WHERE phone IS NULL OR password_hash IS NULL)
       OR EXISTS (SELECT 1 FROM auth_external_identities) THEN
        RAISE EXCEPTION 'OIDC identities exist; preserve users and migrate forward';
    END IF;
END $$;
DROP TABLE auth_exchange_codes;
DROP TABLE auth_login_flows;
DROP TABLE auth_external_identities;
DROP TABLE auth_oidc_admission;
DROP TABLE auth_external_identity_budget;
ALTER TABLE users ALTER COLUMN phone SET NOT NULL;
ALTER TABLE users ALTER COLUMN password_hash SET NOT NULL;
