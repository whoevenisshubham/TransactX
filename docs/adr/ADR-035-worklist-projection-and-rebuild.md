# ADR-035: Worklist Projection, Consumer Inbox, and Safe Offline Rebuild

## Status
**IMPLEMENTED** (Task B7 complete)

## Context
TransactX operations personnel require an aggregated, performant worklist projection (`ops_payment_worklist`) summarizing payment lifecycle states, route banking entities, recovery statuses, attempt counts, and last failure reasons. Because payments and recovery jobs are authoritative tables subject to continuous transaction processing, direct reporting queries across large volumes can introduce lock contention and latency.

Furthermore, event delivery from the transactional outbox (`outbox_events` and `outbox_deliveries`) can be re-delivered due to network hiccups, worker restarts, or lease expiry sweeps. A safe, asynchronous consumer mechanism is needed that:
1. Guarantees idempotent event ingestion.
2. Defends against out-of-order event delivery (ensuring older event versions cannot overwrite newer state).
3. Quarantines unprocessable or corrupted events without stalling projection workers.
4. Provides an offline maintenance rebuild utility (`cmd/rebuild-worklist`) that can safely reconstruct the entire projection from authoritative source tables under exclusive locking with preview validation and operational audit logging.

## Decisions

### 1. Dedicated Consumer Inbox (`consumer_inbox`)
- Introduced via Migration 000025 with composite primary key `(consumer_name, event_id)`.
- When the projector processes an outbox delivery for consumer `ops-worklist-v1`, it records `(consumer_name, event_id)` into `consumer_inbox` in the same transaction as the projection update.
- If `consumer_inbox` already contains `(consumer_name, event_id)`, the projector recognizes duplicate delivery and treats it as a successful no-op (`absorbed`), avoiding duplicate side-effects.

### 2. Derived Worklist Projection (`ops_payment_worklist`)
- Stores flattened projection records per `payment_id`:
  - `payment_id`: UUID primary key.
  - `current_state`: Authoritative payment state (`CREATED`, `ROUTED`, `COMMITTED`, `SETTLED`, etc.).
  - `event_version`: Authoritative monotonic version from the payment aggregate.
  - `amount_paise`, `currency`: Monetary values in smallest currency unit.
  - `source_bank_id`, `destination_bank_id`, `route_bank_id`: Routing banking identifiers.
  - `created_at`, `updated_at`: Timestamps.
  - `recovery_status`, `recovery_attempts`, `last_recovery_reason`, `last_recovery_at`: Associated recovery job lifecycle metadata.
  - `projected_at`: Timestamp when the projection row was updated.
- Indexed by `current_state`, `recovery_status`, and `updated_at DESC` for fast UI worklist querying and filtering.

### 3. Nondecreasing Version Guard
- To prevent out-of-order deliveries from regressing projection state, the projector enforces that updates only modify the projection if `NEW.event_version >= ops_payment_worklist.event_version`.
- If an out-of-order or stale event arrives with a lower version than what is already projected, the projection update is ignored (`NO_OP`) while acknowledging the delivery so the queue makes forward progress.

### 4. Malformed and Unsupported Version Quarantine
- If an event payload has an unsupported schema version or unparseable JSON payload, the projector refuses to drop the event silently.
- Instead, the delivery is quarantined in `outbox_deliveries` (`status = 'quarantined'`, `last_error_code = 'UNSUPPORTED_SCHEMA_VERSION'`), raising alertability for operators while allowing valid deliveries to proceed.

### 5. Shared Transactional Advisory Locking
- Standard projection workers acquire shared transaction advisory locks (`pg_advisory_xact_lock_shared(78891001)`) for `WorklistMaintenanceLockKey`.
- This ensures concurrent projector workers can operate in parallel without locking each other out.
- It guarantees that any active projection worker automatically blocks an offline full rebuild until the batch transaction completes.

### 6. Atomic Offline Rebuild Utility (`cmd/rebuild-worklist`)
- Implemented as a standalone CLI command (`backend/cmd/rebuild-worklist`).
- Acquires an **exclusive** transaction advisory lock (`pg_advisory_xact_lock(78891001)`). If a rebuild is already in progress or projection workers are running, the rebuild blocks or fails safely.
- Supports `--preview` flag:
  - Queries authoritative source tables (`payments` LEFT JOIN `recovery_jobs`) vs `ops_payment_worklist`.
  - Reports count differences and sync status without making any changes.
- Safe Atomic Rebuild Flow:
  1. Populates a session temporary snapshot table `tmp_rebuild_worklist` from authoritative sources.
  2. Verifies snapshot row count exactly matches authoritative source row count.
  3. `TRUNCATE ops_payment_worklist` within the transaction.
  4. Inserts all snapshot rows into `ops_payment_worklist`.
  5. Commits the transaction and logs an operational audit record (`operational_audit_logs`) with `operation = 'REBUILD_WORKLIST'`, `role = 'OPS_ADMIN'`, `reason_code = 'M2_MAINTENANCE_REBUILD'`, duration, and row counts.

## Consequences
- **Positive**: Complete decoupling of heavy operational worklist queries from high-throughput OLTP tables; idempotency guarantees across restarts and redeliveries; deterministic defense against out-of-order event updates; zero-risk offline projection rebuilding with atomic swap and audit verification.
- **Trade-offs**: Worklist is an eventually consistent projection (typically sub-second latency from outbox delivery relay); offline rebuild holds exclusive advisory lock during the truncate-and-insert swap.
