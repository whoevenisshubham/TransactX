# ADR-037: Durable Operational Aggregate Windows and Analytics

## Status
**IMPLEMENTED** (Task B10 complete)

## Context
TransactX Milestone 2 requires robust operational monitoring to detect degraded service availability, elevated failure rates, or recovery backlogs across banks and execution targets. Operators need early operational warnings to inspect anomalous windows rather than waiting for customer escalation or catastrophic outages.

Crucially, this system operates under strict engineering, consistency, and data leakage rules:
1. **Operational, Not Fraud Analytics**: Detects infrastructural and provider performance anomalies (elevated timeouts, bank gateway errors, recovery queue pileups), NOT user fraud, credit scoring, or automated transaction vetoes.
2. **Leakage-Safe Temporal Cutoff**: A five-minute window covering `[from, to)` must only reflect system state and transitions that occurred before `to`. Terminal state changes (such as delayed settlement or recovery completing hours later) must NEVER leak backward into historical windows.
3. **Durable Numerators and Denominators**: Windows must derive from immutable database facts (`payments`, `payment_state_transitions`, `recovery_attempts`, `health_samples`, `outbox_events`), not ephemeral process-local counters that reset on restart.
4. **Idempotent Storage and Retry Safety**: Retries of the worker aggregation loop must safely upsert into `operational_aggregate_windows` without duplicate key errors or partial corruption.
5. **Cold-Start and Minimum Sample Gates**: Scored metrics require a minimum threshold of 20 samples (`MinimumSampleCount = 20`). Windows with fewer observations must report `insufficient_data` and suppress confident alerts.
6. **Pure Go Statistical Baseline**: A transparent baseline runs in native Go without external Python or machine learning runtime dependencies, ensuring high-availability anomaly alerting even if ML artifacts are absent.

## Decisions

### 1. Bounded Five-Minute Completed Windows and Half-Open Ranges
- Aggregation is strictly partitioned into completed 5-minute half-open intervals: `[window_from, window_to)` where `window_to = window_from + 5 minutes`.
- Windows are evaluated per entity (`entity_type = 'bank' | 'execution_target'`, `entity_id = bank_code | target_id`).
- Migration 000028 defines table `operational_aggregate_windows` with a unique constraint on `(entity_type, entity_id, window_from, feature_schema_version)` and index on `(entity_type, entity_id, window_from DESC)`.

### 2. Leakage-Safe Historical State Reconstruction
- In `backend/internal/analytics/repository.go`, `AggregateWindow` evaluates payment states strictly at `transitioned_at <= window_to` using `payment_state_transitions`.
- A payment created within `[from, to)` that only failed or succeeded after `to` is accurately classified as pending within that window.
- Recovery attempts and health sample availability are bounded strictly within `[from, to)`.

### 3. Frozen Feature Vector Schema (`tx.features.v1`)
- Standardized ordered feature vector conforming to `v2.schema.json`:
  - `timeoutRate`: Ratio of timed-out payments to attempts in `[0, 1]`.
  - `failureRate`: Ratio of permanently failed payments to attempts in `[0, 1]`.
  - `p95LatencyMs`: 95th percentile execution latency in milliseconds.
  - `oldestRecoveryAgeMs`: Oldest pending recovery job age at window close.
  - `recoveryAttemptsPerPayment`: Average recovery attempts executed per payment.
  - `healthAvailabilityRate`: Successful health check probe ratio in `[0, 1]`.
- Missing measurements are explicitly serialized as `null`, avoiding synthetic zero substitutions.

### 4. Minimum Sample Gates and Statistical Baseline
- If `sample_count < 20`, the model status is marked `insufficient_data`, and baseline alerts are suppressed.
- When `sample_count >= 20`, a native Go statistical rule evaluates deviations (e.g. failure rate >= 15% or timeout rate >= 10%), emitting a structured `BaselineAlert` with diagnostic reason codes (`ELEVATED_FAILURE_RATE`, `ELEVATED_TIMEOUT_RATE`).

### 5. Persistent Aggregation Cursor and Worker Loop
- Worker progress is tracked in `operational_aggregate_cursors` under key `five_minute_window_aggregator`.
- The worker executes `service.AggregateCompletedWindows(ctx)` every 30 seconds.
- Aggregation processes sequentially from the recorded cursor up to the latest completed 5-minute boundary, advancing the cursor atomically after each window batch.

### 6. Operational REST APIs
- `GET /api/ops/analytics/windows`:
  - Accessible to `OPS_ADMIN` and `OPS_VIEWER` roles.
  - Supports filtering by `entityType`, `entityId`, `from`, `to`, and base64url cursor pagination (`cursor`, `limit`).
  - Returns `AnalyticsPage` with `items`, `nextCursor`, and `asOf`.
- `GET /api/ops/metrics/summary`:
  - Accessible to `OPS_ADMIN` and `OPS_VIEWER` roles.
  - Supports optional `asOf` timestamp.
  - Returns durable decimal string counters across payments, recovery, outbox, runtime, and observational sources with provenance.
- `GET /api/ops/runtime`:
  - Includes `"analyticsEnabled": true` and `"modelStatus": "unavailable"` (pending M3 C5 model wiring).

## Consequences
- **Positive**: Operators gain real-time, durable, historical visibility into banking and route performance without data leakage or reliance on ephemeral memory counters.
- **Positive**: Strict minimum sample checks prevent noisy false alarms during low-traffic periods or startup.
- **Positive**: Full test suite verifies idempotency, pagination, schema compliance against `v2.schema.json`, and transition cutoff on PostgreSQL 18.6.
- **Neutral**: Machine learning isolation forest model integration remains decoupled and ready for M3 Task C5.
