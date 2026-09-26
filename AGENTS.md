# AGENTS.md — TransactX System Repository Rules & Architectural Invariants

> **TransactX is a simulated digital payment infrastructure research prototype.**
> It models distributed payment switch resilience, Merkle-tree accelerated reconciliation, adaptive routing, and offline-first payment capture.
> It does **NOT** process real money, implement UPI/NPCI, or connect to real bank APIs.

---

## 1. Forbidden Technologies & Scope Boundaries

1. **NO External Middleware / Infrastructure**:
   - **Do NOT introduce**: Kafka, Redis, Docker, Docker Compose, Kubernetes, RabbitMQ, Elasticsearch, AI/ML models, or blockchain.
   - Central state and operational metadata reside strictly in **PostgreSQL**.
2. **NO Synthetic / Fake Money**:
   - Never use floating-point numbers (`float32`, `float64`) for money.
   - All monetary balances and transfers are stored as **integer paise** (`BIGINT` in DB, `int64` in Go, exact integer strings in JS/TS).
3. **NO Demo Shortcuts**:
   - Never weaken unit or integration tests to force compilation or passing status.
   - Never fabricate benchmark or experiment results.
   - Never assume an unknown distributed outcome is a failure without status resolution.

---

## 2. Core Architectural & Financial Invariants

### Invariant 1: Central State Authority
PostgreSQL is authoritative for central monetary state, central double-entry ledger transactions, user registration, JWT authentication, user-scoped idempotency, health samples, route decisions, circuit states, and chaos scenarios.

### Invariant 2: Participant Boundary & Bank Ownership
Participant bank state (Bank A, Bank B) is strictly owned by the respective bank service and its isolated schema (`bank_a`, `bank_b`).
The `BankAdapter` interface (9 methods) is the domain boundary for bank participants. It does not expose SQL, HTTP types, or PostgreSQL transactions.

### Invariant 3: Idempotency Semantics
- User-scoped idempotency uniqueness is enforced by `(user_id, key)`.
- Same idempotency key + identical request payload = replay original payment result.
- Same idempotency key + different request payload = `409 IDEMPOTENCY_CONFLICT`.
- Retries of pending or uncertain bank operations **MUST** reuse the original stable operation ID.

### Invariant 4: Distributed Failure & Unknown Outcomes
- Network timeouts, lost responses, or `5xx` errors from participant banks are **UNKNOWN** outcomes (`bank.OperationPending` / `PENDING_RECONCILIATION`), **NOT** failures.
- An unknown bank outcome must be queried via `GetOperationStatus` using the original operation ID before attempting any compensation or settlement.
- Central-only recovery (`BANK_SETTLED_CENTRAL_PENDING`) repairs central PostgreSQL records without repeating bank monetary operations.

### Invariant 5: Read-Only Reconciliation
Reconciliation engines (Merkle tree commitments, root comparisons, divergence localization) are strictly **READ-ONLY** with respect to authoritative monetary state. Reconciliation code **MUST NEVER** silently alter account balances or post auto-correcting ledger transactions.

---

## 3. Operations & Chaos Controls

1. **OPS_ADMIN Only**: Chaos fault injection, circuit breaker state inspections/overrides, and health snapshot endpoints require authenticated `OPS_ADMIN` role. Customer and merchant roles must receive `403 Forbidden`.
2. **Operational Simulation Seam**: Chaos operates exclusively via `ChaosAdapter` decorating the `BankAdapter` / `HealthChecker` interfaces. It **NEVER** mutates central account balances, holds, or ledgers.

---

## 4. Git & Code Discipline

1. **Branch Hygiene**: Never force-push or rewrite shared remote branch history (`origin/main`, `origin/feat/*`).
2. **Commit Frequency**: Commit in logical, bounded slices with descriptive messages.
3. **Verification Before Claiming Success**: Never declare a feature or bug fix complete without executing the corresponding build and automated test suites (`go test ./...`, `npm run build`, `npm run test:money`, `npm run test:offline-queue`, `npm run test:offline-replay`).
