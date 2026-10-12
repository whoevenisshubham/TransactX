# ADR-034: Transactional Outbox Facts and Atomic Event Production

## Status
**IMPLEMENTED** (Task B6 complete)

## Context
TransactX requires an independently maintained operator worklist and audit history that reliably reflects all committed payment lifecycle transitions and recovery attempt facts without data loss across process crashes, worker restarts, or network disruptions.

Relying on post-commit application callbacks or asynchronous brokers risks silent event loss if a process dies between the database commit and the event dispatch. Furthermore, multiple mature repository paths across payments creation, routed execution, and worker recovery mutate payment state; requiring each application code path to explicitly emit events introduces a severe regression risk where a missed call site permanently breaks worklist consistency.

## Decisions

### 1. Atomic Database Trigger for Payment Events
- Migration 000024 adds `payments.event_version BIGINT NOT NULL DEFAULT 0` and attaches a PostgreSQL trigger (`BEFORE INSERT OR UPDATE`) to `payments`.
- **Payment Creation**: An inserted payment row is assigned `event_version = 1` and atomically inserts a `payment.created.v1` event and initial delivery record into the outbox tables in the same transaction.
- **State Changes**: When `payments.state` transitions (`OLD.state IS DISTINCT FROM NEW.state`), the trigger increments `event_version` (`OLD.event_version + 1`) and atomically inserts a `payment.state_changed.v1` event with previous and new states.
- **Identical State & Non-State Updates**: Updates that do not alter the payment's `state` (such as non-state metadata updates or no-op state assignments) emit zero events and leave `event_version` unchanged.
- **Transaction Atomicity**: If the source payment transaction rolls back, all outbox events and deliveries are rolled back atomically. If outbox insertion fails, the source transaction aborts, preventing ghost state changes without event records.

### 2. Immutable Event Store (`outbox_events`)
- Stores events with schema:
  - `id`: UUID primary key.
  - `event_type`: Typed event string (`payment.created.v1`, `payment.state_changed.v1`, `recovery.attempt_started.v1`, etc.).
  - `schema_version`: Integer version (default `1`).
  - `aggregate_type`: String identifier (`payment`, `recovery`).
  - `aggregate_id`: Aggregate UUID.
  - `aggregate_version`: Monotonic integer version per aggregate.
  - `occurred_at`: Authoritative source timestamp.
  - `payload`: Sanitized JSONB content.
  - `traceparent`: Optional W3C distributed trace context.
- **Immutability Enforcement**: A PostgreSQL trigger (`deny_outbox_event_update`) strictly denies all `UPDATE` queries on `outbox_events`. Retention pruning deletes terminal records past retention bounds (30 days) without allowing payload mutation.
- **Unique Version Index**: A partial unique index on `(aggregate_type, aggregate_id, aggregate_version) WHERE aggregate_type = 'payment'` prevents version regressions and duplicate version generation per payment.

### 3. Outbox Deliveries Schema (`outbox_deliveries`)
- Tracks per-consumer dispatch state with primary key `(consumer_name, event_id)`.
- Initial delivery row is created atomically with each outbox event for the required primary consumer `ops-worklist-v1`.
- Columns track `status` (`pending`, `processing`, `delivered`, `quarantined`), `attempts`, `next_attempt_at`, `lease_owner`, `lease_until`, `lease_epoch`, `delivered_at`, and `last_error_code`.
- Indexed by `(consumer_name, status, next_attempt_at)` to support high-throughput `SKIP LOCKED` batch claiming by outbox relays.

### 4. Recovery Lifecycle Event Triggers
- `recovery_attempts`:
  - Insertion emits `recovery.attempt_started.v1` with `lease_epoch`, `worker_identity`, `state_before`, and `started_at`.
  - Updating `finished_at` emits `recovery.attempt_finished.v1` with `state_after`, `result`, and `reason_code`.
- `recovery_jobs`:
  - Status transition to `needs_attention` emits `recovery.attention_required.v1` with `attempt_count` and `last_reason_code`.

### 5. Payload Sanitization
- Payment payloads strictly contain operational attributes: `paymentId`, `previousState`, `newState`, `eventVersion`, `sourceBankId`, `destinationBankId`, `routeBankId`, `amountPaise`, `currency`, `failureReason`, `createdAt`, `updatedAt`.
- Customer notes, passwords, initiated user identities, and account numbers are strictly excluded from event payloads, satisfying data privacy and audit compliance.

## Consequences
- **Positive**: Zero-data-loss guarantee between source transactions and outbox events; automatic outbox production across all existing and future repository paths; strict payload immutability and data sanitization.
- **Trade-offs**: Trigger execution adds minor overhead to payment update transactions; schema changes to events require explicit migrations.
