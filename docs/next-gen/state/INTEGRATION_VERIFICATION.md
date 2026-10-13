# TransactX — M1/M2/M3 Integration & Release Verification Checkpoint

**Date:** 2026-10-13  
**Status:** INTEGRATED & REVERIFIED (GATE 6 COMPLETE)  
**Integration Branch:** `v2/integration-m1-m2-m3`  
**Base:** `origin/release/transactx-v2` (`39fc5c7`)  
**Integrated Commits:**
- **M1 Head (`origin/v2/m1-product-delivery`):** `42d8aef7935b63a9f80a78705f243798b62401c9` (PR #22)
- **M2 Head (`origin/v2/m2-reliability-platform`):** `c9da5fb9035dff259c936fb79785eecbd93efde6` (PR #24)
- **M3 Head (`origin/v2/m3-investigation-analytics`):** `99f3e1425a3c96155852a4b6adccea07d4601cdf` (PR #23)
- **Gate 5 CI Head:** `739a1757f6af6415e6c1d76cb5b3d803c20d3557` (Green on GitHub Actions #38387106936)
- **Gate 6 Integration Work:** Synchronize final M3 head (`99f3e14`), reconcile fresh and upgraded migration history (`000029` + additive `000030`), enforce strict HTTP investigation request decoding (`DecodeCreateInvestigationRequest`), correct B10 healthy-window selection and remove fixed fallback alerts, revalidate M2 crash matrix and financial invariants, and verify browser acceptance.

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

---

## 5. Gate 5: Sync Latest M3 and Verify Canonical Store Compatibility

### 5.1 Baseline & Safe Synchronization of Latest M3
- **Latest Milestone Heads Verified:**
  - M1 (`origin/v2/m1-product-delivery`): `42d8aef7935b63a9f80a78705f243798b62401c9`
  - M2 (`origin/v2/m2-production-readiness`): `c9da5fb9035dff259c936fb79785eecbd93efde6`
  - M3 (`origin/v2/m3-investigation-analytics`): `0947b19c14ac5b7f3eef0fffbe8faa54e6ccd66c`
  - Release Base: `6ad42bff0ff01da6b4c88e5b702aa2b897fd452d`
- **Incorporated M3 Commits:**
  - `e5d2e81` Enforce provider concurrency and cost budgets
  - `4f39c84` Checkpoint provider budget hardening
  - `0947b19` Integrate PostgreSQL store conformance
- **History-Preserving Merge:**
  - Clean merge into `v2/integration-m1-m2-m3` without force-pushing, rebasing, or discarding prior Gate 4 integration work.

### 5.2 Canonical PostgreSQL Schema Reconciled (`000029_m3_investigator_store`)
- Reconciled canonical migration `backend/migrations/000029_m3_investigator_store.up.sql` and `000029_m3_investigator_store.down.sql` with M3's durable store persistence contract:
  - Added `cost_limit_microusd BIGINT NOT NULL DEFAULT 0` to `investigation_usage_limits`.
  - Added `provider_call_pending BOOLEAN NOT NULL DEFAULT FALSE` to `investigation_jobs`.
  - Added `reserved_cost_microusd BIGINT NOT NULL DEFAULT 0` and `actual_cost_microusd BIGINT` to `investigation_usage_reservations`.
  - Dropped legacy non-null constraints on `investigation_usage_reservations` (`actor_id`, `provider`, `model`, `reserved_tokens`) that conflicted with the decoupled reservation schema.

### 5.3 Durable Store Conformance against Canonical Migrations
- Replaced test-only DDLs in `backend/internal/investigator/store_contract_test.go` and `postgres_corpus_integration_test.go` with automated execution of all 29 canonical repository migrations (`database.NewMigrator(pool, ...).Up(ctx)`) against freshly provisioned disposable PostgreSQL databases.
- Executed `TestPostgresInvestigationStoreConformance` using `storetest.Run`:
  - `idempotency_scope_and_copy_isolation`: verified actor and scope isolation, request idempotency, and payload separation.
  - `lease_fencing_and_checkpoint_resume`: verified lease epoch increments, active lease fencing, and resume from durable checkpoints.
  - `global_provider_call_bound`: verified active job concurrency bounds and global pending provider call gates.
  - `transactional_usage_reservations`: verified atomic token/cost reservation and actual-cost finalization preventing budget overspend.
  - `uncertain_usage_is_lease_fenced_and_retains_budget`: verified that expired lease holders cannot complete jobs and uncertain usage is safely fenced while retaining budget accounting.
  - **Result:** All 5 subtests passed in 1.11s.

### 5.4 Corpus Lifecycle & Audience Filtering Verification
- Executed `TestPostgresCorpusLifecycle` on disposable database with canonical migrations:
  - Staging and activation of approved corpus (6 runbook documents).
  - SQL assertion for staged version record (`status = 'staging'`, chunk count = 6).
  - SQL assertion for active version record (`status = 'active'`, `activated_at IS NOT NULL`, exactly 1 active version enforced by partial unique index).
  - SQL assertion for chunk metrics: unique `chunk_id` per version, non-empty `content_sha256` and `source_sha256`.
  - SQL assertion and repository test for role-based audience filtering: `CUSTOMER` role is strictly denied access to operational runbooks (`0` customer-accessible chunks).
  - SQL assertion for version retirement on new version activation (`status = 'retired'`, `deactivated_at IS NOT NULL`) with preserved citation resolution.
  - SQL assertion verifying resistance to SQL injection in search vectors.
  - **Result:** Passed in 0.76s.

### 5.5 Crash Matrix & Invariant Re-verification (B12)
- Fixed test contention in `backend/internal/experiments/crash_matrix.go`: ensured `recovery_jobs` and `recovery_attempts` tables are cleaned before running `REC-08` and `REC-14`, guaranteeing deterministic single-job claim assertions regardless of other test activity.
- Executed full 20-scenario crash matrix (`TestCrashMatrix_FullRecoveryAndEventMatrix`):
  - 20/20 scenarios passed in 0.65s (REC-01 through REC-14, EVT-01 through EVT-06).
  - Financial invariant validation passed: `NonnegativeBalances=true`, `DebitCreditConservation=true`, `SingleLedger=true`, `Violations=0`.
  - Saved artifacts: `crash_matrix.json`, `crash_matrix.csv`, SHA256 digest `5d82d3e5fa1f844043ee17a72c841d54e2365ae23ce3027aa3c8a8e03a434ce6`.

### 5.6 Gate 5 Verification & Acceptance Suite Evidence Matrix

| Suite | Command | Environment | Result | Evidence |
|---|---|---|---|---|
| **M3 Store Conformance** | `go test -v ./internal/investigator -run TestPostgresInvestigationStoreConformance` | Local PostgreSQL 18 | PASS (1.11s) | All 5 conformance subtests pass against canonical migration 000029 |
| **Corpus Lifecycle** | `go test -v ./internal/investigator -run TestPostgresCorpusLifecycle` | Local PostgreSQL 18 | PASS (0.76s) | Staging, activation, retirement, hashing, and audience filtering verified |
| **Crash & Recovery Matrix** | `go test -v ./internal/experiments -run TestCrashMatrix_FullRecoveryAndEventMatrix` | Local PostgreSQL 18 | PASS (0.65s) | 20/20 scenarios pass; 0 financial invariant violations |
| **Full Go Tests** | `go test ./...` | Windows Go 1.24 | PASS | All backend packages pass |
| **Go Vet** | `go vet ./...` | Windows Go 1.24 | PASS | 0 issues |
| **Go Build** | `go build ./...` | Windows Go 1.24 | PASS | All 9 binary targets compile cleanly |
| **Frontend Unit Tests** | `npm test` | Node.js v22 | PASS (498ms) | 96/96 tests pass across 16 suites |
| **Frontend Lint / Typecheck** | `npm run lint` | TypeScript 7 / Node v22 | PASS | 0 type or lint errors |
| **Frontend Production Build** | `npm run build` | Vite 8.3.0 | PASS (176ms) | Built production bundle in `dist/` |
| **Money Parser Integrity** | `npm run test:money` | Node.js v22 | PASS | Integer paise parsing passed |
| **Playwright Fixtures** | `npx playwright test` | Chromium (headless) | PASS (11.6s) | 8/8 tests passed |
| **Playwright Offline Shell** | `npm run test:browser:offline-shell` | Chromium (headless) | PASS (4.3s) | 2/2 tests passed |
| **Release Build Validation** | `powershell -File .\deploy\scripts\build-release.ps1 -ValidateOnly` | Windows / Local | PASS | All 6 planned commands validated |

### 5.7 Remaining Operational & Infrastructure Gates
- **Live Browser Acceptance on Target Staging Host:** Staging environment deployment and live OAuth/IdP sign-in flow pending cluster provisioning.
- **Off-VM Disaster Recovery & Cloud RPO/RTO:** Automated snapshot export to remote object storage and automated restore failover drill pending production infrastructure deployment.
- **Milestone PR Status:** PRs #22 (M1), #23 (M3), and #24 (M2) remain open and unmerged. Integration PR #25 is updated on branch `v2/integration-m1-m2-m3` in draft status.

---

## 6. Gate 6: Close Canonical Upgrade, HTTP Contract, and Analytics Acceptance Gaps

### 6.1 Baseline & Safe Synchronization of Latest M3
- **Latest Milestone Heads Verified:**
  - M1 (`origin/v2/m1-product-delivery`): `42d8aef7935b63a9f80a78705f243798b62401c9` (PR #22, open & unmerged)
  - M2 (`origin/v2/m2-production-readiness`): `c9da5fb9035dff259c936fb79785eecbd93efde6` (PR #24, open & unmerged)
  - M3 (`origin/v2/m3-investigation-analytics`): `99f3e1425a3c96155852a4b6adccea07d4601cdf` (PR #23, open & unmerged)
  - Release Base: `39fc5c7` / `6ad42bf`
- **Incorporated Final 3 Commits from M3 Head:**
  - `7eec9c3` Harden durable investigator integration boundaries
  - `8aa0fe7` Checkpoint current integration audit
  - `99f3e14` Record final integration merge validation
- **History-Preserving Merge:**
  - Merge commit `d4dd55e` into `v2/integration-m1-m2-m3` without rebase, force-push, or history loss.

### 6.2 Canonical Migration History Reconciled (`000029` + Additive `000030`)
- **Restored Canonical Checksum for `000029`:**
  - `backend/migrations/000029_m3_investigator_store.up.sql` and `000029_m3_investigator_store.down.sql` restored to byte-for-byte canonical checksum state matching commit `cc97e21`.
- **Created Additive Migration `000030_m3_investigator_store_upgrade`:**
  - `000030_m3_investigator_store_upgrade.up.sql`:
    - Adds `cost_limit_microusd BIGINT NOT NULL DEFAULT 0` to `investigation_usage_limits`.
    - Adds `provider_call_pending BOOLEAN NOT NULL DEFAULT FALSE` to `investigation_jobs`.
    - Adds `reserved_cost_microusd BIGINT NOT NULL DEFAULT 0` and `actual_cost_microusd BIGINT` to `investigation_usage_reservations`.
    - Safely drops legacy `NOT NULL` constraints on `actor_id`, `provider`, `model`, `reserved_tokens` in `investigation_usage_reservations`.
    - Enforces unique index `idx_investigation_usage_job_uniq` on `investigation_usage_reservations(job_id)`.
    - Maps legacy single-token allocations (`reserved_tokens`, `actual_tokens`) to decoupled input dimensions (`reserved_input`, `actual_input`).
    - Safely cancels unreplayable active legacy jobs (`status = 'cancelled'`, `error_code = 'MIGRATION_LEGACY_UNREPLAYABLE'`).
    - Backfills completed terminal historical jobs with valid request hashes and structured JSON payloads for seamless audit and report reading.
  - `000030_m3_investigator_store_upgrade.down.sql`:
    - Drops added columns and unique index cleanly.
- **Dual-Database Conformance Verified on PostgreSQL 18:**
  - `TestPostgresInvestigationStoreConformance`: fresh database applying migrations 000001 through 000030 passes all 5 store contract subtests (`idempotency_scope_and_copy_isolation`, `lease_fencing_and_checkpoint_resume`, `global_provider_call_bound`, `transactional_usage_reservations`, `uncertain_usage_is_lease_fenced_and_retains_budget`).
  - `TestPostgresInvestigationStoreUpgradedDatabaseConformance`: database seeded with historical data up to migration 000029, upgraded to 000030, passes all 5 store contract subtests plus legacy job validation assertions.

### 6.3 Strict HTTP Investigation Request Contract
- **Bounded Request Decoding (`DecodeCreateInvestigationRequest`):**
  - Integrated `investigator.DecodeCreateInvestigationRequest(request.Body)` directly in `backend/internal/http/operations_handler.go`.
  - Body decoding rejects payloads exceeding 32 KiB (`MaxCreateInvestigationRequestBytes`), trailing JSON tokens, and unknown fields via `decoder.DisallowUnknownFields()`.
  - Client-injected fields (`actor`, `actorId`, `role`, `provider`, `mode`, `unrecognized_field`) are rejected with HTTP 400 Bad Request.
  - Authenticated identity (`actor_id`, `role`, `selected_payment_id`) from trusted JWT session is strictly authoritative and bound in `investigator.TrustedScope`.
- **Exhaustive 15-Assertion Integration Suite (`TestOperations_Investigation_StrictDecoderAndScopeContract`):**
  - Customer role rejected with 403 Forbidden.
  - Injected `actor` field rejected with 400 Bad Request.
  - Injected `role` field rejected with 400 Bad Request.
  - Injected `provider` field rejected with 400 Bad Request.
  - Injected `mode` field rejected with 400 Bad Request.
  - Injected extra fields rejected with 400 Bad Request.
  - Oversized request (>32 KiB) rejected with 400 Bad Request.
  - Trailing JSON rejected with 400 Bad Request.
  - Valid request accepted with 202 Accepted.
  - Database row stores authoritative JWT actor ID and role.
  - Idempotent replay with same key and body returns 202 with identical investigation ID.
  - Conflicting body with same key returns 409 Conflict.
  - Foreign actor using same key receives separate distinct job (no hijacking).
  - Foreign actor denied on GET with 403 Forbidden.
  - Admin permitted on GET with 200 OK.
  - Customer denied on GET with 403 Forbidden.

### 6.4 Independently Reviewed B10 Calibration & Fallback Removal
- **Reviewed Healthy-Window Calibration Query:**
  - In `backend/internal/analytics/repository.go`, `priorQuery` updated to require independent health attestation via `raw_aggregates`:
    - Requires `raw_aggregates` containing `"reviewed_healthy": true`, `"healthy": true`, or `"seeded": true`.
    - Explicitly excludes unreviewed live windows (`"unreviewed": true` or `"reviewed_healthy": false`), anomalies (`"anomalous": true`), flagged windows (`"ineligible": true`), and windows with `baseline_alert IS NOT NULL`.
  - Persists `scoring_outcome`, `baseline_status`, `model_status`, `provenance: "unreviewed_live"`, `calibration_samples`, and `reviewed_healthy: false` into `raw_aggregates` for every completed window.
- **Fixed Fallback Alert Removal:**
  - Removed fixed `failureRate > 0.15` and `timeoutRate > 0.10` alert fallbacks during warmup or evaluation error.
  - Strictly preserves explicit operational states: `insufficient_data` when sample count < 20, `warming_up` when prior healthy baseline is not ready, `error` when evaluation fails, and `alert` ONLY when calibrated baseline deviates.
- **Analytics Regression Suite:**
  - `TestAnalytics_WarmupWithoutBaseline_DoesNotEmitFixedAlerts`: verifies high failure rate during warmup does NOT fire fixed alerts.
  - `TestAnalytics_IneligiblePriorWindows_ExcludedFromCalibration`: verifies unreviewed, anomalous, ineligible, alert, malformed, future, and low-sample windows are excluded from calibration.
  - `TestAnalytics_NullMeasurements_AndPersistedProvenance`: verifies null measurements are handled cleanly and provenance is persisted into PostgreSQL.
  - All analytics tests pass 100%.

### 6.5 M2 Reliability Revalidation & Crash Matrix
- **Full 20-Scenario Crash Matrix (`TestCrashMatrix_FullRecoveryAndEventMatrix`):**
  - Executed against live PostgreSQL 18: 20 passed, 0 failed.
  - Financial invariant validation passed:
    - `NonnegativeBalances=true`
    - `DebitCreditConservation=true`
    - `SingleLedger=true`
    - `Violations=0`
  - Saved artifacts: `crash_matrix.json`, `crash_matrix.csv`, digest verified.

### 6.6 Live Citation & Browser Acceptance
- **Playwright Fixture Suite (`npx playwright test`):**
  - All 8 tests passed (deep-linked worklist, admin retry idempotency, evidence-only investigation citations, investigation retry, worklist cursor/filters, terminal evidence export, saved report stale/missing citation labels, analytics insufficient sample labels).
- **Playwright Offline Shell (`npm run test:browser:offline-shell`):**
  - Both 2 tests passed (offline reload, expired token cache clearing).
- **Native Browser Acceptance in `ops.live.spec.ts`:**
  - Updated to assert HTTP 400 Bad Request rejection on client-injected `actorId` and `role` fields, verifying strict decoder enforcement in browser end-to-end flows.

### 6.7 Gate 6 Verification & Acceptance Suite Evidence Matrix

| Suite | Command | Environment | Result | Evidence |
|---|---|---|---|---|
| **Fresh M3 Store Conformance** | `go test -v ./internal/investigator -run TestPostgresInvestigationStoreConformance` | Local PostgreSQL 18 | PASS (1.1s) | All 5 conformance subtests pass against fresh migrations 000001-000030 |
| **Upgraded Store Conformance** | `go test -v ./internal/investigator -run TestPostgresInvestigationStoreUpgradedDatabaseConformance` | Local PostgreSQL 18 | PASS (1.2s) | All 5 conformance subtests pass against upgraded migrations (1-29 -> 30) |
| **Corpus Integration** | `go test -v ./internal/investigator -run TestPostgresCorpusLifecycle` | Local PostgreSQL 18 | PASS (0.8s) | Staging, activation, hashing, audience filtering verified |
| **Strict HTTP Contract** | `go test -v ./internal/http -run TestOperations_Investigation_StrictDecoderAndScopeContract` | Local PostgreSQL 18 | PASS (0.3s) | 15 distinct assertions pass (400 on unknown/injected fields, 403, 409, 202) |
| **Analytics Calibration** | `go test -v ./internal/analytics` | Local PostgreSQL 18 | PASS (0.4s) | Warmup without fixed alerts, ineligible window exclusion, null measurements pass |
| **Crash & Recovery Matrix** | `go test -v ./internal/experiments -run TestCrashMatrix_FullRecoveryAndEventMatrix` | Local PostgreSQL 18 | PASS (0.7s) | 20/20 scenarios pass; 0 financial invariant violations |
| **Full Go Tests** | `go test -count=1 ./...` | Windows Go 1.24 | PASS | All backend packages pass 100% |
| **Go Vet** | `go vet ./...` | Windows Go 1.24 | PASS | 0 issues |
| **Go Build** | `go build ./...` | Windows Go 1.24 | PASS | All binaries compile cleanly |
| **Frontend Unit Tests** | `npm test` | Node.js v22 | PASS (707ms) | 96/96 tests pass across 16 suites |
| **Frontend Lint / Typecheck** | `npm run lint` | TypeScript 7 / Node v22 | PASS | 0 type or lint errors |
| **Frontend Production Build** | `npm run build` | Vite 8.3.0 | PASS (350ms) | Built production bundle in `dist/` |
| **Money Parser Integrity** | `npm run test:money` | Node.js v22 | PASS | Integer paise parsing passed |
| **Playwright Fixtures** | `npx playwright test` | Chromium (headless) | PASS (18.7s) | 8/8 tests passed |
| **Playwright Offline Shell** | `npm run test:browser:offline-shell` | Chromium (headless) | PASS (6.5s) | 2/2 tests passed |
| **Release Build Validation** | `powershell -File .\deploy\scripts\build-release.ps1 -ValidateOnly` | Windows / Local | PASS | All 6 planned commands validated |

### 6.8 Remaining Operational & Infrastructure Gates
- **Live Browser Acceptance on Target Staging Host:** Staging environment deployment and live OAuth/IdP sign-in flow pending cluster provisioning.
- **Off-VM Disaster Recovery & Cloud RPO/RTO:** Automated snapshot export to remote object storage and automated restore failover drill pending production infrastructure deployment.
- **Milestone PR Status:** PRs #22 (M1), #23 (M3), and #24 (M2) remain open and unmerged. Integration PR #25 is updated on branch `v2/integration-m1-m2-m3` in draft status.



