# Architecture

## Status

### IMPLEMENTED

The Phase 0 foundation contains one Go HTTP API and one React frontend. The API connects to local PostgreSQL and exposes health/readiness checks. The frontend independently starts with Vite and checks API connectivity.

Phase 1A adds the PostgreSQL payment-core schema through explicit SQL migrations. PostgreSQL now provides the data foundation for users, banks, accounts, payments, idempotency records, and the double-entry ledger. The API still does not execute payments or run migrations automatically at startup.

Phase 1B adds Argon2id password hashing, HS256 JWT authentication, request IDs, server-side role checks, atomic user plus initial-account registration, authenticated profile/account reads, and safe recipient lookup. Public registration allows only CUSTOMER and MERCHANT. OPS_ADMIN is provisioned only by the development seed command.

M1-3A implements the authenticated payment foundation at `POST /api/payments`. The server derives the payer from the JWT, verifies source-account ownership and account status, resolves an active recipient account, validates integer paise/INR input, and persists a payment in `CREATED` state. Payment state transitions are defined centrally for later processing stages. Payment creation uses a short PostgreSQL transaction for the payment row only; it does not mutate balances or create ledger entries.

```text
PostgreSQL
    ↓
Go API
    ↓
React + TypeScript frontend
```

### PLANNED

The payment service will eventually depend on a BankAdapter interface. M1 owns BankAdapter and Bank A. M2 owns Bank B and resilience behavior. M3 owns reconciliation, integrity, and research functionality. M1-3A does not implement that adapter or routing.

### FUTURE WORK

Settlement, balance mutation, double-entry ledger writes, idempotency behavior, concurrency locking, bank routing, offline replay, reconciliation, and the Network Console are not implemented. The BankAdapter interface and Bank A are also not implemented. JWT logout is client-side token disposal only; server-side revocation and refresh tokens are not implemented. M1-3A persists payment intents only and does not claim final settlement.