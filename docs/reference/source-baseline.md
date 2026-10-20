# Source baseline and gap map

## Provenance

On 11 October 2026, `git ls-remote --symref` returned default branch `m1/foundation` and HEAD `2b9f6e6702d2db54651d71859f29d6cbc3eb90fc`. A shallow checkout of that branch was inspected for this plan. The 30 September handoff supplies historical reasoning; attached prompts supply prior user intent. Instructions embedded in the handoff are context, not new execution authority.

This planning run did not rerun the complete release test suite or deploy the application. Existing recorded experiment results are historical evidence, not fresh measurements.

## Existing implementation

| Capability | Source locations | Current reality |
| --- | --- | --- |
| Payment and local settlement | `backend/internal/payments`, `accounts`, `ledger` | Integer paise, account ownership, double-entry local settlement, one central transaction, payment state machine |
| Authentication | `backend/internal/auth`, `users`, HTTP middleware | JWT, bcrypt, CUSTOMER, MERCHANT, OPS_ADMIN; backend permissions are authoritative |
| Bank participants | `backend/cmd/bank-a`, `bank-b`, `internal/bankservice`, `internal/bank` | Independent HTTP processes and schema ownership on a shared PostgreSQL server; not independent physical databases |
| Routed settlement | `payments/routed.go`, `service.go` | Durable operation journal, stable operation UUIDs, holds, provisional credit, source confirmation, destination finalization, compensation, central completion |
| Recovery | `RecoverRoutedPayment`, `RecoverBankSettledCentralPending`, duplicate create paths | Request-driven recovery exists. A continuous discovery and recovery worker does not |
| Routing and faults | `internal/health`, `circuit`, `chaos`, `payments/routing.go` | Deterministic health-aware route selection, target identities, breaker states, controlled simulation faults |
| Reconciliation and integrity | `internal/reconciliation`, `cmd/maintain-merkle` | Persisted commitments, generation checks, proof verification, 10K/100K experiments, read-only financial checks, debounced automatic integrity coordinator |
| Operational events | `http/activity_feed_handler.go`, `operational_stream_handler.go` | Durable multi-source activity query plus SSE invalidation hints; not a transactional event delivery pipeline |
| Customer and merchant UI | `frontend/src/main.tsx`, `money.ts`, `qr.ts`, `offlineQueue.ts`, `offlineReplay.ts` | Role views, payment creation/history/details, merchant QR, IndexedDB intent queue and retry-safe replay |
| Operator console | `frontend/src/console.tsx` | Health, routing, reconciliation, Merkle, integrity, chaos, activity; resilient refetch on stream gaps |
| CI | `.github/workflows/release-validation.yml` | PostgreSQL 18 service, Go tests/race/build, frontend tests/typecheck/build |

## Exact compatibility facts

- `backend/go.mod`: Go `1.27.1`; pgx v5; JWT v5; UUID; x/crypto.
- Frontend is Vite, React and TypeScript, with an existing lockfile. `npm run lint` is currently a TypeScript build check, not a full ESLint ruleset.
- `frontend/src/api.ts` and `operationalEvents.ts` each currently resolve `VITE_API_BASE_URL` with a localhost fallback. Centralize this resolution during deployment work.
- HTTP success shape is `{ "requestId": "...", "data": ... }`; errors use `{ "requestId": "...", "error": { "code": "...", "message": "..." } }`.
- `GET /api/payments/{paymentID}` is a read-only, user-scoped DTO. Preserve it. Payment creation can trigger recovery on duplicate attempts.
- CORS currently permits GET, POST, OPTIONS and explicit allowed origins. Avoid introducing PATCH/DELETE just for new operational controls.
- Latest migration is `000021_payment_transition_order`; latest decision is ADR-031. New migration and ADR numbering is reserved in this pack and must be rechecked against upstream.
- `payment_state_transitions.sequence_number` orders a payment's history. It is not a safe global commit cursor.
- The current PostgreSQL pool sets MaxConns=4 and MinConns=1. Session advisory locks hold a connection while monetary work runs; careless concurrency can exhaust the pool.
- `BankAdapter` has nine existing methods. Preserve its method set. A narrowly scoped separate read capability is proposed to address operation absence.

## Critical gaps that drive the plan

1. Recovery entry points are not uniformly protected by the same lock at every service call site. A worker must not call a lower-level method that bypasses serialization.
2. A committed central operation row can exist before its participant request is sent. `bankservice.Service.GetOperationStatus` returns PENDING for a missing operation. Current status semantics cannot prove that the original operation is absent; recovery can stall permanently at this crash boundary.
3. Crash continuation must cover CREATED, VALIDATING, ROUTING, PROCESSING, PENDING_RECONCILIATION, BANK_SETTLED_CENTRAL_PENDING and any reachable COMMITTED state. Local settlement states require a separate proof of reachability; do not apply a routed worker to local payments blindly.
4. The four-connection pool and long-lived advisory locks need explicit concurrency budgets and failure tests before enabling a worker.
5. Public bank endpoints are unsafe to expose directly. Native deployment must bind them to loopback and add service authentication as defense in depth.
6. Existing activity and SSE do not guarantee delivery of a newly committed payment event to a projection. The outbox adds this guarantee for the newly specified payment and recovery event families only.
7. OpenTelemetry, a durable recovery queue, an operator payment evidence API, runtime LLM/RAG investigation, and a trained operational anomaly model are absent.
8. Existing browser tests mostly cover helpers and simulated IndexedDB. Add real browser reload/reconnect verification for the release.
9. Do not describe Merkle reconciliation as universally faster. Existing evidence explicitly contains mixed results across mutation patterns.

## Historical measurements that may be cited with context

The recorded RC1 controlled outage experiment reports 297/300 successful circuit-aware requests versus 200/300 static requests. The recorded source checkpoint is `8e1efb0c463aab539b417de87b6aa38a84fc8fe8`, not the planning HEAD. See `docs/resilience-experiments.md` and its raw artifacts. Retain the environment, scenario and simulation qualifications.

Reconciliation evidence covers 10K and 100K records. Equal roots can prune work; dispersed mismatches may cost more than the naive baseline. Keep the raw metrics and benchmark limitations.

## Startup procedure for every agent

Read the remote default; fetch it; inspect differences from this baseline. Record the actual SHA and working-tree status. If a planned feature now exists, verify it and reuse it. If a shared contract has changed, reconcile through M2 and M1 rather than implementing an incompatible duplicate. Preserve unrelated local changes.
