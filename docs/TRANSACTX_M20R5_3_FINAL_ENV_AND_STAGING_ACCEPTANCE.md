# TransactX M20-R.5.3: Final Environment Hardening, Disaster Recovery & Staging Acceptance Report

**Project:** TransactX — A Resilient Payment Infrastructure for the Next Generation of Digital Payments  
**Milestone:** M20-R.5.3  
**Classification:** Operational Security & Test Integrity Engineering Verification  
**Status:** ACCEPTED (100% Gates & Tests Passing)  
**Date:** 2026-11-23  

---

## 1. Executive Summary

Milestone **M20-R.5.3** establishes operational integrity, environment hardening, and disaster-recovery safety across the TransactX platform prior to project freeze. Previous milestone implementations exhibited minor discrepancies between documentation claims, test assertions, and process coordination. M20-R.5.3 completely resolves these findings by:

1. **Eliminating Insecure Fallback Credentials:** All hardcoded fallback secrets (`1234`, fixed bank tokens, short JWT secrets) have been removed from all operational runner scripts.
2. **Implementing a Safe Non-Evaluating Environment Loader:** `scripts/load-local-staging-env.ps1` safely parses `KEY=VALUE` pairs without executing arbitrary shell code, enforces strict variable naming, derives default service bindings, and validates secret strength (enforcing `JWT_SECRET` length $\ge 32$ characters).
3. **Unifying Process Supervision:** `scripts/start-local-staging.ps1` launches `scripts/supervise-local-staging.ps1`, eliminating competing, unmanaged background processes. Process trees are tracked via PID files, and `scripts/stop-local-staging.ps1` guarantees clean termination of all child trees and verifies that all staging ports (`8080, 8081, 8082, 3000`) are free.
4. **Hermetic Database Restore Safety Testing:** `scripts/test-restore-safety.ps1` executes 12 automated, non-destructive safety tests against unique, isolated disposable databases. The primary staging database (`transactx_local_staging`) is verified to remain completely untouched.
5. **Deterministic Staging Lifecycle Acceptance:** `scripts/test-local-staging-lifecycle.ps1` runs 12 acceptance gates verifying port cleanliness, supervisor tree tracking, health endpoints, JWT issuance, database URL propagation via `pg_stat_activity`, clean shutdown, no-restart invariant, and controlled startup failure.

---

## 2. Configuration Security & Environment Loader

### 2.1 Git Invariant & Secret Protection
To prevent credential leaks into version control, `.gitignore` was updated and verified:
```gitignore
deploy/env/*.env
!deploy/env/*.env.example
deploy/env/local-staging.env
```
Verification command `git check-ignore deploy/env/local-staging.env` confirms the staging file is completely ignored.

### 2.2 Precedence Model
`scripts/load-local-staging-env.ps1` implements a four-tier precedence hierarchy:
1. **Explicit CLI Parameters:** Directly supplied flags (e.g., `-DbUrl`) take top precedence.
2. **Process Environment Variables:** Pre-existing environment variables in the parent process take precedence over configuration files.
3. **Ignored Configuration File (`deploy/env/local-staging.env`):** Parsed line-by-line using non-evaluating string manipulation (ignoring comments and whitespace, stripping matching quotes).
4. **Fail-Closed Validation:** If any of the 14 mandatory configuration variables is missing or empty, or if `JWT_SECRET` is shorter than 32 characters, execution halts immediately with exit code 1.

### 2.3 Automated Configuration Test Suite (`scripts/test-env-config.ps1`)
The configuration loader was verified using 7 automated test cases:

| Test ID | Description | Status | Details |
| :--- | :--- | :---: | :--- |
| `TEST-ENV-01` | Missing JWT_SECRET fails validation | **PASS** | Environment validation failed: missing JWT_SECRET |
| `TEST-ENV-02` | Short JWT_SECRET (< 32 chars) fails validation | **PASS** | Environment validation failed: length < 32 |
| `TEST-ENV-03` | Missing BANK_A_SERVICE_TOKEN fails validation | **PASS** | Environment validation failed: missing BANK_A_SERVICE_TOKEN |
| `TEST-ENV-04` | Missing DATABASE_URL fails validation | **PASS** | Environment validation failed: missing DATABASE_URL |
| `TEST-ENV-05` | Valid environment file loads values and derives defaults | **PASS** | Derived BANK_A_DATABASE_URL and service bindings |
| `TEST-ENV-06` | Pre-existing environment variables take precedence over file | **PASS** | Process environment variable preserved |
| `TEST-ENV-07` | Secret values are not printed to output streams | **PASS** | Zero secret tokens emitted to stdout/stderr |

---

## 3. Staging Supervisor & Process Lifecycle

### 3.1 Architecture
The staging supervisor (`scripts/supervise-local-staging.ps1`) manages five core services in a single supervised process hierarchy:
1. **Bank A Simulator** (`scripts/run-bank-a.ps1`, port `8081`)
2. **Bank B Simulator** (`scripts/run-bank-b.ps1`, port `8082`)
3. **TransactX Core API** (`scripts/run-api.ps1`, port `8080`)
4. **Recovery Worker** (`scripts/run-worker.ps1`)
5. **Caddy Ingress Reverse Proxy** (`scripts/run-caddy.ps1`, port `3000`)

