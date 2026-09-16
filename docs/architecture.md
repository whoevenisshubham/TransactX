# Architecture

## Status

### IMPLEMENTED

The Phase 0 foundation contains one Go HTTP API and one React frontend. The API connects to local PostgreSQL and exposes health/readiness checks. The frontend independently starts with Vite and checks API connectivity.

Phase 1A adds the PostgreSQL payment-core schema through explicit SQL migrations. PostgreSQL now provides the data foundation for users, banks, accounts, payments, idempotency records, and the double-entry ledger. The API still does not execute payments or run migrations automatically at startup.

Phase 1B adds Argon2id password hashing, HS256 JWT authentication, request IDs, server-side role checks, atomic user plus initial-account registration, authenticated profile/account reads, and safe recipient lookup. Public registration allows only CUSTOMER and MERCHANT. OPS_ADMIN is provisioned only by the development seed command.

M1-3B extends the authenticated payment foundation at `POST /api/payments` with user-scoped idempotency. M1-3C now settles a new valid payment through the explicit `LOCAL_SETTLEMENT` path in the same PostgreSQL transaction as its payment and idempotency rows: the sender is debited, the receiver is credited, one debit and one credit ledger entry are written, and the payment reaches `COMPLETED`. Exact retries return the original settled payment; reuse with a different hash returns `409 Conflict`. `ROUTING` and `PROCESSING` remain reserved for future bank-routed payments.

M1-3D experimentally verifies this local settlement boundary under deterministic PostgreSQL contention: conditional balance updates prevent negative balances and overspending, failed attempts roll back payment/ledger/idempotency work, successful transfers preserve double-entry and reconstructed-balance invariants, and concurrent same-key requests produce one logical settlement.

```text
PostgreSQL
    ↓
Go API
    ↓
React + TypeScript frontend
```

### PLANNED

M1-4 introduces the injected domain-level `backend/internal/bank.BankAdapter` seam. It defines account validation, debit, credit, and health operations with typed results and error classifications; it has no HTTP or PostgreSQL dependencies. The current handler injects `nil`, so the current payment flow does not invoke the adapter. Bank A and Bank B will eventually implement the same contract, but no routing logic is introduced in M1-4.

The adapter will own communication with a bank participant and bank-specific operation details. Payment Service will continue to own payment validation, idempotency, state transitions, and authoritative settlement orchestration. The current production path remains `Payment Service -> PostgreSQL LOCAL_SETTLEMENT`.

### FUTURE WORK

The M1-3D tests verify the current conditional `UPDATE` row-lock behavior for this local settlement path; they are not formal verification or production banking certification. Bank A, Bank B, bank routing, offline replay, reconciliation, and the Network Console are not implemented. M1-4 adds only the adapter abstraction; it does not complete routed settlement. JWT logout is client-side token disposal only; server-side revocation and refresh tokens are not implemented.