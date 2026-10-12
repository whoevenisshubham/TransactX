# ADR-032: Shared Recovery Serialization and State Continuation

## Status
**IMPLEMENTED** (Tasks B1 & B2 complete)

## Context
TransactX executes distributed routed payments across independent banking participants (Bank A, Bank B) and central PostgreSQL storage. Failures such as process crashes, timeouts, network drops, or worker restarts can leave payments in non-terminal states (`CREATED`, `VALIDATING`, `ROUTING`, `PROCESSING`, `BANK_SETTLED_CENTRAL_PENDING`, `COMMITTED`, `PENDING_RECONCILIATION`).

Previously, duplicate requests via `CreateWithResult` and recovery paths were handled with disparate logic, risking concurrent execution, race conditions between caller retries and worker background recovery, and duplicate bank operations. Furthermore, session-level advisory locks acquired on pooled database connections risked leaking locked sessions back into the connection pool if unlocking failed or timed out.

## Decisions

### 1. Unified Canonical Continuation Entrypoint
All payment continuation paths—whether triggered by customer duplicate requests via `CreateWithResult`, caller recovery retries, or background recovery workers—must converge on a single entrypoint:
`Service.Resume(ctx context.Context, paymentID uuid.UUID) (ResumeResult, error)`

### 2. Dedicated Session Advisory Locking
Every payment continuation acquires a dedicated connection from the connection pool and attempts a session advisory lock using a 64-bit key derived deterministically from the payment UUID:
```sql
SELECT pg_try_advisory_lock($1)
```
- **Guaranteed Session Cleanup**: The advisory lock is associated with the dedicated connection. Unlocking executes `SELECT pg_advisory_unlock($1)` with a bounded 5-second timeout.
- **Pool Contamination Prevention**: If unlocking returns false or encounters an error/timeout, the dedicated connection is immediately and forcibly closed (`conn.Conn().Close(ctx)`) before releasing it, ensuring no lingering advisory locks pollute the general connection pool.

### 3. State Re-reading Under Lock
After successfully obtaining the advisory lock, the resumer re-reads the authoritative payment state from PostgreSQL before taking any action. If a concurrent execution completed the payment while the lock was pending, the resumer immediately detects the updated state and exits safely.

### 4. Terminal State Handling as No-Op
If the re-read payment is already in a terminal state (`COMPLETED`, `FAILED`, `REVERSED`), `Service.Resume` immediately returns a `ResumeResult` with status `NO_OP` without invoking any bank adapters or altering ledger records.

### 5. Central-Only State Continuation
- **COMMITTED**: Validates that an existing balanced ledger transaction ($\ge 2$ entries) is already recorded. It transitions the payment state to `COMPLETED` without writing a duplicate ledger transaction.
- **BANK_SETTLED_CENTRAL_PENDING**: Completes the central ledger credit/debit leg and advances state to `COMPLETED`.

### 6. Original Persisted Route Preservation
For states requiring bank interaction (`CREATED`, `VALIDATING`, `ROUTING`, `PROCESSING`, `PENDING_RECONCILIATION`), the resumer resolves the original persisted execution target and bank adapters (`RoutedAdaptersForPayment`). If the route configuration has been removed or the execution target is no longer available, the resumer fails closed and leaves the payment in its current pending state (`ResumeStatusAttention`), preventing arbitrary or divergent rail execution.

### 7. Bounded Concurrency and Connection Pool Reserve
To prevent database connection starvation:
- Payment continuation is throttled by a bounded semaphore configured via `TX_PAYMENT_CONCURRENCY` (default 2).
- Configuration validation enforces that the pool size exceeds concurrency limit by a minimum reserve of 2 query connections (`dbMaxConns - paymentConcurrency >= 2`).

### 8. Operation Absence and Safe Replay Capability (Task B2)
When an operation on a banking participant remains unconfirmed or in a pending/unresolved state:
- **Authoritative Absence vs. Network Ambiguity**: "Absent" MUST be an affirmative confirmation directly from the participant's operations log/ledger (`pgx.ErrNoRows`). Transient connection errors, 500s, or timeouts must NEVER be inferred as absence.
- **`OperationPresenceReader` Interface**: Adapters implement `GetOperationPresence(ctx, paymentID, operationID) (OperationPresenceResult, error)`.
- **Safe Replay Semantics**: An unconfirmed operation may be replayed if and only if:
  1. The adapter implements `OperationPresenceReader`.
  2. The participant returns authoritative `PresenceAbsent`.
  3. Replay is executed using the exact immutable parameters (same `operation_id`, amount, currencies, accounts) recorded in the central database before the initial dispatch.
  4. The participant's data store enforces uniqueness on `operation_id` and idempotency hash, rejecting any conflicting payload or racing late-arriving original request with `409 Conflict`.
- **Failing Closed on Unknown or Non-Capable Adapters**: If an adapter does not implement `OperationPresenceReader`, or returns `PresencePresent` or transient errors, the engine strictly refrains from replaying and keeps the operation parked in pending reconciliation for operator review.

### 9. Observability & Operational Metrics
The recovery subsystem tracks atomic metrics for:
- `lock_wait_total_ns` and `lock_wait_count`
- `resume_count`, `resume_noop`, `resume_completed`, `resume_failed`, `resume_attention`
- `overload_count`
- `presence_checked`, `presence_absent`, `presence_present`, `presence_error`, `presence_replayed`

## Consequences
- **Positive**: Strict mutual exclusion between customer retries and recovery workers; eliminate double-credit and double-debit vulnerabilities; zero connection pool leak risk on lock failure; safe recovery of dropped dispatches without duplicate execution risks.
- **Negative**: Callers must wait for the lock or bounded semaphore when retrying actively executing payments; transient delays during peak lock contention.
