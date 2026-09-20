# Architecture Decisions

## ADR-001: No Docker

Status: **IMPLEMENTED**

Local development uses PostgreSQL, the Go backend, and the React frontend as independent local processes. Docker and Docker Compose are intentionally excluded.

## ADR-002: No Redis Initially

Status: **IMPLEMENTED**

Redis is not installed or configured. It may be reconsidered only when a concrete requirement justifies it.

## ADR-003: PostgreSQL Is Authoritative

Status: **IMPLEMENTED**

PostgreSQL is the source of truth for monetary state and financial transactions. Phase 1A creates the authoritative application schema through an explicit migration. The API does not automatically mutate the schema during startup.

## ADR-004: Integer Paise

Status: **IMPLEMENTED**

All monetary values use integer smallest currency units. For example, ₹100.50 is stored as `10050`. Monetary database columns use `BIGINT`; floating-point money is prohibited. The materialized account balance is `accounts.balance_paise`.

## ADR-005: Team Ownership

Status: **IMPLEMENTED**


## ADR-006: User-Scoped Idempotency

Status: **IMPLEMENTED**

Idempotency storage uniqueness is enforced by `(user_id, key)`. The canonical logical request includes the server-selected source account, normalized recipient identifier, integer paise amount, currency, and optional note. Exact retries replay the original payment; a different payload returns `409 Conflict`. Routed operation IDs are deterministic per payment and logical bank step, and a concurrent idempotency winner is recovered from the durable record before any bank call is repeated.

## ADR-007: Phase 1B Authentication

Status: **IMPLEMENTED**

Password hashes use Argon2id with `t=3`, `m=65536 KiB`, `p=2`, a 16-byte salt, and a 32-byte key. Access tokens use `github.com/golang-jwt/jwt/v5` with explicitly pinned HS256 signing and short-lived claims containing only identity, role, issued-at, expiry, and token ID. JWT secrets are supplied through environment configuration; there is no default secret. Logout disposes of the token in the client because server-side revocation is outside Phase 1B.

## ADR-008: Privileged Development Provisioning

Status: **IMPLEMENTED**

Public registration accepts only CUSTOMER and MERCHANT. OPS_ADMIN and the synthetic development bank are provisioned only through `backend/cmd/devseed`, which requires `APP_DEVELOPMENT=true` and a `DEV_ADMIN_PASSWORD` environment variable. The user and initial account are created atomically with the bank setup.

## ADR-009: M1-3B Idempotent Payment Intent Boundary

Status: **IMPLEMENTED**

M1-3B accepts the source account ID, recipient identifier, integer paise amount, currency, and required `Idempotency-Key` header. The authenticated JWT identifies the payer; ownership and active-account checks are server-side. A canonical request hash excludes authentication, timestamps, generated IDs, and JSON formatting. A validated payment is persisted as `CREATED` together with its user-scoped idempotency record in one explicit PostgreSQL transaction. Exact retries return `200` with the original payment; key reuse with a different hash returns `409`.

M1-3B's intent boundary was completed by M1-3C: new valid requests now settle synchronously. The idempotency record, payment state, account debit/credit, and double-entry ledger rows commit together. Exact retries return the completed payment without repeating settlement. Broader row-locking strategy and concurrency stress testing remain deferred to M1-3D.

## ADR-010: M1-3C Atomic Settlement

Status: **IMPLEMENTED**

`POST /api/payments` performs synchronous settlement for a new valid payment in one PostgreSQL transaction when no matching bank adapters are configured for both participant banks. The transaction inserts the payment, debits the active sender only when sufficient funds exist, credits the active receiver, creates one ledger transaction with one debit and one credit entry, transitions the payment through the centralized state machine to `COMPLETED`, and inserts the idempotency record. Any error rolls back all monetary and payment state. `accounts.opening_balance_paise` preserves the baseline needed for balance reconstruction; registration and development provisioning continue to initialize it to zero.

M1-3C uses the explicit `CREATED -> VALIDATING -> LOCAL_SETTLEMENT -> COMMITTED -> COMPLETED` path. `LOCAL_SETTLEMENT` identifies authoritative local PostgreSQL settlement without claiming that a bank route was selected. When both source and destination bank IDs have configured adapters, the payment uses the routed saga (`ROUTING -> PROCESSING`) instead.

## ADR-011: M1-3D Concurrency Verification

Status: **IMPLEMENTED**

M1-3D keeps the existing conditional account debit update as the concurrency control boundary. Deterministic PostgreSQL integration tests verify no negative balance or double spending under 10-way and 75-way contention, exact debit/credit conservation and balance reconstruction, one logical settlement for concurrent same-key requests, and atomic rollback of failed attempts. These results are experimental verification of the exercised local path, not formal verification or production banking certification.

