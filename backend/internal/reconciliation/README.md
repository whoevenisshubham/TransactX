# Canonical reconciliation model

M3-1 defines the research-facing participant boundary and canonical ledger
record format. M3-2 adds pure bucketed Merkle commitments; reconciliation
traversal and APIs remain out of scope.

`ReconciliationParticipant` is separate from the frozen `bank.BankAdapter` and
provides `GetRoot`, `GetChildren`, `GetRecords`, and `GetMetadata` for later
reconciliation layers.

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
ancestor levels, global root, scope, versions, and counts. It does not mutate
authoritative participant ledger state and does not claim financial transaction
atomicity.
