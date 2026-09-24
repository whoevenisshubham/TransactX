# TransactX

TransactX is a simulated payment infrastructure research prototype. It does not process real money, implement UPI/NPCI, or connect to real banks.

## Current status

Implemented through the current routed-payment milestone:

- Go HTTP API, React + TypeScript frontend shell, JWT authentication, accounts, recipients, and user-scoped idempotency.
- Atomic local PostgreSQL settlement with integer paise, double-entry ledger entries, and concurrency protection.
- A typed `BankAdapter` contract and durable Bank A/Bank B participant processes with independent PostgreSQL schemas, HTTP boundaries, holds, provisional/final credits, operation status, ledgers, and restart-safe operation identity.
- Routed saga persistence for source/destination bank identity and bank operations, deterministic retries, compensation, pending-operation recovery, and central-only recovery after bank settlement.
- Customer payment frontend and API contract: exact decimal-to-paise input, stable idempotency attempts, safe account/payment DTOs, notes, incoming/outgoing history, explicit pending status checks, and transaction details.
- Unit, PostgreSQL-backed integration, and race-detector coverage for the critical payment and bank paths.

Adaptive routing, circuit breakers, chaos orchestration, and offline queue UX are implemented. Reconciliation orchestration and the reconciliation API (M3-5) are implemented. Later M3 work (M3-6 and beyond) remains future work. Canonical commitment, Merkle bucket, incremental-maintenance, and participant read-boundary foundations are in production use by the reconciliation engine.

## Local development

Prerequisites:

- Go 1.27+
- Node.js and npm
- PostgreSQL 18 running locally

Create a local database named `transactx`, apply migrations in order with `psql`, and configure the processes. The repository does not load a `.env` file automatically.

```powershell
psql $env:DATABASE_URL -f backend/migrations/000001_phase1a_payment_core.up.sql
psql $env:DATABASE_URL -f backend/migrations/000002_account_opening_balance.up.sql
psql $env:DATABASE_URL -f backend/migrations/000003_local_settlement_state.up.sql
psql $env:DATABASE_URL -f backend/migrations/000004_m1_6_routed_payment_boundary.up.sql
psql $env:DATABASE_URL -f backend/migrations/000005_m1_6_account_identity_hardening.up.sql
psql $env:DATABASE_URL -f backend/migrations/000006_m1_6_bank_operation_identity.up.sql
psql $env:DATABASE_URL -f backend/migrations/000007_phase4_bank_b.up.sql
psql $env:DATABASE_URL -f backend/migrations/000008_m1_customer_payment_contract.up.sql
psql $env:DATABASE_URL -f backend/migrations/000009_m2_health_samples.up.sql
psql $env:DATABASE_URL -f backend/migrations/000010_m2_circuit_state.up.sql
psql $env:DATABASE_URL -f backend/migrations/000011_m2_circuit_snapshots.up.sql
psql $env:DATABASE_URL -f backend/migrations/000012_m2_chaos_scenarios.up.sql
psql $env:DATABASE_URL -f backend/migrations/000013_m3_5_recon_runs.up.sql
```

Start Bank A, Bank B, and the API in separate terminals. Both participants may use the same PostgreSQL server because they use separate `bank_a` and `bank_b` schemas.

```powershell
cd backend
$env:DATABASE_URL = "postgres://postgres@localhost:5432/transactx?sslmode=disable"
$env:BANK_A_DATABASE_URL = $env:DATABASE_URL
$env:BANK_A_ADDR = ":8081"
go run ./cmd/bank-a
```

```powershell
cd backend
$env:BANK_B_DATABASE_URL = $env:DATABASE_URL
$env:BANK_B_ADDR = ":8082"
go run ./cmd/bank-b
```

```powershell
cd backend
$env:DATABASE_URL = "postgres://postgres@localhost:5432/transactx?sslmode=disable"
$env:JWT_SECRET = "replace-with-at-least-32-random-bytes"
$env:DEFAULT_BANK_CODE = "BANK-DEV-001"
$env:BANK_A_URL = "http://localhost:8081"
$env:BANK_B_URL = "http://localhost:8082"
go run ./cmd/api
```

Start the frontend separately:

```powershell
cd frontend
npm install
npm run dev
```

For development seed data, set `APP_DEVELOPMENT=true` and `DEV_ADMIN_PASSWORD`, then run `go run ./cmd/devseed` from `backend`.
Normal API runs default to `APP_DEVELOPMENT=false`; enable development provisioning explicitly only when running the seed command.

## Design boundaries

- Central PostgreSQL owns payment state, idempotency, central accounts, central ledger, and recovery state.
- Bank A and Bank B independently own participant accounts, balances, holds, operations, participant ledger records, and operation status.
- Routed settlement is a durable saga, not a distributed ACID transaction. Unknown outcomes are resolved with the original operation ID; unresolved effects stay pending reconciliation.
- Monetary values are integer paise (`BIGINT`). Docker, Redis, Kafka, Kubernetes, real banking integrations, AI/ML, blockchain, and real-money movement are intentionally excluded.

## Reconciliation API (M3-5)

Four OPS_ADMIN-only endpoints expose the reconciliation engine. All requests require a valid JWT with `role: OPS_ADMIN`.

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/ops/reconciliation/runs` | Execute a reconciliation run for a participant and time scope |
| `GET` | `/api/ops/reconciliation/runs` | List reconciliation runs (paginated, optional `participantId` filter) |
| `GET` | `/api/ops/reconciliation/runs/{runID}` | Get a single run by UUID |
| `GET` | `/api/ops/reconciliation/runs/{runID}/discrepancies` | List discrepancy evidence for a run |

**Request body for `POST /api/ops/reconciliation/runs`:**

```json
{
  "participantId": "BANK-A",
  "scopeFrom": "2026-01-01T00:00:00Z",
  "scopeTo":   "2026-01-01T01:00:00Z"
}
```

`participantId` must be a known bank code configured at startup. Arbitrary IDs are rejected with 400. `scopeFrom` and `scopeTo` must be RFC 3339 timestamps; `scopeTo` must be strictly after `scopeFrom`.

**Run status values:**

- `RUNNING` — execution in progress
- `COMPLETED` — run finished normally; may contain zero or more discrepancies
- `FAILED` — operational execution failure prevented a meaningful comparison

A `COMPLETED` run with discrepancies is not a failure. Discrepancies are evidence, not HTTP 500s. `FAILED` is reserved for participant unavailability, configuration errors, or context cancellations.

**Mismatch categories** (on discrepancy objects):

- `BUCKET_ROOT_MISMATCH` — a Merkle bucket hash differs between canonical and participant side
- `CANONICAL_ROOT_MISMATCH` — the global commitment roots differ
- `PARTICIPANT_UNAVAILABLE` — a tree node could not be fetched
- `SCOPE_MISMATCH` — scope boundary incompatibility between the canonical model and participant
- `VERSION_INCOMPATIBLE` — canonical version or algorithm version is not supported

**Pagination:** All list endpoints accept `limit` (default 20, max 100 for runs; default 50, max 200 for discrepancies) and `offset` query parameters. The response includes `total` and `nextOffset` (when present).

**Migration:** Apply `backend/migrations/000013_m3_5_recon_runs.up.sql` before starting the server. This creates the `recon_runs` and `recon_discrepancies` tables.
