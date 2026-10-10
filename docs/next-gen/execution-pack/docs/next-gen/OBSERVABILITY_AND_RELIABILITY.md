# Recovery and operational observability

## Recovery first

The worker must reuse the financial kernel. It must not implement a second settlement algorithm. Introduce a single `Service.Resume(ctx,paymentID)` entry point and route duplicate request recovery and background work through it. It resolves the persisted route, acquires the existing per-payment serialization lock, rereads current state after locking, then dispatches to private continuation helpers.

The exact lock key remains compatible with the existing UUID-derived key. Use a dedicated acquired connection for the session lock. All monetary continuation callers must use this path; a new wrapper around only the worker is insufficient. If unlock fails or lock ownership becomes uncertain, discard/close that underlying connection rather than return it to the pool with a session lock. Cleanup has a bounded independent timeout.

Lock acquisition has a caller deadline. Collision of hashed UUID lock keys causes extra serialization, not permission to proceed concurrently. Do not hold a payment row transaction open across bank HTTP calls. Participant uniqueness remains necessary if the database session dies while an external call is in flight.

## State dispatch

| Central state | Safe action under payment lock |
| --- | --- |
| CREATED, VALIDATING, ROUTING | Resume recorded routed intent and original target; do not choose a new target |
| PROCESSING, PENDING_RECONCILIATION | Resolve journaled operation outcomes and continue only proven safe steps |
| BANK_SETTLED_CENTRAL_PENDING | Central completion only; never repeat successful bank operations |
| COMMITTED | Verify central ledger and recorded completion preconditions, then complete the state transition without adding another ledger transaction |
| COMPLETED, FAILED, REVERSED | Return terminal, resolve scheduler job, perform no financial effects |
| LOCAL_SETTLEMENT or incomplete route identity | Exclude from routed recovery; record explicit unsupported/attention result if encountered |

Only payments with a durable routed identity enter automatic discovery. If a local state is genuinely crash-reachable, add a separate invariant-backed continuation in the existing local transaction path; no generic saga fallback.

## The operation absence crash gap

Existing flow writes a central operation journal row, then calls the bank. A crash between those actions leaves no bank record. Existing participant status maps a missing record to PENDING. Preserve unknown semantics, but add the separate presence read contract in `API_CONTRACTS.md`.

The algorithm for a journaled unresolved operation is:

1. Query status using original payment, operation and bank identities. A correlated SUCCEEDED or FAILED result is durable evidence; a mismatched response is unresolved.
2. If status is PENDING and presence capability exists, query presence against the original participant. Unavailable or inconclusive presence remains unknown.
3. If present, follow participant status; never create a new operation identity.
4. If absent after a successful authoritative read, verify the participant's replay contract: unique original UUID, stable immutable payload hash, transaction-safe concurrent duplicate handling, and preserved operation records. Reissue that same operation using the saved original fields.
5. A delayed original call racing this retry must converge through participant idempotency to one result. Prove this in PostgreSQL tests.
6. If the replay contract is unavailable or its identity/payload checks fail, park the payment for investigation. Do not compensate or declare failure because attempts ran out.

Do not use an HTTP 404, an empty log, a missing trace, a failed status query or a Merkle discrepancy as absence proof. A zero-row read over a replica or an incomplete retention window cannot establish absence. The immediate capability reads the owning primary and retains operation records throughout the demonstration.

## Scheduler schema

Migration 000022 introduces `recovery_jobs`, `recovery_attempts`, and `worker_heartbeats`.

`recovery_jobs`: payment UUID PK/FK; `status` check (`scheduled,running,deferred,needs_attention,resolved`); `due_at`; `lease_owner` nullable; `lease_until` nullable; monotonically increasing `lease_epoch BIGINT`; `attempt_count`; `last_reason_code`; `last_attempt_id`; created/updated timestamps. Index due unresolved jobs on `(due_at,payment_id)` with a partial status predicate. Retain jobs for terminal payments as operational evidence until retention cleanup.

`recovery_attempts`: UUID PK; payment FK; lease epoch; sanitized worker identity; start/finish; before/after state; result; safe reason; original operation UUID array; trace ID nullable. Unique `(payment_id,lease_epoch)`. No JWT, note, credentials or raw exception body.

`worker_heartbeats`: worker UUID, safe label, started_at, last_seen_at, enabled job families, build SHA. Heartbeat is operational evidence only; do not pretend it proves financial correctness.

Discovery scans a bounded number of old nonterminal routed payments, upserting a job only if none exists or a resolved job's payment became nonterminal through a supported transition. Do not reset a parked job every scan. Default stale age: 30 seconds; scan interval: 10 seconds; page: 100. Use indexes and bounded scans. New payment transitions may create/schedule jobs directly, but discovery remains the restart/backfill safety net.

## Claim protocol

In a short transaction, select due jobs with expired/no lease using `FOR UPDATE SKIP LOCKED`, update owner/until, increment epoch and create the attempt row. Commit before network work. The scheduler claim is a lease, not a financial lock.

Default lease 60 seconds, heartbeat every 15 seconds, attempt timeout 20 seconds, bank call timeout no more than 5 seconds. Reject configuration where heartbeat/attempt timeout cannot fit inside the lease. Claim at most available execution slots; do not lease a large queue that will expire before execution.

