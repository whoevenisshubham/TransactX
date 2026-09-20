# Canonical reconciliation model

M3-1 defines the research-facing participant boundary and canonical ledger
record format. It does not build a Merkle tree or perform reconciliation.

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

Empty record collections sort to a non-nil empty slice. Tree-level empty-root
and bucket rules are deferred to M3-2.
