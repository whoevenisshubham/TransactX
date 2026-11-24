# TransactX: Final System Architecture Specification

**Project:** TransactX — A Resilient Payment Infrastructure for the Next Generation of Digital Payments  
**Document ID:** `DOC-ARCH-FINAL-01`  
**Classification:** Definitive Technical Architecture & Specification  
**Milestone:** M20-R.6 (Operational Runbook Reconciliation & Live Demonstration Acceptance)  
**Date:** November 2026  

---

## 1. Project Objectives, Scope & Nature of the Research Prototype

### 1.1 Objectives
TransactX is a financial infrastructure research prototype designed to investigate and validate engineering mechanisms for high-resilience digital payment networks. Its core objectives are:
1. **Zero Double-Debit Invariant:** Guaranteeing that under arbitrary client drops, network timeouts, database interruptions, or server restarts, customer funds cannot be debited more than once for a given payment intent.
2. **Fail-Closed Offline Capability:** Enabling edge payment capture in network-disconnected environments with verifiable client-side cryptographic reservation and monotonic replay protocols.
3. **Distributed Saga Fault Tolerance:** Orchestrating multi-party bank settlement through a recoverable saga state machine with epoch-fenced background lease reclamation.
4. **Health-Aware Adaptive Routing:** Dynamically steering transaction volume across alternative banking nodes via real-time circuit breaking and health telemetry.
5. **Cryptographic Financial Verifiability:** Enforcing strict double-entry ledger balancing verified against immutable state transition logs via Merkle tree commitments.

### 1.2 Academic & Research Scope Notice
> [!IMPORTANT]
> **Prototype Classification:** TransactX is an experimental academic prototype and simulated digital payment infrastructure. It is **not** an implementation of live National Payments Corporation of India (NPCI) / Unified Payments Interface (UPI) production rails, nor does it interface with real-world central banks, automated clearing houses, or card networks. Banking entities ("Bank A", "Bank B") are simulated software adapters designed to inject controlled latency, network jitter, and transient failure modes.

---

## 2. System Topology & Deployment Architecture

TransactX employs a single-origin, multi-tier distributed architecture deployed locally or in containerized staging:

```
+----------------------------------------------------------------------------------------------------+
|                                    TRANSACTX LOCAL STAGING TOPOLOGY                                |
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
|    | - IndexedDB Active Spend Reservation      |         |                   |            |        |
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

### 2.1 Component Responsibilities Matrix

| Component | Bound Address | Technology | Primary Responsibilities |
|---|---|---|---|
| **Caddy Local Ingress** | `http://127.0.0.1:3000` | Caddy v2.11.9 (HTTP) | Single-origin host routing, reverse-proxying `/api/*`, `/health*`, and `/readyz` to `127.0.0.1:8080`, and serving compiled `frontend/dist` assets. Loopback HTTP only. |
| **Frontend Application** | Served via Caddy | React 19, TypeScript, Vite | Role-based product surfaces (Customer, Merchant, Operator), pre-dispatch IndexedDB reservation locking, offline queue storage, and QR code generation. |
| **Central Backend API** | `http://127.0.0.1:8080` | Go 1.27.1 (Go 1.24+ module) | JWT authentication, request validation, payment idempotency evaluation, routing decisions, ledger accounting, and outbox event publishing. |
| **Database & Ledger** | `127.0.0.1:5432` | PostgreSQL 18.6 | Relational persistence, transactional state machine, 33 migrations, unique idempotency deduplication, immutable transition history, and double-entry ledger bookkeeping. |
| **Recovery Worker** | Background process | Go background goroutine | Autonomous scanning of orphaned non-terminal sagas, distributed lease acquisition with epoch fencing, and crash-safe reconciliation. |
| **Bank Simulators** | `:8081` & `:8082` | Go HTTP microservices | Emulated banking rails exposing hold/settle endpoints, configurable latency, transient network drop simulation, and error injection. Health probe at `/healthz`. |

---

## 3. End-to-End Payment Processing Flow

