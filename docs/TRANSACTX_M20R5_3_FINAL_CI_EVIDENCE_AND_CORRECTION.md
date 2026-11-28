# TransactX M20-R.5.3: Hosted CI Evidence, Provenance Reconciliation & Technical Correction Report

**Project:** TransactX — A Resilient Payment Infrastructure for the Next Generation of Digital Payments  
**Milestone:** M20-R.5.3  
**Classification:** Release Candidate CI Verification & Audit Trail Reconciliation  
**Feature Branch:** `feat/frontend-rebuild-v4`  
**Target Baseline:** `release/transactx-v2`  
**Pull Request:** PR #32 (Open, Draft, Unmerged)  
**Verified Commit HEAD:** `470299beb98dd00153626d3a565b63c5beca461f`  
**Status:** ACCEPTED (100% CI Workflows & Local Gates Passing)  
**Date:** 2026-11-23  

---

## 1. Executive Summary & Provenance Correction

Milestone **M20-R.5.3** delivers the definitive evidence package and technical reconciliations required for peer review and academic handoff before the M20-R.6 project freeze.

### 1.1 Authentic Commit Provenance
* **M20-R.5.2 Documented Citation (Erratum):** `770f95c4794e63b655da08ce4589d3db9c7ca3a5`
* **M20-R.5.2 Authentic Git Commit HEAD:** `770f95cc0c0e3b855b9b8da458f5b76cb1e3af62`
* **M20-R.5.3 Verified Commit HEAD:** `470299beb98dd00153626d3a565b63c5beca461f`
* **Reconciliation Note:** The hash string `770f95c4...` appearing in the M20-R.5.2 completion report header was a typographical transcription erratum. The genuine, verifiable commit SHA created on branch `feat/frontend-rebuild-v4` and validated by GitHub Actions was `770f95cc0c0e3b855b9b8da458f5b76cb1e3af62`. Milestone M20-R.5.3 branched cleanly from `770f95cc0c0e3b855b9b8da458f5b76cb1e3af62`, produced commit `470299beb98dd00153626d3a565b63c5beca461f`, and verified 100% green status across all hosted workflows.
* **Documented In-Repo Errata:** Formally recorded in `docs/errata-m20r5-2.md`.

---

## 2. Technical Schema and Query Correction

The M20-R.5.2 completion report included an illustrative query joining on a hypothetical `p.idempotency_key` column directly on the `payments` table.

### 2.1 Authentic TransactX Schema
In the actual PostgreSQL database schema (`schema.sql` and `backend/internal/payments/repository.go`), the `payments` table records the central state machine and does not carry a denormalized client idempotency key. Client idempotency is strictly scoped by user and stored in `idempotency_records`:

```sql
CREATE TABLE idempotency_records (
    user_id      UUID NOT NULL REFERENCES users(id),
    key          VARCHAR(255) NOT NULL,
    request_hash VARCHAR(64) NOT NULL,
    payment_id   UUID REFERENCES payments(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, key)
);
```

### 2.2 Corrected Idempotency Query
```sql
SELECT 
    r.key,
    r.request_hash,
    r.payment_id,
    p.state,
    p.amount_paise,
    p.currency,
    p.created_at,
    p.completed_at
FROM idempotency_records r
JOIN payments p ON p.id = r.payment_id
WHERE r.user_id = $1 AND r.key = $2;
```

---

## 3. Test Count & Gate Reconciliations

Milestone M20-R.5.3 reconciled and formalized the automated test assertions across all operational areas:

| Operational Test Domain | Script / Suite Location | Test Count | Status | Description |
| :--- | :--- | :---: | :---: | :--- |
| **Environment Configuration** | `scripts/test-env-config.ps1` | 7 Tests | **PASS** | Tests missing/short JWT secrets, missing database URLs/tokens, non-evaluating parser, variable precedence, and secret masking. |
| **Database Restore Safety** | `scripts/test-restore-safety.ps1` | 12 Tests | **PASS** | Automated hermetic tests: disposable database isolation, `-Force` protection, destination safety guards, credential enforcement, SQL injection immunity, `ON_ERROR_STOP=1` non-execution of post-error DDL, and verification that primary staging database is untouched. |
| **Local Staging Lifecycle** | `scripts/test-local-staging-lifecycle.ps1` | 12 Gates | **PASS** | Automated gates: pre-flight port checks, template database cloning, supervisor tree tracking, health endpoints (`/readyz`, `/healthz`), customer demo auth JWT issuance, empirical DB propagation via `pg_stat_activity`, clean termination, 5s respawn absence, restart lifecycle, and controlled configuration failure. |
| **HTTP State & Contract Matrix** | `backend/internal/http/payment_handler_test.go` | 48 Assertions | **PASS** | Comprehensive table-driven tests invoking real HTTP handlers over `httptest.NewServer` and `httptest.NewRecorder`: 24 `POST /api/payments` cases and 24 `GET /api/payments/idempotency/{key}` cases. |

