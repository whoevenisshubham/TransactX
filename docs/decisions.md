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

## ADR-017: Deterministic Health Observation

Status: **IMPLEMENTED**

M2-3 stores explicit `SUCCESS`, `FAILURE`, or `TIMEOUT` samples for stable execution-target IDs. The default rolling window is 15 minutes with at most 500 samples, one sample minimum, a 2-second timeout threshold, weights availability `0.35`, success `0.35`, latency `0.20`, and timeout `0.10`; latency is normalized between 10ms and 1000ms using nearest-rank p95. Scores clamp to `[0,1]`. Health is observational only: it cannot mutate payment state, balances, participant financial state, or the ledger. The read-only snapshot contract is `GET /api/ops/health/{targetID}` and is restricted to `OPS_ADMIN`; customer and merchant roles are denied. Legacy or future routing/circuit decisions must consume stable target IDs rather than account ownership.

## ADR-018: Deterministic Route Candidates and Genuine Execution Targets

Status: **IMPLEMENTED**

M2-4 selects legitimate switch-level route candidates without modifying logical bank ownership:
- **ExecutionTarget vs Bank Ownership**: `ExecutionTargetID` represents a switch-level route, rail, or endpoint path identity. It is strictly distinct from `sourceBankID` and `destinationBankID` account ownership, which remain immutable across all candidates. Routing never swaps or alters sender/receiver bank authority.
- **Multiple Genuine Execution Targets**: The live payment path supports multiple explicitly configured execution targets (e.g., `direct`, `RAIL-A`, `RAIL-B`) for the same logical source/destination bank pair, each providing genuine `SourceAdapter` and `DestinationAdapter` instances actually used during routed execution.
- **Runtime Modes**:
  - `STATIC`: Deterministically selects the configured baseline candidate ID (from `ROUTING_STATIC_BASELINE`) or falls back to alphabetical candidate ID order. If the baseline candidate is unavailable or not eligible, selection fails safely with `ErrNoRouteCandidate`.
  - `ADAPTIVE`: Queries authoritative M2-3 health snapshots for each candidate's `ExecutionTargetID`, excludes unavailable (`availabilityScore == 0`) and unhealthy (`score <= 0`) targets, compares scores, and selects the highest health score.
- **Deterministic Tie-Break**: Equal scores break ties stably by `ExecutionTargetID` ascending, then `CandidateID` ascending. Identical inputs produce identical decisions.
- **Route History & Durable Recovery**: Persists an immutable `PAYMENT_ROUTED` fact to `payment_route_decisions` containing payment ID, candidate ID, source/destination bank IDs, execution target ID, score, health snapshot JSON, reason code, mode, and timestamp. During pending payment recovery or duplicate processing, the durable route decision is queried to resolve the originally selected execution target's adapters. If the selected execution target is no longer configured, recovery fails closed and leaves the payment pending; it never blindly defaults to `targets[0]`.
- **Configuration**: Explicitly configured via `ROUTING_MODE`, `ROUTING_STATIC_BASELINE`, and `ROUTING_TARGETS` (delimited string or JSON). Configuration strictly requires `endpoint` as the dedicated health probe endpoint for `ExecutionTargetID` (missing or malformed health endpoints are rejected; execution endpoints are not silently substituted for health targets). Optional `sourceEndpoint` and `destinationEndpoint` specify execution adapters.
- **Circuit Breaker**: Integrates with M2-5 circuit breaker through `CircuitEligibility` hook.

## ADR-019: Deterministic Circuit Breaker and Gradual Recovery

Status: **IMPLEMENTED**

