# Projection and anomaly operations

## Capability status

The current M3 branch contains a synthetic offline dataset pipeline, a median/MAD statistical baseline, and a bounded pure-Go IsolationForest scorer. The durable transactional outbox, worklist projection, completed aggregate windows, scorer worker wiring, persistence, and live analytics HTTP response are planned M2 integration work. Offline fixtures and parity tests do not prove that live windows are collected or scored.

## Outbox and projection lag

The planned outbox inserts an operational event atomically with its source fact. Delivery is at least once, so the consumer uses an inbox or equivalent idempotency key before updating the projection. Duplicate delivery must not increment counts or regress a worklist row. Transport is not described as exactly once.

Projection lag is operational context. A pending or quarantined outbox item can make a worklist stale while the authoritative payment remains correct. Operators should compare projection status, oldest pending event age, and the direct evidence endpoint. Rebuilding a projection must consume durable events into a staging generation and switch only after validation; it must not mutate payment, participant, or ledger state.

The planned quarantine path records bounded retries and a stable reason after delivery repeatedly fails. A quarantined event is not a failed payment. An investigator may recommend projection repair only when the cited projection evidence shows lag or quarantine. It cannot run a rebuild or use projection state as superior to the central payment row.

## Operational analytics windows

The planned M2 aggregate job writes completed half-open five-minute windows for a configured bank or execution target. Windows are unique by entity, start time, and feature schema. Missing latency, health, or recovery measurements stay null; they are never invented as zero. Fewer than twenty samples yields `insufficient_data` rather than a confident anomaly score.

The `tx.features.v1` order is timeout rate, permanent failure rate, p95 latency milliseconds, oldest recovery age milliseconds, recovery attempts per payment, and health availability rate. Every feature must be known by window end. Scenario labels, fault names, future recovery outcomes, payment identifiers, and end-of-run statistics are excluded from inference.

## Baseline and experimental model

The median/MAD baseline is the recommended default because it outperformed the experimental IsolationForest on the frozen synthetic held-out set. Baseline held-out precision, recall, F1, and PR-AUC were approximately `0.942029`, `0.677083`, `0.787879`, and `0.849214`, with `0.031250` healthy false alerts. The IsolationForest values were approximately `0.814815`, `0.458333`, `0.586667`, and `0.765660`, with `0.078125` healthy false alerts.

These measurements cover 42 seeded simulator runs and 672 virtual windows; they are not production fraud accuracy or live-service calibration. The model remains experimental. Missing, invalid, stale, or out-of-distribution artifacts fall back to the baseline. Model and baseline alerts are advisory and can link to evidence, but they never route, settle, reverse, approve, reject, or recover a payment.

## Drift and limitations

Monitor missing-feature ratio, minimum samples, feature distribution changes, artifact age, schema version, model version, and threshold. The default model age limit is thirty days. Drift requests human review; it does not retrain or promote a model automatically. Each saved scored window keeps the model version used at that time.

The synthetic projection-backlog scenario now conserves a virtual event queue, but backlog counters remain excluded from `tx.features.v1`; the six model features do not observe that stress directly. Traffic bursts may also be hard to distinguish without durable denominators. Surface those limitations instead of inventing a root cause.
