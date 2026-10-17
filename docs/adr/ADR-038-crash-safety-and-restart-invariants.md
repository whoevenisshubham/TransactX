# ADR-038: Crash Safety, Fault Boundaries, and Event Restart Invariants

## Status
**IMPLEMENTED** (Task B12 complete)

## Context
TransactX Milestone 2 requires formal empirical proof that the unified recovery architecture (B1, B2, B3), native worker runtime (B4), transactional outbox (B6), and projection pipeline (B7) maintain financial and platform invariants across all operational failure boundaries.

Specifically, the system must guarantee:
1. **Financial Invariants Under Chaos**: Non-negative balances, exact double-entry debit/credit conservation ($\sum \text{debits} = \sum \text{credits}$), single ledger transaction per payment, and strict participant operation idempotency across arbitrary mid-saga process kills.
2. **Presence-Supported Replay**: Distinguish true participant absence from unknown/present outcomes, safely replaying only when absence is affirmatively proven, and continuing without duplicate credit when presence is detected.
3. **Lease Stealing & Worker Resilience**: Recover cleanly when a worker process terminates abruptly holding an unexpired or expired lease, permitting a secondary worker to claim and continue without split-brain anomalies or duplicate execution.
4. **Pool & Resource Constriction**: Tolerate connection kills (`pg_terminate_backend`) and connection pool exhaustion without connection leaks, deadlocks, or unhandled panics.
5. **Event Restart & Outbox Delivery Invariants**: Ensure transactional outbox facts are generated atomically with state changes, tolerate dispatcher crashes, deduplicate events at the projection consumer inbox, guard against out-of-order state regression using `event_version`, quarantine poison pills without pipeline starvation, and support atomic projection rebuild.

## Decisions

### 1. Deterministic Fault Matrix Architecture
Implemented in `backend/internal/experiments/crash_matrix.go` and surfaced via CLI in `backend/cmd/experiments/main.go`, the test harness exercises 20 deterministic scenarios against a live PostgreSQL 18 instance with fixed seed (42):

#### Recovery Fault Boundaries (REC-01 to REC-14)
- **REC-01 (Pre-Hold Kill)**: Payment persisted in `VALIDATING`; process killed before initiating source hold. Recovery acquires session advisory lock and converges payment to `COMPLETED`.
- **REC-02 (Mid-Hold Network Loss)**: Source hold dispatched to Bank A; process killed before central response processing. Recovery inspects operation status, verifies hold, and completes saga.
- **REC-03 (Post-Hold Interruption)**: Bank A hold recorded; process killed before Bank B provisional credit dispatch. Recovery advances saga through provisional credit, confirmation, and settlement.
- **REC-04 (Mid-Credit Crash)**: Destination provisional credit dispatched to Bank B; process killed before response recording. Recovery resolves participant status and completes transfer.
- **REC-05 (Pre-Confirm Interruption)**: Destination credit confirmed; process killed before source hold confirmation. Recovery confirms hold and settles centrally.
- **REC-06 (Pre-Ledger Kill)**: Bank operations confirmed; process killed before central ledger write. Recovery executes central settlement and emits ledger entries.
- **REC-07 (Post-Ledger Race)**: Central ledger committed; process killed before client receives HTTP response. Concurrent client duplicate request (`CreateWithResult`) races worker recovery (`Service.Resume`); session advisory lock serializes callers, returning identical `COMPLETED` result with zero duplicate ledger entries.
- **REC-08 (Lease Expiry & Steal)**: Worker crashes holding an unexpired recovery job lease. Second worker waits for lease duration expiry, steals the claim via `FOR UPDATE SKIP LOCKED`, and drives payment to completion.
- **REC-09 (PostgreSQL Connection Kill)**: Simulates abrupt network drop mid-saga using `pg_terminate_backend`. Dedicated connection pool detects broken session, discards tainted socket, reconnects, and recovery resumes payment to completion.
- **REC-10 (Pool Constriction & Backpressure)**: Recovery executed under heavily constrained connection pool (`MaxConns = 4`) under concurrent load. Queries execute without connection leaks or pool deadlocks.
- **REC-11 (Route Target Removed)**: Persisted payment references an unconfigured/unregistered execution target. Recovery safely parks the job in `needs_attention` with `UNKNOWN_ROUTE` without modifying account balances.
- **REC-12 (Presence Absent Replay)**: Bank provisional credit returns transient 500 error. Recovery checks participant presence via `OperationPresenceReader`, receives authoritative `ABSENT`, safely replays operation using original stable UUID, and completes payment.
- **REC-13 (Presence Present Convergence)**: Bank provisional credit returns transient 500 error. Recovery queries presence, receives authoritative `PRESENT`, avoids duplicate credit, and completes payment.
- **REC-14 (Exhaustion & Safety Parking)**: Participant uncontactable for 20 attempts. Backoff scheduler increments attempts, hits `MaxAttempts = 20`, and parks payment safely in `needs_attention` (`ATTEMPTS_EXHAUSTED`). Invariants remain intact.