M2-5 implements a production-inspired, deterministic, thread-safe circuit breaker and gradual recovery architecture:
- **State Identity**: Circuit state belongs strictly to switch-level `ExecutionTargetID` (e.g. `RAIL-A`, `RAIL-B`). It does not belong to logical bank ownership, accounts, balances, payments, or ledger transactions. Circuit states of different execution targets are strictly isolated; tripping `RAIL-A` never affects `RAIL-B`.
- **State Machine & Transitions**:
  - `CLOSED`: Target is healthy and eligible for routing. Transitions to `OPEN` when failures in the rolling window reach `failureThreshold` (or timeouts reach `timeoutThreshold`).
  - `OPEN`: Target is excluded from routing. Transitions to `HALF_OPEN` when `now - openedAt >= openCooldown`.
  - `HALF_OPEN`: Target enters recovery probing state. Excluded from payment routing; the bounded probe budget applies to recovery health probes. Transitions to `CLOSED` when `successfulProbes >= successThreshold`. Transitions back to `OPEN` immediately if any recovery probe fails.
- **Concurrency & Probe Budget**: Thread-safe with per-target mutex protection. In `HALF_OPEN`, atomic probe budget counting ensures concurrent recovery health probes never exceed the configured probe limit. HALF_OPEN targets are excluded from payment routing; the bounded probe budget applies to recovery health probes. After successful recovery to CLOSED, payment traffic resumes with gradual restoration.
- **Gradual Restoration**: When a target transitions `HALF_OPEN -> CLOSED`, it does not instantly absorb 100% of traffic if multiple execution targets exist. Restoration progresses through discrete steps (`1..RestorationSteps`). At step $k$ of $N$, the target admits $\frac{k}{N}$ of traffic and deterministically sheds the remainder (`GRADUAL_RESTORATION_SHED`) to alternate healthy route candidates.
- **M2-4 Integration**: Plugs into `payments.SelectRoute` via the `CircuitEligibility` hook. The circuit breaker is purely an eligibility gate: it NEVER calls monetary `BankAdapter` operations (`Reserve`, `Settle`, `HoldFunds`, `ProvisionalCredit`) and NEVER alters central or participant balances.
- **Observability**: Every state transition emits an immutable `TransitionEvent` persisted to `circuit_transition_events` and recorded in an in-memory audit trail. Snapshots and transition history are exposed only via authenticated `OPS_ADMIN` endpoints (`/api/ops/circuit`, `/api/ops/circuit/{targetID}`, `/api/ops/circuit/{targetID}/events`). Public `CUSTOMER` and `MERCHANT` roles are rejected with `403 Forbidden`.

## ADR-020: Controlled Chaos Controller and Operational Fault Injection

Status: **IMPLEMENTED**

M2-6 implements a controlled, reversible, deterministic, target-isolated chaos controller for resilience engineering:
- **Operational Simulation Boundary**: Chaos injection is strictly operational simulation (`mode = "SIMULATION"`). Faults are injected only at the network/service communication seam via `ChaosAdapter`, which decorates `bank.BankAdapter` and `health.HealthChecker`. Chaos NEVER touches the central payment ledger, account balances, holds, or settlements, and NEVER creates fake payments or fake settlements. The frozen 9-method `BankAdapter` interface remains strictly unchanged with zero methods added or removed.
- **Four Supported Scenarios**:
  1. `BANK_OUTAGE`: Simulates target endpoint downtime (`ErrBankOutage`). Downstream health checks observe failures; monetary operations fail safely with `ErrCodeBankUnavailable`.
  2. `LATENCY`: Injects configurable, validated latency (1ms to 30s) via context-aware sleeps without uncontrolled thread blocking.
  3. `TRANSIENT_DROP` (alias `TRANSIENT`, `MESSAGE_DROP`): Controlled transient/request-drop simulation. Injects deterministic pre-call transient failure (`ErrTransientDrop`, error code `ErrCodeTransientFailure`) without executing the downstream call. It does not falsely claim to prove an unknown bank execution outcome (`UNKNOWN != FAILURE`). Genuine unknown outcomes remain handled exclusively by the existing M1/M2 saga recovery path (`bank.OperationPending` -> `PENDING_RECONCILIATION` -> `GetOperationStatus`).
  4. `TEMPORARY_PARTITION`: Simulates communication partition between the payment switch and the target (`ErrNetworkPartition`). Reversible upon stop, reset, or auto-expiry.
