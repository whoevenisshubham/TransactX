# ADR-040: OpenTelemetry Distributed Tracing, Bounded Storage, and Span Links

## Status
**IMPLEMENTED** (Task B9 complete)

## Context
TransactX Milestone 2 requires comprehensive distributed tracing across all four platform processes (`transactx-api`, `transactx-worker`, `transactx-bank-a`, `transactx-bank-b`) and across external bank HTTP boundaries. Distributed operational investigation demands correlated execution traces for payment intents, route selections, bank authorizations, settlement completions, and outbox event deliveries.

However, observability infrastructure must operate under strict reliability and security constraints:
1. **Zero Financial Impact**: Telemetry export or database storage failures must NEVER fail a payment, abort a database transaction, or block settlement execution. If trace export degrades, the platform must degrade visibility gracefully, not availability or consistency.
2. **Cardinality and Security Bounds**: Traces must never leak sensitive authentication credentials, JWTs, customer names, passwords, notes, or idempotency keys. Attribute count and length must be strictly bounded.
3. **Causal Linkage Across Restarts**: Background recovery jobs running seconds or minutes after an interrupted payment cannot manufacture synthetic parent spans that pretend a process never crashed. Instead, a new recovery trace must correlate causally with the original trace using OpenTelemetry span links (`trace.Link`).
4. **Bounded Storage and Retention**: In self-hosted and local demonstration modes without paid external SaaS dependencies, traces must be durably stored in a bounded PostgreSQL table (`trace_spans`) with strict queue capacity, drop-on-overload semantics, and automated TTL cleanup.

## Decisions

### 1. W3C Trace Context Propagation
- Configured W3C standard trace context propagation globally using `propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})`.
- HTTP server boundaries (`HTTPMiddleware`) extract incoming `traceparent` and `tracestate` headers and inject `X-Trace-ID` into response headers.
- HTTP client outbound calls (`ClientTransport`) inject W3C trace headers into outbound requests to participants (Bank A, Bank B), enabling end-to-end trace correlation across independently deployed services.

### 2. Service Identification and Resources
- Configured OpenTelemetry resources identifying each process:
  - `transactx-api`: Central payment gateway and operational API.
  - `transactx-worker`: Durable background recovery and outbox delivery worker.
  - `transactx-bank-a`: Bank A participant core adapter.
  - `transactx-bank-b`: Bank B participant core adapter.
- Resources include `service.name`, `service.version` (Build SHA), and `deployment.environment`.

### 3. Non-Blocking Bounded PostgreSQL Exporter (`BoundedPostgresExporter`)
- Implemented in `backend/internal/telemetry/exporter.go`:
  - Uses an asynchronous, buffered Go channel bounded to 1,000 spans (`QueueCapacity`).
  - If the export queue is full due to database pressure or overload, spans are immediately dropped (`select { case queue <- rec: default: metrics.DroppedSpans.Add(1) }`).
  - `ExportSpans` NEVER blocks callers and NEVER returns an error to the runtime.
  - A background worker flushes batches (up to 100 spans or 250ms timeout) to the `trace_spans` table.
  - Database write errors are logged as debug/warn telemetry events and discarded without affecting caller transactions.

### 4. Attribute Sanitization and Cardinality Limits
- Enforced strictly prior to serialization:
  - Maximum 20 attributes per span.
  - Maximum attribute string length of 256 characters (truncated safely).
  - Explicit sensitive key blacklist: any attribute containing `auth`, `token`, `password`, `secret`, `jwt`, `cookie`, `idempotency`, `prompt`, `url`, `note`, `customer_name`, or `identifier` is stripped.
  - Permitted operational attributes: `payment_id`, `operation_id`, `bank_code`, `target_id`, `reason_code`, `event_id`, `status`, `route_key`, `error_code`, `http.method`, `http.route`, `http.status_code`.
  - Automatically extracts `payment_id` into a indexed UUID column on `trace_spans` for indexed correlation with operational evidence bundles.

### 5. Asynchronous Recovery Span Links
- Implemented in `backend/internal/telemetry/links.go` via `StartRecoverySpan`:
  - When the recovery worker resumes an interrupted payment, it initiates a new root span (`trace.WithNewRoot()`), establishing a new independent trace ID.
  - If the original payment trace ID and span ID are present on the recovery job, the new span attaches an OpenTelemetry `trace.Link` pointing to that original span context.
  - This faithfully preserves causal provenance without distorting span duration hierarchies across service restarts.

### 6. Bounded Storage Schema and Cleanup (Migration 000026)
- Table `trace_spans`:
  - Composite primary key `(trace_id, span_id)`.
  - Indexes: `idx_trace_spans_trace_id` for trace queries, `idx_trace_spans_payment_id` for payment evidence correlation, `idx_trace_spans_created_at` for retention sweeps.
  - Bounded TTL cleanup: deletes spans older than 24 hours (`RetentionHours`) and caps total stored spans to 20,000 (`MaxStoredSpans`).

### 7. Structured JSON Logging with Trace Correlation
- Implemented `TraceCorrelatingHandler` for `slog`:
  - Automatically extracts `trace_id` and `span_id` from the active context.
  - Injects `trace_id` and `span_id` into standard JSON stdout logs (`slog.NewJSONHandler`).
  - Correlates application logs with traces without requiring experimental logging SDKs.

### 8. Operational Read-Only Trace API (`GET /api/ops/traces/{traceID}`)
- Mounted under role-based access control permitting `OPS_ADMIN` and `OPS_VIEWER`.
- Validates trace ID format (strict 32-character lowercase hex string).
- Returns allowlisted DTO matching `API_CONTRACTS.md` capped at 200 spans.
- If a trace has expired or was not collected, returns `200 OK` with `sourceStatus: "not_collected"` and an empty spans array, explicitly indicating absence of telemetry rather than payment failure.

## Consequences
- **Positive**: Complete distributed observability across all services without any third-party infrastructure cost; strict privacy and security boundary preventing credential leakage; non-blocking exporter guarantees zero impact on payment transactions; causality across background recovery runs preserved via standard span links.
- **Trade-offs**: Local PostgreSQL storage introduces slight write overhead during demo load (mitigated by batching and drop-on-overload); head sampling (default 0.2, test 1.0) means not every trace is retained in high-throughput environments unless configured to 1.0.
