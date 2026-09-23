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
