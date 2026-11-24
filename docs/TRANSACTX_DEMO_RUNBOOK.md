# TransactX: End-to-End Demonstration Runbook & Operational Guide

**Project:** TransactX — A Resilient Payment Infrastructure for the Next Generation of Digital Payments  
**Document ID:** `DOC-DEMO-RUNBOOK-01`  
**Milestone:** M20-R.6 (Operational Runbook Reconciliation & Live Demonstration Acceptance)  
**Classification:** Operational Runbook & Evaluation Guide  
**Target Environment:** Local Single-Origin Staging Topology  
**Date:** November 2026  

---

## 1. System Requirements & Verified Runtimes

Ensure the following runtimes and tools are available before deploying TransactX:

| Runtime / Tool | Verified Reference Version | Minimum Engine Range | Verification Command | Operational Role |
|---|---|---|---|---|
| **Go** | `go 1.27.4` directive | `go 1.24+` with toolchain auto-selection (`GOTOOLCHAIN=auto`) | `go version` | Central API, background recovery worker, and bank simulators |
| **Node.js** | `22.x` (LTS, CI reference) | `>=20.0.0` (per `package.json`) | `node --version` | Frontend Vite application and Playwright test harness |
| **npm** | `10.9.x` | `>=10.0.0` | `npm --version` | Frontend package management |
| **PostgreSQL** | `18.6` (Local & CI) | `16.x+` | `psql --version` | Transactional ACID database and double-entry ledger |
| **Caddy** | `v2.11.9` | `2.8+` | `.\bin\caddy.exe version` | Single-origin HTTP reverse proxy (Loopback only) |
| **PowerShell** | `5.1` or `7.x` | Windows PowerShell | `$PSVersionTable.PSVersion` | Operational supervision and lifecycle execution scripts |

> [!NOTE]
> **Toolchain Management:** The `backend/go.mod` file declares directive `go 1.27.4`. Standard Go toolchains (version 1.21+) feature automatic toolchain selection (`GOTOOLCHAIN=auto`), enabling any standard Go 1.24+ installation (such as the verified local Go 1.27.1) to compile, test, and vet without manual intervention. Node.js 22 is the verified reference runtime used in hosted continuous integration (`.github/workflows/release-validation.yml`); the frontend package engine metadata declares `>=20.0.0` compatibility.

---

## 2. Reconciled Local Staging Topology

The authoritative local staging deployment topology runs as a single-origin HTTP loopback system:

```
+----------------------------------------------------------------------------------------------------+
|                                  TRANSACTX LOCAL STAGING TOPOLOGY                                  |
+----------------------------------------------------------------------------------------------------+
|                                                                                                    |
|    +------------------------------------------------------------------------------------------+    |
|    |                         CADDY LOCAL INGRESS (http://127.0.0.1:3000)                      |    |
|    |                      (Loopback HTTP Only - No TLS/HTTPS Termination)                     |    |
|    +----------------------------+-----------------------------------+-------------------------+    |
|                                 |                                   |                              |
|           / (Static Assets)     |                                   |  /api/*, /health*, /readyz   |
|                                 v                                   v                              |
|    +-------------------------------------------+     +----------------------------------------+    |
|    |            REACT 19 FRONTEND              |     |               GO REST API              |    |
|    |        (Served from frontend/dist)        |     |           (http://127.0.0.1:8080)      |    |
|    | - Customer Shell (Alice Demo)             |     +---+-------------------+------------+---+    |
|    | - Merchant Receive Station (Demo Store)   |         |                   |            |        |
|    | - Operations Console (OPS_VIEWER)         |         |                   |            |        |
|    | - IndexedDB Active Reservation Gate       |         |                   |            |        |
|    | - Offline Replay Queue Engine             |         |                   |            |        |
|    +-------------------------------------------+         |                   |            |        |
|                                                          |                   |            |        |
|              +-------------------------------------------+                   |            |        |
|              |                                                               |            |        |
|              v                                                               v            v        |
|    +-------------------+    +--------------------+                     +----------------------+    |
|    |   POSTGRESQL 18   |    |  RECOVERY WORKER   |                     |    BANK SIMULATORS   |    |
|    | (127.0.0.1:5432)  |<-->|  (Orphaned Sagas)  |                     | Bank A (:8081)       |    |
|    | - 33 Migrations   |    | - Lease Fencing    |                     | Bank B (:8082)       |    |
|    | - Double-Entry    |    | - Epoch Tracking   |                     | - Health: /healthz   |    |
|    | - Idempotency     |    +--------------------+                     +----------------------+    |
|    +-------------------+                                                                           |
+----------------------------------------------------------------------------------------------------+
```

