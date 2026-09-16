ALTER TABLE accounts
    ADD COLUMN opening_balance_paise BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT accounts_opening_balance_paise_check CHECK (opening_balance_paise >= 0);