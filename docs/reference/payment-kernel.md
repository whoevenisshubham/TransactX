# Payment kernel and reconciliation reference

Preserved engineering reference from the pre-V3 README. For current setup, use
[local development](../runbooks/local-development.md) and apply all migrations.
Historical evidence below applies only to its recorded source.

## Design boundaries

- Central PostgreSQL owns payment state, idempotency, central accounts, central ledger, and recovery state.
- Bank A and Bank B independently own participant accounts, balances, holds, operations, participant ledger records, and operation status.
- Routed settlement is a durable saga, not a distributed ACID transaction. Unknown outcomes are resolved with the original operation ID; unresolved effects stay pending reconciliation.
- Monetary values are integer paise (`BIGINT`). Real banking integrations and real-money movement are excluded. Current optional investigator/analytics capabilities are described in the main README.

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
