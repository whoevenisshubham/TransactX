# Architecture Decisions

## ADR-001: No Docker

Status: **IMPLEMENTED**

Local development uses PostgreSQL, the Go backend, and the React frontend as independent local processes. Docker and Docker Compose are intentionally excluded.

## ADR-002: No Redis Initially

Status: **IMPLEMENTED**

Redis is not installed or configured. It may be reconsidered only when a concrete requirement justifies it.

## ADR-003: PostgreSQL Is Authoritative

Status: **IMPLEMENTED**

PostgreSQL is the source of truth for monetary state and financial transactions. Phase 1A creates the authoritative application schema through an explicit migration. The API does not automatically mutate the schema during startup.

## ADR-004: Integer Paise

Status: **IMPLEMENTED**

All monetary values use integer smallest currency units. For example, ₹100.50 is stored as `10050`. Monetary database columns use `BIGINT`; floating-point money is prohibited. The materialized account balance is `accounts.balance_paise`.

## ADR-005: Team Ownership

Status: **IMPLEMENTED**


## ADR-006: User-Scoped Idempotency

Status: **IMPLEMENTED**

Idempotency storage uniqueness is enforced by `(user_id, key)`. M1-3B hashes the canonical logical request fields (source account, normalized recipient identifier, amount in paise, and currency), creates the payment and idempotency record in one PostgreSQL transaction, returns the original payment for an exact retry, and returns `409 Conflict` for a different hash. The database uniqueness constraint resolves concurrent duplicate requests.

## ADR-007: Phase 1B Authentication

Status: **IMPLEMENTED**

Password hashes use Argon2id with `t=3`, `m=65536 KiB`, `p=2`, a 16-byte salt, and a 32-byte key. Access tokens use `github.com/golang-jwt/jwt/v5` with explicitly pinned HS256 signing and short-lived claims containing only identity, role, issued-at, expiry, and token ID. JWT secrets are supplied through environment configuration; there is no default secret. Logout disposes of the token in the client because server-side revocation is outside Phase 1B.

## ADR-008: Privileged Development Provisioning

Status: **IMPLEMENTED**

Public registration accepts only CUSTOMER and MERCHANT. OPS_ADMIN and the synthetic development bank are provisioned only through `backend/cmd/devseed`, which requires `APP_DEVELOPMENT=true` and a `DEV_ADMIN_PASSWORD` environment variable. The user and initial account are created atomically with the bank setup.

## ADR-009: M1-3B Idempotent Payment Intent Boundary

Status: **IMPLEMENTED**

M1-3B accepts the source account ID, recipient identifier, integer paise amount, currency, and required `Idempotency-Key` header. The authenticated JWT identifies the payer; ownership and active-account checks are server-side. A canonical request hash excludes authentication, timestamps, generated IDs, and JSON formatting. A validated payment is persisted as `CREATED` together with its user-scoped idempotency record in one explicit PostgreSQL transaction. Exact retries return `200` with the original payment; key reuse with a different hash returns `409`.

M1-3B's intent boundary was completed by M1-3C: new valid requests now settle synchronously. The idempotency record, payment state, account debit/credit, and double-entry ledger rows commit together. Exact retries return the completed payment without repeating settlement. Broader row-locking strategy and concurrency stress testing remain deferred to M1-3D.

## ADR-010: M1-3C Atomic Settlement

Status: **IMPLEMENTED**

`POST /api/payments` performs synchronous settlement for a new valid payment in one PostgreSQL transaction. The transaction inserts the payment, debits the active sender only when sufficient funds exist, credits the active receiver, creates one ledger transaction with one debit and one credit entry, transitions the payment through the centralized state machine to `COMPLETED`, and inserts the idempotency record. Any error rolls back all monetary and payment state. `accounts.opening_balance_paise` preserves the baseline needed for balance reconstruction; registration and development provisioning continue to initialize it to zero.

M1-3C uses the explicit `CREATED -> VALIDATING -> LOCAL_SETTLEMENT -> COMMITTED -> COMPLETED` path. `LOCAL_SETTLEMENT` identifies authoritative local PostgreSQL settlement without claiming that a bank route was selected or that bank processing occurred. The existing `ROUTING -> PROCESSING` path remains available for future BankAdapter-backed flows.

## ADR-011: M1-3D Concurrency Verification

Status: **IMPLEMENTED**

M1-3D keeps the existing conditional account debit update as the concurrency control boundary. Deterministic PostgreSQL integration tests verify no negative balance or double spending under 10-way and 75-way contention, exact debit/credit conservation and balance reconstruction, one logical settlement for concurrent same-key requests, and atomic rollback of failed attempts. These results are experimental verification of the exercised local path, not formal verification or production banking certification.

## ADR-012: M1-4 BankAdapter Contract

Status: **IMPLEMENTED**

M1-4 adds `backend/internal/bank.BankAdapter` as an injected domain-only boundary for future bank participants. The contract covers account validation, debit, credit, and health, and uses typed results plus error codes for insufficient funds, invalid or inactive accounts, bank unavailability, transient failures, and permanent business failures. Operation results carry payment and bank-operation correlation metadata. `PENDING` explicitly means the operation outcome is unknown or unresolved; it may have been accepted or committed, so the payment layer must not blindly repeat it before using correlation metadata and later status or reconciliation mechanisms.

The adapter does not expose SQL, PostgreSQL transactions, or HTTP types. Routed orchestration uses `HOLD -> PROVISIONAL_CREDIT -> CONFIRM_HOLD`; raw debit is retained only as a legacy primitive and is not part of routed execution. Operation status lookup is part of the frozen contract.

## ADR-013: M1-5 Simulated Bank A

Status: **IMPLEMENTED**

M1-5 adds `backend/internal/bank.BankA`, the in-process deterministic adapter test implementation. It now models durable-contract semantics in memory for adapter tests, including holds, provisional/final credits, compensation, operation identity, and participant ledger records. The deployed participant is the separate `backend/cmd/bank-a` process backed by the `bank_a` PostgreSQL schema.

Bank A is not a real financial institution and does not imply real external-bank connectivity.

## ADR-014: M1-6 Durable Routed Saga

Status: **IMPLEMENTED**

Routed payments persist source and destination bank identity, participant account identity, and one stable operation ID for every logical bank step. Central PostgreSQL and Bank A commit independently. The saga uses status lookup, explicit release/reversal compensation, `PENDING_RECONCILIATION`, and `BANK_SETTLED_CENTRAL_PENDING` rather than pretending to provide distributed ACID.

## ADR-015: M1-6 Unknown-Outcome Recovery

Status: **IMPLEMENTED**

An adapter timeout or lost response is not treated as proof of failure. A duplicate payment request or explicit `RecoverRoutedPayment` call loads the persisted operation, calls `GetOperationStatus` with the original operation ID, and either advances the saga, compensates a definite failure, or leaves the payment pending. Central-only recovery repairs central persistence without repeating bank monetary operations.

## ADR-016: M1-6 Bank Participant Persistence

Status: **IMPLEMENTED**

Bank A owns its accounts, balances, operation records, and ledger entries. Bank operation idempotency validates payment, operation identity, idempotency key, operation type, account, amount, currency, and related operation IDs. A retry with a conflicting payload is rejected.