Result/heartbeat updates require payment ID, owner and epoch and a currently valid lease. If the predicate affects zero rows, the claim was lost; do not update another owner's job. The resumer still prevents unsafe monetary races. An expired attempt is marked interrupted by a bounded sweeper and left traceable. If a stale worker later observes financial completion, it can stop safely but cannot rewrite the new claim's history.

Use exponential backoff with jitter: base 2 seconds, cap 5 minutes, attempts limited to 20 automatic attempts before `needs_attention`. Jitter is supplied through an injectable source and seeded in tests. Exhaustion changes only scheduler status. Terminal business failure requires authoritative financial evidence and the existing state machine.

A manual recheck schedules a parked job with audited authorization. It does not bypass the payment lock, clear bank facts, or generate a new payment. Missing target configuration is `TARGET_NOT_CONFIGURED`, parked and visible.

## Concurrency and resource budgets

Defaults: API monetary concurrency 2; worker recovery concurrency 2; one outbox delivery loop; one analytics job; one investigator generation job. Banking processes each retain a small pool. Introduce validated pool settings: API 8, worker 8, bank A/B 4 each, and a sufficient PostgreSQL connection ceiling. These are starting settings, not benchmark claims.

Leave at least two pool slots for non-lock queries in each process. Acquire the local concurrency semaphore before acquiring the session lock connection. Never use one pool slot per unbounded queued request. Reject overload with 429/503 and a retry hint before financial execution; if intent already exists, return its pending identity rather than create another one.

Graceful shutdown stops claiming, cancels/finishes bounded active work, closes stream handlers, flushes telemetry with a five-second limit and closes the pool. Kill -9 tests validate crash behavior independently from graceful shutdown.

Worker uses common validated bank/route wiring extracted from current API setup. Worker recovery does not rerun route selection or invent a separate circuit authority. Status reads and original-target execution remain bounded; unavailable participants cause backoff. Keep one API replica while breaker/chaos state remains process local.

## OpenTelemetry plan

Instrument all four Go processes with the OpenTelemetry API and SDK. Add `otelhttp` server/client integration and W3C trace context propagation. Configure service names `transactx-api`, `transactx-bank-a`, `transactx-bank-b`, `transactx-worker`; include build SHA and environment as resources.

Create semantic spans for payment intent, route decision, operation status/presence, bank operation, central completion, recovery attempt, outbox delivery, projection update, evidence read, retrieval and optional generation. A new recovery trace links to the original trace; it does not manufacture a parent that remained live across a restart. Stable payment and operation UUIDs correlate durable facts even if traces are missing.

Trace attributes may include payment UUID, operation UUID, bank/target code, sanitized state, reason code and event UUID. Never include payment notes, customer names, identifiers, auth headers, idempotency keys, DB URL, provider keys or full model prompts. Metrics never use payment IDs, operation IDs, user IDs or trace IDs as labels.

Metrics include HTTP latency/status by route template, bank latency/result, recovery attempt/result, recovery queue age/depth, outbox delivery/result/lag, projection refresh, pool wait, worker heartbeat age, retrieval latency, generation token usage and anomaly scorer status. Histograms have deliberately small bucket sets. Runtime counters reset on restart and are labelled as such; durable analytics is queried separately.

Use JSON `slog` with request, trace and span correlation. Do not require the experimental/less-stable OTel logging pipeline; normal JSON logs are sufficient. Slow/error details are sanitized and bounded.

## Export and trace explorer

Immediate free mode supports a Prometheus metrics endpoint on loopback/private administration, optional OTLP HTTP export, and a bounded safe-span exporter to operational `trace_spans` storage. The safe-span exporter is asynchronous, queue bounded to 1,000 spans, maximum attributes 20, max value length 256, and drops with a metric on overload. It never blocks financial work or rolls back settlement.

The database telemetry role writes only its operational table. Retention: 24 hours by default; maximum 20,000 stored spans with bounded cleanup, configurable downward. Trace viewer shows collection status and expiry. Exporter failure is a visibility loss, never transaction failure. If database storage is disabled, UI clearly reports traces not collected and OTLP remains available for an external compatible backend.

Use sampling appropriate to low demo traffic, configurable default 0.2. Pin test sampling to 1.0. Do not claim every error trace is retained by ordinary head sampling; tail sampling requires a separate collector and is future work.

## SLI and SLO specification

Define, measure and report these as engineering targets:

- Valid simulated payments reaching a justified terminal outcome within 60 seconds under healthy conditions; exclude explicitly rejected input and insufficient funds from successful-settlement denominator, but report them separately.
- Recovery time from participant restoration to safe convergence: target p95 under 60 seconds for supported resolvable scenarios.
- Outbox-to-worklist visibility lag: target p95 under 5 seconds under demo load; zero permanently lost committed events in restart tests.
- Operator evidence availability when central DB is healthy: target 99 percent within the controlled test, with partial participant evidence explicitly counted.
- Financial conservation, one ledger transaction per completed payment and stable operation identity: zero violations in the specified experiments.

These are acceptance targets on a controlled environment, not public production SLAs. Record denominators, observation windows, failures and exclusions.
