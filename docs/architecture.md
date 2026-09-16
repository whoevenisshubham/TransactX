# Architecture

## Status

### IMPLEMENTED

The Phase 0 foundation contains one Go HTTP API and one React frontend. The API connects to local PostgreSQL and exposes health/readiness checks. The frontend independently starts with Vite and checks API connectivity.

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

Authentication, payment processing, offline replay, routing, reconciliation, and the Network Console are not implemented in Phase 0.