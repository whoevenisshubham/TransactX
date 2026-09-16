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

Status: **IMPLEMENTED FOR STORAGE**

Idempotency storage uniqueness is enforced by `(user_id, key)`. Phase 1A does not implement idempotency behavior: comparing request hashes, returning prior results, and rejecting conflicting payloads remain part of the payment slice.

## ADR-007: Phase 1B Authentication

Status: **IMPLEMENTED**

Password hashes use Argon2id with `t=3`, `m=65536 KiB`, `p=2`, a 16-byte salt, and a 32-byte key. Access tokens use `github.com/golang-jwt/jwt/v5` with explicitly pinned HS256 signing and short-lived claims containing only identity, role, issued-at, expiry, and token ID. JWT secrets are supplied through environment configuration; there is no default secret. Logout disposes of the token in the client because server-side revocation is outside Phase 1B.

## ADR-008: Privileged Development Provisioning

Status: **IMPLEMENTED**

Public registration accepts only CUSTOMER and MERCHANT. OPS_ADMIN and the synthetic development bank are provisioned only through `backend/cmd/devseed`, which requires `APP_DEVELOPMENT=true` and a `DEV_ADMIN_PASSWORD` environment variable. The user and initial account are created atomically with the bank setup.

## ADR-009: M1-3A Payment Intent Boundary

Status: **IMPLEMENTED FOR PAYMENT CREATION ONLY**

M1-3A accepts only the source account ID, recipient identifier, integer paise amount, currency, and optional `Idempotency-Key` header. The authenticated JWT identifies the payer; ownership and active-account checks are server-side. A validated payment is persisted as `CREATED` in an explicit PostgreSQL transaction. The centralized state machine records permitted future transitions.

Balance mutation, ledger entries, idempotency semantics, row locking/concurrency control, bank routing, retries, reconciliation, and final settlement are intentionally excluded from M1-3A. They remain M1-3B/M1-3C or later work and must not be inferred from a `CREATED` response.
