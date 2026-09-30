# TransactX

TransactX is a simulated payment infrastructure research prototype. It does not process real money, implement UPI/NPCI, or connect to real banks.

## Current status

Implemented release-candidate subsystems:

- Go HTTP API, React + TypeScript frontend shell, JWT authentication, accounts, recipients, and user-scoped idempotency.
- Atomic local PostgreSQL settlement with integer paise, double-entry ledger entries, and concurrency protection.
- A typed `BankAdapter` contract and durable Bank A/Bank B participant processes with independent PostgreSQL schemas, HTTP boundaries, holds, provisional/final credits, operation status, ledgers, and restart-safe operation identity.
- Routed saga persistence for source/destination bank identity and bank operations, deterministic retries, compensation, pending-operation recovery, and central-only recovery after bank settlement.
- Customer payment frontend and API contract: exact decimal-to-paise input, stable idempotency attempts, safe account/payment DTOs, notes, incoming/outgoing history, explicit pending status checks, and transaction details.
- Durable, independently owned canonical and participant Merkle commitments, explicit operator maintenance, stable generations across process and PostgreSQL restarts, compatible proof round trips, and read-only financial integrity checks.
- Unit, PostgreSQL-backed integration, race-detector, frontend, and CI coverage for the critical payment, bank, reconciliation, proof, and integrity paths.

Adaptive routing, circuit breakers, chaos orchestration, and offline queue UX are implemented. M3 reconciliation, proofs, runtime integrity, durable execution metrics, the operational activity feed, and resilient live console invalidation are implemented. Canonical commitment, Merkle bucket, incremental-maintenance, and participant read-boundary foundations are in production use by the reconciliation engine.

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
psql $env:DATABASE_URL -f backend/migrations/000010_m2_route_decisions.up.sql
psql $env:DATABASE_URL -f backend/migrations/000011_m2_circuit_transitions.up.sql
psql $env:DATABASE_URL -f backend/migrations/000012_m2_chaos_scenarios.up.sql
psql $env:DATABASE_URL -f backend/migrations/000013_m3_5_recon_runs.up.sql
psql $env:DATABASE_URL -f backend/migrations/000014_m3_7_integrity_runs.up.sql
psql $env:DATABASE_URL -f backend/migrations/000015_m3_7_integrity_violations.up.sql
psql $env:DATABASE_URL -f backend/migrations/000016_m3_7_payment_state_transitions.up.sql
psql $env:DATABASE_URL -f backend/migrations/000017_m3_7_merkle_commitments.up.sql
psql $env:DATABASE_URL -f backend/migrations/000018_m3_reconciliation_metrics.up.sql
psql $env:DATABASE_URL -f backend/migrations/000019_m3_reconciliation_discrepancy_categories.up.sql
psql $env:DATABASE_URL -f backend/migrations/000020_m3_commitment_owners.up.sql
psql $env:DATABASE_URL -f backend/migrations/000021_payment_transition_order.up.sql
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
$env:DEFAULT_BANK_CODE = "BANK-A"
$env:BANK_A_CODE = "BANK-A"
$env:BANK_B_CODE = "BANK-B"
$env:BANK_A_URL = "http://localhost:8081"
$env:BANK_B_URL = "http://localhost:8082"
$env:TX_SIMULATION_MODE = "true"
$env:TX_PARTICIPANT_PROVISIONING = "true"
$env:CORS_ALLOWED_ORIGINS = "http://localhost:5173"
go run ./cmd/api
```

Start the frontend separately:

```powershell
cd frontend
npm ci
npm run dev
```

For development seed data, set `APP_DEVELOPMENT=true`, `DEFAULT_BANK_CODE=BANK-A`, and `DEV_ADMIN_PASSWORD`, then run `go run ./cmd/devseed` from `backend`. The command provisions both configured banks and attaches the development administrator account to `DEFAULT_BANK_CODE`, so the two-bank API configuration above starts without additional database setup.
Normal API runs default to `APP_DEVELOPMENT=false`; enable development provisioning explicitly only when running the seed command.

Simulation participant provisioning is opt-in. `TX_PARTICIPANT_PROVISIONING=true` requires `TX_SIMULATION_MODE=true`. Registration then creates the central account and its matching Bank A or Bank B participant account in one PostgreSQL transaction. Provisioning uses the same account UUID on both sides and is retry safe. New accounts start with zero balance.

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

**Migrations:** Apply `backend/migrations/000013_m3_5_recon_runs.up.sql`, `000018_m3_reconciliation_metrics.up.sql`, and `000019_m3_reconciliation_discrepancy_categories.up.sql` in sequence. They create the run/evidence tables, add durable execution metrics, and allow the record-level discrepancy categories emitted by the engine.

Completed runs expose `elapsedNs`, `nodesVisited`, `recordsInspected`, `bytesExamined`, `divergentBuckets`, and `divergentRecords`. `bytesExamined` is the exact number of Merkle hash bytes and canonical serialized record bytes inspected in process. It does not claim physical network traffic. Equal-root runs count the two returned root commitments, their hash bytes, and zero record scans.

## Reconciliation participant boundary

The reconciliation package exposes a separate `ReconciliationParticipant` read boundary. `BankAdapter` remains frozen as the payment-switch contract and is not extended with reconciliation methods.

- A `Scope` is a normalized UTC half-open interval `[From, To)`. Node references carry a participant identifier, deterministic scope identity, and a path (`empty` or `L<level>/<index>`); bucket references carry the versioned `BucketID.String()` key.
- `MemoryParticipant` is a deterministic fixture over logical `CanonicalRecord` values. `RepositoryParticipant` reads the existing participant ledger through `bankservice.Service.GetLedgerSnapshot`; the participant ledger remains authoritative and commitment state is derived, read-only data.
- Canonical and participant owners maintain separate derived commitments for the same logical bank partition and closed scope. Normal reconciliation, tree, proof, and integrity reads load `merkle_commitments`; they never call `Bootstrap` or scan the full ledger. Missing maintained state returns an operational error.
- Root, node, and bucket references include a durable commitment generation. Maintenance creates a new generation and makes the prior reference stale. PostgreSQL restart restores the exact stored generation and root. Generation metadata never enters canonical records or Merkle hash inputs.
- Roots, child nodes, and bucket records use the existing canonical v1 and Merkle v1 implementations. Results are ordered deterministically and exclude snapshot IDs, capture times, and physical database row IDs from canonical content.
- Missing or malformed references return typed reconciliation errors (`ErrNodeNotFound`, `ErrBucketNotFound`, and their invalid-reference counterparts). Participant mismatches are explicit. Every participant method checks and propagates context cancellation and source errors.

## Commitment maintenance and proof verification

Choose a completed half-open UTC scope `[scope-from, scope-to)` and maintain each configured bank explicitly after its participant service is available:

```powershell
cd backend
go run ./cmd/maintain-merkle `
  -database-url $env:DATABASE_URL `
  -participant BANK-A `
  -bank-url http://localhost:8081 `
  -scope-from 2026-09-29T00:00:00Z `
  -scope-to 2026-09-29T01:00:00Z
```