- **Target Isolation & Validation**: All scenarios are keyed strictly to switch-level `executionTargetID` (e.g. `RAIL-A`) and explicit non-bank health target IDs via `BuildTargetValidator(explicitHealthTargetIDs, executionTargetIDs, bankCodes...)`. Injected faults on target $X$ never impact target $Y$, alternate routes, unrelated banks, or unrelated payment flows. Target IDs are validated on start against genuine execution/health target identities; bank codes (e.g., `BANK-A`, `BANK-B`, `sourceBankID`, `destinationBankID`) are strictly rejected unless explicitly configured as target IDs, preventing conflation between logical bank ownership and switch-level execution target identity. Unknown targets return `ErrTargetNotFound`.
- **Fail-Closed Seam on Persistence Failure**:
  - `GetActiveFault(ctx, targetID)` returns `(*ChaosScenario, bool, error)`, strictly distinguishing `ErrScenarioNotFound` from real database failures.
  - If durable chaos state cannot be determined because the repository is unavailable, `GetActiveFault` returns `ErrRepoUnavailable` instead of falsely returning "no fault".
  - In `ChaosAdapter`, database unavailability causes the operational seam to fail closed (`bank.ErrCodeBankUnavailable` wrapping `ErrRepoUnavailable`) rather than silently letting traffic bypass an un-inspectable chaos boundary.
  - In `Start()`, durable active-state lookup must be trustworthy; if the repository query fails, `Start()` aborts and returns an error without activating an in-memory fault or persisting an inconsistent scenario.
- **Fatal Startup Hydration**: Active chaos state survives process restarts. On startup, `Hydrate()` queries active scenarios from PostgreSQL, atomically finalizing any discovered stale scenarios where `expires_at <= now` to inactive with `SYSTEM_AUTO_EXPIRY` and a single `CHAOS_EXPIRED` event, while loading unexpired active scenarios into memory cache. Similarly, `Start()` inspects durable storage for active rows on the target and atomically finalizes stale expired scenarios before starting new ones. If hydration fails due to a database or connection error, startup logs a fatal error and terminates (`os.Exit(1)`) rather than running with incomplete, partitioned, or un-hydrated chaos state.
- **Lifecycle & Exactly-Once Expiry Guarantees**:
  - `START`: Explicit start with a validated duration (100ms to 10m). Scenario state and `CHAOS_STARTED` audit event are persisted atomically in PostgreSQL before in-memory activation.
  - `INSPECT`: Inspect all active scenarios or a specific scenario by ID. Excludes expired scenarios (`expires_at <= now`).
  - `STOP`: Immediately and safely terminates the active fault, persisted atomically with `CHAOS_STOPPED`. Repeated stops are safe and idempotent. Never resurrects an expired scenario.
  - `RESET`: Clears active faults for a specific target or globally across all targets within a single atomic transaction. Crucially, `RESET` classifies active scenarios deterministically against current time:
    - Active unexpired scenarios (`expires_at > now`): set `stopped_at = now`, `stopped_by = requesting OPS_ADMIN`, and emit exactly one `CHAOS_RESET` event.
    - Already-expired scenarios (`expires_at <= now`): set `stopped_at = expires_at`, `stopped_by = "SYSTEM_AUTO_EXPIRY"`, and emit exactly one `CHAOS_EXPIRED` event (never `CHAOS_RESET`).
    - Exactly-once terminal lifecycle event: each scenario receives either `CHAOS_RESET` or `CHAOS_EXPIRED`, never both. Repeated resets against already inactive scenarios emit no new terminal events.
  - `AUTO-EXPIRY`: Evaluated against the unified boundary: `expires_at <= now => EXPIRED` (`!now.Before(expiresAt)`). Expired scenarios automatically become inactive in PostgreSQL and emit `CHAOS_EXPIRED` exactly once using a single atomic transaction with `WHERE scenario_id = $1 AND active = true AND expires_at <= $2 RETURNING scenario_id...`.
  - Expiry persistence errors are surfaced to callers (`GetScenario`, `ListScenarios`, `GetActiveFault`, `Start`). If durable expiry fails, in-memory state is NOT prematurely cleared, preserving memory/database consistency without duplicate expiry events.