### 2.1 Authoritative Address & Port Allocation

| Component | Bound Address | Protocol | Primary Responsibilities |
|---|---|---|---|
| **Caddy Local Ingress** | `http://127.0.0.1:3000` | HTTP (Loopback) | Single-origin entry point. Serves `frontend/dist` assets, proxies `/api/*`, `/health*`, and `/readyz` to `127.0.0.1:8080`. |
| **Central Go API** | `http://127.0.0.1:8080` | HTTP | Central payment state machine, JWT authentication, idempotency validation, and adaptive routing. |
| **Bank A Simulator** | `http://127.0.0.1:8081` | HTTP | Emulated banking participant (holds, credits, reversals). Health probe at `/healthz`. |
| **Bank B Simulator** | `http://127.0.0.1:8082` | HTTP | Alternative banking participant. Health probe at `/healthz`. |
| **Recovery Worker** | Background process | Internal / SQL | Background saga lease claimant and reconciler. |
| **PostgreSQL Database** | `127.0.0.1:5432` | TCP (Postgres wire) | ACID transaction storage, 33 migrations, unique idempotency deduplication, and immutable ledger. |

> [!WARNING]
> **No TLS in Local Staging:** The local Caddy configuration (`deploy/caddy/Caddyfile.local-staging`) is configured strictly for HTTP loopback at `http://127.0.0.1:3000`. Do not navigate to `https://` or expect TLS certificate termination in local staging.

---

## 3. First-Time Setup & Startup Runbook

Follow these 11 sequential steps to prepare, initialize, start, and verify the local staging environment from a clean repository clone.

### Step 1: Prerequisite Verification
Verify that required runtimes and tools are installed:
```powershell
go version
node --version
npm --version
psql --version
```

### Step 2: Environment Configuration File
Create your local environment configuration file from the template:
```powershell
Copy-Item deploy/env/local-staging.env.example deploy/env/local-staging.env
```

### Step 3: Credential Configuration
Edit `deploy/env/local-staging.env` with your local PostgreSQL password and staging parameters:
```ini
DATABASE_URL=postgres://postgres:your_password@127.0.0.1:5432/transactx_local_staging?sslmode=disable
BANK_A_DATABASE_URL=postgres://postgres:your_password@127.0.0.1:5432/transactx_local_staging?sslmode=disable
BANK_B_DATABASE_URL=postgres://postgres:your_password@127.0.0.1:5432/transactx_local_staging?sslmode=disable

PGHOST=127.0.0.1
PGPORT=5432
PGUSER=postgres
PGPASSWORD=your_password
TX_SOURCE_DB=transactx_local_staging
TX_TARGET_DB=transactx_restore_drill

APP_ADDR=127.0.0.1:8080
JWT_SECRET=supersecretjwtkeythatisatleast32byteslongforsecurity!
JWT_ISSUER=transactx-api
APP_DEVELOPMENT=true
TX_SIMULATION_MODE=true
TX_PUBLIC_DEMO_AUTH=true
STAGING_INGRESS_URL=http://127.0.0.1:3000
```

### Step 4: Backend Executable Preparation
Because the repository does not maintain a custom build helper script, compile the four Go executables directly into `bin\` and place the Caddy executable in `bin\`:
```powershell
# Create bin directory if not present
New-Item -ItemType Directory -Force -Path bin | Out-Null

