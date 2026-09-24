# Canonical reconciliation model

M3-1 defines the research-facing participant boundary and canonical ledger
record format. M3-2 adds pure bucketed Merkle commitments; reconciliation
traversal and APIs remain out of scope.

`ReconciliationParticipant` is separate from the frozen `bank.BankAdapter` and
provides `GetRoot`, `GetChildren`, `GetRecords`, and `GetMetadata` for later
reconciliation layers.

## Participant implementations

The participant boundary is a research-facing read model. It is deliberately
not part of `bank.BankAdapter`: the adapter owns payment execution and status
lookup, while reconciliation reads a participant ledger and derives verifiable
metadata from it. Adding Merkle or traversal methods to the adapter would mix
these authority and lifecycle boundaries.

`MemoryParticipant` is a deterministic fixture. The caller supplies its
participant identity and canonical logical records; it creates no random
financial state, timestamps, or metrics. `RepositoryParticipant` wraps the
existing participant ledger snapshot service, converts persisted
`bank.LedgerEntry` values into `CanonicalRecord`, and materializes the shared
incremental Merkle state. The participant ledger remains authoritative; roots,
bucket records, and metadata are derived research state. An injected
`IncrementalCommitmentStore` can persist that derived state without becoming a
second ledger of truth.

## Scope and reference semantics

Scopes are normalized to UTC and use the half-open interval `[From, To)`.
Zero endpoints are unbounded for the deterministic in-memory fixture;
repository-backed reads require both endpoints. `To` must be after `From`.
The normalized endpoints form the stable `ScopeID` carried by node and bucket
references. A scope change therefore selects a distinct commitment, and a
record exactly at `To` is excluded.

Root results carry the participant identity, scope identity, commitment
generation, and deterministic node path. `GetChildren` returns commitment
nodes in level/index order. `GetRecords` accepts only a matching participant,
scope, generation, and serialized bucket identity, and returns records in the
frozen canonical order. Empty scopes have the explicit empty Merkle root and
no children. Malformed scopes or references return `ErrInvalidScope`,
`ErrInvalidNodeReference`, or `ErrInvalidBucketReference`; well-formed absent
nodes and buckets return `ErrNodeNotFound` or `ErrBucketNotFound`. A reference
from a different participant returns `ErrParticipantMismatch`, and a
reference from a superseded refresh returns `ErrStaleReference`.

Repository reads propagate context cancellation, deadlines, and source/store
errors. Normal reads use the initialized maintained commitment; they do not
silently rebuild from the authoritative ledger. `Initialize` and explicit
`Refresh` are the materialization/recovery operations and install a new
generation, invalidating prior references. This is local derived-state
maintenance and makes no distributed-transactionality claim.

Canonical version `v1` contains only these logical fields:

1. operation UUID
2. payment UUID
3. account UUID
4. entry type
5. signed integer amount in paise
6. currency
7. occurred-at timestamp

`CanonicalBytes` emits the ASCII header `TXCANON|v1`, then each field as a
uint32 big-endian byte length followed by UTF-8 bytes. UUIDs use their standard
lowercase string form, amounts use base-10 `int64` form, and timestamps use UTC
`RFC3339Nano`. Snapshot IDs, capture times, and physical database row IDs are
not canonical fields.

Records are ordered by UTC occurrence time, operation UUID, entry type, account
UUID, payment UUID, amount, and currency. `LeafHash` computes
`SHA-256("TXLEAF|v1|" || CanonicalBytes(record))`.

Empty record collections sort to a non-nil empty slice. M3-2 uses
`SHA-256("TXEMPTY|v1")` for empty buckets and bucket sets. Internal nodes use
`SHA-256("TXNODE|v1|" || left_hash || right_hash)`; an odd final child is
promoted unchanged. Bucket identities are UTC start time + explicit width +
logical partition, serialized under `TXBUCKET|v1|`. Bucket roots are sorted by
UTC start, partition, width, then serialized identity before the upper root is
computed. Commitment metadata records canonical/algorithm versions, bucket,
scope, root, and record count; the in-memory store is test/development only.

## Incremental maintenance

`BucketForRecord` maps `CanonicalRecord.OccurredAt` to the UTC bucket whose
start is `floor(unix_nanos / width) * width`. The interval is half-open: an
instant exactly at a bucket start belongs to that bucket, while an instant at
the scope end is excluded. The mapping is independent of the source timezone
and uses floor division for pre-epoch timestamps. A ledger fixes one logical
partition and width at construction time.

