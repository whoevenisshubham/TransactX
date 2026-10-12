# ADR-033: Durable Recovery Scheduling, Leases, and Attempts

## Status
**IMPLEMENTED** (Task B3 complete)

## Context
TransactX executes distributed routed payments across independent banking participants and central PostgreSQL storage. While `Service.Resume` (ADR-032) provides the canonical serialized continuation entrypoint under dedicated PostgreSQL advisory locks, an autonomous background recovery subsystem is required to discover stale non-terminal payments, claim them safely across multiple worker instances, manage backoff and retry budgets, track comprehensive operational attempt audit histories, and record worker heartbeats without risking concurrent double-execution or connection pool starvation.

## Decisions

### 1. Durable Recovery Schema (Migration 000022)
- **`recovery_jobs`**: Stores the queue state per payment (`payment_id UUID PK/FK`). Tracks `status` (`scheduled`, `running`, `deferred`, `needs_attention`, `resolved`), `due_at`, `lease_owner`, `lease_until`, monotonically increasing `lease_epoch BIGINT`, `attempt_count`, and last attempt/reason metadata.
  - Partial index `recovery_jobs_due_idx` on `(due_at, payment_id) WHERE status != 'resolved'`.
- **`recovery_attempts`**: Immutable append-only audit trail recording every execution attempt under a lease. Tracks `id`, `payment_id`, `lease_epoch`, `worker_identity`, `started_at`, `finished_at`, `state_before`, `state_after`, `result`, `reason_code`, `operation_ids`, and `trace_id`.
  - Unique constraint on `(payment_id, lease_epoch)` ensures strict per-lease audit singularity.
- **`worker_heartbeats`**: Tracks active worker processes, safe worker labels, heartbeat timestamps (`last_seen_at`), enabled job families, and build SHA.

### 2. Lease-Based Claim Protocol via `FOR UPDATE SKIP LOCKED`
Workers claim batches of due jobs in short database transactions:
```sql
SELECT ... FROM recovery_jobs
WHERE ((status IN ('scheduled', 'deferred') AND (lease_until IS NULL OR lease_until < NOW()))
    OR (status = 'running' AND lease_until < NOW()))
  AND due_at <= NOW()
ORDER BY due_at ASC
LIMIT $1
FOR UPDATE SKIP LOCKED;
```
Claims update `status = 'running'`, assign `lease_owner`, advance `lease_until = NOW() + leaseDuration`, increment `lease_epoch = lease_epoch + 1`, and insert the corresponding `recovery_attempts` row before transaction commit. The claim is a lease, not a monetary lock.

### 3. Strict Monotonic Lease Epochs and Conditional CAS Updates
Completion of an attempt uses a conditional Compare-And-Swap (CAS) update requiring the exact `(payment_id, lease_owner, lease_epoch)` and a currently valid lease (`lease_until >= NOW()`):
- If the CAS predicate matches 0 rows, the worker lost the lease (e.g. lease expired and another worker claimed epoch + 1). The stale worker's result is safely discarded (`LostClaimCount` incremented), preserving the integrity of the active lease holder's history.
- The underlying `Service.Resume` advisory lock independently guarantees that monetary operations are never raced.

### 4. Interrupted Attempt Sweeper
A bounded background sweeper periodically queries running jobs whose lease has expired (`status = 'running' AND lease_until < NOW()`). It marks the abandoned attempt row as `result = 'INTERRUPTED'`, `reason_code = 'LEASE_EXPIRED'`, and reschedules the job with backoff (or parks it in `needs_attention` if attempt limits are reached).

### 5. Exponential Backoff with Injectable Jitter
Transient failures and non-terminal deferrals apply exponential backoff:
$$\text{backoff} = \min(\text{cap}, \text{base} \times 2^{\text{attempt}-1}) + \text{jitter}$$
- Default base: 2 seconds, default cap: 5 minutes.
- Jitter is supplied via an injectable `JitterFn` to ensure reproducible, deterministic test assertions while avoiding thundering herds in production.

### 6. Operational Parking on Attempt Exhaustion
Automatic retries are capped at `MaxAttempts = 20`. Upon exhaustion:
- The scheduler transitions the job to `status = 'needs_attention'` with reason `ATTEMPTS_EXHAUSTED`.
- The scheduler does not invent financial completion or trigger unwarranted reversal. The payment remains safely parked until operator investigation or audited manual recheck (`ScheduleManualRecheck`).

### 7. Bounded Stale Discovery Safety Net
Background discovery scans non-terminal routed payments older than `staleAge` (default 30s) that lack active unresolved recovery jobs.
- Bounded by pagination (`LIMIT 100`).
- Strictly preserves parked attention jobs; discovery **never** resets or un-parks a `needs_attention` job.

### 8. Rollback and Disaster Mitigation
If background scheduling must be rolled back:
- Workers can be stopped immediately via configuration (`JOB_FAMILIES=""` or stopping `cmd/worker`).
- Request-driven continuation via customer retries and caller `Service.Resume` remains fully operational.
- All durable audit records in `recovery_jobs` and `recovery_attempts` are preserved.

## Consequences
- **Positive**: Resilient distributed coordination without external queue dependencies; zero lost attempts or race conditions; safe automatic recovery of dropped dispatches; complete visibility into recovery history.
- **Negative**: Adds database write operations per recovery attempt; requires worker heartbeat maintenance and periodic expired lease sweep passes.
