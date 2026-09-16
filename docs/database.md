# Database

## Status

### IMPLEMENTED

The Go API creates a PostgreSQL connection pool and verifies connectivity during startup. `GET /health/db` performs a live readiness check.

Phase 1A adds the first PostgreSQL migration:

- `users`
- `banks`
- `accounts`
- `payments`
- `ledger_transactions`
- `ledger_entries`
- `idempotency_records`

The migration is applied explicitly with `psql`; the API does not migrate the database during startup. The migration files are `backend/migrations/000001_phase1a_payment_core.up.sql` and `backend/migrations/000001_phase1a_payment_core.down.sql`.

Accounts store the materialized balance in `accounts.balance_paise BIGINT` and preserve a nonnegative `accounts.opening_balance_paise BIGINT` baseline for ledger reconstruction. There is no separate balances table. All monetary values are integer paise; floating-point and decimal monetary representations are not used.

### Financial constraints

The schema enforces positive payment and ledger-entry amounts, non-negative account balances and versions, known roles/statuses/payment states, unique user payment identifiers, unique account numbers, unique bank codes, user-scoped idempotency keys, and required foreign-key relationships.

The schema does not by itself enforce debit/credit conservation, legal state transitions, account ownership by the authenticated user, or protection against double spending. Those require later application transactions, locking, and integrity checks.

Phase 1B uses the existing schema plus migrations `000002_account_opening_balance` and `000003_local_settlement_state`. Registration selects the active bank identified by the server-side `DEFAULT_BANK_CODE` and inserts the user and zero-balance initial account in one PostgreSQL transaction. Public input cannot choose the bank, account number, balance, or account status. Account reads include `user_id` ownership predicates. Recipient lookup uses the unique `users.upi_id` and returns only safe identity/status fields.

M1-3C settles payments in one transaction containing payment state, account mutations, ledger transaction, ledger entries, and idempotency linkage. A normal transfer writes exactly one debit and one credit for the payment amount. Reconstructed balance is opening balance plus credits minus debits.

M1-3D PostgreSQL integration tests experimentally verify that the conditional debit update serializes concurrent attempts safely: insufficient-funds attempts do not leave payment, ledger, or idempotency artifacts; successful attempts conserve debit/credit totals; and materialized balances match opening balance plus ledger entries after contention. These are tested guarantees for the exercised local path, not formal verification or production certification.

### FUTURE WORK

The domain contract at `backend/internal/bank` defines the operations bank implementations provide. M1-5 adds Bank A as a local deterministic simulation whose explicitly supplied account state is not persisted or authoritative. The current handler injects `nil`, so the payment flow does not invoke an adapter. Bank-specific persistence, Bank B, routing, and payment seed data remain future implementation work. The current authoritative settlement remains the PostgreSQL `LOCAL_SETTLEMENT` transaction. `backend/cmd/devseed` is a development-only provisioning command for the synthetic bank and OPS_ADMIN; its password must be supplied through `DEV_ADMIN_PASSWORD` and is never stored in the repository.