`IncrementalMerkleLedger.AppendRecord` adds a new logical record and
`UpsertRecord` replaces an existing logical record only within its current
bucket. A new bucket must arrive after the current ordered bucket frontier;
moving an existing record across buckets is rejected instead of triggering a
hidden global rebuild. Each normal operation sorts and hashes only the affected
bucket, then recomputes the corresponding parent path. Unaffected bucket roots
and sibling nodes are reused byte-for-byte. The returned instrumentation means:

- `BucketsRecomputed`: touched bucket roots recalculated (one per operation)
- `AncestorNodesRecomputed`: SHA-256 internal nodes recomputed on the path
- `BucketsReused`: existing buckets not touched by the operation
- `TotalRecordsConsidered`: records sorted/hashed in the affected bucket
- `ResultingRoot`: the resulting global commitment

`Bootstrap` is the separately named full rebuild for initial population or
recovery. It is never called by normal append/upsert operations; its rebuild
count and full-scan counters are exposed so tests can prove that separation.
`IncrementalCommitmentStore` persists only derived ordered bucket roots,
ancestor levels, canonical bucket records needed to continue derived updates,
global root, scope, logical partition, bucket width, versions, rebuild count,
and counts. `NewIncrementalMerkleLedgerFromState` / `Restore` validate all of
those fields, reconstruct bucket and record indexes plus the append frontier,
and never call `Bootstrap` or mutate authoritative participant ledger state.
State lookup is keyed by the complete `(partition, width, scope)` identity, so
commitments cannot be silently reused across configurations.

`Bootstrap` is serialized with `AppendRecord` and `UpsertRecord` by the
ledger's operation lock. A bootstrap therefore either completes before an
update begins or the update runs afterward; it cannot overwrite a concurrent
committed update. This is local in-memory synchronization, not distributed
transactionality.

## Reconciliation Engine & API (M3-5)

M3-5 implements the authoritative reconciliation runtime and REST API, building
on top of the M3-0 through M3-4 foundations.

### Run lifecycle

Every reconciliation run transitions through an explicit, durable lifecycle:

1. **RUNNING**: A durable record is inserted into `recon_runs` with a new UUID,
   the target participant, normalized UTC `Scope [From, To)`, and `started_at`
   timestamp.
2. **COMPLETED**: The tree comparison completes. If financial divergences are
   found, discrepancy records are written to `recon_discrepancies` and the run
   records the total `discrepancy_count`. The run is marked `COMPLETED`. A run
   with discrepancies is a successful operational execution that produced
   divergence evidence; it is not an error.
3. **FAILED**: Terminal state reached only on unrecoverable operational or
   infrastructure failures (such as database connectivity loss, malformed scope,
   or context cancellation).

```
   [POST /runs]
        |
        v
    (RUNNING)
     /      \
    / (ok)   \ (operational failure)
   v          v
(COMPLETED) (FAILED)
```

### Participant boundary & isolation

- The participant boundary is strictly isolated from central financial state.
- Central PostgreSQL is the authoritative financial ledger; bank participant
  state remains in the participant's domain and is accessed via the
  `ReconciliationParticipant` read model (`GetRoot`, `GetChildren`, `GetRecords`,
  `GetMetadata`).
- The engine rejects any request for an unknown participant ID with
  `ErrInvalidParticipant` before execution begins.
- Participant boundaries cannot be crossed: a run targets exactly one
  configured participant and compares only that participant's records against
  canonical state.

### True two-sided reconciliation architecture

Reconciliation in TransactX is a strictly two-sided comparison between two independent read models:

1. **Canonical Source**:
   - Authoritative central PostgreSQL financial ledger (`payment_bank_operations` joined with bank routing metadata via `CentralLedgerSnapshotSource`).
   - Represents TransactX's authoritative central recording of bank operations for the specified participant.
   - Built through `NewCentralRepositoryParticipant` in production or a separate fixture in tests.
   - BankAdapter is never used as or mixed into the canonical source.

2. **Participant Source**:
   - Bank participant ledger (`RepositoryParticipant` backed by participant PostgreSQL or `MemoryParticipant` in tests).
   - Selected strictly according to the requested participant (`BANK-A` reads Bank A; `BANK-B` reads Bank B).
   - Preserves complete participant boundary isolation.

3. **Normalized Half-Open Scope `[From, To)`**:
   - Both sources receive the exact same normalized UTC interval.
   - Roots are fetched independently via `canonical.GetRoot(ctx, scope)` and `participant.GetRoot(ctx, scope)`.
   - Both roots are persisted separately in `recon_runs` (`canonical_root` and `participant_root`).

