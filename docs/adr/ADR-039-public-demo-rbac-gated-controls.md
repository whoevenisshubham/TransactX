# ADR-039: Public Demo RBAC and Gated Operational Controls

## Status
**IMPLEMENTED** (Task B5 complete)

## Context
TransactX operates as a high-integrity distributed transaction platform supporting both controlled public demonstrations and production deployments. In previous milestones:
1. Operational endpoints lacked role granularity: all operations were either restricted strictly to `OPS_ADMIN` or lacked a dedicated read-only role for auditors, monitoring dashboards, and demo observers.
2. Self-registration risked privilege escalation if user role assignments were not strictly bounded.
3. Simulation and testing capabilities (such as chaos fault injection, ledger corruption fixtures, and manual recovery triggers) could inadvertently remain accessible if not explicitly governed by configuration gates.
4. Inter-service HTTP communication between the TransactX core engine and external/internal bank participants lacked mandatory authenticated service tokens in production environments.

## Decisions

### 1. Strict Public Role Boundary and Escalation Prevention
- Public self-registration (`POST /api/auth/register`) strictly permits only public participant roles: `CUSTOMER` and `MERCHANT`.
- Attempts to register with privileged roles (`OPS_ADMIN`, `OPS_VIEWER`, or arbitrary administrative strings) are rejected immediately with `400 Bad Request`.
- Privileged accounts (`OPS_ADMIN`, `OPS_VIEWER`) may only be provisioned through the controlled `backend/cmd/devseed` tool (in development) or authoritative infrastructure seeding migrations.

### 2. Four-Role RBAC Model with `OPS_VIEWER`
The application implements four distinct user roles:
- **`CUSTOMER` & `MERCHANT`**: End-user participant roles. Strictly forbidden (`403 Forbidden`) from accessing any `/api/ops/*` endpoint.
- **`OPS_VIEWER`**: Dedicated read-only operational role. Permitted to inspect system health, circuit breaker state, chaos scenario catalogs, routing distributions, reconciliation runs, and runtime status (`GET /api/ops/*`). Denied (`403 Forbidden`) from triggering mutations, chaos injection, corruption fixtures, or manual reconciliation runs.
- **`OPS_ADMIN`**: Full administrative operational role. Authorized for both operational telemetry inspection and operational state mutations.

### 3. Gated Control Flags
To safely support public demos while guaranteeing production isolation, three explicit runtime control flags are introduced:
- `ALLOW_REGISTRATION` (`TX_ALLOW_REGISTRATION`): Governs public self-registration. When false, `POST /api/auth/register` returns `403 Forbidden` (`REGISTRATION_DISABLED`).
- `CHAOS_ENABLED` (`TX_CHAOS_ENABLED`): Governs fault injection, chaos scenario execution, and corruption fixtures. When false, chaos endpoints return `403 Forbidden` (`CHAOS_DISABLED`).
- `RECOVERY_CONTROL_ENABLED` (`TX_RECOVERY_CONTROL_ENABLED`): Governs operator-triggered manual recovery dispatches.
- **Environment Defaults**: In development (`APP_DEVELOPMENT=true`), these flags default to `true` for developer agility. In production (`APP_DEVELOPMENT=false`), they default to `false` (fail-safe closed).
- **Runtime Telemetry**: The exact authoritative boolean state of these flags is exposed via `GET /api/ops/runtime`.

### 4. Append-Only Operational Audit Logging
- Migration 000023 introduces `operational_audit_logs`.
- Every operational mutation (chaos start, stop, reset, corruption fixture execution, manual recovery actions) records an immutable audit entry capturing `actor_id`, `actor_role`, `operation`, `entity_type`, `entity_id`, `details`, `request_id`, `trace_id`, and `result` (`SUCCESS` or `FAILURE`).

### 5. Participant Inter-Service Service Tokens
- Banking participant endpoints (e.g., operation presence checks, ledger snapshots, dispatch) require service token verification via the `X-Service-Token` header or `Authorization: Bearer <token>`.
- In production (`APP_DEVELOPMENT=false`), `config.Load()` enforces that `BANK_A_SERVICE_TOKEN` and `BANK_B_SERVICE_TOKEN` must be non-empty whenever the respective bank service URL is configured; missing tokens fail startup immediately.
- Bank client adapters attach the configured service token to all outbound participant requests.

## Consequences
- **Positive**: Complete auditability of operational actions; safe public demo exposure without risking mutation or chaos execution; strict prevention of privilege escalation; hardened inter-service boundary between TransactX and banking rails.
- **Trade-offs**: Read-only demo observers require an `OPS_VIEWER` credential rather than anonymous access. Production deployments must provision service tokens across bank participant services.
