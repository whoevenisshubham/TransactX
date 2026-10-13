# TransactX — M1/M2/M3 Integration & Release Verification Checkpoint

**Date:** 2026-10-13  
**Status:** INTEGRATED & VERIFIED  
**Integration Branch:** `v2/integration-m1-m2-m3`  
**Base:** `origin/release/transactx-v2` (`39fc5c7`)  
**Integrated Commits:**
- M2 (`origin/v2/m2-reliability-platform`): `c9da5fb9035dff259c936fb79785eecbd93efde6` (PR #24)
- M1 (`origin/v2/m1-product-delivery`): `8afec7afdb2f78f43283e6c7d78c65aee28331a8` (PR #22)
- M3 (`origin/v2/m3-investigation-analytics`): `4100dd713ade1e1ddd078bbaa78a5436f5825561` (PR #23)
- Integration Commits: `6c680a5` (merge & conflict resolution), `cc97e21` (composition adapters, migration 000029, runner wiring, CSS overflow fix)

---

## 1. Integration Scope & Architecture Decisions

1. **Isolation & Safety Guarantees:**
   - Isolated worktree: `TransactX_Integration` on branch `v2/integration-m1-m2-m3`.
   - Feature branches (`v2/m1-product-delivery`, `v2/m2-reliability-platform`, `v2/m3-investigation-analytics`) and PRs #22, #23, #24 were preserved unmerged.
   - `release/transactx-v2` remained completely untouched.
   - External spend: ₹0 / $0. Zero external LLM or cloud provider costs incurred.

2. **Database Migrations (000001–000029):**
   - Canonical M2 migrations (000001–000028) were preserved with exact SHA256 ledger checksum matches.
   - Forward migration `000029_m3_investigator_store.up.sql` was added to provision M3's corpus and investigator store (`corpus_versions`, `runbook_chunks`, `investigation_usage_limits`, plus alter columns for `investigation_jobs` and `investigation_usage_reservations`).
   - Line endings were normalized to LF via `.gitattributes` (`backend/migrations/*.sql text eol=lf`).
   - Verification command: `go run ./cmd/migrate -database-url "..." verify` passed with all 29 migrations verified.

3. **Investigator Service & Store Resolution:**
   - M3's durable investigator state machine, bounded evidence bridge, grounding, lease fencing, checkpoint resumption, and zero-budget evidence-only behavior was selected as the canonical runtime investigator service.
   - M2's authenticated operations evidence, recovery tracking, OpenTelemetry trace store, and analytics windows were connected to M3's `BridgedEvidenceReader` and `ToolDependencies` via composition adapters:
     - `backend/internal/app/investigator_adapters.go` (runtime composition in `Environment`)
     - `backend/internal/http/investigator_adapters.go` (HTTP layer composition)
   - Background investigator worker processing was wired into `backend/internal/app/worker.go` using `investigatorStore.Claim` and `investigatorSvc.Run` in an asynchronous loop.
   - HTTP endpoint `/api/ops/investigations` adheres to asynchronous queue semantics (202 Accepted on enqueue, background claim/execution by workers).

4. **Corpus Management & Decisions Integrity:**
   - Updated `docs/runbooks/corpus-manifest.json` SHA256 checksum for `docs/decisions.md` to account for merged M2 ADRs (ADR-033 through ADR-040).
   - Staged and atomically activated the 35 approved runbook chunks into PostgreSQL using `cmd/corpus`.

5. **Frontend & Browser Compatibility:**
   - Patched `frontend/src/design/ops.css` with `.ops-investigation .ops-muted { overflow-wrap: anywhere; }` to resolve the narrow-screen 360px overflow gate for runbook citation references.

---

## 2. Test Execution & Verification Evidence

| Test Suite | Command | Result | Duration | Notes |
|---|---|---|---|---|
| **Migration Checksums** | `go run ./cmd/migrate verify` | PASS | 1.0s | All 29 migrations verified against live PostgreSQL |
| **Corpus Staging & Activation** | `go run ./internal/investigator/cmd/corpus -activate` | PASS | 0.8s | 35 chunks validated and activated atomically |
| **Backend Go Vet** | `go vet ./...` | PASS | 1.9s | Zero warnings across all packages |
| **Backend Compilation** | `go build ./...` | PASS | 1.2s | All binaries and packages build cleanly |
| **Backend Test Suite** | `go test ./...` | PASS | ~35s | All packages pass (`cmd/*`, `internal/*`) |
| **Crash Matrix (B12)** | `go run ./cmd/experiments -experiment crash` | PASS | 3.2s | 20/20 scenarios pass with all invariants true |
| **Frontend Lint** | `npm run lint` | PASS | 2.5s | `tsc -b` passes with zero type errors |
| **Frontend Unit Tests** | `npm test` | PASS | 0.6s | 95/95 tests pass across 16 test suites |
| **Frontend Money Checks** | `npm run test:money` | PASS | 0.4s | Exact paise integer parsing verified |
| **Frontend Production Build** | `npm run build` | PASS | 0.3s | `vite build` completed cleanly |
| **Playwright Browser Tests** | `npm run test:browser:fixtures` | PASS | 11.7s | 8/8 tests pass (360px viewport, axe accessibility, citations) |

---

## 3. Changed Files in Integration

- `.gitattributes` — Added LF enforcement for migrations and markdown docs.
- `backend/migrations/000029_m3_investigator_store.up.sql` — Schema extensions for M3 store and corpus.
- `backend/migrations/000029_m3_investigator_store.down.sql` — Rollback DDL for 000029.
- `backend/internal/app/app.go` — Wiring M3 investigator store and service into environment.
- `backend/internal/app/worker.go` — Worker loop integration for background investigator execution.
- `backend/internal/app/investigator_adapters.go` — Runtime adapters connecting M2 operations to M3 evidence bridge.
- `backend/internal/http/handler.go` — HTTP router integration for investigator service and store.
- `backend/internal/http/operations_handler.go` — Enqueue and fetch handlers mapped to M3 service with UUID validation.
- `backend/internal/http/investigator_adapters.go` — HTTP-level adapters for operations and telemetry readers.
- `backend/internal/http/operations_integration_test.go` — Test runner updated to construct M3 investigator service.
- `backend/internal/investigator/investigator_test.go` — Conformed test cases to M3 report types and rate limiter.
- `docs/runbooks/corpus-manifest.json` — Updated `docs/decisions.md` checksum for merged repository.
- `frontend/src/design/ops.css` — Added `overflow-wrap: anywhere` rule for investigation muted text.
