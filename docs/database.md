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

Accounts store the materialized balance in `accounts.balance_paise BIGINT`. There is no separate balances table. All monetary values are integer paise; floating-point and decimal monetary representations are not used.

### Financial constraints

The schema enforces positive payment and ledger-entry amounts, non-negative account balances and versions, known roles/statuses/payment states, unique user payment identifiers, unique account numbers, unique bank codes, user-scoped idempotency keys, and required foreign-key relationships.

The schema does not by itself enforce debit/credit conservation, legal state transitions, account ownership by the authenticated user, or protection against double spending. Those require later application transactions, locking, and integrity checks.

### FUTURE WORK

Payment execution, authentication, idempotency behavior, ledger balancing logic, concurrency control, bank adapters, and seed data remain future implementation work.