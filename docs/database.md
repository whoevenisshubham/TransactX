# Database

## Status

### IMPLEMENTED

The Go API creates a PostgreSQL connection pool and verifies connectivity during startup. `GET /health/db` performs a live readiness check.

No application tables or migrations have been created yet.

### PLANNED

PostgreSQL will remain authoritative for users, accounts, payments, idempotency records, balances, and the double-entry ledger. Monetary columns will use `BIGINT` values representing paise.

### FUTURE WORK

Database migrations for application data will be added after the Phase 0 foundation is accepted.