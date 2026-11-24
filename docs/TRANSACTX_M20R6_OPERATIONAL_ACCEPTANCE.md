# TransactX: M20-R.6 Operational Acceptance & Live Demonstration Record

**Project:** TransactX — A Resilient Payment Infrastructure for the Next Generation of Digital Payments  
**Document ID:** `DOC-ACCEPT-M20R6-01`  
**Milestone:** M20-R.6 (Operational Runbook Reconciliation & Live Demonstration Acceptance)  
**Date:** November 2026  
**Execution Host OS:** Windows 11 (64-bit)  
**Repository:** `whoevenisshubham/TransactX`  
**Feature Branch:** `feat/frontend-rebuild-v4`  
**Target Branch:** `release/transactx-v2`  
**Pull Request:** [#32](https://github.com/whoevenisshubham/TransactX/pull/32) (Open, Draft, Unmerged)  
**Frozen Baseline HEAD:** [`81a13ddc54e99db393fb927fae1aa15755ba507e`](https://github.com/whoevenisshubham/TransactX/commit/81a13ddc54e99db393fb927fae1aa15755ba507e)  
**Status:** ACCEPTED AND DEMONSTRATION READY  

---

## 1. Verified Local Runtime & Environment Baseline

| Component / Runtime | Version Observed | Verification Command | Notes |
|---|---|---|---|
| **Go Toolchain** | `go1.27.1 windows/amd64` | `go version` | Module directive specifies `go 1.27.4` / Go 1.24+ compatibility |
| **Node.js Runtime** | `v22.20.0` | `node --version` | Verified LTS reference matching hosted CI runner |
| **npm Package Manager** | `10.9.3` | `npm --version` | Clean dependency resolution |
| **PostgreSQL Server** | `PostgreSQL 18.6 on x86_64-windows` | `psql -c "SELECT version();"` | Standard local port 5432 |
| **Caddy Ingress Server** | `v2.11.9` | `.\bin\caddy.exe version` | Executable residing in `bin\caddy.exe` |
| **PowerShell Engine** | `5.1.26100.2161` | `$PSVersionTable.PSVersion` | Operational supervision scripts runner |

---

## 2. Reconciled Staging Deployment Topology & Health Evidence

The local staging stack was executed under the unified supervisor (`scripts/start-local-staging.ps1 -Foreground`).

### 2.1 Managed Process Allocation
- **Supervisor Process:** PID `37224`
- **Bank A Simulator:** PID `40516` (listening on `127.0.0.1:8081`)
- **Bank B Simulator:** PID `22836` (listening on `127.0.0.1:8082`)
- **Central Go API:** PID `1648` (listening on `127.0.0.1:8080`)
- **Recovery Worker:** PID `31148` (autonomous background saga lease monitor)
- **Caddy Reverse Proxy:** PID `472` (listening on `http://127.0.0.1:3000`)

### 2.2 Health & Readiness Verification Evidence

| Endpoint | Target URL | HTTP Status | Response Payload | Result |
|---|---|:---:|---|:---:|
| **Central API Direct Readiness** | `http://127.0.0.1:8080/readyz` | `200 OK` | `{"data":{"readiness":true,"service":"transactx-api","status":"ok"},"requestId":"8b6f3520..."}` | **HEALTHY** |
| **Caddy Proxied Readiness** | `http://127.0.0.1:3000/readyz` | `200 OK` | `{"data":{"readiness":true,"service":"transactx-api","status":"ok"},"requestId":"1aa39924..."}` | **HEALTHY** |
| **Bank A Participant Health** | `http://127.0.0.1:8081/healthz` | `200 OK` | `{"status":"ok"}` | **HEALTHY** |
| **Bank B Participant Health** | `http://127.0.0.1:8082/healthz` | `200 OK` | `{"status":"ok"}` | **HEALTHY** |

---

## 3. Disposable Database Migration & Demo Seeding Record

A clean, explicitly named disposable database `transactx_m20r6_acceptance` was provisioned and validated:

1. **Database Creation:**
   ```sql
   CREATE DATABASE transactx_m20r6_acceptance;
   ```
2. **Schema Migration Execution:**
   - Command: `go run ./cmd/migrate -database-url "postgres://.../transactx_m20r6_acceptance?sslmode=disable" -dir ./migrations up`
   - Result: **33 migrations applied successfully** (`000001` through `000033`).
   - Verification: `go run ./cmd/migrate verify` confirmed all checksums match canonical SQL files.
3. **Public Demo Seeding with Guard Acknowledgment:**
   - Environment guards passed:
     - `TX_DEMO_INITIALIZE=I_ACKNOWLEDGE_NEW_EMPTY_DEMO_DATABASE`
     - `TX_DEMO_EXPECTED_DATABASE=transactx_m20r6_acceptance`
     - `TX_DEMO_PASSWORD=[SUPPRESSED]` (minimum 12 characters)
   - Executed: `go run ./cmd/demo-seed`
   - Log observed: `"fresh demo initialized; no historical transition data imported; public roles contain no administrator"`
4. **Verified Seeded Account Balances:**
   - Alice Demo (`alice@transactx`, Bank A): `1,000,000` paise (**₹10,000.00**)
   - Bob Demo (`bob@transactx`, Bank B): `100,000` paise (**₹1,000.00**)
   - Demo Store (`store@transactx`, Bank B): `0` paise (**₹0.00**)
   - Demo Operator (`viewer@transactx`, Bank A): `0` paise (**₹0.00**, role `OPS_VIEWER`)

---

## 4. Live Demonstration Execution Results

### 4.1 Demo Authentication & JWT Issuance
- **Request:** `POST http://127.0.0.1:3000/api/auth/demo`
- **Payload:** `{"persona":"customer"}`
- **Response:** HTTP 200 OK
  ```json
  {
    "data": {
      "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
      "user": {
        "id": "73ea57aa-8b3f-4090-845b-05b100fbd345",
        "name": "Alice Demo",
        "paymentIdentifier": "alice@transactx",
        "role": "CUSTOMER"
      }
    },
    "requestId": "e4dcdd34cd008a52809709a3e81c5a8d"
  }
  ```

### 4.2 Scenario 1: Initial Payment Dispatch
- **Endpoint:** `POST http://127.0.0.1:3000/api/payments`
- **Headers:** `Idempotency-Key: demo-m20r6-key-001`, `Authorization: Bearer <Token>`
- **Payload:** `{"recipient":"bob@transactx","amountPaise":25000,"currency":"INR","note":"Coffee and pastry"}`
- **Observed Response:**
  ```json
  {
    "data": {
      "id": "53fb77f9-8e45-43f7-a4c2-d5c3bc566cc8",
      "amountPaise": 25000,
      "currency": "INR",
      "note": "Coffee and pastry",
      "origin": "ONLINE",
      "state": "COMPLETED",
      "createdAt": "2026-11-24T09:27:08.350407+05:30",
      "completedAt": "2026-11-24T09:27:08.542826+05:30",
      "senderName": "Alice Demo",
      "senderPaymentIdentifier": "alice@transactx",
      "receiverName": "Bob Demo",
      "receiverPaymentIdentifier": "bob@transactx",
      "direction": "SENT",
      "sourceBankName": "TransactX Development BANK-A",
      "sourceBankCode": "BANK-A",
      "destinationBankName": "TransactX Development BANK-B",
      "destinationBankCode": "BANK-B",
      "durationMs": 192
    },
    "requestId": "5c3a570888579e99de7c2870470648c2"
  }
  ```
- **Outcome:** Payment completed end-to-end through Bank A hold, bank settlement, and central ledger commit in **192ms**.

### 4.3 Scenario 2A: Identical Idempotent Replay
- **Endpoint:** `POST http://127.0.0.1:3000/api/payments`
- **Headers:** Identical `Idempotency-Key: demo-m20r6-key-001`
- **Payload:** Identical payload (`amountPaise: 25000`)
- **Observed Response:**
  - Status: HTTP 200 OK
  - Returned Payment ID: `53fb77f9-8e45-43f7-a4c2-d5c3bc566cc8`
  - Equality Check: `pay1.id == pay2.id` evaluates to **`True`**
- **Outcome:** Request was recognized as an identical replay; no secondary debit occurred.

### 4.4 Scenario 2B: Conflicting Payload Replay
- **Endpoint:** `POST http://127.0.0.1:3000/api/payments`
- **Headers:** Same `Idempotency-Key: demo-m20r6-key-001`
- **Payload:** Modified amount (`amountPaise: 50000`)
- **Observed Response:**
  - Status: **`HTTP 409 Conflict`** (`IDEMPOTENCY_CONFLICT`)
- **Outcome:** The mutation was rejected fail-closed; zero rows in `payments` or `ledger_entries` were altered.

### 4.5 Scenario 6A: Double-Entry Ledger Conservation Verification
Direct SQL query on the PostgreSQL database after execution:
```sql
SELECT lt.payment_id, le.entry_type, le.amount_paise 
FROM ledger_transactions lt 
JOIN ledger_entries le ON lt.id = le.ledger_transaction_id 
WHERE lt.payment_id = '53fb77f9-8e45-43f7-a4c2-d5c3bc566cc8';
```
**Results:**
| `payment_id` | `entry_type` | `amount_paise` |
|---|---|---|
| `53fb77f9-8e45-43f7-a4c2-d5c3bc566cc8` | `DEBIT` | 25,000 |
| `53fb77f9-8e45-43f7-a4c2-d5c3bc566cc8` | `CREDIT` | 25,000 |

Aggregate ledger audit:
```sql
SELECT SUM(CASE WHEN entry_type = 'DEBIT' THEN amount_paise ELSE 0 END) AS total_debits,
       SUM(CASE WHEN entry_type = 'CREDIT' THEN amount_paise ELSE 0 END) AS total_credits 
FROM ledger_entries;
```
**Results:** `total_debits = 34500`, `total_credits = 34500`. Total balance is conserved across all accounts.

### 4.6 Clean Shutdown & Port Release
Executing `./scripts/stop-local-staging.ps1` signaled supervisor PID `37224`, which terminated its child process tree (PIDs `31148`, `22836`, `1648`, `472`, `40516`).
`Get-NetTCPConnection` confirmed that ports `8080`, `8081`, `8082`, and `3000` were immediately freed.
Disposable test database `transactx_m20r6_acceptance` was dropped cleanly.

---

## 5. Hosted GitHub Actions Continuous Integration Audit

For commit [`81a13ddc54e99db393fb927fae1aa15755ba507e`](https://github.com/whoevenisshubham/TransactX/commit/81a13ddc54e99db393fb927fae1aa15755ba507e):

| Workflow Name | Workflow ID | Cloud Environment | Conclusion |
|---|---|---|:---:|
| **Release validation** | `45317095347` | Ubuntu Latest (Go 1.24+, Node 22, Postgres 18) | **`success`** |
| **Demo host validation** | `45317095296` | Ubuntu Latest (Containerized multi-service) | **`success`** |
| **Auth and optional provider PostgreSQL acceptance** | `45317095294` | Ubuntu Latest (Postgres container migrations) | **`success`** |

---

## 6. Documented System Limitations & Boundaries

1. **Topology:** Local staging uses unencrypted HTTP loopback (`127.0.0.1:3000`). It does not simulate TLS certificate handshakes or WAN network partitions.
2. **Banking Rails:** Bank A and Bank B are simulated software adapters; they do not connect to production NPCI/UPI switches.
3. **Operator Scope:** The public demo operator persona (`viewer@transactx`) has role `OPS_VIEWER` and cannot trip circuit breakers or start chaos simulations. Administrative actions require provisioning an `OPS_ADMIN` account via `cmd/devseed`.
4. **Offline Storage:** IndexedDB stores structured payment intents locally; AES-GCM encryption is not implemented at rest.
5. **Integrity vs. Conservation:** Merkle roots verify state transition journal immutability; balance conservation must be audited independently via SQL ledger balance summation.

---

## 7. Acceptance Declaration

**VERDICT: RUNBOOK VERIFIED AND DEMONSTRATION READY**

The TransactX M20-R.6 staging runbook, system topology, startup instructions, and demonstration scenarios have been reconciled against the committed codebase and proven through live execution on a fresh, disposable database.
