# TransactX: End-to-End Demonstration Runbook & Operational Guide

**Project:** TransactX — A Resilient Payment Infrastructure for the Next Generation of Digital Payments  
**Document ID:** `DOC-DEMO-RUNBOOK-01`  
**Milestone:** M20-R.6 (Final Project Freeze)  
**Classification:** Operational Runbook & Evaluation Guide  
**Target Environment:** Local Single-Origin Staging Topology  

---

## 1. System Requirements & Prerequisites

Ensure the following runtimes and tools are installed before running TransactX:

| Runtime / Tool | Minimum Version | Verification Command | Notes |
|---|---|---|---|
| **Go** | `1.24+` | `go version` | Backend API, worker, and simulators |
| **Node.js** | `20.0.0+` (LTS) | `node --version` | Frontend SPA and Playwright harness |
| **npm** | `10.0.0+` | `npm --version` | Package manager |
| **PostgreSQL** | `16.x` | `psql --version` | Core transactional database |
| **Caddy** | `2.8+` | `caddy version` | Reverse proxy for single-origin staging |
| **PowerShell** | `7.x` or `5.1` | `$PSVersionTable.PSVersion` | Staging supervision scripts |

---

## 2. Environment Configuration & Secret Management

TransactX separates configuration from credentials using environment variables. All non-sensitive defaults are documented in `deploy/env/local-staging.env.example`.

### 2.1 Configuration File Setup
Create your local staging environment file `deploy/env/local-staging.env` by copying the template:
```powershell
Copy-Item deploy/env/local-staging.env.example deploy/env/local-staging.env
```

### 2.2 Standard Development Variables (Non-Production)
```ini
# Core Service Ports
TRANSACTX_PORT=8080
API_PORT=8083
BANK_A_PORT=8081
BANK_B_PORT=8082

# Database Connection (Disposable local staging database)
DATABASE_URL=postgres://postgres:postgres@localhost:5432/transactx_staging?sslmode=disable

# Authentication Secrets (Development strings ONLY - never use in production)
JWT_SECRET=dev-staging-jwt-secret-key-32-chars-long!!
JWT_ISSUER=transactx-local-staging
JWT_EXPIRATION_HOURS=24

# Bank Simulator Endpoints
BANK_A_URL=http://localhost:8081
BANK_B_URL=http://localhost:8082

# Feature Switches
GEMINI_ENABLED=false
RATE_LIMITING_ENABLED=false
```

> [!CAUTION]
> **Secret Hygiene:** Never commit `deploy/env/local-staging.env` or actual production credentials to version control. The repository's `.gitignore` explicitly excludes `.env` and `.env.local`.

---

## 3. Database Initialization, Migrations & Seeding

### 3.1 Initialize Database & Apply Migrations
Execute the automated migration tool against the staging database:
```powershell
cd backend
go run ./cmd/migrate -database "postgres://postgres:postgres@localhost:5432/transactx_staging?sslmode=disable" up
```
*Expected Output:* 33 migrations applied successfully (from `000001_phase1a_payment_core` up to `000033_gemini_disable_switch`).

### 3.2 Seed Staging Data
Seed the standard demonstration accounts and bank accounts:
```powershell
go run ./cmd/devseed
```

---

## 4. Demo Login Accounts & Expected Balances

The staging seed creates pre-configured personas across the three distinct product surfaces:

| Role | Username / Identifier | Demo Password | Starting Balance | Initial Purpose |
|---|---|---|---|---|
| **Customer (Payer)** | `alice@transactx` | `Alice123!` | ₹100,000.00 (10,000,000 paise) | Customer Shell: Send money, scan QR, test offline mode |
| **Customer (Peer)** | `charlie@transactx` | `Charlie123!` | ₹50,000.00 (5,000,000 paise) | Peer transfer recipient & cross-user isolation testing |
| **Merchant (Payee)** | `bob@transactx` | `Bob123!` | ₹5,000.00 (500,000 paise) | Merchant Shell: Receive station, static QR, settlement feed |
| **Operator (Admin)** | `ops@transactx` | `Admin123!` | N/A | Operations Console: Routing, circuit breakers, Merkle tree |

---

## 5. Starting the Local Staging Environment

### 5.1 Automated Supervision Script (Recommended)
Run the automated supervisor script from the project root:
```powershell
./scripts/start-local-staging.ps1
```
This script validates prerequisites, starts the background services (Bank A, Bank B, Go API, Worker, Caddy), and monitors process health.

### 5.2 Manual Multi-Process Startup
Alternatively, start services in separate terminal tabs:
```powershell
# Tab 1: Bank A Simulator
cd backend; go run ./cmd/bank-a -port 8081

# Tab 2: Bank B Simulator
cd backend; go run ./cmd/bank-b -port 8082

# Tab 3: Central API
cd backend; go run ./cmd/api -port 8083

# Tab 4: Background Recovery Worker
cd backend; go run ./cmd/worker

# Tab 5: Caddy Reverse Proxy
caddy run --config deploy/caddy/Caddyfile.local-staging
```

### 5.3 Verify Staging Health
Open a browser or run curl to verify single-origin readiness:
```powershell
Invoke-RestMethod -Uri "http://localhost:8080/health"
```
*Expected Output:* `{"status":"HEALTHY","service":"transactx-api"}`