## ADR-012: M1-4 BankAdapter Contract

Status: **IMPLEMENTED**

M1-4 freezes `backend/internal/bank.BankAdapter` as the injected domain-only boundary for bank participants. The contract covers:

`GetHealth`, `ResolveAccount`, `HoldFunds`, `ProvisionalCredit`, `ConfirmHold`, `ReleaseHold`, `ReverseProvisionalCredit`, `GetOperationStatus`, `GetLedgerSnapshot`.

Typed results and error codes cover insufficient funds, invalid or inactive accounts, bank unavailability, transient failures, and permanent business failures. Operation results carry payment and bank-operation correlation metadata. `PENDING` explicitly means the operation outcome is unknown or unresolved; it may have been accepted or committed, so the payment layer must not blindly repeat it before using correlation metadata and `GetOperationStatus`.

The adapter does not expose SQL, PostgreSQL transactions, or HTTP types. Routed orchestration uses `HOLD -> PROVISIONAL_CREDIT -> CONFIRM_HOLD -> finalize credit`; raw debit/credit helpers may remain for compatibility tests but are not the routed protocol. Operation results must correlate to the requested payment and operation IDs; unknown or malformed statuses remain pending and never count as success. Bank HTTP errors use typed safe codes/messages.

## ADR-013: M1-5 Simulated Bank A

Status: **IMPLEMENTED**

M1-5 adds `backend/internal/bank.BankA`, the in-process deterministic adapter test implementation. It now models durable-contract semantics in memory for adapter tests, including holds, provisional/final credits, compensation, operation identity, and participant ledger records. The deployed participant is the separate `backend/cmd/bank-a` process backed by the `bank_a` PostgreSQL schema.

Bank A is not a real financial institution and does not imply real external-bank connectivity.

## ADR-014: M1-6 Durable Routed Saga

Status: **IMPLEMENTED**

Routed payments persist source and destination bank identity, participant account identity, and one stable operation ID for every logical bank step. Central PostgreSQL and Bank A commit independently. The saga uses status lookup, explicit release/reversal compensation, `PENDING_RECONCILIATION`, and `BANK_SETTLED_CENTRAL_PENDING` rather than pretending to provide distributed ACID.

## ADR-015: M1-6 Unknown-Outcome Recovery

Status: **IMPLEMENTED**

An adapter timeout or lost response is not treated as proof of failure. A duplicate payment request or explicit `RecoverRoutedPayment` call loads the persisted operation, calls `GetOperationStatus` with the original operation ID, and either advances the saga, compensates a definite failure, or leaves the payment pending. Central-only recovery repairs central persistence without repeating bank monetary operations.

## ADR-016: M1-6 Bank Participant Persistence

Status: **IMPLEMENTED**

Bank A owns its accounts, balances, operation records, and ledger entries. Bank operation idempotency validates payment, operation identity, idempotency key, operation type, account, amount, currency, and related operation IDs. A retry with a conflicting payload is rejected.

## ADR-017: M3-1 Canonical Reconciliation Record

Status: **IMPLEMENTED**

M3-1 freezes canonical reconciliation records at version `v1`. A canonical
record contains exactly these logical fields, in this serialization order:

1. operation UUID
2. payment UUID
3. account UUID
4. entry type
5. signed integer amount in paise
6. currency
7. occurred-at timestamp

The serializer emits the ASCII header `TXCANON|v1`, followed by each field as
a uint32 big-endian byte length and UTF-8 bytes. UUIDs use their standard
lowercase string form. Amounts use base-10 `int64` text with no padding or
floating-point representation. Currency and entry type are preserved as the
logical participant values, including case; no locale or implicit formatting
is applied. Timestamps are converted to UTC and formatted with
`time.RFC3339Nano`.

Leaf hashing is domain-separated and exact:

`SHA-256("TXLEAF|v1|" || canonical_bytes)`

Before future Merkle construction, records are ordered by UTC occurrence time,
operation UUID, entry type, account UUID, payment UUID, amount, and currency.
This total logical ordering does not use database row IDs or other physical
storage metadata. Snapshot IDs, snapshot capture timestamps, and physical row
IDs are excluded from canonical content and cannot affect a leaf hash.

Bank participants must produce byte-for-byte identical canonical bytes and leaf
hashes for logically identical records, regardless of participant, database,
snapshot, or time-zone representation. Any incompatible change to fields,
ordering, encoding, normalization, or the domain separator requires a new
canonical version and an explicit compatibility decision; version `v1` remains
readable and verifiable for existing commitments.