#### Event Restart Invariants (EVT-01 to EVT-06)
- **EVT-01 (Atomic Event Production)**: Database trigger on `payments` writes outbox event and delivery atomically within the same database transaction. Transaction rollback produces zero events.
- **EVT-02 (Dispatcher Lease Expiry)**: Dispatcher crashes after claiming due outbox deliveries. Lease expires, and secondary dispatcher reclaims and dispatches deliveries without event loss.
- **EVT-03 (Inbox Idempotency)**: Worklist projector receives duplicate event deliveries. Deduplication via `consumer_inbox` prevents redundant projection mutations.
- **EVT-04 (Out-of-Order Version Guard)**: Outbox deliveries arrive out of order (event version 2 processed before version 1). Projector applies non-decreasing `event_version` guard, preventing state regression.
- **EVT-05 (Poison Pill Quarantine)**: Malformed event payload dispatched. Projector flags delivery as quarantined (`last_error_code = 'UNSUPPORTED_SCHEMA_VERSION'`), allowing healthy deliveries to proceed.
- **EVT-06 (Atomic Offline Rebuild)**: Offline CLI utility (`cmd/rebuild-worklist`) acquires exclusive transaction advisory lock, computes staging table diff, truncates, and repopulates `ops_payment_worklist` matching source payments exactly.

### 2. Connection Pool & Harness Safety Rules
To avoid test harness deadlocks on live PostgreSQL pools:
1. **Unscanned QueryRow Elimination**: Replaced un-scanned `QueryRow` statements with `Exec` to ensure connections are immediately returned to `pgxpool.Pool` rather than leaking until garbage collection.
2. **Isolated Termination Connection**: In REC-09, `pg_terminate_backend` uses a standalone `pgx.Connect` instance so the termination query itself is unaffected by the terminated backend PID and does not contaminate the application pool.
3. **Pre-scenario Queue Cleaning**: Scenarios testing specific dispatcher claims (EVT-02, EVT-05) clean prior unclaimed queue rows to ensure strict test isolation.
4. **Foreign Key Conformance for Unconfigured Routes**: In REC-11, the unknown destination bank row is inserted into `banks` table to satisfy relational foreign keys, while the bank adapter is omitted from `payments.Service` routing map to simulate an unconfigured target.

### 3. Verification Artifacts and Cryptographic Integrity
The experiment run produces structured artifacts committed under `artifacts/v2/crash_matrix/`:
- `crash_matrix.json`: Machine-readable run metadata, toolchain details, per-scenario checkpoints, durations, and invariant assertions.
- `crash_matrix.csv`: Tabular scenario summary for reporting.
- Cryptographic SHA-256 checksum recorded and verified across runs.

## Consequences
- **Positive**: Proves complete crash recovery and idempotency under 20 real-world failure scenarios on live PostgreSQL 18.
- **Positive**: Zero financial invariant violations observed across all chaos runs.
- **Positive**: Establishes a permanent, reproducible experiment CLI for regression testing during future deployments.
- **Trade-off**: Requires a live PostgreSQL database running for execution (covered by `run_tests.ps1` and `cmd/experiments`).
