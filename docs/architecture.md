# Architecture

## Status

### IMPLEMENTED

The Phase 0 foundation contains one Go HTTP API and one React frontend. The API connects to local PostgreSQL and exposes health/readiness checks. The frontend independently starts with Vite and checks API connectivity.

Phase 1A adds the PostgreSQL payment-core schema through explicit SQL migrations. PostgreSQL now provides the data foundation for users, banks, accounts, payments, idempotency records, and the double-entry ledger. The API still does not execute payments or run migrations automatically at startup.

```text
PostgreSQL
    ↓
Go API
    ↓
React + TypeScript frontend
```

### PLANNED

The payment service will eventually sit behind the API and depend on a BankAdapter interface. M1 owns BankAdapter and Bank A. M2 owns Bank B and resilience behavior. M3 owns reconciliation, integrity, and research functionality.

### FUTURE WORK

Authentication, payment processing, offline replay, routing, reconciliation, and the Network Console are not implemented. The BankAdapter interface and Bank A are also not implemented. Phase 1A only establishes their database foundation.