---

## 6. Demonstration Scenarios & Step-by-Step Script

### Scenario 1: Normal Customer-to-Merchant Payment & Instant Receipt
1. Open `http://localhost:8080` in a browser.
2. Select **Customer Login** and enter `alice@transactx` / `Alice123!`.
3. In the navigation bar, click **Send Money**.
4. Enter recipient: `bob@transactx`. Click **Find**. The system verifies Bob's identity and displays the green verification badge.
5. Enter amount: `₹250.00`. Enter note: `Coffee and pastry`.
6. Click **Review payment**. Verify the two-phase summary dialog.
7. Click **Confirm payment**.
8. *Observation:* Payment resolves to `COMPLETED` in < 250ms. An instant digital receipt with cryptographic transaction ID and timestamp is generated. Alice's balance updates to `₹99,750.00`.

### Scenario 2: Idempotent Duplicate Request Protection (Zero Double Debit)
1. Using the browser developer tools (F12 Network tab), capture the `Idempotency-Key` header from Scenario 1.
2. Using PowerShell, execute an immediate replay with the exact same key:
```powershell
$headers = @{
    "Authorization" = "Bearer <Alice-Token>"
    "Idempotency-Key" = "<Captured-Key>"
    "Content-Type" = "application/json"
}
$body = '{"recipient":"bob@transactx","amountPaise":25000,"currency":"INR","note":"Coffee and pastry"}'
Invoke-RestMethod -Uri "http://localhost:8080/api/payments" -Method Post -Headers $headers -Body $body
```
3. *Observation:* Returns HTTP 200 with the identical `paymentId`. Alice's account is **not** debited a second time.
4. Now change `"amountPaise": 50000` while keeping the same `Idempotency-Key`.
5. *Observation:* Returns HTTP 409 `IDEMPOTENCY_CONFLICT`. The request is rejected fail-closed.

### Scenario 3: Bank Outage Simulation & Adaptive Routing
1. Sign in as `ops@transactx` / `Admin123!` to the **Operations Console** (`/console`).
2. Navigate to **Routing & Circuits**. Observe that both Bank A and Bank B are healthy (`CLOSED` circuit state).
3. Under Bank A, click **Simulate Outage** (or trip circuit breaker).
4. Return to Alice's Customer tab and send `₹100.00` to `bob@transactx`.
5. *Observation:* The backend router detects Bank A's failure, marks its circuit `OPEN`, and automatically switches the active route to Bank B. The payment succeeds with zero end-user interruption.
6. The Operations console logs an audit event: `CIRCUIT_TRIPPED: BANK-A -> TRAFFIC DIVERTED TO BANK-B`.

### Scenario 4: Unresolved Payment Recovery across Page Reload
1. In the Network tab of Alice's browser, throttle network to "Offline" or stop the Go API right as payment is confirmed.
2. Click **Confirm payment** for `₹500.00`.
3. *Observation:* The payment enters the **Uncertain State** view: *"We couldn't confirm the response. Your transfer may still be processing on the ledger. We preserved your payment request ID and idempotency key."*
4. Refresh the page (`F5`).
5. *Observation:* The browser rehydrates from IndexedDB. The active reservation lock is intact; the send form is disabled to prevent duplicate attempts.
6. Restore network / restart the API and click **Check status**.
7. *Observation:* The API reconciles the payment, returns `COMPLETED`, releases the active reservation lock, and presents the completion receipt.

### Scenario 5: Offline Edge Payment Capture & Monotonic Replay
1. Toggle browser to Offline mode in Developer Tools.
2. In Customer Shell, enter `bob@transactx`, amount `₹50.00`, and click **Pay**.
3. *Observation:* The UI detects offline mode and displays: *"Payment queued locally in encrypted browser storage."*
4. The payment is assigned a cryptographic offline nonce and held in `offline-queue`.
5. Restore network connectivity.
6. Click **Sync Queue** (or observe automatic background sync).
7. *Observation:* The replay engine claims the intent with a local lease, submits it to `/api/payments`, receives confirmation, and transitions the intent to `RESOLVED_COMPLETED`.

### Scenario 6: Cryptographic Merkle Tree Integrity Verification
1. In the Operations Console, navigate to **Financial Integrity**.
2. Click **Generate Merkle Commitment**.
3. *Observation:* The system hashes all newly settled transitions and produces a 256-bit cryptographic root.
4. Click **Verify Ledger Consistency**.
5. *Observation:* The engine verifies that the sum of debits equals the sum of credits across all accounts ($\sum D - \sum C = 0$) and matches the root commitment.

---

## 7. Shutdown Procedure & Database Cleanup

### 7.1 Graceful Staging Shutdown
Run the stop script to terminate all staging background processes cleanly:
```powershell
./scripts/stop-local-staging.ps1
```

### 7.2 Database Reset (Disposable Staging Only)
To restore the staging database to a clean baseline:
```powershell
./scripts/restore-db.ps1 -BackupFile deploy/backups/staging-clean-baseline.dump
```
*(Or drop and re-run migrations via `cmd/migrate up` and `cmd/devseed`)*.