# Build the four Go binaries
Set-Location backend
go build -o ../bin/api.exe ./cmd/api
go build -o ../bin/bank-a.exe ./cmd/bank-a
go build -o ../bin/bank-b.exe ./cmd/bank-b
go build -o ../bin/worker.exe ./cmd/worker
Set-Location ..

# Ensure caddy.exe is present in bin\ (copy from installed PATH or download)
if (-not (Test-Path "bin\caddy.exe")) {
    $caddyCmd = Get-Command caddy -ErrorAction SilentlyContinue
    if ($caddyCmd) {
        Copy-Item $caddyCmd.Source "bin\caddy.exe"
    } else {
        Write-Error "caddy.exe must be downloaded or copied into bin\caddy.exe"
    }
}
```

### Step 5: Frontend Production Build
Build the React 19 single-page application into `frontend/dist`:
```powershell
Set-Location frontend
npm ci
npm run build
Set-Location ..
```
Verify that `frontend/dist/index.html` exists.

### Step 6: Database Creation
Create the local staging database:
```powershell
& ./scripts/load-local-staging-env.ps1
psql -h $env:PGHOST -p $env:PGPORT -U $env:PGUSER -d postgres -c "CREATE DATABASE transactx_local_staging;"
```

### Step 7: Migration Execution
Apply the 33 canonical database schema migrations using `backend/cmd/migrate`:
```powershell
Set-Location backend
go run ./cmd/migrate -database-url "postgres://postgres:$env:PGPASSWORD@127.0.0.1:5432/transactx_local_staging?sslmode=disable" -dir ./migrations up
go run ./cmd/migrate -database-url "postgres://postgres:$env:PGPASSWORD@127.0.0.1:5432/transactx_local_staging?sslmode=disable" -dir ./migrations status
go run ./cmd/migrate -database-url "postgres://postgres:$env:PGPASSWORD@127.0.0.1:5432/transactx_local_staging?sslmode=disable" -dir ./migrations verify
Set-Location ..
```
> [!IMPORTANT]
> The migration tool supports `-database-url` and `-dir`. Flags such as `-database` or `-path` are unsupported.

### Step 8: Public Demo Initialization
Seed the fresh database with synthetic public demo personas using the required safety guards:
```powershell
Set-Location backend
$env:TX_DEMO_INITIALIZE = "I_ACKNOWLEDGE_NEW_EMPTY_DEMO_DATABASE"
$env:TX_DEMO_EXPECTED_DATABASE = "transactx_local_staging"
$env:TX_DEMO_PASSWORD = "DemoPassword2026!Strong"
$env:DATABASE_URL = "postgres://postgres:$env:PGPASSWORD@127.0.0.1:5432/transactx_local_staging?sslmode=disable"
go run ./cmd/demo-seed
Set-Location ..
```

### Step 9: Staging Supervisor Startup
Start the entire local staging stack via the supervisor. Use `-IncludeFrontend` to ensure the production build is verified:
```powershell
./scripts/start-local-staging.ps1 -IncludeFrontend
```
To run the supervisor attached in the current console for debugging, pass `-Foreground`.

### Step 10: Service Health & Readiness Verification
Poll both direct and proxied readiness endpoints:
```powershell
# 1. Direct Go API readiness
Invoke-RestMethod -Uri "http://127.0.0.1:8080/readyz"
# Expected response: {"data":{"readiness":true,"service":"transactx-api","status":"ok"},"requestId":"..."}

# 2. Caddy ingress proxied readiness
Invoke-RestMethod -Uri "http://127.0.0.1:3000/readyz"
# Expected response: identical JSON proxied through Caddy

# 3. Bank A Simulator health
Invoke-RestMethod -Uri "http://127.0.0.1:8081/healthz"
# Expected response: {"status":"ok"}

