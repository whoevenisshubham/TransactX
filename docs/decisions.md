# Architecture Decisions

## ADR-001: No Docker

Status: **IMPLEMENTED**

Local development uses PostgreSQL, the Go backend, and the React frontend as independent local processes. Docker and Docker Compose are intentionally excluded.

## ADR-002: No Redis Initially

Status: **IMPLEMENTED**

Redis is not installed or configured. It may be reconsidered only when a concrete requirement justifies it.

## ADR-003: PostgreSQL Is Authoritative

Status: **PLANNED**

PostgreSQL will be the source of truth for monetary state and financial transactions. Phase 0 currently proves only the database connection.

## ADR-004: Integer Paise

Status: **PLANNED**

All monetary values will use integer smallest currency units. For example, ₹100.50 is stored as `10050`. Database monetary columns will use `BIGINT`; floating-point money is prohibited.

## ADR-005: Team Ownership

Status: **IMPLEMENTED**

- M1 owns the BankAdapter interface and Bank A.
- M2 owns Bank B, bank health, adaptive routing, circuit breaker, and chaos engineering.
- M3 owns reconciliation, integrity, benchmarking, and research-console functionality.

## ADR-006: User-Scoped Idempotency

Status: **PLANNED**

Idempotency uniqueness will be enforced by `(user_id, idempotency_key)`. The same user and key with the same request payload will return the original logical result. A different payload will produce an idempotency conflict.

The complete implementation is intentionally deferred until the payment slice.