```mermaid
sequenceDiagram
    autonumber
    actor Customer as Customer / Payer
    participant Browser as React Frontend (IndexedDB)
    participant Caddy as Caddy Ingress (http://127.0.0.1:3000)
    participant API as Go Central API (http://127.0.0.1:8080)
    participant DB as PostgreSQL 18
    participant Bank as Selected Bank Adapter (A/B)
    participant Worker as Background Recovery Worker

    Customer->>Browser: Enters Amount, Recipient & Clicks Pay
    Browser->>Browser: Compute SHA-256 Hash & Reserve in IndexedDB (active-reservations)
    Note over Browser: Spend Lock Held; Form Disabled
    Browser->>Caddy: POST /api/payments (Idempotency-Key, X-Request-ID)
    Caddy->>API: Reverse proxy request to Go backend (:8080)
    API->>DB: BEGIN TX: Insert into payments & idempotency_records
    alt Key Conflict (Different Payload)
        DB-->>API: Unique violation / Hash mismatch
        API-->>Browser: HTTP 409 IDEMPOTENCY_CONFLICT
        Browser-->>Customer: Preserves intent; prompts for safe review
    else Identical Replay
        DB-->>API: Match existing payment record
        API-->>Browser: HTTP 200 OK (Duplicate response)
    else New Valid Payment
        DB-->>API: Transaction Committed (State: CREATED)
        API->>DB: Transition CREATED -> VALIDATING -> ROUTING
        API->>Bank: HoldFundsRequest (Amount, Accounts, PaymentID)
        alt Bank Holds Successfully
            Bank-->>API: HoldResult (Success)
            API->>DB: Settle Local Ledger: Debit Sender, Credit Receiver (State: COMMITTED)
            API->>DB: Transition COMMITTED -> COMPLETED
            API-->>Browser: HTTP 201 Created (State: COMPLETED)
            Browser->>Browser: Delete from active-reservations; mark unresolved-intents RESOLVED_COMPLETED
            Browser-->>Customer: Display Payment Completed & Digital Receipt
        else Transient Bank Timeout / Interruption
            Bank-->>API: Timeout / 503 Service Unavailable
            API->>DB: Transition ROUTING -> PENDING_RECONCILIATION
            API-->>Browser: HTTP 202 Accepted (State: PENDING_RECONCILIATION)
            Note over Browser: Active reservation retained; UI enters uncertain view
            Worker->>DB: Claim lease on PENDING_RECONCILIATION job
            Worker->>Bank: GetOperationStatus(PaymentID)
            alt Bank Settled
                Worker->>DB: Settle central ledger -> State: COMPLETED
            else Bank Aborted
                Worker->>DB: Refund / Release local hold -> State: FAILED
            end
            Customer->>Browser: Clicks "Check status"
            Browser->>API: GET /api/payments/{id}
            API-->>Browser: HTTP 200 (State: COMPLETED or FAILED)
            Browser->>Browser: Releases active-reservations; marks intent terminal
        end
    end
```

---

## 4. Central Payment State Machine

The central payment lifecycle is governed by an immutable, forward-progressing state machine. Every state change is recorded in PostgreSQL table `payment_state_transitions` with an incremental sequence number and monotonic timestamp.

```
       +-------------------------------------------------------------+
       |                           CREATED                           |
       +------------------------------+------------------------------+
                                      |
                                      v
       +-------------------------------------------------------------+
       |                          VALIDATING                         |
       +------------------------------+------------------------------+
                                      |
                                      v
       +-------------------------------------------------------------+
       |                       LOCAL_SETTLEMENT                      |
       +------------------------------+------------------------------+
                                      |
                                      v
       +-------------------------------------------------------------+
       |                           ROUTING                           |
       +------------------------------+------------------------------+
                                      |
                                      v
       +-------------------------------------------------------------+
       |                          PROCESSING                         |
       +-------+----------------------+----------------------+-------+
               |                      |                      |
               v                      v                      v
    +--------------------+  +--------------------+  +--------------------+
    |     COMMITTED      |  | BANK_SETTLED_PEND  |  | PENDING_RECON      |
    +----------+---------+  +---------+----------+  +---------+----------+
               |                      |                       |
               |                      v                       |
               |            +--------------------+            |
               +----------->|     COMPLETED      |<-----------+
               |            +--------------------+            |
               |                      |                       |
               v                      v                       v
    +--------------------+  +--------------------+  +--------------------+
    |       FAILED       |  |      REVERSED      |  |       FAILED       |
    +--------------------+  +--------------------+  +--------------------+
```

### State Classification & Invariants

1. **Terminal States (`IsTerminal == true`):**
   - `COMPLETED`: Funds permanently settled across bank rail and central ledger.
   - `FAILED`: Transaction definitively halted prior to settlement; customer funds unlocked.
   - `REVERSED`: Funds debited and subsequently restored via compensating reversal.