# 4. Bank B Simulator health
Invoke-RestMethod -Uri "http://127.0.0.1:8082/healthz"
# Expected response: {"status":"ok"}
```

### Step 11: Graceful Shutdown
To cleanly stop all managed processes and verify that staging ports are freed:
```powershell
./scripts/stop-local-staging.ps1
```

---

## 4. Public Demo Personas vs. Private Administrator Provisioning

### 4.1 Public Demo Personas (`cmd/demo-seed`)
The public demo environment provides pre-configured synthetic personas accessed through the `/demo` workspace selector:

| Persona | Name | Role | Bank | Payment Identifier | Starting Balance |
|---|---|---|---|---|---|
| **Customer** | Alice Demo | `CUSTOMER` | Bank A | `alice@transactx` | `1,000,000` paise (**₹10,000.00**) |
| **Recipient** | Bob Demo | `CUSTOMER` | Bank B | `bob@transactx` | `100,000` paise (**₹1,000.00**) |
| **Merchant** | Demo Store | `MERCHANT` | Bank B | `store@transactx` | `0` paise (**₹0.00**) |
| **Operator** | Demo Operator | `OPS_VIEWER` | Bank A | `viewer@transactx` | `0` paise (**₹0.00**) |

> [!IMPORTANT]
> **Authentication Mechanism:** In the public demo, users do **not** enter individual usernames and passwords. Selecting a persona card on `/demo` calls `POST /api/auth/demo` with `{"persona":"customer|merchant|operator"}`. The backend verifies the presence of the `demo_limits.budgets` table and issues a scoped JWT.

### 4.2 Private Administrator Provisioning (`cmd/devseed`)
The public demo operator persona (`viewer@transactx`) has role `OPS_VIEWER`. It can inspect live network topology, routing distribution, circuit states, and Merkle commitments, but **cannot** mutate circuit breakers or inject chaos.

If a demonstration requires privileged controls (e.g. tripping circuits or starting chaos scenarios):
1. `cmd/devseed` provisions an `OPS_ADMIN` user (`admin@transactx`).
2. **Collision Warning:** `cmd/demo-seed` already claims phone `8888888888` and `viewer@transactx`. To avoid unique constraint collisions on `users_phone_key` and `users_upi_id_key`, set non-colliding viewer environment variables before running `devseed`:
```powershell
Set-Location backend
$env:APP_DEVELOPMENT = "true"
$env:DEV_ADMIN_PASSWORD = "AdminPassword2026!Secure"
$env:DEV_VIEWER_PHONE = "8888888889"
$env:DEV_VIEWER_PAYMENT_ID = "devops-viewer@transactx"
go run ./cmd/devseed
Set-Location ..
```
3. Sign in as Administrator using the standard login interface (`/login`) with `admin@transactx` and your configured password.

---

## 5. Reconciled Demonstration Scenarios

### Scenario 1: Normal Customer-to-Merchant Payment & Ledger Update
1. Open `http://127.0.0.1:3000/demo` in a web browser.
2. Click **Customer (Alice Demo · Bank A)**.
3. In the navigation bar, click **Send Money**.
4. In the recipient input, enter `bob@transactx` (or `store@transactx`). Click **Find**. The green KYC verification checkmark displays.
5. Enter amount: `₹250.00` (`25000` paise). Enter note: `Coffee and pastry`.
6. Click **Review payment**. Confirm the review dialog displaying source account (`TX-...`), destination account, and zero fee.
7. Click **Confirm payment**.
8. **Observation:**
   - The payment resolves to `COMPLETED` (observed local duration: ~192ms).
   - Alice's balance decreases from `₹10,000.00` to `₹9,750.00`.
   - Bob's balance increases from `₹1,000.00` to `₹1,250.00`.
   - A digital receipt displays with the assigned UUID payment ID (e.g. `53fb77f9-...`).
   - The PostgreSQL ledger records exactly 1 debit entry of 25,000 paise on Alice's account and 1 credit entry of 25,000 paise on Bob's account.

---

