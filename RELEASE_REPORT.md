# TransactX Milestone 2 (M2) Platform & Reliability Release Report

Current combined delivery status is **pending release gates**, recorded in
[M1's delivery report](docs/next-gen/DELIVERY_REPORT.md). This historical M2
lane report preserves its reported runs; its completion statements do not
certify native crash coverage, current calibration provenance, an archive or deployment.

## 1. Executive Summary
- **Milestone**: Milestone 2 (M2 Reliability & Platform)
- **Repository Branch**: `v2/m2-reliability-platform`
- **Target Release Base**: `origin/release/transactx-v2`
- **Verification Date**: October 12, 2026
- **Current Status**: **COMPLETE** (Tasks B0 through B13 fully implemented and verified)
- **Draft Pull Request**: [#24 (Add M2 reliability foundations, transactional outbox, and platform recovery)](https://github.com/whoevenisshubham/TransactX/pull/24)
- **Zero Paid Token / Cloud Cost**: **CONFIRMED** (100% native execution; ₹0.00 external spend)
- **Release Ownership Boundary**: M2 code and gate verification are complete. As specified in project rules, M2 does not directly merge into `release/transactx-v2`; M1 owns integration, delivery, and tagging.

---

## 2. Toolchain & Runtime Baseline
| Component | Verified Specification | Execution Details |
| :--- | :--- | :--- |
| **Operating System** | Windows 11 (amd64) | Local development and verification environment |
| **Go Toolchain** | Go 1.27.0 (windows/amd64) | All backend packages compiled with `go build ./...` and vetted with `go vet ./...` |
| **Node.js Runtime** | Node.js v20.18+, npm 10.8+ | Frontend verification (`npm test` 85/85 PASS, `npm run build` PASS) |
| **Database Cluster** | PostgreSQL 18.6 | Live database at `postgres://postgres@localhost:5432/transactx?sslmode=disable` |
| **Migration Sequence** | 000001 through 000028 | 28 applied migrations, 0 pending, 28 checksums verified |
| **Token / Cloud Spending** | Zero external API spending | Default `evidence_only` mode, local PostgreSQL FTS, native statistics |

---

## 3. Milestone 2 Gate Verification Matrix (Tasks B0–B13)

| Task | Title | Key Artifacts & Commits | Verification Command | Gate Status |
| :--- | :--- | :--- | :--- | :--- |
| **B0** | Verify baseline & build migration tooling | `backend/cmd/migrate`, `internal/database`, `000022..000028` (`fdfd6d8`) | `cmd/migrate status` & `cmd/migrate verify` (28/28 verified) | **VERIFIED** |
| **B1** | Unify serialization & continuation | `internal/payments/resume.go`, ADR-032 (`52da61c`) | Integration tests with advisory locking and state re-reads | **VERIFIED** |
| **B2** | Prove safe operation absence & replay | `internal/bankservice`, `internal/payments/presence_replay_test.go` (`df554a6`) | 4 presence integration tests on PostgreSQL 18.6 | **VERIFIED** |
| **B3** | Durable recovery scheduling & leases | Migration 000022, `internal/recovery`, ADR-033 (`d297653`) | 9 scheduler integration tests on PostgreSQL 18.6 | **VERIFIED** |
| **B4** | Wire native worker & shared composition | `cmd/worker`, `internal/app`, `docs/deployment/native-worker.md` (`46a423a`) | 5 lifecycle & bank outage convergence tests | **VERIFIED** |
| **B5** | Harden public roles & participant access | Migration 000023, `internal/audit`, `internal/http`, ADR-039 (`8e7325d`) | Role matrix (6 suites), viewer denial, devseed check | **VERIFIED** |
| **B6** | Produce transactional outbox facts | Migration 000024, `internal/outbox`, ADR-034 (`29990d2`) | 8 outbox integration test suites on PostgreSQL 18.6 | **VERIFIED** |
| **B7** | Deliver & rebuild worklist projection | Migration 000025, `internal/projections`, `cmd/rebuild-worklist` (`29d1efa`) | Idempotent inbox, version guard, rebuild CLI tests | **VERIFIED** |
| **B8** | Expose read evidence & worklist APIs | `internal/operations`, `internal/http`, `docs/api/operations.md` (`543c64e`) | 6 operations unit tests, 4 HTTP integration suites | **VERIFIED** |
| **B9** | OpenTelemetry & distributed tracing | Migration 000026, `internal/telemetry`, ADR-040 (`e572315`) | 6 telemetry test suites on PostgreSQL 18.6 | **VERIFIED** |
| **B10** | Durable operational analytics windows | Migration 000028, `internal/analytics`, ADR-037 (`10845f4`) | 8 analytics test suites, temporal cutoff verified | **VERIFIED** |
| **B11** | Mount investigator & corpus packages | Migration 000027, `internal/investigator`, ADR-036 (`74f8042`) | 6 investigator suites, 9-phase HTTP contract test | **VERIFIED** |
| **B12** | Prove crash safety & event restart | `internal/experiments/crash_matrix.go`, ADR-038 (`fa1454c`) | 20 fault boundary scenarios (20/20 PASS in 0.91s) | **VERIFIED** |
| **B13** | Close backend release gates & resolve CI | `RELEASE_REPORT.md`, `docs/next-gen/state/M2.md` (`5b779f0`) | Resolved 4 CI failures (REC-08 lease clock, export isolation, trace flush), full backend/frontend suites | **VERIFIED** |

---

## 4. Architectural Proofs & Invariants Verified

### 4.1 Financial Invariants Under Chaos & Crash (Task B12 & ADR-038)
The experiment matrix (`backend/internal/experiments/crash_matrix.go`) executed 20 exhaustive failure scenarios against live PostgreSQL 18.6:
- **14 Recovery Fault Boundaries (REC-01 to REC-14)**:
  - REC-01 (Pre-hold kill in VALIDATING): Resumes under per-payment lock, completes successfully.
  - REC-02 (Central journal committed, request dropped): Resumes and verifies hold.
  - REC-03 (Bank hold committed, reply dropped): Re-reads hold and proceeds to provisional credit.
  - REC-04 (Mid-credit crash): Resolves status and settles.
  - REC-05 (Pre-confirm interruption): Confirms hold and settles.
  - REC-06 (Pre-ledger kill): Completes central settlement.
  - REC-07 (Post-ledger race): Client duplicate (`CreateWithResult`) races background recovery (`Service.Resume`); session advisory lock serializes, returns exact completed payment with zero duplicate ledger rows.
  - REC-08 (Worker crash mid-lease): Second worker steals expired lease via `FOR UPDATE SKIP LOCKED` and drives to resolution.
  - REC-09 (PostgreSQL connection terminated mid-saga): `pgxpool.Pool` purges severed backend connection, reconnects, and completes payment.
  - REC-10 (Pool constriction & load): Executed under `MaxConns = 4`; zero connection leaks or deadlocks.
  - REC-11 (Route removed): Persisted payment referencing removed adapter parks safely in `needs_attention` without modifying account balances.
  - REC-12 (Presence absent replay): Transient 500 on provisional credit queries `OperationPresenceReader`, verifies `ABSENT`, safely replays with original UUID.
  - REC-13 (Presence present convergence): Querying presence discovers `PRESENT`, avoids duplicate credit, and completes payment.
  - REC-14 (Attempt exhaustion): 20 unsuccessful attempts trigger safe transition to `needs_attention` (`ATTEMPTS_EXHAUSTED`).
- **6 Event Restart Invariants (EVT-01 to EVT-06)**:
  - EVT-01: Outbox events emitted atomically within payment transaction; rollback produces zero events.
  - EVT-02: Outbox dispatcher killed after claim; secondary dispatcher claims upon lease expiration.
  - EVT-03: Worklist projection processes duplicate deliveries idempotently via `consumer_inbox`.
  - EVT-04: Non-decreasing `event_version` guard blocks out-of-order state regression.
  - EVT-05: Malformed event payload quarantined with `UNSUPPORTED_SCHEMA_VERSION` without blocking adjacent deliveries.
  - EVT-06: Offline CLI utility (`cmd/rebuild-worklist`) acquires exclusive transaction advisory lock, diffs staging tables, and performs atomic projection rebuild.
- **Financial Assertion Summary**:
  - `NonnegativeBalances = true`
  - `DebitCreditConservation = true` ($\sum \text{debits} = \sum \text{credits} = 19,100 \text{ paise}$)
  - `SingleLedgerPerPayment = true`
  - `IdempotencyUnique = true`
  - `ParticipantUnique = true`
  - `Total Violations = 0`

### 4.2 Security, Roles, and Gated Controls (Task B5 & ADR-039)
- **Self-Registration Boundary**: Public registration accepts only `CUSTOMER` and `MERCHANT`. Attempts to register `OPS_ADMIN`, `OPS_VIEWER`, or privileged roles reject with 400 Bad Request.
- **Role Enforcement Matrix**:
  - Unauthenticated requests to `/api/ops/*` receive 401 Unauthorized.
  - Customer/Merchant tokens receive 403 Forbidden on all operational endpoints.
  - `OPS_VIEWER` permitted on operational GETs (worklist, evidence, traces, analytics, investigations); denied with 403 Forbidden on mutations (`/api/ops/payments/{id}/recovery/recheck`).
  - `OPS_ADMIN` permitted on both reads and mutations.
- **Gated Runtime Controls**: `AllowRegistration`, `ChaosEnabled`, `RecoveryControlEnabled` fail closed in production and are reported via `GET /api/ops/runtime`.
- **Participant Service Tokens**: Banking participants authenticate inter-service requests with constant-time comparison against configured service tokens.

### 4.3 Transactional Outbox & Worklist Projection (Tasks B6, B7, B8 & ADR-034, ADR-035)
- Monotonic `event_version` incremented by PostgreSQL trigger on state changes.
- Append-only `outbox_events` and durable `outbox_deliveries`.
- Idempotent `consumer_inbox` deduplication.
- Worklist maintenance protected by dedicated advisory lock (`WorklistMaintenanceLockKey = 78891001`).
- Standalone `rebuild-worklist` CLI supporting `--preview` diff and atomic rebuild with operational audit logging.

### 4.4 Distributed Tracing & Observability (Task B9 & ADR-040)
- Full OpenTelemetry W3C Trace Context propagation across all binaries (`transactx-api`, `transactx-worker`, `transactx-bank-a`, `transactx-bank-b`).
- Bounded, non-blocking PostgreSQL exporter with 1,000-span buffer capacity. Overload drops spans without failing monetary transactions.
- Strict attribute cardinality bounds (20 attributes, 256 char strings, automatic credential/PII redaction).
- OpenTelemetry span links (`trace.Link`) correlating asynchronous recovery attempts to original payment traces.
- Automated 24-hour TTL and 20,000-row table capacity cleaner.
- Read API `GET /api/ops/traces/{traceID}` with safe `not_collected` response for missing spans.

### 4.5 Operational Analytics Windows (Task B10 & ADR-037)
- 5-minute non-overlapping completed windows (`tx.features.v1`) evaluated strictly as of `transitioned_at <= window_to` using `payment_state_transitions` to eliminate retrospective leakage.
- 20-sample minimum threshold (`MinimumSampleCount = 20`) returning `insufficient_data` when under-sampled.
- Statistical baseline in pure native Go alerting on failure rate $\ge 15\%$ and timeout rate $\ge 10\%$.
- Worker aggregation loop advancing every 30 seconds with cursor checkpointing.

### 4.6 Payment Investigator & Runbook Corpus (Task B11 & ADR-036)
- Pure native Go `evidence_only` execution mode with zero external LLM API dependencies.
- Grounded facts and typed assertions citing stable `evidenceIds`.
- Local PostgreSQL full-text search over versioned runbooks in `runbook_corpus_chunks`.
- Concurrency ceiling: max 1 active job per actor, max 2 globally; rate limit: 3 requests/min/actor.
- Distributed worker lease claim using `FOR UPDATE SKIP LOCKED` and Compare-And-Swap (CAS) lease protection.

---

## 5. Test Suite Verification Summary

| Package / Suite | Tests | Result | Duration | Notes |
| :--- | :--- | :--- | :--- | :--- |
| `backend/internal/database` | 24 | **PASS** | 5.8s | Migrations, locks, checksums, disposable DB |
| `backend/internal/config` | 13 | **PASS** | 0.05s | Runtime flags, participant tokens, pool reserve |
| `backend/internal/recovery` | 9 | **PASS** | 1.1s | Lease claims, epochs, backoff, sweeper |
| `backend/cmd/worker` | 5 | **PASS** | 1.2s | Graceful shutdown, restart, outage recovery |
| `backend/internal/bankservice` | 10 | **PASS** | 0.8s | Presence, service tokens, replay, compensations |
| `backend/internal/outbox` | 8 | **PASS** | 0.9s | Atomic triggers, rollback, event versioning |
| `backend/internal/telemetry` | 6 | **PASS** | 0.4s | W3C context, span links, bounded drop |
| `backend/internal/analytics` | 8 | **PASS** | 0.6s | Window cutoffs, leakage safety, metrics summary |
| `backend/internal/investigator` | 6 | **PASS** | 0.5s | Evidence reader, corpus search, CAS execution |
| `backend/internal/http` | All suites | **PASS** | 2.1s | Operations APIs, role matrix, RBAC, investigation |
| `backend/internal/payments` | All suites | **PASS** | 5.2s | Resumes, concurrency stress, presence replay |
| `backend/internal/experiments` | 20 Scenarios | **PASS** | 0.91s | 14 recovery boundaries, 6 event restart invariants |
| `frontend` (TypeScript / Vite) | 85 | **PASS** | 0.65s | Shell routing, console, offline queue, QR, SSE |
| `frontend` Production Build | Build | **PASS** | 0.33s | `tsc -b && vite build` (dist size: 385 kB JS, 43 kB CSS) |

---

## 6. Known Operational Boundaries & Limitations
1. **Live PostgreSQL Authority**: TransactX requires a running PostgreSQL 18 instance with `DATABASE_URL` configured. In-memory mode is reserved for unit testing; production state requires relational durability.
2. **Phase C5/C6 Anomaly Scoring Handoff**: Feature aggregation windows are fully persisted in `tx.features.v1` format. The runtime currently surfaces `modelStatus: unavailable` pending integration with Milestone 3 (C5/C6) external scorers.
3. **Database Concurrency Reserve**: The platform strictly enforces a connection pool reserve invariant (`dbMaxConns - paymentConcurrency >= 2`) to guarantee administrative and migration access under peak payment load.

---

## 7. Delivery & Release Sign-Off
All Milestone 2 tasks (B0 through B13) are complete, validated against live PostgreSQL 18.6, and committed to `v2/m2-reliability-platform`.

- **Draft Pull Request**: https://github.com/whoevenisshubham/TransactX/pull/24
- **Branch**: `v2/m2-reliability-platform` (tracking `origin/v2/m2-reliability-platform`)
Branch is clean, tested, pushed to origin, and ready for M1 release integration.
