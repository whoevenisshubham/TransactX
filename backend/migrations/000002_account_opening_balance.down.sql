ALTER TABLE accounts
    DROP CONSTRAINT IF EXISTS accounts_opening_balance_paise_check,
    DROP COLUMN IF EXISTS opening_balance_paise;