- **Authorization Boundary**: All mutation and control endpoints (`/api/ops/chaos/start`, `/api/ops/chaos/scenarios/{scenarioID}/stop`, `/api/ops/chaos/reset`) and inspection endpoints require authenticated `OPS_ADMIN` role. Public roles (`CUSTOMER`, `MERCHANT`) receive `403 Forbidden`. Unauthenticated requests receive `401 Unauthorized`. Customer-facing payment APIs do NOT expose chaos controls.
- **Durable Persistence & Audit**: Scenario definitions are persisted to `chaos_scenarios`, and all lifecycle transitions (`CHAOS_STARTED`, `CHAOS_STOPPED`, `CHAOS_RESET`, `CHAOS_EXPIRED`) are appended to `chaos_events` atomically in the same PostgreSQL transaction. If database persistence fails, the operation fails fast and in-memory faults are not activated.
- **Integration with M2-3 / M2-4 / M2-5**:
  - M2-3 Health Monitoring samples targets through `ChaosAdapter.GetHealth()`, observing outages, elevated latency, or timeouts.
  - M2-5 Circuit Breaker observes health failure samples and trips `CLOSED -> OPEN` once the failure threshold is reached. `HALF_OPEN` remains recovery-health-probe-only and excludes payment routing.
  - M2-4 Adaptive Routing naturally routes eligible traffic around unhealthy/open targets to healthy alternate rails. Routing never fabricates fake rerouting or mutates sender/receiver bank authority.

## ADR-021: M3-1 Canonical Reconciliation Record

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
## ADR-022: M3-7 Runtime Financial Integrity Engine

Status: **IMPLEMENTED**

### Context
Reconciliation (M3-5) verifies consistency between canonical central ledger records and participant bank ledgers using Merkle commitments (M3-2..M3-4) and cryptographic proofs (M3-7-C1/C2). However, internal financial invariants must also be verifiable at runtime without relying solely on two-sided reconciliation runs.

### Decision
Implement an observational, read-only Runtime Financial Integrity Engine in `backend/internal/reconciliation`:

1. **Typed Check Registry**:
   - `DEBIT_CREDIT_CONSERVATION` (CRITICAL): Aggregate and per-transaction debit/credit balance.
   - `NON_NEGATIVE_BALANCES` (CRITICAL): Materialized and spendable account balances must be non-negative.
   - `TRANSACTION_UNIQUENESS` (CRITICAL): Uniqueness of payment identities and bank operation IDs.
   - `IDEMPOTENCY_MAPPING` (HIGH): Consistent mapping between users, keys, and payments.
   - `PAYMENT_STATE_VALIDITY` (HIGH): Adherence to the authoritative payment state machine.
   - `COMPLETED_PAYMENT_LEDGER_COMPLETENESS` (CRITICAL): Completed payments must possess balanced ledger rows.
   - `MERKLE_COMMITMENT_CONSISTENCY` (CRITICAL): Maintained Merkle commitment roots match recomputed records.

2. **Observational & Read-Only Guarantee**:
   Checks inspect current state. They never mutate balances, create missing rows, repair data, or alter routing.

3. **Status & Result Semantics**:
   - `PASS`: Invariant held.
   - `FAIL`: Financial or data invariant violated (recorded in `Violations`).
   - `ERROR`: Operational failure (database error, network partition). Never reported as `PASS`.
   - `NOT_APPLICABLE`: Check does not apply to the provided scope.

4. **State History Limitations**:
   Because TransactX does not maintain an immutable state transition log table in PostgreSQL, the engine validates current states and any explicitly recorded transition events without synthesizing or inventing historical transitions from log strings.

5. **Merkle Consistency Integration**:
   Uses accepted canonical records and `IncrementalMerkleLedger` to recalculate roots over the scope interval without duplicating hashing logic.
