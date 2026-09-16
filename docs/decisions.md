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
