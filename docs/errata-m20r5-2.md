# TransactX Milestone M20-R.5.2 Errata & Technical Reconciliation

This document formally records the technical corrections, provenance reconciliations, and schema clarifications identified during the M20-R.5.3 audit of Milestone M20-R.5.2 deliverables.

---

## 1. Commit Provenance Reconciliation

* **M20-R.5.2 Completion Report Text Citation:** `770f95c4794e63b655da08ce4589d3db9c7ca3a5`
* **Authentic Git Commit SHA:** `770f95cc0c0e3b855b9b8da458f5b76cb1e3af62`
* **Clarification:** The string `770f95c4...` in the narrative header was a transcription erratum introduced during completion report drafting. The authentic, immutable Git commit created on branch `feat/frontend-rebuild-v4` and verified by hosted GitHub Actions workflows is `770f95cc0c0e3b855b9b8da458f5b76cb1e3af62`. All subsequent work in M20-R.5.3 branches directly from this verified commit.

---

## 2. PostgreSQL Idempotency Schema Correction

* **M20-R.5.2 Illustrative Snippet:** The report contained an illustrative SQL snippet joining on a hypothetical `p.idempotency_key` column on the `payments` table.
* **Authentic Schema Implementation:** In the TransactX relational schema (`schema.sql` and `internal/payments/repository.go`), the `payments` table stores central transaction state without a denormalized key column. Instead, the dedicated `idempotency_records` table establishes the user-scoped idempotency mapping:
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
* **Authentic Query:**
  ```sql
  SELECT r.key, r.request_hash, r.payment_id, p.state, p.amount_paise
  FROM idempotency_records r
  JOIN payments p ON p.id = r.payment_id
  WHERE r.user_id = $1 AND r.key = $2;
  ```

---

## 3. Test Count & Acceptance Gate Reconciliation

* **Database Restore Safety Suite (`scripts/test-restore-safety.ps1`):**
  * The M20-R.5.2 report mentioned 7 core safety cases.
  * In M20-R.5.3, the hermetic safety suite was expanded and formalized into 12 explicit, non-destructive automated test assertions (`TEST-RESTORE-01` through `TEST-RESTORE-12`), verifying disposable database creation, `-Force` protection, destination safety guards, credential requirements, SQL injection immunity, `ON_ERROR_STOP=1` non-execution of post-error DDL, exit code signaling, and preservation of the untouched primary staging database.
* **Local Staging Lifecycle Suite (`scripts/test-local-staging-lifecycle.ps1`):**
  * Formalized into 12 deterministic gates (`GATE-LIFE-01` through `GATE-LIFE-12`), verifying pre-flight port hygiene, disposable database template provisioning, supervisor process tree tracking, direct and Caddy `/readyz` health polling, bank health endpoints, JWT issuance via demo auth, active connection propagation via `pg_stat_activity`, clean tree termination, 5-second respawn absence, full restart cycle, and controlled configuration failure.

---

## 4. Execution Context Separation (Local vs. Hosted)

* **Local Staging & Disaster Recovery:**
  * Tests exercising Windows PowerShell process supervision, TCP port binding (`3000, 8080, 8081, 8082`), Caddy ingress proxying, PostgreSQL physical dumps via `pg_dump`/`psql`, and process signal handling run exclusively in the dedicated local staging environment against running services.
* **Hosted CI Pipelines (GitHub Actions):**
  * Hosted CI runs the automated Go unit and integration suites (`go test -count=1 ./...`, `go test -race ./...`), Go vulnerability scanning (`govulncheck`), and frontend TypeScript/React test and build gates (`npm test`, `npm run test:money`, `npm run build`).
  * The hosted environment does not bind local Windows supervisor scripts or interactive Caddy processes; operational staging tests are validated via local automated scripts committed to the repository.