### Scenario 2: Idempotent Duplicate Request Protection (Zero Double Debit)
1. Open a PowerShell terminal.
2. Obtain Alice's demo token:
```powershell
$authBody = @{ persona = 'customer' } | ConvertTo-Json -Compress
$authResp = Invoke-RestMethod -Uri 'http://127.0.0.1:3000/api/auth/demo' -Method Post -Body $authBody -ContentType 'application/json'
$token = $authResp.data.token
```
3. Dispatch an initial payment with a fresh `Idempotency-Key`:
```powershell
$headers = @{
    'Authorization' = "Bearer $token"
    'Idempotency-Key' = 'demo-runbook-key-1001'
    'Content-Type' = 'application/json'
}
$body = @{
    recipient = 'bob@transactx'
    amountPaise = 25000
    currency = 'INR'
    note = 'Pastry run'
} | ConvertTo-Json -Compress

$pay1 = Invoke-RestMethod -Uri 'http://127.0.0.1:3000/api/payments' -Method Post -Headers $headers -Body $body
Write-Host "Initial Payment ID: $($pay1.data.id), State: $($pay1.data.state)"
```
4. **Identical Replay:** Immediately re-send the exact same request:
```powershell
$pay2 = Invoke-RestMethod -Uri 'http://127.0.0.1:3000/api/payments' -Method Post -Headers $headers -Body $body
Write-Host "Replay Payment ID: $($pay2.data.id), State: $($pay2.data.state)"
Write-Host "Payment IDs Identical: $($pay1.data.id -eq $pay2.data.id)"
```
   **Observation:** Returns HTTP 200 with the exact same `paymentId`. Zero secondary debits occur.
5. **Conflicting Replay:** Send a request with the *same* `Idempotency-Key` but modified `amountPaise: 50000`:
```powershell
$conflictBody = @{
    recipient = 'bob@transactx'
    amountPaise = 50000
    currency = 'INR'
    note = 'Pastry run'
} | ConvertTo-Json -Compress

try {
    Invoke-RestMethod -Uri 'http://127.0.0.1:3000/api/payments' -Method Post -Headers $headers -Body $conflictBody
} catch {
    Write-Host "Caught expected conflict: $($_.Exception.Response.StatusCode)"
}
```
   **Observation:** Returns HTTP 409 Conflict (`IDEMPOTENCY_CONFLICT`). The database rejects the mutation fail-closed.

---

### Scenario 3: Bank Outage & Health-Aware Routing Failover
1. Navigate to `http://127.0.0.1:3000/demo` and select **Operator (Read-only viewer)**.
2. In the console, inspect **Routing & Circuits**. Observe that Bank A and Bank B circuits are in `CLOSED` state.
3. To demonstrate operational failover without administrative API tokens, simulate an outage by stopping the Bank A process:
```powershell
# Stop Bank A simulator process
$bankA_pid = (Get-Content logs/staging-pids.json | ConvertFrom-Json).'bank-a'
Stop-Process -Id $bankA_pid -Force
```
4. Return to the Customer view and dispatch a payment to `bob@transactx` for `₹100.00`.
5. **Observation:**
   - The Central Go API detects that Bank A's health probe (`http://127.0.0.1:8081/healthz`) fails to respond.
   - The circuit breaker for Bank A transitions toward `OPEN`.
   - The adaptive router excludes Bank A from the candidate set and steers execution to Bank B.
   - The payment completes successfully on Bank B.
6. Restart Bank A or let the supervisor restart it to observe circuit recovery.

---

### Scenario 4: Post-Dispatch Uncertain Payment Recovery across Page Reload
1. In Alice's Customer view (`/customer`), fill out the Send Money form for `₹500.00` to `bob@transactx`.
2. Open Developer Tools (F12) -> Network tab.
3. Click **Confirm payment**. In an automated Playwright fixture (`E2E-18`), network interruption is simulated immediately upon dispatch.
4. **Observation:**
   - The UI enters the **Uncertain State** screen: *"We couldn't confirm the response. Your transfer may still be processing on the ledger. We preserved your payment request ID and idempotency key."*
   - An active spend reservation lock is written to IndexedDB (`active-reservations`).
