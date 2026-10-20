# Transactional events and projection delivery

## Why this exists

The current activity feed reads durable tables and SSE tells browsers to refetch. That is useful but does not guarantee a committed payment update will reach an independently maintained operator worklist after a sender restarts. The new outbox provides durable pending delivery for specified payment and recovery facts.

No external broker is required. PostgreSQL is already durable, and the demonstration has a small number of consumers. Keep the event boundary explicit so a broker can be added later without redefining event semantics.

## Event families

- `payment.created.v1` and `payment.state_changed.v1`.
- `recovery.attempt_started.v1`, `recovery.attempt_finished.v1`, `recovery.attention_required.v1`.

Existing health, circuit, chaos, reconciliation and integrity activity remain on their current durable-source read model. Do not imply that all legacy event families have moved to the new transport.

Envelope: UUID `eventId`; `eventType`; `schemaVersion:1`; `aggregateType`; `aggregateId`; decimal-string `aggregateVersion`; `occurredAt`; optional `traceparent`; redacted typed `payload`. Event IDs deduplicate delivery. Aggregate versions reject stale projection updates; they do not establish global commit order.

Payment payload includes payment UUID, previous/new state, event version, bank identities and creation/update times. It excludes notes, credentials and public customer identifiers. Recovery payload references an attempt/job identity and safe result code.

## Atomic producer

Migration 000024 adds `payments.event_version BIGINT NOT NULL DEFAULT 0`, `outbox_events`, and delivery state. A trigger on payment insertion/state change increments per-payment event_version and inserts the corresponding immutable event in the same transaction. Skip non-state updates and identical state assignments. Initial create gets version 1. Serialize changes through the payment row as the existing code already does; do not derive the version from wall time or a global sequence.

Use a database trigger for payment facts because multiple mature repository paths write state. A post-commit callback could lose events, and a missed repository call site would undermine the guarantee. Trigger failure aborts the source transaction; this is the required atomicity. Validate all current payment paths against the trigger.

Recovery attempt facts are inserted with their source row in the same transaction, via a typed producer or a narrowly scoped trigger. Do not duplicate the same fact from a trigger and a callback. Producer metadata such as traceparent is optional and sanitized; inability to supply it must not cause fabricated correlations.

Do not backfill old historical transitions as if they were emitted now. Initial worklist bootstrap reads current authoritative payment snapshots; optional historical import has a separate event namespace and provenance.

## Storage

`outbox_events`: event UUID PK; typed event name; schema version; aggregate type/UUID/version; occurred_at; payload JSONB; traceparent nullable; created_at. Unique payment `(aggregate_type,aggregate_id,aggregate_version)` where applicable. Deny updates to event payloads. Index created_at for retention and aggregate identity for diagnostics.

`outbox_deliveries`: `(consumer_name,event_id)` PK; status (`pending,processing,delivered,quarantined`); attempts; next_attempt_at; lease owner/until/epoch; delivered_at; safe last_error_code. Index pending due deliveries. The required consumer name is `ops-worklist-v1`.

`consumer_inbox`: `(consumer_name,event_id)` PK; processed_at; handler_version. Insert only in the same transaction as projection work. A duplicate row means the original consumer transaction already committed.

`ops_payment_worklist`: payment UUID PK; current central state, event_version, bank codes, timestamps, safe recovery summary; projected_at. This is derived operational data. It cannot act as a ledger or financial source of truth.

New producer transaction creates the event and initial delivery row atomically for the required consumer. Adding a consumer later backfills its delivery rows explicitly from retained events; do not silently assume prior delivery.

## Dispatch and acknowledgement

1. Claim at most 25 due deliveries with SKIP LOCKED in a short transaction; use lease epoch and owner.
2. Commit claim before handling. Bound active handling to one loop initially.
3. Start the consumer transaction. Validate schema version/type/payload. Insert inbox row with conflict handling.
4. If duplicate, commit and acknowledge without another projection effect.
5. Otherwise fetch the current authoritative payment and recovery job. Upsert that current snapshot using a nondecreasing event_version predicate. Never apply the old event's state blindly.
6. Commit inbox and projection together.
7. Acknowledge delivery in a separate CAS update using owner/epoch. A crash here leaves a duplicate delivery, which the inbox absorbs.

Unknown event versions are quarantined with explicit reason. Delivery failures use bounded exponential backoff with jitter, maximum ten attempts before quarantine. Quarantine applies to delivery only; it never fails a payment. Manual replay is an admin CLI command or gated OPS_ADMIN workflow, using original event IDs and a new replay consumer namespace when needed.

Because the projector refreshes authoritative current state, out-of-order events converge without requiring global ordering. For any future consumer that applies incremental state changes, define and enforce per-aggregate ordering and poison-event blocking separately. Do not reuse this snapshot consumer as proof that arbitrary downstream ordering is solved.

## Cursor and sequence rule

Never dispatch using only `WHERE id > last_seen_id`. A transaction can allocate a lower sequence value, pause, and commit after a higher value is processed. Scanning only above a high watermark loses the late commit. Query delivery status/due time and inbox identity. SSE remains a lossy invalidation signal and HTTP reads remain authoritative.

## Rebuild and replay

Provide `cmd/rebuild-worklist` for derived data only. It takes an exclusive projection maintenance advisory lock. Normal projection transactions take the corresponding shared transaction lock. Rebuild constructs a temporary snapshot from central payments and recovery facts, validates row counts/versions, then replaces the derived rows in a single transaction. Readers see the prior or replacement committed view, never a partially deleted table.

Financial source writers do not take this maintenance lock. Events committed during the rebuild remain pending and refresh the projection after the rebuild. Version guards prevent old retained events from regressing it. Keep inbox history: it protects original deliveries. Explicit replay uses a new consumer namespace and snapshot refresh semantics, not new money operations.

Rebuild must reject a live financial table target and never delete source rows. A failed rebuild rolls back the derived replacement. Record started/completed/failed audit facts and duration. An optional preview command compares projected and authoritative counts without writes.

## Retention

Demo defaults: immutable events 30 days; delivered delivery/inbox rows retain at least as long as their event; quarantined/pending rows are never deleted by age alone. Cleanup is bounded and deletes child delivery/inbox rows only after an event is safely terminal and past retention. Finite inbox retention means deduplication guarantees apply to retained event identities; snapshot upserts are also idempotent beyond that window.

Do not delete ledger, idempotency, participant operation or payment authority to make a demo fit. If disk fills, stop nonessential telemetry/analytics and raise attention. Reducing operational retention is allowed; silently deleting financial evidence is not.

## Required failure evidence

- Source rollback leaves neither event nor projection effect.
- Source commit followed by process death still leaves a pending delivery.
- Consumer commit followed by acknowledgement loss causes no duplicate worklist effect.
- Two dispatchers claim distinct valid leases; expired claims are reclaimed safely.
- A lower sequence ID committing after a higher one is still processed.
- Old events arriving after COMPLETED cannot return the projection to PROCESSING.
- Unsupported schema is quarantined and visible without blocking unrelated payments.
- Concurrent rebuild plus new payment transitions converges to current state.
- Database outage cannot create a fictitious delivered event or completed projection.