2. **Intermediate States (`IsTerminal == false`):**
   - `CREATED`: Intent registered in central database.
   - `VALIDATING`: Cryptographic signature, KYC, and account status verified.
   - `LOCAL_SETTLEMENT`: Intrabank balances locked.
   - `ROUTING`: Bank adapter selected based on latency and circuit breaker status.
   - `PROCESSING`: Settlement instruction dispatched to bank rail.
   - `BANK_SETTLED_CENTRAL_PENDING`: Bank hold confirmed; central ledger update in flight.
   - `COMMITTED`: Double-entry ledger transaction inserted.
   - `PENDING_RECONCILIATION`: Saga interrupted; assigned to recovery worker.
3. **Offline Lifecycle States (Edge-only):**
   - `OFFLINE_CAPTURED`: Signed offline payment packet stored in client IndexedDB.
   - `QUEUED`: Awaiting online connectivity in browser queue.
   - `SYNCING`: Replay worker holds exclusive lease and is submitting intent to API.
   - `REPLAY_FAILED`: Replay rejected by server validation (requires manual intervention).

---

## 5. Client Active Reservation & Browser Storage Durability

To ensure financial safety at the edge, the frontend implements a strict **Fail-Closed Storage Gate** via IndexedDB (`transactx-unresolved-intents`, database version 2):

```
+----------------------------------------------------------------------------------------------------+
|                                    BROWSER INDEXEDDB ARCHITECTURE                                  |
+----------------------------------------------------------------------------------------------------+
|                                                                                                    |
|    +-----------------------------------------------+   +--------------------------------------+    |
|    |         STORE: "active-reservations"          |   |       STORE: "unresolved-intents"    |    |
|    |      (Exclusive Lock on Customer Balance)     |   |         (Permanent Audit Log)        |    |
|    +-----------------------------------------------+   +--------------------------------------+    |
|    | - KeyPath: clientRequestId                    |   | - KeyPath: clientRequestId           |    |
|    | - Fields: idempotencyKey, amountPaise,        |   | - Indices: by-owner, by-state,       |    |
|    |   recipient, state, payloadHash, createdAt    |   |            by-key                    |    |
|    |                                               |   |                                      |    |
|    | LIFECYCLE:                                    |   | LIFECYCLE:                           |    |
|    | 1. Created ATOMICALLY BEFORE network dispatch |   | 1. Appended on intent creation       |    |
|    | 2. HELD during in-flight states, 404s, 500s,   |   | 2. Updated on status transitions     |    |
|    |    network drops, and 401/403 interruptions   |   | 3. Never deleted; transitions to     |    |
|    | 3. REMOVED ONLY upon verified terminal state  |   |    RESOLVED_COMPLETED / FAILED       |    |
|    +-----------------------------------------------+   +--------------------------------------+    |
|                                                                                                    |
+----------------------------------------------------------------------------------------------------+
```

### Invariant Rules
- If IndexedDB is blocked or disabled by private browser settings, payment dispatch is **blocked fail-closed** (`StorageUnavailableError`).
- A browser page reload while an intent is in-flight rehydrates the reservation from IndexedDB, displays the in-flight recovery view, and keeps the send form locked.
- If a server responds with HTTP 401 (token expired) or 403 (forbidden role), the client marks the intent as `UNCERTAIN` and **preserves the reservation lock**, preventing duplicate submissions until re-authentication.
- *Epistemic note:* IndexedDB records are owner-scoped, structured browser objects; AES-GCM encryption is not implemented in this repository.

---

## 6. Server Idempotency & Request Identity Model

TransactX enforces idempotency across all mutating operations using three distinct identifiers:

1. **`clientRequestId` (`X-Request-ID`)**: UUID generated by the client to track UI submission attempts and correlate browser logs.
2. **`idempotencyKey` (`Idempotency-Key`)**: Unique string generated per discrete financial intent (max 255 chars).
3. **`requestHash`**: SHA-256 cryptographic digest calculated over the normalized payment payload (`amountPaise`, `currency`, `recipient`, `note`).