4. **Two-Sided Tree Traversal Algorithm**:
   - If `canonicalRoot == participantRoot`, the run completes immediately with 0 discrepancies.
   - If unequal, a work queue is initialized with the root pair: `nodePair{canonical: canonicalRoot, participant: participantRoot}`.
   - For every pair:
     - `canonicalChildren := canonical.GetChildren(ctx, pair.canonical)`
     - `participantChildren := participant.GetChildren(ctx, pair.participant)`
     - Matching children are paired by deterministic node path.
     - Identical child hashes are skipped; differing hashes are enqueued for deeper traversal.
     - Unmatched nodes existing on only one side are immediately flagged as discrepancies.
     - Traversal continues exhaustively until the queue is empty; it **never** exits early after the first mismatch.
   - When a divergent bucket leaf is reached, the engine retrieves records from both sides (`canonical.GetRecords` and `participant.GetRecords`) and performs deterministic record-level diffing, detecting:
     - Missing participant records (`MISSING_PARTICIPANT_RECORD`)
     - Extra participant records (`EXTRA_PARTICIPANT_RECORD`)
     - Field-level attribute differences (`RECORD_MISMATCH`: amount, currency, entry type, payment ID, account ID, timestamp)

### Non-mutation guarantee

- Reconciliation is strictly read-only comparison: it **never** alters account
  balances, modifies payment states, settles funds, or silently repairs ledger
  values.
- Financial settlement and dispute resolution are separate domain processes
  outside reconciliation.

### Discrepancy model & evidence preservation

When the engine traverses Merkle commitment trees and finds mismatched nodes,
it walks down to bucket leaves to identify all divergent regions.

- Every mismatching region is preserved and persisted in `recon_discrepancies`
  (the comparison does not truncate at the first mismatch).
- Fields captured: `run_id`, `participant_id`, `bucket_key`, `bucket_partition`,
  `bucket_start`, `bucket_width_ns`, `expected_root`, `observed_root`,
  `mismatch_category`, `resolved`, and `evidence` (JSONB).
- Mismatch categories include:
  - `ROOT_MISMATCH`: Bucket root hash does not match canonical commitment.
  - `RECORD_MISMATCH`: Record count or contents diverge within a bucket.
  - `EXTRA_PARTICIPANT_RECORD`: Record exists on participant side but not in canonical ledger.
  - `MISSING_PARTICIPANT_RECORD`: Canonical record is missing from participant side.
- A discrepancy is evidence, never an HTTP 500 error.

### API surface

Reconciliation endpoints are accessible under `/api/ops/reconciliation/`:

| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/api/ops/reconciliation/runs` | Trigger a new reconciliation run for a participant and scope |
| `GET` | `/api/ops/reconciliation/runs` | List reconciliation runs with bounded pagination |
| `GET` | `/api/ops/reconciliation/runs/{runID}` | Get run details and summary metrics |
| `GET` | `/api/ops/reconciliation/runs/{runID}/discrepancies` | List discrepancies for a specific run |

#### Request body (`POST /runs`):
```json
{
  "participant_id": "BANK-A",
  "scope": {
    "from": "2026-09-01T00:00:00Z",
    "to": "2026-09-02T00:00:00Z"
  }
}
```

### Authorization

- All reconciliation endpoints require the `OPS_ADMIN` role via server-validated
  JWT bearer token (`auth.RequireRole(users.RoleOpsAdmin)`).
- Authenticated `CUSTOMER` and `MERCHANT` roles receive `HTTP 403 Forbidden`.
- Unauthenticated requests receive `HTTP 401 Unauthorized`.
- Arbitrary client SQL, table selection, or file paths are prohibited.

### Bounded reads & pagination

List endpoints enforce bounded queries with deterministic ordering:
- `GET /runs`: `limit` (default 20, max 100), `offset` (default 0).
  Ordered by `started_at DESC, id DESC`.
- `GET /runs/{runID}/discrepancies`: `limit` (default 50, max 200), `offset` (default 0).
  Ordered by `created_at ASC, id ASC`.

### What M3-5 implements

- Authoritative reconciliation orchestrator (`Engine`) executing against
  `ReconciliationParticipant` implementations.
- Durable PostgreSQL models and migrations (`recon_runs`, `recon_discrepancies`
  in migration `000013`).
- Bounded, authenticated OPS_ADMIN REST API endpoints matching TransactX envelope.
- Unit and HTTP handler test suites validating lifecycle, mismatch preservation,
  determinism, bounded pagination, and authorization.

### Future scope (M3-6+)

The following capabilities are reserved for later milestones and are **not**
part of M3-5:
- **M3-6**: Integrity Engine (verifiable proofs, consistency auditing across intervals)
- **M3-7**: Benchmark Harness (high-volume performance profiling)
- **M3-8**: Corruption Fixture (controlled injection of ledger anomalies for detection testing)
- **M3-9**: Merkle Visualization (interactive tree exploration in Network Console)
- **M3-10**: Automated Scheduled Reconciliation (continuous background cron orchestration)