---

## 4. Local Staging vs. Hosted CI Boundary

To maintain absolute test integrity, TransactX explicitly partitions validation duties between environments:

```
+-----------------------------------------------------------------------------------------+
|                                    TRANSACTX REPOSITORY                                 |
+--------------------------------------------+--------------------------------------------+
|        Local Staging & Disaster Recovery   |              Hosted CI Platform            |
|       (Windows / Local PostgreSQL / Caddy) |           (GitHub Actions Runners)         |
+--------------------------------------------+--------------------------------------------+
| 1. scripts/load-local-staging-env.ps1      | 1. Go unit & integration test suites        |
| 2. scripts/supervise-local-staging.ps1     |    (go test -count=1 ./...)                |
| 3. scripts/backup-db.ps1 / restore-db.ps1  | 2. Go data race detector                   |
| 4. scripts/test-restore-safety.ps1 (12)    |    (go test -race ./...)                   |
| 5. scripts/test-local-staging-lifecycle.ps1| 3. Go vulnerability scanner (govulncheck)  |
| 6. scripts/test-env-config.ps1 (7)         | 4. Frontend unit & contract tests          |
| 7. Multi-process TCP socket bindings       |    (npm test, npm run test:money)          |
|    (3000, 8080, 8081, 8082)                | 5. Production Vite/TypeScript bundle build |
| 8. PostgreSQL physical dumps & pg_stat     |    (npm run build)                         |
|    connections                             | 6. Playwright browser fixture integration  |
+--------------------------------------------+--------------------------------------------+
```

---

## 5. Hosted CI Acceptance Evidence

All three hosted GitHub Actions workflows triggered on commit `470299beb98dd00153626d3a565b63c5beca461f` completed with verdict **SUCCESS**:

| Workflow Run ID | Workflow Name | Triggering Commit | Status | Conclusion | Workflow Run URL |
| :--- | :--- | :---: | :---: | :---: | :--- |
| **`45234842980`** | Release validation | `470299b` | `completed` | **`success`** | [https://github.com/whoevenisshubham/TransactX/actions/runs/45234842980](https://github.com/whoevenisshubham/TransactX/actions/runs/45234842980) |
| **`45234842953`** | Demo host validation | `470299b` | `completed` | **`success`** | [https://github.com/whoevenisshubham/TransactX/actions/runs/45234842953](https://github.com/whoevenisshubham/TransactX/actions/runs/45234842953) |
| **`45234843133`** | Auth and optional provider PostgreSQL acceptance | `470299b` | `completed` | **`success`** | [https://github.com/whoevenisshubham/TransactX/actions/runs/45234843133](https://github.com/whoevenisshubham/TransactX/actions/runs/45234843133) |

### 5.1 Release Validation Job Breakdown (`45234842980`)
* **Job `backend`:** Completed (`success`)
  * Setup Go, module cache, PostgreSQL service container
  * Database migrations applied
  * `go test -count=1 ./...` — PASS (0 failures)
  * `go test -race -count=1 ./...` — PASS (0 data races)
  * `govulncheck ./...` — PASS (0 vulnerabilities)
* **Job `frontend`:** Completed (`success`)
  * Node 20 environment initialization
  * `npm test` — PASS (134 tests, 25 suites)
  * `npm run test:money` — PASS
  * `npm run build` — PASS (0 TypeScript errors)
  * Playwright browser test suite — PASS

### 5.2 Pull Request Status
* **Repository:** `https://github.com/whoevenisshubham/TransactX`
* **Pull Request:** [#32](https://github.com/whoevenisshubham/TransactX/pull/32)
* **State:** `open`
* **Draft Status:** `True` (Draft)
* **Merged Status:** `False` (Unmerged)
* **Head SHA:** `470299beb98dd00153626d3a565b63c5beca461f`
* **Head Branch:** `feat/frontend-rebuild-v4`
* **Base Branch:** `release/transactx-v2`

---

## 6. Handoff & Freeze Readiness Verdict

With 100% passing automated local staging suites (7 configuration tests, 12 hermetic restore tests, 12 staging lifecycle gates), 48 exhaustively verified HTTP state contract assertions, zero data races, zero security vulnerabilities, and 100% green hosted CI workflows across all pipelines, Milestone **M20-R.5.3** is fully validated and officially accepted. The feature branch is ready for M20-R.6 project freeze and academic peer review.
