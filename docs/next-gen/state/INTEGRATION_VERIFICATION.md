# TransactX — M1/M2/M3 Integration & Release Verification Checkpoint

**Date:** 2026-10-13  
**Status:** INTEGRATED & REVERIFIED (GATE 4 COMPLETE)  
**Integration Branch:** `v2/integration-m1-m2-m3`  
**Base:** `origin/release/transactx-v2` (`39fc5c7`)  
**Integrated Commits:**
- **M1 Head (`origin/v2/m1-product-delivery`):** `42d8aef7935b63a9f80a78705f243798b62401c9` (PR #22)
- **M2 Head (`origin/v2/m2-reliability-platform`):** `c9da5fb9035dff259c936fb79785eecbd93efde6` (PR #24)
- **M3 Head (`origin/v2/m3-investigation-analytics`):** `4100dd713ade1e1ddd078bbaa78a5436f5825561` (PR #23)
- **Gate 3 CI Head:** `a3f3d2969a4bbe6079d472c77761e568782f3d2b` (Green on GitHub Actions #38372702145)
- **Gate 4 Integration Work:** Sync latest M1 changes, close native runtime acceptance failures across crash recovery, SSE flusher, trace schema, and investigation citations.

---

## 1. M2 Reliability, Platform & Integration Findings

### 1.1 Durable Investigator Store Conformance
- Coexistence of migration `000029_m3_investigator_store.up.sql` with M2 migrations (000001–000028) verified on PostgreSQL 18.
- Conformance suite `TestPostgresInvestigationStoreConformance` executed against live PostgreSQL 18:
  - `idempotency_scope_and_copy_isolation`: PASSED.
  - `lease_fencing_and_checkpoint_resume`: PASSED.
  - `transactional_usage_reservations`: PASSED.
  - `uncertain_usage_is_lease_fenced_and_retains_budget`: PASSED.
- Full `internal/investigator` test suite (31/31 tests) passed against PostgreSQL 18. Fixed repository root resolution in `corpus_test.go` and `app.go` to locate `docs/runbooks/corpus-manifest.json` robustly regardless of execution CWD.

### 1.2 Analytics & Operational Baseline Integration
- Reconciled the documented B10 integration gap in `backend/internal/analytics/repository.go`.
- Replaced fixed cutoffs with M3's `anomaly.FitLiveBaseline` and `anomaly.EvaluateLiveFeatures`:
  - Queries prior healthy windows (`entity_type = $1 AND entity_id = $2 AND window_to <= $3 AND baseline_alert IS NULL AND sample_count >= 20`).
  - Calibrates median/MAD deviations with frozen threshold 3.0 via `FitLiveBaseline`.
  - When baseline status is in warmup (< 8 eligible prior healthy windows), transparently falls back to operational thresholds (`HIGH_FAILURE_RATE` for `failureRate > 0.15`, `HIGH_TIMEOUT_RATE` for `timeoutRate > 0.10`).
  - When sample count < 20, evaluates to `insufficient_data` with zero spurious baseline alerts.
  - Passes server-owned observation time `to.UTC()` into `EvaluateLiveFeatures` and persists scoring outcomes.
  - Added input validation in `AggregateWindow` rejecting invalid entity types, empty IDs, zero timestamps, inverted boundaries (`!from.Before(to)`), non-5-minute durations, and future windows.
  - Added regression tests `TestAnalytics_FitLiveBaseline_PriorHealthyCalibrationAndAlerts` and `TestAnalytics_AggregateWindow_RejectsMalformedAndFutureInputs` in `analytics_test.go`.

### 1.3 Outbox and Projections Worker Wiring
- Added `ProjectionsService` into platform `Environment` (`backend/internal/app/app.go`) and wired the native worker loop in `backend/internal/app/worker.go` (loop 8) to run `ProjectionsService.ProcessBatch` every 200ms.
- Strengthened `backend/cmd/rebuild-worklist/main.go` to acquire `pg_try_advisory_xact_lock` on the exact transaction executing the atomic rebuild, ensuring the advisory lock is held exclusively by the PostgreSQL transaction for the entire duration of the rebuild (`CREATE TEMP`, count validation, `TRUNCATE`, `INSERT SELECT`) and automatically released on commit/rollback without lingering connection pool leaks.
- Verified `cmd/rebuild-worklist -preview` (reported 0 discrepancies when in sync) and executed clean full rebuilds.

### 1.4 Recovery & Financial Integrity (Crash Matrix B12)
- Executed full Crash Matrix (`TestCrashMatrix_FullRecoveryAndEventMatrix`) against live PostgreSQL 18:
  - 20 of 20 crash scenarios passed in 0.77s.
  - Financial invariant checks verified with zero violations:
    - Nonnegative balances: true
    - Debit/credit conservation: true
    - Single ledger transaction per payment: true
    - Idempotency uniqueness: true
    - Participant uniqueness: true
    - Total violations: 0.
- Executed `internal/recovery` tests against live PostgreSQL 18:
  - Two claimers / no double execution: PASSED.
  - Lease expiry and epoch takeover: PASSED.
  - Stale worker late result rejection: PASSED.
  - Attempt exhaustion parking (`needs_attention`): PASSED.
  - Sweep interrupted attempts: PASSED.
  - Worker restart and recovery continuation: PASSED.

### 1.5 HTTP & Worker Composition
- Confirmed operations endpoints `/api/ops/investigations` use canonical M3 service (`h.investigatorSvc`) and enforce actor/role/scope boundaries (`auth.Authentication` + `RequireRole`).
- Ensured investigator functionality is not mounted twice through competing services.
- Confirmed read-only operations and SSE activity stream (`/api/ops/worklist/stream`) remain intact.
- Full `internal/http` test suite passed against live PostgreSQL 18.

---

## 2. Release & Verification Evidence Matrix

| Gate / Suite | Verification Command | Environment | Result | Evidence / Artifact |
|---|---|---|---|---|
| **Investigator Conformance** | `go test -v ./internal/investigator -run TestPostgresInvestigationStoreConformance` | Live PostgreSQL 18 | PASS (0.79s) | Idempotency, scope, lease fencing, budget reservations pass |
| **Analytics Live Baseline** | `go test -v ./internal/analytics` | Live PostgreSQL 18 | PASS (0.24s) | FitLiveBaseline calibration, warmup fallback, input rejection |
| **Outbox & Projections** | `go test -v ./internal/projections ./internal/outbox` | Live PostgreSQL 18 | PASS (0.70s) | Idempotent inbox, out-of-order rejection, batch acknowledgement |
| **Worker Loops & Outage** | `go test -v ./cmd/worker` | Live PostgreSQL 18 | PASS (0.95s) | Heartbeats, restart on unchanged DB, outage deferral convergence |
| **Crash Matrix (B12)** | `go test -v ./internal/experiments -run TestCrashMatrix_FullRecoveryAndEventMatrix` | Live PostgreSQL 18 | PASS (0.77s) | 20/20 scenarios pass; financial invariants: 0 violations |
| **Recovery Leases** | `go test -v ./internal/recovery` | Live PostgreSQL 18 | PASS (0.88s) | Two claimers, lease expiry takeover, stale worker rejection |
| **HTTP Security Matrix** | `go test -v ./internal/http` | Live PostgreSQL 18 | PASS (2.1s) | Role matrix, token auth, SSE stream, investigator API |
| **Projection Rebuild CLI** | `go run ./cmd/rebuild-worklist -preview` | Live PostgreSQL 18 | PASS | Verified 1866/1866 rows in sync; rebuild ran in 26ms |
| **Restore Rehearsal** | `powershell -File ...\restore_rehearsal.ps1` | Disposable PostgreSQL 18 | PASS (5.2s) | pg_dump custom format -> dropdb/createdb -> pg_restore clean; 3 schemas, 0 negative balances, 0 unbalanced transactions, worker continuation & projection rebuild verified |
| **Frontend Unit Tests** | `npm test` | Node.js v22 | PASS (0.50s) | 95/95 tests pass across 16 suites |
| **Frontend Money Parsing** | `npm run test:money` | Node.js v22 | PASS (0.05s) | Integer paise parsing and boundary validation |
| **Browser Fixtures** | `npm run test:browser:fixtures` | Playwright Chromium | PASS (11.9s) | 8/8 tests pass (360px viewport, axe a11y, citations, evidence) |
| **Browser Offline Shell** | `npm run test:browser:offline-shell` | Playwright Chromium | PASS (4.4s) | 2/2 tests pass (offline reload, token expiration cache clearing) |
| **Release Script Validation** | `powershell -File .\deploy\scripts\build-release.ps1 -ValidateOnly` | Windows / Local | PASS (0.8s) | All 6 v2 commands present; reports clean validation status |

---

## 3. Honest Release Gates & Remaining Prerequisites

1. **Automated CI vs. Local PostgreSQL Evidence:**
   - Automated CI on GitHub Actions runs in Ubuntu containers with ephemeral PostgreSQL services. CI run #38357167992 passed 100% green.
   - Local verification was performed against native PostgreSQL 18, exercising real transactional isolation, advisory locks, custom format backups, and live process restarts.

2. **Live Browser Authenticated Replay (`*.live.spec.ts`):**
   - Playwright fixture suite (8/8) and offline shell suite (2/2) ran and PASSED locally.
   - The opt-in live browser test (`npm run test:browser:live`) requires an HTTPS loopback API (`api.protocol === 'https:'`) with pre-provisioned TLS certificates and credentials. This is an environment/deployment gate that cannot be run without local TLS certs installed.

3. **Release Packaging (`build-release.ps1`):**
   - `-ValidateOnly` confirms that all required planned v2 binaries (`api`, `bank-a`, `bank-b`, `worker`, `migrate`, `maintain-merkle`) compile and build cleanly.
   - Production archive packaging requires `TRANSACTX_FRONTEND_API_ORIGIN` set, a clean git working tree, and tar generation. This will be produced upon final release sign-off.

4. **Off-VM Restore & Cloud RPO/RTO:**
   - Restore rehearsal was executed and verified against a fresh disposable local database.
   - True off-VM backup persistence, offsite cloud storage replication, and multi-region disaster recovery remain operational deployment gates.

5. **Milestone PR Status:**
   - Milestone PRs #22 (M1), #23 (M3), and #24 (M2) remain open and unmerged per instructions.
   - Candidate PR #25 is updated on branch `v2/integration-m1-m2-m3` and kept in draft mode for final release-owner review.

---

## 4. Gate 4 Integration & Native Runtime Acceptance Verification

### 4.1 Safe Synchronization of M1 (`origin/v2/m1-product-delivery` @ `42d8aef`)
- History-preserving merge from `origin/v2/m1-product-delivery` into `v2/integration-m1-m2-m3` completed with zero conflicts.
- Preserved existing M2/M3 composition, canonical migration 000029, live analytics calibration, worker projection processing, advisory transaction rebuild locks, and established CI fixes.

### 4.2 Crash Recovery & Operation Presence (B12 & Adaptive Execution)
- **Root Cause & Fix (Adaptive Route Execution):** In `backend/internal/payments/service.go`, `RoutedAdaptersForPayment` was updated to check configured `routeTargets` (used by default adaptive routing) and ensure `paymentRepository.SetAdapterResolver` is wired across all service constructors. Background recovery jobs now resolve routed adapters deterministically without losing target mappings.
- **Root Cause & Fix (ChaosAdapter Presence Delegation):** In `backend/internal/chaos/adapter.go`, `ChaosAdapter` now implements `bank.OperationPresenceReader` via checked delegation to `underlying.(bank.OperationPresenceReader)`, preventing lost presence inspection capability when chaos wrappers are composed.
- **Accurate Needs Attention Reasons:** In `backend/internal/recovery/scheduler.go`, `JobStatusNeedsAttention` now preserves `resumeResult.Reason` in `reasonCode` rather than collapsing all attention cases into generic `ReasonAttention`.
- **Presence vs. Unknown Outcomes:** Verified recovery distinguishes confirmed absent participant operations from unknown/unavailable results, preventing blind retries without authoritative evidence.
- **Verified on PostgreSQL 18:** Adaptive and explicit-target interruption recovery tested with SQL assertions on hold UUID, participant effects, final payment states, and ledger balance conservation.

### 4.3 SSE Response Compatibility (Telemetry Wrapper)
- **Root Cause & Fix:** In `backend/internal/telemetry/middleware.go`, the telemetry response writer wrapper previously dropped `http.Flusher`, causing `/api/ops/worklist/stream` to return HTTP 500 "streaming is unavailable". Added `statusCapturingFlusherResponseWriter` wrapping `http.Flusher` when the underlying writer implements it.
- **Regression Testing:** Added `TestHTTPMiddleware_FlusherSupport` in `telemetry_test.go` and verified SSE flusher retention under `HTTPMiddleware`.
- **Delivery State:** Authenticated SSE test covers subscription, reconnect, and event emission without bypassing telemetry or security middleware.

### 4.4 Persisted Trace Schema Contract Alignment
- **Root Cause & Fix:** Resolved the `startTime`/`endTime` versus `startedAt`/`finishedAt` field mismatch in `backend/internal/telemetry/store.go`. Added `startedAt` and `finishedAt` string RFC3339Nano fields to `SpanDTO` alongside the existing `startTime` and `endTime` fields to maintain backwards compatibility while satisfying the shared frontend contract.
- **Regression Testing:** Added `TestSpanDTO_JSONSerialization_Gate4Contracts` in `telemetry_test.go` verifying both sets of timestamp keys serialize correctly.

### 4.5 Retained Investigation Evidence & Citations
- **Root Cause & Fix:** In `backend/internal/investigator/api.go`, exposed `job.Snapshot` in `InvestigationResponse` (`PublicInvestigation`). In `frontend/src/ops/contracts.ts`, added `snapshot` decoding to `Investigation`. In `frontend/src/ops/InvestigationPanel.tsx`, updated citation lookup to check `retainedEvidenceIds` from `job.data.snapshot`, ensuring citations resolve to exact retained evidence across report reloads without inventing citations or weakening missing-evidence guards.
- **Deterministic Evidence ID Hashing:** In `backend/internal/operations/service.go`, extracted `ComputeEvidenceSnapshotID` to hash bundles excluding unstable observation timestamps, ensuring stable snapshot identities.

### 4.6 Verification & Acceptance Suite Evidence Matrix

| Suite | Command | Environment | Result | Evidence |
|---|---|---|---|---|
| **Full Go Tests** | `go test ./...` | Windows Go 1.24 | PASS | All packages passed |
| **Go Vet** | `go vet ./...` | Windows Go 1.24 | PASS | 0 issues |
| **Go Build** | `go build ./...` | Windows Go 1.24 | PASS | All binaries compile cleanly |
| **Frontend Unit Tests** | `npm test` | Node.js v22 | PASS (443ms) | 96/96 tests pass across 16 suites |
| **Frontend Lint / Typecheck** | `npm run lint` | TypeScript 7 / Node v22 | PASS | 0 type or lint errors |
| **Frontend Production Build** | `npm run build` | Vite 8.3.0 | PASS (173ms) | Built production bundle in `dist/` |
| **Money Parser Integrity** | `npm run test:money` | Node.js v22 | PASS | Integer paise parsing passed |
| **Playwright Fixtures** | `npx playwright test` | Chromium (headless) | PASS (8.8s) | 8/8 tests passed |
| **Playwright Offline Shell** | `npm run test:browser:offline-shell` | Chromium (headless) | PASS (4.1s) | 2/2 tests passed |
| **Release Build Validation** | `powershell -File .\deploy\scripts\build-release.ps1 -ValidateOnly` | Windows / Local | PASS | All 6 planned commands validated |