### 3.2 Termination Guarantees
- The supervisor writes its PID to `logs/supervisor.pid`.
- Shutdown is signaled by placing `logs/supervisor.stop`.
- The supervisor detects the flag, exits its monitoring loop, and executes `taskkill.exe /PID <pid> /T /F` on every managed child process tree.
- `scripts/stop-local-staging.ps1` awaits supervisor exit, verifies all ports (`8080, 8081, 8082, 3000`) are free, and removes PID files only after all ports are confirmed vacant.

---

## 4. Hermetic Database Restore Safety Suite (`scripts/test-restore-safety.ps1`)

The database restore procedure was subjected to 12 automated, isolated safety tests:

```
TestID           Description                                                                       Result Details
------           -----------                                                                       ------ -------
TEST-RESTORE-01  Valid restore into fresh disposable database with data integrity                  PASS   Tables restored: 1, Rows: 5
TEST-RESTORE-02  Restore into existing disposable database with explicit -Force                    PASS   Target recreated & data intact
TEST-RESTORE-02B Restore into existing disposable database without -Force is rejected              PASS   Target exists and Force not specified
TEST-RESTORE-03  Target equals custom source database (TX_SOURCE_DB) is rejected                   PASS   Safety violation: target equals source
TEST-RESTORE-04  System database (postgres) selected as restore target is rejected                 PASS   Safety violation: target is postgres
TEST-RESTORE-05  Invalid identifier format is rejected                                             PASS   Invalid target database identifier
TEST-RESTORE-06  SQL-injection-style database identifier is rejected before invocation             PASS   Invalid target database identifier
TEST-RESTORE-07  Missing password configuration is rejected without inherited credentials          PASS   PGPASSWORD or -Password required
TEST-RESTORE-08  Empty (0 bytes) backup file is rejected                                           PASS   Backup file is empty
TEST-RESTORE-09  Simulated SQL restore error triggers ON_ERROR_STOP=1 and halts                    PASS   psql halted on syntax error
TEST-RESTORE-10  Statements after failing SQL statement did not execute (ON_ERROR_STOP verified)   PASS   post_err_table does NOT exist
TEST-RESTORE-11  Failed restore returns non-zero exit code and never reports success               PASS   ExitCode=1, reported failure
TEST-RESTORE-12  Original staging database (transactx_local_staging) remained completely untouched PASS   Table count unchanged: 19 tables
```

### Key Safety Invariant Verified:
In `TEST-RESTORE-10`, a backup file containing a deliberate SQL syntax error followed by `CREATE TABLE post_err_table (...)` was restored. `psql` halted immediately under `-v ON_ERROR_STOP=1`, and the test verified via `SELECT to_regclass('post_err_table')` that the post-error table was never created.

---

## 5. Local Staging Lifecycle Acceptance Suite (`scripts/test-local-staging-lifecycle.ps1`)

The staging lifecycle suite executed all 12 gates against a live disposable template database:

```
GateID       Description                                                                       Result Details
------       -----------                                                                       ------ -------
GATE-LIFE-01 Pre-flight: Staging ports (8080, 8081, 8082, 3000) are free                       PASS   All staging ports free
GATE-LIFE-02 Disposable lifecycle database provisioned                                         PASS   Created transactx_staging_lifecycle_309968
GATE-LIFE-03 Supervisor started and PID recorded accurately                                    PASS   Supervisor PID 35864 active
GATE-LIFE-04 Direct API /readyz returns readiness=true (HTTP 200)                              PASS   Response: {"data":{"readiness":true,...}}
GATE-LIFE-05 Caddy-proxied /readyz returns readiness=true (HTTP 200)                           PASS   Response: {"data":{"readiness":true,...}}
GATE-LIFE-06 Bank A & Bank B /healthz endpoints respond status=ok                              PASS   BankA=ok, BankB=ok
GATE-LIFE-07 Customer auth endpoint is reachable and issues JWT                                PASS   Token received for alice@transactx
GATE-LIFE-08 Database URL propagation empirically verified via pg_stat_activity                PASS   Active connections from API/Worker verified
GATE-LIFE-09 Shutdown verified: supervisor exited, all ports free, PID files cleaned           PASS   ExitCode=0, Supervisor stopped, ports free
GATE-LIFE-10 No-restart invariant: zero processes restarted after stop                         PASS   Respawn check: 0 processes after 5s
GATE-LIFE-11 Restart lifecycle verified: second startup ready and second stop cleans all ports PASS   RestartReady=True, RestartPortsFree=True
GATE-LIFE-12 Controlled startup failure test fails visibly with zero orphan processes          PASS   FailedVisibly=True, ExitCode=1, PortsFree=True
```

### Empirical Propagation Verification (`GATE-LIFE-08`):
The test verified that the custom disposable database received active connections from the managed API and Worker services by querying:
```sql
SELECT application_name, count(*) 
FROM pg_stat_activity 
WHERE datname = 'transactx_staging_lifecycle_309968' AND pid <> pg_backend_pid()
GROUP BY application_name;
```
Connections were confirmed open for `transactx-api` and `transactx-recovery-worker`.

---

## 6. Verification Verdict

All 7 configuration tests, 12 hermetic restore safety tests, and 12 local staging lifecycle acceptance gates have passed with 100% compliance. TransactX local staging deployment and recovery operations are fully validated and operationally sealed.