### PostgreSQL Schema & Deduplication Invariant
```sql
CREATE TABLE idempotency_records (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id),
    key VARCHAR(255) NOT NULL,
    request_hash VARCHAR(128) NOT NULL,
    payment_id UUID REFERENCES payments(id),
    response_snapshot JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ,
    CONSTRAINT idempotency_records_user_id_key_key UNIQUE (user_id, key)
);
```
- **Atomicity:** `payments` and `idempotency_records` rows are inserted in the same database transaction (`CreateIdempotent`).
- **Conflict Handling:** If a subsequent request arrives with the same `(user_id, key)` but a differing `request_hash`, the backend rejects it with `ErrIdempotencyConflict` (HTTP 409).
- **Safe Replay:** If an identical request arrives, the backend queries the existing `payment_id`, loads the current payment state, and returns HTTP 200 without creating secondary ledger debits or duplicate payments.

---

## 7. Distributed Saga Tracking & Background Recovery Worker

Distributed transactions spanning external bank adapters and internal ledgers are coordinated via a compensatory saga pattern:

```
[Start Payment]
       |
       v
[Reserve Local Hold] ---> (Fail) ---> [Halt & Unlock]
       |
       v
[Dispatch Bank Hold] ---> (Timeout) -> [Enter PENDING_RECONCILIATION]
       |                                          |
    (Success)                                     v
       |                              [Recovery Worker Claim]
       v                              (Leader Lease & Epoch Fencing)
[Commit Local Ledger]                             |
       |                                          v
       v                              [Query Bank Operation Status]
[Transition COMPLETED]                 /                         \
                                 (Settled)                     (Failed)
                                     |                             |
                                     v                             v
                         [Commit Central Ledger]        [Compensate & Release Hold]
```

### Worker Lease Protocol
1. **Lease Acquisition:** Autonomous worker instances poll `recovery_jobs` where `status = 'PENDING'` and `(lease_expires_at IS NULL OR lease_expires_at < NOW())`.
2. **Epoch Fencing:** Workers execute an atomic `UPDATE recovery_jobs SET worker_id = $1, lease_epoch = lease_epoch + 1, lease_expires_at = NOW() + INTERVAL '30 seconds'`. Any stale worker operating on an older epoch is rejected.
3. **Heartbeat:** Active workers periodically renew their lease while communicating with external bank APIs.

---

## 8. Health-Aware Adaptive Routing & Circuit Breakers

The Go backend monitors bank adapter health and error rates in real-time to avoid cascading failures:

```
     +-------------------------------------------------------------+
     |                       CLOSED (Normal)                       |
     |         Requests routed proportionally to Bank A & B        |
     +------------------------------+------------------------------+
                                    | Failures > Threshold (50%)
                                    v
     +-------------------------------------------------------------+
     |                        OPEN (Tripped)                       |
     |          Traffic diverted entirely to healthy rail          |
     +------------------------------+------------------------------+
                                    | Cool-down period expires (15s)
                                    v
     +-------------------------------------------------------------+
     |                      HALF-OPEN (Canary)                     |
     |         Probe canary requests sent to test recovery         |
     +------------------------------+------------------------------+
               |                                           |
               | Success                                   | Failure
               v                                           v
     +--------------------+                     +--------------------+
     |       CLOSED       |                     |        OPEN        |
     +--------------------+                     +--------------------+
```

---

## 9. Double-Entry Ledger, Financial Reconciliation & Merkle Integrity

### 9.1 Double-Entry Conservation Law
Every financial movement requires equal and balancing debits and credits:
$$\sum \text{Debits} = \sum \text{Credits}$$
- Payer account: `DEBIT` amount $A$.
- Payee account: `CREDIT` amount $A$.
- All ledger entries are linked to an atomic `ledger_transactions` record.
- In PostgreSQL, balance conservation is verified via:
  ```sql
  SELECT SUM(CASE WHEN entry_type = 'DEBIT' THEN amount_paise ELSE 0 END) AS total_debits,
         SUM(CASE WHEN entry_type = 'CREDIT' THEN amount_paise ELSE 0 END) AS total_credits
  FROM ledger_entries;
  ```

### 9.2 Merkle Tree Cryptographic Commitments
To prevent retroactive database tampering by database administrators:
1. Every state transition in `payment_state_transitions` produces a leaf hash:
   $$H_{\text{leaf}} = \text{SHA256}(\text{id} \parallel \text{payment\_id} \parallel \text{from\_state} \parallel \text{to\_state} \parallel \text{transitioned\_at})$$
2. Periodic commitments compute the cryptographic root over all uncommitted leaves.
3. Commitments are stored in `merkle_commitments` and exposed via `/api/ops/merkle` for independent mathematical verification.
4. *Epistemic distinction:* A valid Merkle root proves transition log immutability and tamper-evidence. It does not by itself prove that account balances are conserved.
