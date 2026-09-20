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