5. Refresh the browser tab (`F5`).
6. **Observation:**
   - The application rehydrates the reservation from IndexedDB.
   - The send form remains locked, preventing the customer from accidentally paying twice.
7. Click **Check status**.
8. **Observation:**
   - The client queries `GET /api/payments/idempotency/{key}` using the preserved `idempotencyKey`.
   - Once the terminal status (`COMPLETED`) is confirmed, the IndexedDB reservation lock in `active-reservations` is released, and the completion receipt is presented.

---

### Scenario 5: Offline Edge Intent Capture & Oldest-First Replay
1. In Developer Tools, toggle the network to **Offline**.
2. Select **Pay Offline (Edge Mode)**.
3. Enter amount `₹150.00` to `store@transactx`. Click **Queue Offline Intent**.
4. **Observation:**
   - An offline payment intent is stored in IndexedDB database `transactx-offline-queue` (schema version 2), object store `offline-intents`.
   - *Epistemic note:* IndexedDB storage is structured and owner-scoped; AES-GCM encryption is not implemented at rest.
   - The intent record contains a unique `clientRequestId`, generated `idempotencyKey`, normalized payload, `payloadHash`, initial state `QUEUED`, and `createdAt` timestamp. (No `seq` counter is used).
5. Toggle the network back to **Online**.
6. Click **Sync Offline Payments**.
7. **Observation:**
   - The client's `OfflineReplayWorker` processes eligible queued intents **oldest-first according to `createdAt`** (`left.createdAt.localeCompare(right.createdAt)`).
   - An exclusive lease is acquired per intent (`leaseOwner`, `leaseExpiresAt`, 30-second duration), transitioning the intent to `SYNCING` to prevent concurrent replay races.
   - Each intent is submitted to the API via standard atomic idempotency evaluation. Upon terminal server response, the intent state transitions to `SYNCED`.

---

### Scenario 6: Financial Integrity & Cryptographic Merkle Auditing
1. In the Operations Console (`/console`), navigate to **Reconciliation**.
2. **Ledger Conservation Check:**
   - Inspect the dual-ledger settlement summary.
   - Confirm that total debits match total credits ($\sum \text{Debits} = \sum \text{Credits}$).
   - Run the SQL verification in PostgreSQL:
   ```sql
   SELECT SUM(CASE WHEN entry_type = 'DEBIT' THEN amount_paise ELSE 0 END) AS total_debits,
          SUM(CASE WHEN entry_type = 'CREDIT' THEN amount_paise ELSE 0 END) AS total_credits
   FROM ledger_entries;
   ```
   *Expected Outcome:* `total_debits = total_credits`.
3. **Merkle Proof Verification Check:**
   - In the console, navigate to **Integrity / Merkle Tree**.
   - Select participant `BANK-A` and click **Fetch Merkle Root**.
   - Observe the computed SHA-256 root commitment over immutable payment state transitions.
   - Expand tree nodes to inspect cryptographic inclusion proofs for individual state transitions.
   - *Epistemic distinction:* A valid Merkle root proves that historical state transitions have not been tampered with or rewritten; it is an audit proof, distinct from double-entry debit/credit balance conservation.

---

## 6. Disaster Recovery & Clean Teardown

### 6.1 Database Backup
Execute the tested database backup script:
```powershell
./scripts/backup-db.ps1
```
Backups are archived into `backups\transactx_backup_<timestamp>.dump`.

### 6.2 Safe Restoration Procedure
To restore into an explicitly designated disposable recovery database:
```powershell
./scripts/restore-db.ps1 -TargetDatabase "transactx_restore_drill"
```
The script validates that the target database is not a protected source database before restoring.

### 6.3 Complete Stack Teardown
```powershell
./scripts/stop-local-staging.ps1
```
All ports (`8080`, `8081`, `8082`, `3000`) and background processes are released.