The command reads the authoritative central and participant ledgers and emits both owners, scope, generation, root, record count, canonical version, and algorithm version. Run it separately for Bank B. It replaces the maintained scope atomically; it does not change monetary authority.

`GET /api/ops/reconciliation/proof/{operationId}` returns a lower-camel-case hex DTO. POST that JSON unchanged to `/api/ops/reconciliation/proof/verify`. Verification reloads the trusted maintained root server-side and rejects wrong participant, scope, operation, version, root, or stale generation. A stale generation returns `409 Conflict`.

## Integrity lifecycle policy

Successful central settlement, completed routed recovery, reconciliation completion, and selected chaos recovery events trigger a background scan after a two-second debounce. The scan runs the six bounded financial checks: debit/credit conservation, non-negative balances, transaction uniqueness, idempotency mapping, payment-state validity, and completed-payment ledger completeness. Full `MERKLE_COMMITMENT_CONSISTENCY` remains an explicit operator action because it recomputes authoritative records. All checks are read-only. If an integrity result cannot be durably stored, the run is marked failed and HTTP returns `503 INTEGRITY_PERSISTENCE_UNAVAILABLE`.

The OPS runtime endpoint and console expose simulation mode. Credentialed CORS accepts only the comma-separated origins in `CORS_ALLOWED_ORIGINS`; wildcard origins are rejected during configuration loading.

The dedicated PostgreSQL restart gate is run against the isolated database `transactx_m3_restart`:

```powershell
cd backend
.\scripts\verify-merkle-postgres-restart.ps1 `
  -DatabaseUrl "postgres://postgres@127.0.0.1:55432/transactx_m3_restart?sslmode=disable" `
  -PostgresDataDirectory "C:\path\to\isolated\postgres-data"
```

## RC1 evidence checkpoint

The release evidence source checkpoint is
`8e1efb0c463aab539b417de87b6aa38a84fc8fe8`. The current scale,
PostgreSQL concurrency, M2 resilience, and offline artifacts are stored under
`artifacts/experiments/` and `artifacts/rc1-8e1efb0/`. Exact hashes,
environment details, measured results, and limitations are recorded in
`docs/reconciliation-experiments.md` and `docs/resilience-experiments.md`.
