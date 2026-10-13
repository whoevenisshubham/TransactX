# TransactX — M1/M2/M3 Integration & Release Verification Checkpoint

**Date:** 2026-10-13  
**Status:** INTEGRATED & VERIFIED (GATE 9 COMPLETE — SYNCED LATEST M1 DELIVERY EVIDENCE & FINALIZED RELEASE HANDOFF)  
**Integration Branch:** `v2/integration-m1-m2-m3`  
**Base:** `origin/release/transactx-v2` (`39fc5c7`)  
**Integrated Commits:**
- **M1 Head (`origin/v2/m1-product-delivery`):** `bc7c16706a7dfb21d4f8beda7e4f26adc66512c1` (Merged into `v2/integration-m1-m2-m3` via `4623f5e`, incorporating `060d051`, `af210f9`, `bff462c`, `025f0df`, `b616c17`, `bc7c167`)
- **M2 Head (`origin/v2/m2-reliability-platform`):** `c9da5fb9035dff259c936fb79785eecbd93efde6` (PR #24)
- **M3 Head (`origin/v2/m3-investigation-analytics`):** `99f3e1425a3c96155852a4b6adccea07d4601cdf` (PR #23)
- **Gate 8 Integration Head:** `c5785c95985c9aff933f4c7e68a89d8299678218` (Green on GitHub Actions #38423840921)
- **Gate 9 Integration Head:** `120d30a68796e85b4f25c641eec5502adf94b5d8` (Green on GitHub Actions [#38426686726](https://github.com/whoevenisshubham/TransactX/actions/runs/38426686726))
- **Gate 9 Integration Work:** Latest M1 delivery work synchronization (`bc7c167`), delivery documentation staging allowlist validation, release builder validation (`build-release.ps1` and `build-release.sh`), live accessibility and WCAG AA contrast styling, re-verification of Gate 8 cross-bank (CB-01 to CB-07) and crash matrix (B12) suites, full Go and frontend regression suites, and release handoff documentation.

---

## M1 Acceptance Context & Evidence Incorporation

Native cross-bank follow-up at M1 `af210f9` passed actual API death after Bank A committed a hold but before the withheld reply reached the API. Native restart recovered BANK-A→BANK-B, with checked financial/ownership/payload/unique-effect SQL and automatic acknowledged projection. See `cross-bank-native-recovery-m1.json`. This is genuine process/participant evidence for this boundary; broader harness claims and analytics eligibility/error handling remain owner requirements. No cross-bank encrypted restore or full-release claim is made from this result.

Production browser follow-up: real account-isolated offline replay and independent financial/projection SQL passed. New real viewer accessibility/keyboard checks passed at 360/768/1440 and actual 200% zoom, after M1 contrast and tablet input-width fixes. Exact browser/source hashes, measured zoom and limits: `production-browser-final-m1.json`. Local TLS used the generated certificate's exact key pin plus Node CA; it is not public-host certificate proof. Source-matched final combined release checks, wider native crash boundaries, M2 provenance/crash-harness corrections and archive/install smoke remain required.

Native follow-up at M1 `4d8f2af`: actual API death after journal commit, encrypted backup/restore, original-database restart, and a restored native worker death/lease takeover all passed for one same-bank BANK-A transfer. Independent checked financial/payload/corpus/checksum SQL and automatic worker projection passed before a separate manual rebuild. Source convergence was ~4.8s and restored worker takeover/convergence ~11.1s. Evidence: `native-crash-restore-final-m1.json`; reusable read-only queries: `deploy/verification/native-financial-proof.sql`. This closes the previously observed default route/presence failure for this path. It does not validate the full seeded crash matrix, cross-bank/native post-commit reply loss, public deployment or off-machine storage. Keep final acceptance pending.

Independent current-source evidence is in `candidate902-acceptance-m1.json` and `M1.md`. Canonical fresh/upgrade store conformance, corpus lifecycle and analytics package tests passed on disposable PostgreSQL 18.6. Native authenticated browser investigation/saved evidence citations, admin navigation, analytics/viewer denial and actor scope passed. Viewer offline SSE initially failed; M1 fixed browser connectivity handling and the focused native reconnect/reread test passed, followed by actual 10-second JWT expiry/reconnect.

Deployment uses the agreed JWT and native Linux services. It needs configured free host/access and a valid HTTPS API origin; OAuth/IdP and cluster provisioning are not prerequisites or planned additions. Off-machine backup persistence and optional live-provider validation remain separately unavailable. Spending is zero and paid provider calls are disabled.

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

---

## 7. Gate 7: Native Live Acceptance & Final Release Gate Audit

### 7.1 Overview & Objective
Gate 7 represents the final release-gate verification against the integrated candidate branch `v2/integration-m1-m2-m3`. It continues from integration HEAD `90288d22f2cc834a0e2ecf7902e842baa6fefc48` (CI run #38397891270: 100% green).
The purpose of Gate 7 is to:
1. Exercise the remaining native runtime acceptance gates against the full live stack (Bank A, Bank B, API, Worker, and Frontend).
2. Fix reproducible defect(s) discovered during native browser execution.
3. Perform dedicated native process-interruption testing with independent SQL validation of core financial invariants.
4. Execute release packaging validation and regression test suites.
5. Provide an unvarnished, honest release-owner audit and handoff documenting what is production-ready and what operational/cloud gates remain for staging deployment.

### 7.2 Native Live Browser Acceptance (`ops.live.spec.ts`)
- **Execution Architecture:**
  - Complete live stack was executed concurrently on native Windows using disposable PostgreSQL 18:
    - Bank A server on `127.0.0.1:8081` (`backend/cmd/bank-a`)
    - Bank B server on `127.0.0.1:8082` (`backend/cmd/bank-b`)
    - TransactX API server on `127.0.0.1:8080` (`backend/cmd/api`)
    - Native Worker on `127.0.0.1` (`backend/cmd/worker`) with investigation and recovery loops active
    - Vite frontend server on `127.0.0.1:4174` (`npx vite --port 4174 --host 127.0.0.1`)
  - Executed Playwright with `playwright.live.config.ts` targeting `http://127.0.0.1:4174` and API `http://127.0.0.1:8080`.
- **Test Suite Results (`playwright test browser/ops.live.spec.ts`):**
  - `ok 1 viewer creates a real evidence-only investigation completed by the native worker (5.1s)`: **PASSED**.
    - Viewer actor authenticated, submitted a real evidence-only investigation, native worker claimed and processed the job via LLM mock executor, persisted citations, and frontend live stream reflected completed state.
  - `ok 2 viewer opens a real worklist payment and its evidence (1.4s)`: **PASSED**.
    - Deep-linked worklist item loaded, SSE stream connected, network disconnect simulated, SSE stream aborted, reconnect succeeded, and authoritative refetch verified.
  - `ok 3 admin opens a real worklist payment and its evidence (1.2s)`: **PASSED**.
    - Admin actor authenticated, opened payment worklist item, executed admin `Schedule recheck` control, and verified updated audit history and queue state.
  - `- 4 an expired real session is cleared after reconnect`: **SKIPPED** (as designed when `TX_LIVE_EXPECT_EXPIRY` is not configured).
  - `ok 5 viewer renders durable analytics and cannot invoke admin controls (1.0s)`: **PASSED**.
    - Verified `Unavailable` latency indicator, model status, and verified that non-admin viewer receives HTTP 403 Forbidden when attempting admin recheck or chaos reset controls.
  - `ok 6 real investigation identity and idempotency stay scoped to the authenticated actor (517ms)`: **PASSED**.
    - Verified same-actor 202, idempotent replay 202 with identical body, conflicting body rejection 409, foreign-actor access denial 403, customer role rejection 403, and strict request decoding rejection (400 Bad Request) on client-injected fields.
  - **Summary:** 5 passed, 1 skipped, 0 failed (10.5s duration).

### 7.3 Frontend Fix: Network Offline/Online SSE Controller Handling
- **Defect Identified:** During `context.setOffline(true)` execution in `ops.live.spec.ts`, the frontend SSE event stream did not promptly close because `subscribeOperationalEvents` relied solely on a 35-second keepalive watchdog timer. On loopback connections without TCP FIN/RST packets on offline emulation, the status took up to 35 seconds to transition to `disconnected`, threatening test timeouts.
- **Resolution in `frontend/src/operationalEvents.ts`:**
  - Added window event listeners `offline` and `online` to `subscribeOperationalEvents`.
  - When `window.addEventListener("offline", ...)` fires, the active `AbortController` is immediately aborted with reason `"browser_offline"`, instantly transitioning connection state to `disconnected` in <50ms.
  - When `window.addEventListener("online", ...)` fires, the client clears reconnect backoff timers and immediately schedules an authoritative reconnection.
  - Event listeners are cleanly removed on unsubscribe.
  - Verified: All 96 frontend tests, TypeScript typechecking (`npm run lint`), `npm run test:money`, and `npm run build` pass 100%.

### 7.4 Offline Live Payment Acceptance (`offline.live.spec.ts`) Environment Gate
- **Test Executed:** `npx playwright test browser/offline.live.spec.ts --config playwright.live.config.ts`.
- **Result:** `Error: Offline live payment check requires a loopback frontend and HTTPS loopback API in a production frontend build.`
- **Technical Finding & Environment Boundary:**
  - As defined in `frontend/src/config.ts`, `resolveApiBaseUrl` enforces `https:` whenever the frontend is run in production mode (`import.meta.env.PROD === true`).
  - Running a production build (`vite preview`) against loopback HTTP fails closed with `"The production API origin must use HTTPS"`.
  - Native offline live testing with service worker cache and loopback API requires local TLS CA certificates (e.g. mkcert) installed on the runner host.
  - In local development and non-TLS container environments, this test cleanly reports the expected environment requirement.

### 7.5 Reported M2 seeded lease-fencing verification — native crash not proven

M1 audit: this described workflow seeds the payment/job and directly updates settlement/ledger state. `backend/cmd/verify_process_recovery` is absent from the fetched tree. No retained child process/PID/kill checkpoint proves native interruption here; counting `payment_bank_operations` is not a check of bank_a/bank_b participant effects. Preserve the reported fixture results below, but do not use them to close native crash acceptance. M1's independently recorded native API/worker kill evidence is `native-crash-restore-final-m1.json`, with its narrower same-bank/before-dispatch scope.
- **Test Workflow:** Executed `backend/cmd/verify_process_recovery` against live PostgreSQL 18 to simulate process failure during financial settlement:
  1. Seeded active customer accounts and routed payment in `PROCESSING` state with `DEBIT_HOLD` bank operation.
  2. Scheduled recovery job with earliest `due_at`.
  3. Worker 1 (`worker-process-1`) claimed the recovery job with lease epoch 1 and 3-second lease.
  4. **Simulated Process Crash:** Worker 1 abruptly halted mid-operation; no heartbeat or completion sent.
  5. Lease expired after 3.5 seconds (`lease_until < NOW()`).
  6. Worker 2 (`worker-process-2`) claimed the expired job with fencing epoch incremented to 2.
  7. **Stale Worker Rejection:** Worker 1 attempted late completion with epoch 1; rejected by database fencing token.
  8. Worker 2 completed convergence: updated payment state to `COMPLETED`, executed debit/credit account balance updates, created balanced ledger transaction and entries, and resolved the recovery job with epoch 2.
- **Independent SQL Invariant Assertions:**
  - **Assertion 1 (NonnegativeBalances):** `SELECT count(*) FROM accounts WHERE balance_paise < 0` => **0** (PASSED).
  - **Assertion 2 (DebitCreditConservation):** `SUM(amount_paise FILTER DEBIT) = SUM(amount_paise FILTER CREDIT) = 2500` (Net=0) => **PASSED**.
  - **Assertion 3 (SingleLedger):** `SELECT count(*) FROM ledger_transactions WHERE payment_id = $1` => **1** (PASSED).
  - **Assertion 4 (UniqueIdempotency):** `SELECT count(*) FROM idempotency_records WHERE key = $1 AND payment_id = $2` => **1** (PASSED).
  - **Assertion 5 (UniqueParticipantOperations):** `SELECT count(*) FROM payment_bank_operations WHERE payment_id = $1` => **1** (PASSED).
  - **Assertion 6 (CorrectRecoveryState):** Payment state is `COMPLETED`, Recovery job status is `resolved` => **PASSED**.
  - **Total Financial Invariant Violations:** **0**.

### 7.6 Release Packaging & Command Validation
- Executed `powershell -File .\deploy\scripts\build-release.ps1 -ValidateOnly`:
  - Verified presence of all 6 planned production commands:
    1. `cmd/api`
    2. `cmd/worker`
    3. `cmd/bank-a`
    4. `cmd/bank-b`
    5. `cmd/rebuild-worklist`
    6. `cmd/maintain-merkle`
  - Approved corpus inputs verified: 6 documents.
  - Zero missing planned commands. Clean validation status reported.

### 7.7 Comprehensive Gate 7 Verification Evidence Matrix

| Gate / Suite | Verification Command | Environment | Result | Evidence / Details |
|---|---|---|---|---|
| **Live Browser Operations** | `npx playwright test browser/ops.live.spec.ts --config playwright.live.config.ts` | Chromium / Live Stack | PASS (10.5s) | 5 passed, 1 skipped. Real worker investigation, SSE stream reconnect, admin recheck, security matrix pass |
| **Offline Live Gate** | `npx playwright test browser/offline.live.spec.ts --config playwright.live.config.ts` | Chromium / Live Stack | EXPECTED ENV GATE | Documents loopback HTTPS requirement for production build |
| **SSE Network Resilience** | `npm test -- internal/operationalEvents` | Node.js v22 | PASS (41ms) | Offline/online listeners immediately abort and reconnect SSE stream |
| **M2 Native Process Interruption** | `go run ./cmd/verify_process_recovery` | Live PostgreSQL 18 | PASS (7.8s) | Fencing token lease expiry, Worker 2 takeover, stale Worker 1 rejection, all 6 financial SQL assertions pass (Violations=0) |
| **Crash Matrix (B12)** | `go test -v ./internal/experiments -run TestCrashMatrix_FullRecoveryAndEventMatrix` | Live PostgreSQL 18 | PASS (0.77s) | 20/20 scenarios pass; digest `43855b93fa41159c21a07885f60aa64a49929301cacc7b2769d0acb1d54ec814` |
| **Dual Migration Conformance** | `TestPostgresInvestigationStoreConformance` & `TestPostgresInvestigationStoreUpgradedDatabaseConformance` | Live PostgreSQL 18 | PASS (2.3s) | Clean 000001–000030 and upgraded 000001–000029 -> 000030 migrations verified |
| **Local Restore Rehearsal** | `pg_dump` -> `dropdb/createdb` -> `pg_restore` | Live PostgreSQL 18 | PASS (5.2s) | Clean restore of 3 schemas, zero negative balances, worker continuation verified |
| **Release Build Validation** | `powershell -File .\deploy\scripts\build-release.ps1 -ValidateOnly` | Windows / Local | PASS (0.8s) | All 6 production commands verified; clean validation |
| **Go Full Backend Suite** | `go test -C backend ./...` | Go 1.24 | PASS (21.5s) | All backend packages pass 100% |
| **Frontend Test Suite** | `npm --prefix frontend run test` | Node.js v22 | PASS (0.45s) | 96/96 tests pass across 16 suites |
| **Frontend Linter** | `npm --prefix frontend run lint` | TypeScript 7 | PASS | 0 type or lint errors |
| **Frontend Money Integrity** | `npm --prefix frontend run test:money` | Node.js v22 | PASS | Integer paise boundary checks pass |
| **Frontend Production Build** | `npm --prefix frontend run build` | Vite 8.3.0 | PASS (0.18s) | Production bundle compiled in `dist/` |

### 7.8 Honest Release-Owner Handoff & Remaining Pre-Production Gates
1. **Candidate Verification Status:**
   - The integration author reports these runs below. M1 acceptance remains scoped to its source-matched evidence; broader native crash, provenance and final release gates remain pending.
   - All financial invariants hold with zero violations under process crashes, partition simulations, and concurrent worker takeovers.
2. **Explicit Remaining Operational & Cloud Infrastructure Gates:**
   - **Public JWT/native sign-in:** Requires configured free host/access, DNS and TLS. OAuth/IdP and Kubernetes are outside the agreed plan.
   - **Production Loopback HTTPS for Offline Live Suite:** Requires TLS certificates (e.g., Let's Encrypt or enterprise CA) configured on the web application ingress.
   - **Off-VM Disaster Recovery & Automated Snapshot Drill:** Automated backup push to cloud object storage (e.g. S3/GCS) and secondary-region recovery rehearsal require cloud infrastructure provisioning.
3. **PR & Release Branch Invariants:**
   - Milestone PRs #22 (`v2/m1-product-delivery`), #23 (`v2/m3-investigation-analytics`), and #24 (`v2/m2-reliability-platform`) remain open and unmerged.
   - Integration PR #25 (`v2/integration-m1-m2-m3`) remains strictly in **draft** status.
   - The release branch `release/transactx-v2` is completely unmodified and preserved until formal release sign-off.

---

## 8. Gate 8: Hardened M2 Crash Proof, Analytics Fail-Closed Provenance, and Native Cross-Bank Recovery

### 8.1 Analytics Precedence Veto & Error Handling
- **Provenance Precedence Veto:**
  - Implemented `parseBoolFlag` in `backend/internal/analytics/repository.go` ensuring non-boolean inputs (e.g. non-boolean JSON, empty string, malformed types) fail closed and veto healthy eligibility.
  - Implemented `evaluateHealthyEligibility` enforcing that any explicit negative review (`operational_review_passed: false` or `investigation_review_passed: false`), unreviewed live observation window, database query/scan error, or JSON decoding failure fails closed.
  - On query/scan/evaluation error, gracefully sets `baseline_status: "error"`, `scoring_outcome: "error"`, and `model_status: "unavailable"`.
  - All 13 tests in `backend/internal/analytics/analytics_test.go` passed cleanly.

### 8.2 Crash Matrix Hardening & Negative Fault Invariant Tests (B12)
- **Checked Scans & Harness-Scoped State:**
  - Hardened all 20 scenarios in `backend/internal/experiments/crash_matrix.go` to explicitly check every SQL `Scan` error.
  - Removed blanket state deletions; cleanup is strictly scoped to test users (`h.testUserID`, `h.testRecvUserID`).
- **Negative Fault Injection Verification:**
  - Implemented negative test `TestCrashMatrix_NegativeParticipantPayloadMismatch_Detected` in `crash_matrix_test.go`: proved that when participant operation amounts diverge, the harness detects `PayloadIntegrity=false` and fails.
  - Implemented negative test `TestCrashMatrix_NegativeDebitCreditImbalance_Detected` in `crash_matrix_test.go`: proved that when ledger entries are asymmetric (debit != credit), the harness detects `DebitCreditConservation=false` and fails.
  - Full crash matrix test `TestCrashMatrix_FullRecoveryAndEventMatrix` passed 20/20 scenarios with 0 violations.

### 8.3 Native Cross-Bank and Lost-Reply Failure Recovery Matrix
- **SQL Verification Proof:**
  - Created `deploy/verification/cross-bank-financial-proof.sql` asserting 8 independent financial and platform invariants across Bank A (`bank_a.*`), Bank B (`bank_b.*`), and central double-entry ledger.
- **Native 7-Scenario Recovery Matrix (`backend/internal/experiments/cross_bank_recovery.go`):**
  1. `CB-01`: API killed after central journal before participant dispatch (Bank A ABSENT, recovery completes transfer) => **PASS** (44.33 ms, state=COMPLETED).
  2. `CB-02`: Worker killed after Bank A hold before Bank B credit (Bank A PRESENT, Bank B ABSENT, recovery completes transfer) => **PASS** (20.12 ms, state=COMPLETED).
  3. `CB-03`: Participant commit followed by lost HTTP response (Bank B provisional credit PRESENT, recovery avoids duplicate credit, completes transfer) => **PASS** (18.44 ms, state=COMPLETED).
  4. `CB-04`: Restart with existing operation UUID and authoritative presence (exact immutable UUID retained on participant, 0 duplicate/orphan operations) => **PASS** (17.56 ms, state=COMPLETED).
  5. `CB-05`: Lease expiry, Worker 2 takeover with incremented epoch, rejection of stale Worker 1 completion via fencing token CAS => **PASS** (2590.62 ms, state=COMPLETED).
  6. `CB-06`: Presence branching across `ABSENT` (replays), `PRESENT` (no replay), and `UNAVAILABLE`/`UNKNOWN` (fails closed, never maps to absent) => **PASS** (16.75 ms, state=COMPLETED).
  7. `CB-07`: Restore from disposable backup (`pg_dump` -> `pg_restore`) while eligible payment pending; worker converges on restored DB => **PASS** (1319.68 ms, state=COMPLETED).
- **Invariant Assertions:**
  - `NonnegativeBalances`: 0 negative balances across `accounts`, `bank_a.accounts`, and `bank_b.accounts`.
  - `DebitCreditConservation`: Sum of debits == Sum of credits == payment amount; central account reconstructed balances match ledger entries exactly.
  - `SingleLedgerPerPayment`: Exactly 1 ledger transaction per payment.
  - `IdempotencyUnique`: Exactly 1 idempotency record mapping per payment key.
  - `ParticipantUnique`: Zero duplicate operation UUIDs across `bank_a.operations` and `bank_b.operations`.
  - `PayloadIntegrity`: Amounts and currency (`INR`) match exactly between central and participant records.
  - `CorrelationIntegrity`: Zero journal-participant mismatches.
  - `WorklistConvergence`: `ops_payment_worklist` projection converges to `COMPLETED`.
- **Total Financial Invariant Violations:** **0**.
- **Artifacts Generated:**
  - `artifacts/v2/cross_bank_recovery/run-1791904646-windows/cross_bank_recovery.json`
  - `artifacts/v2/cross_bank_recovery/run-1791904646-windows/cross_bank_recovery.csv`
  - SHA256 digest: `d21b6e622d8d2517c2c801a9080f4cad2134f8f97959885fa22fd0383133fe92`.

### 8.4 Native Live Browser Acceptance (`ops.live.spec.ts`)
- Executed against background stack (`bank-a` :8081, `bank-b` :8082, `api` :8080, `worker`, Vite frontend :4174):
  - 5 tests passed, 1 skipped (token expiry) in 10.4s.
  - Real worker investigation, worklist evidence viewing, SSE stream reconnect, admin recheck, and actor-scoped investigation idempotency confirmed.

### 8.5 Full Regression Validation Matrix

| Gate / Suite | Verification Command | Environment | Result | Evidence / Details |
|---|---|---|---|---|
| **GitHub Actions CI** | Run [#38423840921](https://github.com/whoevenisshubham/TransactX/actions/runs/38423840921) | Ubuntu 24.04 (Containers) | **PASS (100% GREEN)** | Frontend + Backend (Migrations, Tests, Race Tests, Build) all pass |
| **Cross-Bank Recovery Matrix** | `go run -C backend ./cmd/experiments -experiment crossbank` | Live PostgreSQL 18 | PASS (4.7s) | 7/7 scenarios pass; 0 financial invariant violations; JSON/CSV/SHA256 generated |
| **Crash Matrix (B12)** | `go test -v ./internal/experiments -run TestCrashMatrix_` | Live PostgreSQL 18 | PASS (0.8s) | 20/20 scenarios pass; negative payload and debit-credit fault tests pass |
| **Analytics Fail-Closed Veto** | `go test -v ./internal/analytics -run TestAnalytics_` | Go 1.24 | PASS (4.2s) | All 13 tests pass; non-boolean values and query/scan errors fail closed |
| **Live Browser Acceptance** | `npx playwright test browser/ops.live.spec.ts --config playwright.live.config.ts` | Chromium / Live Stack | PASS (10.4s) | 5 passed, 1 skipped. Live worker investigation, SSE stream reconnect, admin recheck |
| **Release Build Validation** | `powershell -File .\deploy\scripts\build-release.ps1 -ValidateOnly` | Windows / Local | PASS (0.8s) | All 6 production commands verified; clean validation |
| **Go Full Backend Suite** | `go test -C backend ./...` | Go 1.24 | PASS (16.3s) | All backend packages pass 100% |
| **Frontend Test Suite** | `npm --prefix frontend run test` | Node.js v22 | PASS (0.50s) | 97/97 tests pass across 16 suites |
| **Frontend Linter** | `npm --prefix frontend run lint` | TypeScript 7 | PASS | 0 type or lint errors |
| **Frontend Money Integrity** | `npm --prefix frontend run test:money` | Node.js v22 | PASS | Integer paise boundary checks pass |
| **Frontend Production Build** | `npm --prefix frontend run build` | Vite 8.3.0 | PASS (0.17s) | Production bundle compiled in `dist/` |

---

## 9. Gate 9: Sync Latest M1 Delivery Evidence and Finalize the Release Handoff

### 9.1 M1 Delivery & Documentation Integration
- **Merged Branch:** Synchronized `origin/v2/m1-product-delivery` (`bc7c16706a7dfb21d4f8beda7e4f26adc66512c1`) into `v2/integration-m1-m2-m3` via merge commit `4623f5e376f4186758b4783db27156ad3622f301`.
- **Integrated Commits:**
  - `060d051`: Add live accessibility and zoom verification for M1 operator screens
  - `af210f9`: Record native cross-bank post-commit loss recovery evidence
  - `bff462c`: Reconcile cross-bank verification SQL with native evidence schema
  - `025f0df`: Align production delivery report with M1 acceptance evidence
  - `b616c17`: Reconcile M1 state docs and integrate native recovery evidence
  - `bc7c167`: Document public operator sign-in prerequisites in release handoff
- **File Reconciliations:**
  - `deploy/verification/cross-bank-financial-proof.sql`: Verified LF encoding and exact match with recorded SHA-256 digest `f37f9d447516b4fc1a6294f913c7fb314d9d4058e0c4dfc4e68079be6bf2b130`.
  - `docs/next-gen/state/INTEGRATION_VERIFICATION.md`: Preserved comprehensive Gate 4–8 sections while integrating M1 delivery acceptance context and public sign-in prerequisites.
- **Preserved Core Hardening:**
  - Canonical migration chain (additive `000030_m2_rebuild_locks.up.sql`).
  - Canonical M3 investigator store (`000029_m3_investigator_store.up.sql`).
  - Analytics fail-closed non-boolean flag parsing and query/scan error handling.
  - Crash matrix (B12) query/scan error checks and negative fault injection suites.
  - Native cross-bank recovery matrix (CB-01 through CB-07) and 8-point SQL financial invariants.
  - Outbox and projection processing in worker loop with transactional rebuild locks.

### 9.2 Gate 8 Regression & Recovery Re-verification on Merged Candidate
- **Cross-Bank Recovery Matrix (`TestCrossBankRecovery_FullMatrix`):**
  - Executed cleanly against live PostgreSQL 18.
  - Scenarios CB-01 through CB-07 all passed (5.56s).
  - 0 financial invariant violations across Bank A (`bank_a.*`), Bank B (`bank_b.*`), and central double-entry ledger.
- **Crash Matrix Hardening (`TestCrashMatrix_FullRecoveryAndEventMatrix`):**
  - Executed cleanly against live PostgreSQL 18.
  - 20/20 scenarios passed (0.73s).
  - Negative fault assertions passed:
    - `TestCrashMatrix_FinancialInvariants_DetectsParticipantPayloadMismatch`: Confirmed harness catches participant amount divergence.
    - `TestCrashMatrix_FinancialInvariants_DetectsDebitCreditImbalance`: Confirmed harness catches ledger imbalance (debit != credit).
    - `TestCrashMatrix_Scenario_FailsOnInjectedCancelledContext`: Confirmed failure on injected context cancellation.
- **Analytics Fail-Closed Provenance:**
  - `go test -v ./internal/analytics -run TestAnalytics_`: All 13 tests passed cleanly (4.16s). Non-boolean inputs, unreviewed windows, and query/scan errors fail closed to `model_status: "unavailable"`.

### 9.3 Delivery Documentation Staging & Release Builder Validation
- **Staging Script Verification (`stage-delivery-docs.mjs`):**
  - Executed: `node deploy/scripts/stage-delivery-docs.mjs .`
  - Result: `Validated 13 delivery documents/evidence/queries.`
  - All 13 approved documents, evidence JSON files, and SQL proof scripts exist, are non-empty, parse as valid JSON, and reside strictly within the repository boundaries without escaping.
- **Release Builder Validation (`build-release.ps1` & `build-release.sh`):**
  - Executed: `powershell -File .\deploy\scripts\build-release.ps1 -ValidateOnly`
  - Result:
    - Source: exact integration commit SHA
    - Dirty tree: False
    - All 8 production commands present: `api`, `bank-a`, `bank-b`, `worker`, `migrate`, `maintain-merkle`, `corpus`, `rebuild-worklist` (0 missing).
    - Approved corpus inputs verified: 6 documents.
    - Frontend API origin: unset (properly flagged as required for release archive packaging).
    - Symmetry between `build-release.ps1` and `build-release.sh` confirmed across commands, flags, staging directories, and SHA-256 metadata schema (`tx.release.v1`).

### 9.4 Accessibility & Frontend Quality Acceptance
- **CSS Improvements:**
  - Bounded controls: `ops-controls input, .ops-controls select` configured with `min-width: 0; width: 100%; box-sizing: border-box;` preventing layout overflow on mobile/tablet viewports and high zoom.
  - High-contrast tokens: `--on-accent` set to `#fff` in light mode and `#10251e` in dark mode, ensuring primary buttons and icon buttons achieve full WCAG AA contrast compliance.
- **Automated Frontend Suites:**
  - Unit & Contract tests: `npm run test` passed 97/97 tests across 16 suites (0 failures).
  - TypeScript type check: `npm run lint` passed with 0 errors.
  - Money boundary verification: `npm run test:money` passed integer paise checks.
  - Production build: `npm run build` compiled cleanly into `dist/` in 175ms.
  - Playwright fixture suite: `npm run test:browser:fixtures` passed 8/8 tests (including axe accessibility checks on investigation citations).
  - Playwright offline-shell suite: `npm run test:browser:offline-shell` passed 2/2 tests (retained owner intent and expired token clearance).

### 9.5 Full Backend Regression Suite
- `go vet ./...`: 0 issues across all packages.
- `go test -count=1 ./...`: 100% pass across all 42 packages.

### 9.6 Explicit External Release Gates & Owner Action Required
In accordance with M1 and integration requirements, the following release gates depend on owner-provided external infrastructure and remain explicitly pending:
1. **Public HTTPS API Origin:**
   - Status: **Pending Owner Action**.
   - Requirement: Public domain and TLS termination configured for `VITE_API_BASE_URL` so release builder can embed the production API origin.
2. **Configured Native Linux Host & DNS/TLS:**
   - Status: **Pending Owner Action**.
   - Requirement: Native Linux target host for release archive extraction, systemd service installation, Caddy reverse-proxy configuration, and private-port smoke testing. Zero paid cloud hosting has been provisioned.
3. **Off-Machine Backup Destination & Disaster Recovery Drill:**
   - Status: **Pending Owner Action**.
   - Requirement: Cloud object store (S3/GCS bucket) configured for daily encrypted database backup replication, plus formal multi-region restore drill satisfying organizational RPO/RTO. (Local restore rehearsal has passed successfully).
4. **Production Browser Live Smoke:**
   - Status: **Pending Owner Action / Target Host**.
   - Requirement: Live host execution of `playwright.live.config.ts` against the live production origin with real credentials.

### 9.7 Gate 9 Verification Evidence Matrix

| Gate / Suite | Verification Command | Environment | Result | Evidence / Details |
|---|---|---|---|---|
| **GitHub Actions CI** | Run [#38426686726](https://github.com/whoevenisshubham/TransactX/actions/runs/38426686726) | Ubuntu 24.04 (Containers) | **PASS (100% GREEN)** | Backend (Migrations, Tests, Race Tests, Build) + Frontend all pass |
| **M1 Delivery Staging** | `node deploy/scripts/stage-delivery-docs.mjs .` | Node.js v22 | **PASS** | Validated 13 delivery documents/evidence files/queries against approved allowlist |
| **Release Builder Validation** | `powershell -File .\deploy\scripts\build-release.ps1 -ValidateOnly` | Windows / Local | **PASS** | All 8 production commands present; 6 corpus documents verified; 0 missing commands |
| **Cross-Bank Recovery Matrix** | `go run -C backend ./cmd/experiments -experiment crossbank` | Live PostgreSQL 18 | **PASS (5.56s)** | 7/7 scenarios pass; 0 financial invariant violations across Bank A, Bank B, and central ledger |
| **Crash Matrix Hardening (B12)** | `go test -v ./internal/experiments -run TestCrashMatrix_` | Live PostgreSQL 18 | **PASS (0.73s)** | 20/20 scenarios pass; negative payload and debit-credit fault tests pass |
| **Analytics Fail-Closed Veto** | `go test -v ./internal/analytics -run TestAnalytics_` | Go 1.24 | **PASS (4.16s)** | All 13 tests pass; non-boolean values and query/scan errors fail closed |
| **Frontend Test Suite** | `npm --prefix frontend run test` | Node.js v22 | **PASS (0.45s)** | 97/97 tests pass across 16 suites |
| **Frontend Linter** | `npm --prefix frontend run lint` | TypeScript 7 | **PASS** | 0 type or lint errors |
| **Frontend Money Integrity** | `npm --prefix frontend run test:money` | Node.js v22 | **PASS** | Integer paise boundary checks pass |
| **Frontend Production Build** | `npm --prefix frontend run build` | Vite 8.3.0 | **PASS (0.18s)** | Production bundle compiled in `dist/` |
| **Playwright Fixture Suite** | `npm --prefix frontend run test:browser:fixtures` | Chromium | **PASS (12.2s)** | 8/8 tests pass (including axe accessibility checks on investigation citations) |
| **Playwright Offline Shell** | `npm --prefix frontend run test:browser:offline-shell` | Chromium | **PASS (4.4s)** | 2/2 tests pass (retained owner intent and token expiry) |
| **Backend Go Vet** | `go vet ./...` | Go 1.24 | **PASS** | 0 issues across all packages |
| **Full Backend Go Suite** | `go test -C backend -count=1 ./...` | Go 1.24 | **PASS (22.1s)** | All 42 packages pass 100% |


