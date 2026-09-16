# TransactX: A Resilient Payment Infrastructure for the Next Generation of Digital Payments

## A Fault-Tolerant Payment Network with Offline-First Resilience, Adaptive Routing, and Merkle-Accelerated Reconciliation

> \*\*Project type:\*\* Final-year CSE project / research prototype / hackathon-ready fintech application  
> \*\*Team size:\*\* 3 students  
> \*\*Primary goal:\*\* Build a genuinely working, production-inspired digital payment network that can be demonstrated as a real application while experimentally evaluating resilience, reconciliation, offline operation, routing, and financial correctness.  
> \*\*Important scope note:\*\* TransactX is a \*\*simulated digital-payment research prototype\*\*, not an implementation of any proprietary production payment network or a system intended to process real money.

\---

# 1\. Executive Summary

TransactX is a distributed digital payment platform that models the difficult engineering problems encountered by large-scale payment infrastructure rather than merely reproducing a consumer payment UI.

The system contains a consumer-facing payment application, merchant capabilities, a central payment switch, two simulated bank participants in the MVP, exposed through an extensible bank-adapter interface, independent bank ledgers, a reconciliation engine, adaptive routing, offline-first transaction capture, fault injection, self-healing behavior, and continuous financial-consistency verification.

The project has four primary technical contributions:

1. **Merkle-tree accelerated reconciliation** — bank ledgers are represented using hierarchical Merkle commitments so that reconciliation starts with constant-time root comparison and recursively localizes divergent regions instead of performing a full ledger comparison every time.
2. **Offline-first transaction processing** — payment requests can be captured locally during connectivity loss and safely replayed after reconnection using persistent local state and idempotent transaction handling.
3. **Adaptive and failure-aware payment routing** — the payment switch continuously evaluates participating bank health and dynamically adjusts routing instead of blindly using static routing.
4. **Continuous financial invariant checking** — the system validates critical financial correctness properties after transaction operations and during recovery/reconciliation.

A fifth cross-cutting capability, **chaos engineering**, deliberately introduces controlled outages, latency, message loss, and other failures so that resilience claims can be measured rather than merely described.

The resulting project should look and behave like a real fintech application to a normal user, while exposing a second operational layer that allows evaluators to observe payment routing, bank health, reconciliation, offline synchronization, failure injection, and financial integrity in real time.

\---

# 2\. Product Vision

## 2.1 What the user experiences

A normal customer should experience TransactX as a polished payment product:

* Login / registration
* Personal payment identity
* Account balance
* Send money
* Receive money
* QR-based merchant payment
* Transaction history
* Transaction details
* Offline mode
* Synchronization status
* Basic notifications

The user should not need to understand the distributed architecture.

## 2.2 What the evaluator experiences

An evaluator can switch into the **TransactX Network Console** and observe:

* Live payment throughput
* Bank health
* Bank latency
* Routing decisions
* Transaction-state transitions
* Reconciliation runs
* Merkle-tree divergence localization
* Offline queue synchronization
* Failure injection
* Circuit-breaker behavior
* Traffic rerouting
* Financial invariant status
* System recovery

This separation is critical: the project is simultaneously a **working application** and a **systems research prototype**.

\---

# 3\. Core Research Thesis

TransactX investigates whether a payment network can remain useful and financially consistent under unreliable connectivity and partial infrastructure failure while reducing unnecessary reconciliation work between independent participants.

The core experimental thesis is:

> A payment-switch architecture combining idempotent transactional processing, offline-first capture, health-aware routing, controlled self-healing, and hierarchical Merkle reconciliation can provide better continuity and reconciliation efficiency than a baseline architecture using connectivity-dependent processing, static routing, and full ledger comparison, without violating core financial invariants.

\---

# 4\. Research Questions

### RQ1 — Reconciliation

Can hierarchical Merkle-tree commitments reduce the practical cost of locating ledger divergence compared with naive full-ledger comparison?

### RQ2 — Offline resilience

Can local transaction persistence plus safe idempotent replay maintain payment continuity during temporary connectivity loss without duplicate processing?

### RQ3 — Failure resilience

Can adaptive health-aware routing maintain a higher payment success rate and lower recovery time than static routing under injected participant failures?

### RQ4 — Financial correctness

Can continuously checked financial invariants detect corruption, concurrency defects, duplicate processing, and invalid state transitions during normal operation and chaos testing?

\---

# 5\. Explicit Scope Boundaries

## 5.1 MVP / Core Deliverables

The first implementation target is a **two-bank payment network**. The architecture must use bank adapters/interfaces so a third or additional bank can be added without redesigning the payment switch.

Implement fully:

* Consumer payment flow
* Merchant payment flow
* Two simulated bank participants (Bank A and Bank B)
* Extensible bank-adapter interface for N banks
* Payment switch
* Authentication and role-based authorization
* Idempotency
* Atomic payment workflow
* Double-entry ledger
* Explicit transaction state machine
* Concurrency-safe balance updates
* Incrementally maintained, bucketed Merkle-tree reconciliation
* Offline local queue and safe idempotent replay
* Health-aware routing
* Circuit breaker
* Controlled fault injection
* Runtime financial invariant checking
* Customer + merchant frontend
* Network / research console
* Reconciliation visualization
* Benchmark harness

## 5.2 Stretch Goals

Only after the MVP is stable:

* Third simulated bank
* Redis-backed rate limiting
* Prometheus/Grafana dashboards
* Richer audit/event history
* More advanced offline queue controls
* Additional chaos scenarios
* Playwright end-to-end coverage
* Higher benchmark scale if hardware permits

## 5.3 Explicitly Out of Scope for This Version

Do not implement as part of the first research prototype:

* Real banking APIs
* Real payment-rail connectivity
* Real-money transfers
* Production-grade KYC/AML
* Cryptographically delegated offline money / digital-cash credentials
* Formal model checking as a required deliverable
* Large-scale multi-region deployment
* Machine-learning fraud detection
* Agentic AI payments
* Blockchain-based settlement

## 5.4 Future Research Extensions

These may be discussed in the paper without being claimed as implemented:

* Cryptographically delegated offline spending authority
* Formal verification with TLA+ or Alloy
* Distributed multi-region deployment
* Advanced settlement optimization
* Privacy-preserving payment proofs
* Federated fraud detection

# 6\. Functional Requirements

## 6.1 User Management

* Register user
* Login
* Logout
* View profile
* View UPI-like payment identifier
* Associate a simulated bank account
* Associate one or more devices

## 6.2 Payments

* Initiate P2P payment
* Initiate merchant payment
* Validate recipient
* Validate amount
* Authenticate request
* Generate unique payment transaction ID
* Accept idempotency key
* Execute debit/credit flow
* Return transaction status
* Persist complete transaction history

## 6.3 Offline Mode

* Detect network unavailability in the client
* Capture payment intent locally
* Persist the request in IndexedDB/local durable storage
* Mark request as `QUEUED\_OFFLINE`
* Allow queue inspection
* Automatically synchronize when connectivity returns
* Reuse original transaction identity/idempotency key
* Prevent duplicate processing during replay
* Show synchronization outcome

## 6.4 Bank Simulation

Each bank must expose:

* Account lookup
* Balance
* Debit operation
* Credit operation
* Hold operation if used by implementation
* Release/confirm operation if used by implementation
* Health endpoint
* Configurable latency
* Configurable failure mode
* Transaction history
* Ledger access for reconciliation

## 6.5 Reconciliation

* Generate bank ledger commitments
* Compare Merkle roots
* Detect equal roots without traversal
* Recursively locate divergent branches
* Identify affected bucket(s)
* Identify affected transaction(s)
* Compare with naive baseline
* Record reconciliation duration
* Record number of hashes inspected

## 6.6 Routing

* Observe bank health
* Maintain routing score
* Select healthy participant
* De-prioritize degraded participant
* Open circuit breaker after threshold breach
* Restore traffic gradually after recovery
* Record routing decision and reason

## 6.7 Chaos Engineering

* Kill/disable simulated bank
* Add artificial latency
* Drop messages / induce transient failures
* Simulate temporary network partition
* Corrupt selected test ledger record
* Trigger concurrent transaction storm
* Observe recovery behavior

## 6.8 Financial Integrity

The system must continuously verify at least:

* Debit/credit conservation
* No negative balances unless explicitly allowed by a configured overdraft policy
* Unique transaction identity
* Unique idempotency key mapping
* Valid transaction state transitions
* Every completed monetary transfer has matching ledger entries
* Reconciliation commitments match underlying ledger data

\---

# 7\. Non-Functional Requirements

## Performance

* Normal API response should be low-latency in local deployment.
* Payment processing should avoid unnecessary synchronous network hops.
* Reconciliation must have measurable benchmark results.
* Dashboard updates should be near real-time.

## Reliability

* Payment retries must not duplicate transactions.
* Temporary bank failures should not corrupt the ledger.
* Offline replay must preserve original request identity.
* Recovery must leave the system in a consistent state.

## Security

* Password hashes, not plaintext passwords
* JWT-based authentication or equivalent secure sessions
* Role-based access for customer, merchant, and operations roles
* Input validation
* Amount validation
* Authorization checks
* Audit logging
* Rate limiting (stretch)
* Secrets stored via environment configuration

## Observability

* Structured logs
* Request/transaction IDs
* Metrics
* Bank health history
* Routing decisions
* Reconciliation results
* Error counters
* Recovery events

## Reproducibility

\---

# 8\. High-Level Architecture

```text
                                    ┌─────────────────────────┐
                                    │     React Frontend      │
                                    │ Customer / Merchant /   │
                                    │ Network Console         │
                                    └────────────┬────────────┘
                                                 │
                                      HTTPS / WebSocket
                                                 │
                                    ┌────────────▼────────────┐
                                    │      API Gateway        │
                                    │ Auth / RBAC / Rate      │
                                    │ Limit / Idempotency    │
                                    └────────────┬────────────┘
                                                 │
                                    ┌────────────▼────────────┐
                                    │     Payment Switch      │
                                    │                         │
                                    │ Orchestrator             │
                                    │ State Machine            │
                                    │ Adaptive Router          │
                                    │ Recovery Coordinator     │
                                    └──────┬──────────┬────────┘
                                           │          │
                       ┌───────────────────┘          └───────────────────┐
                       │                                                 │
              ┌────────▼────────┐                               ┌────────▼─────────┐
              │ Bank Adapter(s) │                               │ Offline Sync     │
              └────────┬────────┘                               │ Coordinator      │
                       │                                        └────────┬─────────┘
          ┌────────────┼────────────┐                                   │
          ▼            ▼            │                                   │
     ┌─────────┐  ┌─────────┐       │                                   │
     │  Bank A │  │  Bank B │       │                                   │
     │ Core    │  │ Core    │       │                                   │
     │ Ledger  │  │ Ledger  │       │                                   │
     └────┬────┘  └────┬────┘       │                                   │
          │            │            │                                   │
          └────────────┼────────────┘                                   │
                       │                                                │
                ┌──────▼───────┐                                        │
                │ PostgreSQL   │◄───────────────────────────────────────┘
                │ Authoritative│
                │ Transactional│
                │ State/Ledger │
                └──────┬───────┘
                       │
         ┌─────────────┼──────────────────────────┐
         │             │                          │
         ▼             ▼                          ▼
┌────────────────┐ ┌───────────────┐    ┌──────────────────┐
│ Reconciliation │ │ Integrity     │    │ Metrics / Events │
│ Engine         │ │ Engine        │    │ / Notifications  │
└───────┬────────┘ └───────────────┘    └──────────────────┘
        │
        ▼
┌────────────────────┐
│ Hierarchical       │
│ Merkle Commitments │
└────────────────────┘

        ┌──────────────────────────────┐
        │ Chaos / Failure Controller   │
        │ latency / outage / loss /    │
        │ corruption / concurrency     │
        └──────────────┬───────────────┘
                       │
                       ▼
              routing + recovery
```

\---

# 9\. Deployment Architecture

```text
Browser
  │
  ▼
frontend
  │
  ▼
api / payment-switch
  │
  ├── PostgreSQL
  ├── Redis (optional but recommended for idempotency/rate limiting)
  ├── Bank A service
  ├── Bank B service
  ├── Bank C (stretch, optional) service
  ├── Reconciliation worker
  ├── Metrics service / endpoint
  └── Chaos controller
```

Messaging can initially be implemented through an application event abstraction or Redis Streams. Do not let messaging infrastructure become the project itself. The payment correctness path must remain understandable.

\---

# 9.5. Bank Adapter and N-Bank Extension Contract

The MVP contains exactly two active simulated banks: **Bank A and Bank B**. The payment switch must never contain bank-specific business logic. It depends on an adapter interface so additional participants can be introduced without changing payment orchestration.

Conceptual interface:

```go
type BankAdapter interface {
    GetHealth(ctx context.Context) HealthStatus
    ResolveAccount(ctx context.Context, accountID string) (Account, error)
    HoldFunds(ctx context.Context, req HoldRequest) (HoldResult, error)
    ConfirmHold(ctx context.Context, holdID string) error
    ReleaseHold(ctx context.Context, holdID string) error
    Debit(ctx context.Context, req DebitRequest) error
    Credit(ctx context.Context, req CreditRequest) error
    GetLedgerSnapshot(ctx context.Context, scope LedgerScope) (LedgerSnapshot, error)
}
```

The concrete implementation can expose these methods over HTTP. The switch should depend only on the interface.

Adding Bank C later must be a configuration/deployment change, not a redesign of the payment engine.

# 10\. Recommended Technology Stack

## Backend

**Go** is the preferred option for this version because it keeps the payment switch compact, concurrent, easy to containerize, and suitable for explicit systems programming.

Alternative: Node.js/TypeScript if the team is substantially faster in it.

Recommended backend libraries/concepts:

* HTTP framework: Gin / Echo / Fiber / standard library
* PostgreSQL driver: pgx
* DB migrations: Goose / Atlas / equivalent
* JWT library
* WebSocket library
* Structured logging
* Prometheus client

## Frontend

* React
* TypeScript
* Vite
* Tailwind CSS
* React Router
* Zustand
* Recharts
* D3.js or react-d3-tree for Merkle visualization
* WebSocket client
* IndexedDB via Dexie or a small abstraction layer

## Data

* PostgreSQL — primary authoritative transactional database
* Redis — rate limiting, short-lived state, optional idempotency acceleration

## Infrastructure

* Git / GitHub

## Testing

* Go unit/integration tests
* Playwright for end-to-end browser tests if time permits
* k6 or Go-based load generator

\---

# 11\. Repository Structure

Recommended monorepo:

```text
transactx/
│
├── README.md
├── LICENSE
├── .env.example
├── Makefile
│
├── docs/
│   ├── architecture.md
│   ├── api.md
│   ├── database.md
│   ├── reconciliation.md
│   ├── offline.md
│   ├── routing.md
│   ├── chaos.md
│   ├── invariants.md
│   ├── benchmarking.md
│   └── diagrams/
│       ├── system-architecture.png
│       ├── payment-sequence.png
│       ├── reconciliation-sequence.png
│       └── offline-sequence.png
│
├── backend/
│   ├── cmd/
│   │   ├── api/
│   │   │   └── main.go
│   │   ├── bank/
│   │   │   └── main.go
│   │   ├── reconciler/
│   │   │   └── main.go
│   │   └── chaos/
│   │       └── main.go
│   │
│   ├── internal/
│   │   ├── auth/
│   │   ├── users/
│   │   ├── accounts/
│   │   ├── payments/
│   │   ├── ledger/
│   │   ├── idempotency/
│   │   ├── bank/
│   │   ├── routing/
│   │   ├── reconciliation/
│   │   ├── offline/
│   │   ├── integrity/
│   │   ├── chaos/
│   │   ├── notifications/
│   │   ├── metrics/
│   │   └── common/
│   │
│   ├── migrations/
│   └── tests/
│       ├── integration/
│       ├── concurrency/
│       └── reconciliation/
│
├── frontend/
│   ├── src/
│   │   ├── app/
│   │   ├── components/
│   │   ├── pages/
│   │   │   ├── auth/
│   │   │   ├── customer/
│   │   │   ├── merchant/
│   │   │   └── network/
│   │   ├── features/
│   │   │   ├── payments/
│   │   │   ├── offline/
│   │   │   ├── banks/
│   │   │   ├── reconciliation/
│   │   │   ├── chaos/
│   │   │   └── integrity/
│   │   ├── stores/
│   │   ├── services/
│   │   ├── hooks/
│   │   ├── lib/
│   │   └── types/
│   └── public/
│
├── simulator/
│   ├── seed/
│   ├── load-generator/
│   └── scenarios/
│
└── scripts/
    ├── seed.sh
    ├── benchmark.sh
    └── demo.sh
```

\---

# 12\. Backend Module Responsibilities

## `auth`

Responsibilities:

* registration
* login
* password hashing
* JWT creation/validation
* authorization middleware

Functions:

```go
Register()
Login()
ValidateToken()
RequireRole()
HashPassword()
ComparePassword()
```

## `users`

```go
CreateUser()
GetUserByID()
GetUserByUPI()
UpdateUser()
ListUserDevices()
```

## `accounts`

```go
CreateAccount()
GetBalance()
ReserveFunds()
ReleaseFunds()
Debit()
Credit()
GetAccountState()
```

## `payments`

This is the central business module.

```go
CreatePayment()
GetPayment()
ProcessPayment()
RetryPayment()
CancelPayment()
RefundPayment()
TransitionState()
ValidatePaymentRequest()
```

## `ledger`

```go
CreateLedgerTransaction()
AppendDebit()
AppendCredit()
GetEntriesForTransaction()
GetEntriesForAccount()
ComputeLedgerBalance()
VerifyLedgerBalance()
```

## `idempotency`

```go
GetExistingRequest()
StoreRequestResult()
ValidateKeyOwnership()
CreateIdempotencyRecord()
```

## `bank`

```go
GetBankHealth()
DebitAccount()
CreditAccount()
HoldFunds()
ConfirmHold()
ReleaseHold()
GetBankLedger()
SetFailureMode()
```

## `routing`

```go
CalculateBankScore()
SelectBank()
RecordSuccess()
RecordFailure()
OpenCircuit()
CloseCircuit()
HalfOpenCircuit()
GetRoutingState()
```

## `reconciliation`

```go
BuildMerkleTree()
GetMerkleRoot()
CompareRoots()
LocateDivergence()
GenerateReconciliationReport()
RunNaiveDiff()
RunMerkleDiff()
```

## `offline`

```go
AcceptOfflineRequest()
ValidateOfflineRequest()
ReplayQueuedTransaction()
MarkQueued()
MarkSynced()
MarkReplayFailed()
```

## `integrity`

```go
CheckDebitCreditConservation()
CheckNoNegativeBalance()
CheckTransactionUniqueness()
CheckIdempotencyInvariant()
CheckStateTransition()
CheckLedgerCommitment()
RunAllChecks()
```

## `chaos`

```go
KillBank()
RestoreBank()
InjectLatency()
ClearLatency()
DropMessages()
CorruptTestLedgerRecord()
StartConcurrentStorm()
GetActiveFaults()
```

\---

# 13\. Core Database Model

## 13.1 `users`

```text
id UUID PRIMARY KEY
name VARCHAR
phone VARCHAR UNIQUE
upi\_id VARCHAR UNIQUE
password\_hash VARCHAR
role VARCHAR
created\_at TIMESTAMP
updated\_at TIMESTAMP
```

## 13.2 `banks`

```text
id UUID PRIMARY KEY
code VARCHAR UNIQUE
name VARCHAR
status VARCHAR
base\_latency\_ms INT
created\_at TIMESTAMP
```

## 13.3 `accounts`

```text
id UUID PRIMARY KEY
user\_id UUID REFERENCES users(id)
bank\_id UUID REFERENCES banks(id)
account\_number VARCHAR UNIQUE
balance NUMERIC(18,2)
version BIGINT
status VARCHAR
created\_at TIMESTAMP
updated\_at TIMESTAMP
```

Use an integer representation such as paise internally where practical to avoid floating-point monetary arithmetic.

## 13.4 `payments`

```text
id UUID PRIMARY KEY
idempotency\_key VARCHAR UNIQUE
sender\_account\_id UUID
receiver\_account\_id UUID
merchant\_id UUID NULL
amount BIGINT
currency VARCHAR
state VARCHAR
route\_bank\_id UUID NULL
failure\_reason TEXT NULL
created\_at TIMESTAMP
updated\_at TIMESTAMP
completed\_at TIMESTAMP NULL
```

## 13.5 `ledger\_transactions`

```text
id UUID PRIMARY KEY
payment\_id UUID UNIQUE
created\_at TIMESTAMP
```

## 13.6 `ledger\_entries`

```text
id UUID PRIMARY KEY
ledger\_transaction\_id UUID
account\_id UUID
entry\_type VARCHAR
amount BIGINT
created\_at TIMESTAMP
```

The core invariant is that a transfer's entries preserve conservation:

```text
sum(credits) - sum(debits) = 0
```

The implementation should choose a consistent sign convention and never mix conventions.

## 13.7 `idempotency\_records`

```text
id UUID PRIMARY KEY
key VARCHAR UNIQUE
user\_id UUID
request\_hash VARCHAR
payment\_id UUID
response\_snapshot JSONB
created\_at TIMESTAMP
expires\_at TIMESTAMP NULL
```

## 13.8 `bank\_health\_samples`

```text
id BIGSERIAL PRIMARY KEY
bank\_id UUID
latency\_ms INT
success BOOLEAN
failure\_rate NUMERIC
health\_score NUMERIC
sampled\_at TIMESTAMP
```

## 13.9 `reconciliation\_runs`

```text
id UUID PRIMARY KEY
bank\_a\_id UUID
bank\_b\_id UUID
root\_a VARCHAR
root\_b VARCHAR
matched BOOLEAN
algorithm VARCHAR
elapsed\_ms BIGINT
nodes\_visited BIGINT
divergent\_records INT
created\_at TIMESTAMP
```

## 13.10 `offline\_queue\_records`

```text
id UUID PRIMARY KEY
client\_request\_id UUID UNIQUE
idempotency\_key VARCHAR UNIQUE
payload JSONB
status VARCHAR
queued\_at TIMESTAMP
synced\_at TIMESTAMP NULL
last\_error TEXT NULL
retry\_count INT
```

## 13.11 `audit\_events`

```text
id BIGSERIAL PRIMARY KEY
event\_type VARCHAR
entity\_type VARCHAR
entity\_id UUID
actor\_id UUID NULL
metadata JSONB
created\_at TIMESTAMP
```

## 13.12 `chaos\_events`

```text
id UUID PRIMARY KEY
target\_type VARCHAR
target\_id UUID NULL
fault\_type VARCHAR
parameters JSONB
started\_at TIMESTAMP
ended\_at TIMESTAMP NULL
```

\---

# 14\. Transaction State Machine

Recommended states:

```text
CREATED
   │
   ▼
VALIDATING
   │
   ▼
ROUTING
   │
   ▼
PROCESSING
   │
   ├──────────────► FAILED
   │
   ▼
COMMITTED
   │
   ▼
COMPLETED
```

For partial/deferred states:

```text
PROCESSING
    │
    ▼
PENDING\_RECONCILIATION
    │
    ├────► COMPLETED
    └────► REVERSED
```

Offline:

```text
OFFLINE\_CAPTURED
       │
       ▼
QUEUED
       │
       ▼
SYNCING
       │
       ├────► COMPLETED
       └────► REPLAY\_FAILED
```

Every state transition must be explicit.

Implement a transition table instead of allowing arbitrary strings:

```go
CanTransition(from, to PaymentState) bool
```

\---

# 15\. Payment Processing Flow

```text
1. Client creates payment request.
2. Client attaches idempotency key.
3. API authenticates user.
4. API validates amount and recipient.
5. Idempotency layer checks whether key already exists.
6. Payment record is created in CREATED/VALIDATING state.
7. Routing layer selects bank participant.
8. Payment orchestrator starts database transaction.
9. Bank-side account operation is performed according to implementation model.
10. Double-entry ledger records debit and credit.
11. Transaction reaches COMMITTED / COMPLETED.
12. Event/notification is emitted.
13. Invariant engine validates post-transaction correctness.
14. Client receives final result or intermediate state.
```

For a student implementation, keep the actual monetary commit path transactionally understandable. Avoid spreading a single logical balance mutation across too many independently committed databases before the correctness model is proven.

\---

# 16\. Idempotency Design

Every payment mutation request requires an idempotency key.

### Rules

* Same authenticated user + same idempotency key + same request payload → return original result.
* Same idempotency key + materially different payload → reject with an idempotency conflict.
* Never create a second payment record for an already-consumed key.

### Request flow

```text
POST /payments
Idempotency-Key: abc
        │
        ▼
lookup key
   │          │
found       absent
   │          │
   ▼          ▼
return     create record
original      │
result        ▼
           process payment
```

Store the request hash so an accidental key reuse cannot change the meaning of an existing operation.

\---

# 17\. Double-Entry Ledger

The ledger is more important than maintaining only a mutable balance field.

Each payment produces corresponding ledger entries.

Conceptually:

```text
Payment P
  ├── Ledger Transaction
  │      ├── Sender: DEBIT amount
  │      └── Receiver: CREDIT amount
```

The account balance may be maintained as a materialized current state, but the ledger remains the authoritative explanation of monetary movement.

This enables:

* auditability
* balance reconstruction
* reconciliation
* invariant validation
* recovery analysis

\---

# 18\. Concurrency Control

A critical requirement is avoiding two concurrent payment requests spending the same available balance.

Recommended database strategy:

* Use PostgreSQL transaction boundaries.
* Lock the sender account row when necessary.
* Re-check balance inside the transaction.
* Perform the debit and ledger write atomically.
* Update version or use row locking consistently.

The implementation must be tested with a concurrent load generator.

### Required test

Create a controlled balance and run many concurrent payment requests that collectively attempt to exceed it.

Measure:

* successful payments
* rejected payments
* negative balances
* duplicate ledger entries
* invariant violations

Expected result:

```text
negative balances = 0
conservation violations = 0
unexpected duplicate payments = 0
```

\---

# 19\. Offline-First Architecture

The initial offline implementation is intentionally practical rather than attempting to reinvent digital cash.

## Client-side behavior

```text
User initiates payment
        │
        ▼
Network available?
   │            │
  yes           no
   │            │
   ▼            ▼
API request   IndexedDB queue
                  │
                  ▼
             QUEUED\_OFFLINE
                  │
            network restored
                  │
                  ▼
            replay request
                  │
                  ▼
          normal payment path
```

The offline request preserves:

* client request ID
* idempotency key
* request payload
* creation timestamp
* retry metadata

This prevents a replay from being interpreted as a new payment.

## UI state

Display:

```text
ONLINE
OFFLINE — Payment queued
SYNCING — 2 requests
SYNCED
SYNC FAILED — Retry required
```

\---

# 20\. Offline Safety Model

The offline-first MVP does **not** authorize arbitrary new money while disconnected.

It captures and persists a payment request so it can be safely submitted later.

Therefore:

* No fake confirmation should be shown as final settlement while offline.
* The UI should clearly label the state as queued/pending.
* The user should be able to inspect the queue.
* Final settlement only occurs after the central payment infrastructure accepts the replay.

This is a critical product decision and should be preserved in the demo.

\---

# 21\. Merkle Tree Design

## 21.1 Goal

Efficiently detect and localize differences between two ledger snapshots without rebuilding the entire Merkle structure for every reconciliation run.

## 21.2 Incremental Maintenance Decision

**The implementation must not rebuild the complete Merkle tree from scratch for every reconciliation.** The tree is maintained incrementally as ledger records are appended.

Use deterministic reconciliation buckets (for example, date/hour windows). Each bucket owns a Merkle structure over its ordered ledger records. When a new record arrives, only the affected bucket's path is updated. A higher-level commitment aggregates bucket roots.

This design matters for the paper because the benchmark must distinguish:

1. cost of maintaining commitments as transactions arrive; and
2. cost of performing reconciliation once commitments already exist.

Both should be measured separately where practical.

## 21.3 Leaf construction

Canonicalize transaction data before hashing. The canonical representation must be deterministic across participating banks.

Conceptual input fields:

```text
transaction\_id | account\_id | direction | amount | timestamp | status
```

Then:

```text
leaf\_hash = SHA256(canonical\_transaction\_bytes)
```

## 21.4 Internal nodes

```text
parent = SHA256(left\_hash || right\_hash)
```

Document the ordering rule, duplicate-leaf handling, and odd-node handling so both banks compute identical roots.

## 21.5 Hierarchical partitioning

Partition the ledger into predictable buckets such as:

```text
Global
 ├── Day 1
 │    ├── Hour 1
 │    ├── Hour 2
 │    └── ...
 ├── Day 2
 └── ...
```

The exact bucket duration is configurable for experiments.

# 22\. Merkle Reconciliation Algorithm

```text
1. Both banks expose the root commitment for the same reconciliation scope.
2. Compare roots.
3. If equal:
      mark scope reconciled.
4. If different:
      compare child hashes.
5. Recursively descend only into mismatching branches.
6. Continue until leaf/bucket level.
7. Retrieve exact divergent transactions.
8. Produce reconciliation report.
```

### Important complexity wording

Do **not** claim simply that the entire reconciliation algorithm is always O(log n).

A more accurate formulation is:

> Root comparison is O(1) when commitments are already available; when divergence exists, the tree can localize a divergent region in logarithmic traversal per divergent branch, with total work depending on the number and distribution of divergent regions.

This wording is safer for a research report and an interview.

\---

# 23\. Naive Baseline

The project must implement a baseline for comparison.

### Naive approach

* Fetch all records in the reconciliation scope.
* Sort/canonicalize if required.
* Compare transaction records one by one.

Measure:

* elapsed time
* records scanned
* bytes transferred
* mismatches detected

### Merkle approach

Measure:

* root comparison time
* tree nodes inspected
* records fetched
* bytes transferred
* mismatches detected

This is essential for a publishable experimental section.

\---

# 24\. Reconciliation Output

A reconciliation run should produce:

```json
{
  "runId": "...",
  "bankA": "BANK\_A",
  "bankB": "BANK\_B",
  "algorithm": "MERKLE",
  "matched": false,
  "rootA": "...",
  "rootB": "...",
  "divergentBuckets": 1,
  "divergentTransactions": 2,
  "nodesVisited": 37,
  "elapsedMs": 18
}
```

The frontend should visualize the same result.

\---

# 25\. Adaptive Routing

Each bank receives a health score based on measurable signals.

Suggested signals:

```text
availability
recent success rate
recent failure rate
P95/P99 latency
open circuit state
recent timeout count
configured capacity
```

A simple deterministic score is preferable to an ML-based router.

Conceptually:

```text
score =
  availability\_weight
+ success\_rate\_weight
+ latency\_weight
+ capacity\_weight
- timeout\_penalty
- circuit\_penalty
```

Normalize every component to a consistent range.

The chosen routing policy must be documented and deterministic.

\---

# 26\. Circuit Breaker

Recommended states:

```text
CLOSED
   │ repeated failures
   ▼
OPEN
   │ cooldown elapsed
   ▼
HALF\_OPEN
   │ successful probe
   └────────► CLOSED

HALF\_OPEN -- failed probe --> OPEN
```

When a bank is OPEN:

* stop routing new traffic to it
* continue health probes
* expose state to the dashboard

On recovery:

* do not immediately dump all traffic back
* slowly reintroduce it

This makes the self-healing demonstration more realistic.

\---

# 27\. Chaos Engineering

The chaos controller should expose deterministic scenarios.

## Scenario A — Bank outage

Disable one bank endpoint.

Expected behavior:

```text
health ↓
  ↓
circuit opens
  ↓
router avoids bank
  ↓
traffic shifts to healthy bank
```

## Scenario B — High latency

Inject configurable latency.

Observe:

* latency increase
* routing-score decrease
* possible circuit opening

## Scenario C — Message loss

Drop selected inter-service calls/events.

Observe:

* retries if implemented
* pending state
* eventual recovery
* no duplicate completion

## Scenario D — Ledger corruption

Modify a controlled test record **only in the development/test simulation environment**. This operation must require the `OPS\_ADMIN` role and must never be exposed through customer or merchant interfaces.

Expected:

* Merkle root mismatch
* divergence localization
* integrity alert

## Scenario E — Concurrency storm

Run a large number of concurrent payment requests.

Expected:

* no double spend
* no negative balance
* no duplicate transaction completion

\---

# 28\. Self-Healing Flow

```text
Bank B latency rises
       │
       ▼
Health Monitor
       │
       ▼
Health Score falls
       │
       ▼
Circuit Breaker
       │
       ▼
Routing Engine recalculates
       │
       ▼
New payments → healthy banks
       │
       ▼
Bank B health recovers
       │
       ▼
HALF\_OPEN probe
       │
       ▼
Gradual traffic restoration
```

The exact thresholds should be configurable so experiments can repeat the same conditions.

\---

# 29\. Financial Invariant Engine

This is a **runtime invariant-checking layer**, not formal verification.

## Invariant 1 — Debit/credit conservation

For every completed transfer:

```text
sum(debits) == sum(credits)
```

## Invariant 2 — No invalid negative balance

```text
balance >= minimum\_allowed\_balance
```

## Invariant 3 — Idempotency

```text
idempotency\_key → at most one logical payment
```

## Invariant 4 — Transaction uniqueness

```text
payment\_id → exactly one payment record
```

## Invariant 5 — State transition validity

Only transitions defined in the state machine are allowed.

## Invariant 6 — Ledger integrity

All expected entries for a completed payment must exist.

## Invariant 7 — Reconciliation commitment

If a ledger snapshot claims a given Merkle root, recomputing that snapshot must yield the same root.

\---

# 30\. Integrity Dashboard

Display:

```text
FINANCIAL INTEGRITY

Debit = Credit             ✓
No Negative Balance        ✓
Unique Payments            ✓
Idempotency                ✓
State Machine              ✓
Ledger Commitment          ✓

Violations: 0
Last verification: 12 ms ago
```

When chaos injects corruption, the dashboard should visibly switch into an alert state and show which invariant failed.

\---

# 31\. Frontend Architecture

Use a single React application with role-aware navigation.

```text
TransactX
│
├── Customer
│   ├── Home
│   ├── Pay
│   ├── Scan \& Pay
│   ├── Transactions
│   ├── Transaction Details
│   └── Offline Queue
│
├── Merchant
│   ├── Dashboard
│   ├── Receive Payment
│   ├── QR
│   ├── Transactions
│   └── Settlement View
│
└── Network Console
    ├── Overview
    ├── Bank Health
    ├── Routing
    ├── Reconciliation
    ├── Integrity
    └── Chaos Lab
```

\---

# 32\. Customer Frontend

## Home

Elements:

* greeting
* UPI ID
* balance
* send money CTA
* scan/pay CTA
* recent transactions
* online/offline status
* pending sync count

## Pay

Fields:

* recipient UPI ID
* amount
* note
* payment button

After submit, show real-time transaction state.

## Transaction Details

Display:

* transaction ID
* amount
* sender
* receiver
* time
* status
* routing bank
* processing duration
* offline/online origin

## Offline Queue

Display:

* queued payment
* queue timestamp
* retry count
* synchronization state
* last sync

\---

# 33\. Merchant Frontend

Merchant dashboard:

```text
Today's transactions
Successful
Pending
Failed
Total received
```

Additional screens:

* Generate QR
* Display payment QR
* Payment feed
* Settlement status
* Transaction search

This makes the system feel like a payment ecosystem rather than a personal wallet only.

\---

# 34\. Network Console

This is the primary hackathon/research visualization surface.

## Overview

```text
TPS
Success Rate
P95/P99 Latency
Pending Payments
Reconciliation Status
Invariant Status
```

## Bank Health

Each bank card:

```text
BANK A
Status: HEALTHY
Latency: 48 ms
Success: 99.8%
Health Score: 94
Current Traffic: 41%
```

## Routing

Show a traffic distribution chart over time.

## Reconciliation

Show:

* current roots
* matched/mismatched status
* naive vs Merkle time
* tree visualization
* divergent branches
* exact affected transactions

## Integrity

Show invariant status and historical violations.

## Chaos Lab

Buttons/controls:

```text
Kill Bank A
Restore Bank A
Add Latency
Remove Latency
Drop Messages
Corrupt Test Record
Run Concurrency Storm
Reset Scenario
```

Every control should state clearly that it operates on the simulation/test environment.

\---

# 35\. Real-Time Communication

Use WebSockets for:

* payment-state changes
* bank health changes
* routing changes
* chaos events
* reconciliation progress
* integrity status

Do not use aggressive client-side polling for everything.

The frontend should subscribe to an event stream such as:

```text
payment.updated
bank.health.updated
routing.updated
reconciliation.started
reconciliation.updated
reconciliation.completed
integrity.updated
chaos.started
chaos.completed
```

\---

# 36\. Frontend State Management

Suggested Zustand stores:

```text
useAuthStore
usePaymentStore
useOfflineStore
useBankHealthStore
useRoutingStore
useReconciliationStore
useChaosStore
useIntegrityStore
```

Keep network/server state separate from local UI state as much as practical.

\---

# 37\. Offline Client Implementation

Use IndexedDB.

Recommended data object:

```text
OfflinePaymentIntent
--------------------
id
idempotencyKey
recipient
amount
payloadHash
createdAt
status
retryCount
lastError
```

Service functions:

```ts
queuePayment()
listQueuedPayments()
removeQueuedPayment()
markSyncing()
markSynced()
markFailed()
replayAll()
```

Listen for connectivity changes:

```text
window.online
window.offline
```

The replay worker should use bounded exponential backoff.

\---

# 38\. API Design

## Authentication

```http
POST /api/auth/register
POST /api/auth/login
POST /api/auth/logout
GET  /api/auth/me
```

## Users

```http
GET /api/users/me
GET /api/users/resolve/{upiId}
```

## Accounts

```http
GET /api/accounts/me
GET /api/accounts/{id}/balance
```

## Payments

```http
POST /api/payments
GET  /api/payments/{id}
GET  /api/payments
POST /api/payments/{id}/retry
POST /api/payments/{id}/refund
```

Required header:

```http
Idempotency-Key: <uuid>
```

## Offline

```http
POST /api/offline/sync
GET  /api/offline/status
```

## Network

```http
GET /api/network/overview
GET /api/network/banks
GET /api/network/routing
```

## Reconciliation

```http
POST /api/reconciliation/run
GET  /api/reconciliation/{id}
GET  /api/reconciliation/{id}/tree
```

## Integrity

```http
GET /api/integrity/status
POST /api/integrity/check
```

## Chaos

```http
GET  /api/chaos/scenarios
POST /api/chaos/scenarios/{scenario}/start
POST /api/chaos/scenarios/{scenario}/stop
POST /api/chaos/reset
```

Chaos endpoints must require an operations/admin role.

\---

# 39\. API Response Conventions

Use a predictable envelope where beneficial:

```json
{
  "success": true,
  "data": {},
  "error": null,
  "requestId": "..."
}
```

For failures:

```json
{
  "success": false,
  "data": null,
  "error": {
    "code": "IDEMPOTENCY\_CONFLICT",
    "message": "Idempotency key was already used with a different request"
  },
  "requestId": "..."
}
```

\---

# 40\. Authentication and Authorization

Roles:

```text
CUSTOMER
MERCHANT
OPS\_ADMIN
```

Rules:

* Customers access their own payments/accounts.
* Merchants access their own incoming transactions.
* Operations users access network diagnostics and chaos controls.

Do not rely solely on frontend route hiding. Authorization must be enforced server-side.

\---

# 41\. Security Requirements

Minimum baseline:

* Argon2id or bcrypt password hashing
* JWT with expiration or secure session mechanism
* TLS in production deployment if externally hosted
* Input validation
* SQL parameterization/ORM safety
* CORS configuration
* Request size limits
* Audit events for privileged actions
* Rate limiting (stretch; implement after core correctness and resilience are stable)
* Environment-based secrets
* No secret values committed to Git

For the report, distinguish clearly between **prototype security** and **production-grade financial security**.

\---

# 42\. Rate Limiting (Stretch)

When implemented, rate-limit at least:

* login attempts
* payment creation
* recipient-resolution lookups
* chaos-control endpoints
* network diagnostic endpoints

A token-bucket or sliding-window approach using Redis is sufficient.

Do not over-engineer rate limiting before payment correctness is complete.

\---

# 43\. Observability

Every request should carry a correlation/request ID.

Every payment should have:

```text
request\_id
payment\_id
idempotency\_key
```

Structured logs should include:

```text
timestamp
service
level
request\_id
payment\_id
bank\_id
event
latency\_ms
status
```

Core structured logging is required. Prometheus/Grafana is optional/stretch and must not delay research-critical work.

Metrics:

```text
payments\_total
payments\_success\_total
payments\_failed\_total
payment\_latency\_ms
bank\_health\_score
bank\_failures\_total
routing\_changes\_total
reconciliation\_runs\_total
reconciliation\_duration\_ms
reconciliation\_nodes\_visited
invariant\_violations\_total
offline\_queue\_depth
offline\_replay\_success\_total
chaos\_events\_total
```

\---

# 44\. Benchmarking Framework

The paper needs repeatable experiments, not anecdotal demos.

## 44.1 Reconciliation benchmark

Dataset sizes:

* 10,000 transactions (guaranteed)
* 100,000 transactions (guaranteed)
* 1,000,000 transactions (optional, only if the benchmark environment is stable)

Compare:

* naive full diff
* Merkle reconciliation

Metrics:

* elapsed time
* CPU
* memory
* nodes visited
* records inspected
* bytes transferred

Run each experiment multiple times and report median/percentiles.

## 44.2 Routing benchmark

Baseline:

* static routing

Proposed:

* adaptive routing

Failure conditions:

* bank outage
* elevated latency
* intermittent failure

Metrics:

* payment success rate
* P95/P99 latency
* recovery time
* dropped requests
* traffic distribution

## 44.3 Offline benchmark

Compare:

* normal online operation
* connectivity loss + queueing
* reconnect + replay

Metrics:

* queued count
* replay success
* duplicate processing count
* average synchronization delay

## 44.4 Financial correctness benchmark

Generate:

* concurrent requests
* duplicate retries
* malformed transitions
* injected ledger corruption

Metrics:

* invariant violations
* false alarms if any
* recovered transactions
* inconsistent balances

\---

# 45\. Load Testing

Use k6 or a Go-based load generator.

Workloads:

### Workload A — Normal

Mixed payment traffic across banks.

### Workload B — Burst

Sudden spike in payment requests.

### Workload C — Hot account

Many simultaneous attempts against the same sender account.

### Workload D — Failure

Load continues while one bank is degraded.

### Workload E — Duplicate retry

Many repeated requests using the same idempotency key.

The important outcome is correctness under load, not an arbitrary TPS number.

\---

# 46\. Test Strategy

## Unit tests

Test:

* state transition rules
* routing scoring
* circuit breaker
* Merkle hashing
* Merkle traversal
* invariant functions
* idempotency checks
* amount calculations

## Integration tests

Test:

* payment + PostgreSQL
* payment + simulated bank
* offline replay + API
* reconciliation + bank ledger
* chaos + routing

## End-to-end tests

Test complete user journey:

```text
login → pay → transaction success → history
```

## Adversarial tests

Test:

* same payment twice
* same idempotency key with changed amount
* insufficient balance
* bank unavailable
* timeout after debit
* invalid state transition
* corrupted ledger record
* reconnect after multiple offline requests

\---

# 47\. Failure Matrix

|Failure|Expected System Behavior|Main Mechanism|
|-|-|-|
|Duplicate request|Return original payment result|Idempotency|
|Insufficient balance|Reject atomically|Transaction + ledger|
|Bank unavailable|Avoid unhealthy route|Adaptive routing|
|Persistent bank failure|Open circuit|Circuit breaker|
|Bank recovery|Gradual traffic restoration|Half-open recovery|
|Temporary connectivity loss|Queue request locally|Offline queue|
|Reconnect|Replay safely|Idempotency|
|Ledger corruption|Detect mismatch|Merkle reconciliation|
|Concurrent spending|Preserve balance correctness|DB locking/transactions|
|Invalid state transition|Reject operation|State machine|
|Message loss|Preserve safe state / retry|Recovery mechanism|

\---

# 48\. Demo Script

The final demonstration should tell one continuous story.

## Step 1 — Normal payment

Use the customer UI to execute a normal payment.

Show:

* payment progress
* successful completion
* transaction detail
* bank route

## Step 2 — Bank degradation

Open Network Console.

Trigger high latency for one bank.

Show:

* health score decline
* traffic redistribution
* payments continuing through healthy participants

## Step 3 — Bank outage

Stop the bank.

Show:

* circuit breaker opens
* switch routes around it
* service continuity remains available

## Step 4 — Offline

Turn the client offline.

Initiate payment.

Show:

* queued state
* no false success
* local persistence

Restore network.

Show:

* synchronization
* original idempotency identity
* final settlement

## Step 5 — Reconciliation

Inject a controlled ledger divergence.

Run Merkle reconciliation.

Show:

* root mismatch
* tree traversal
* exact divergent branch
* transaction localization
* naive vs Merkle timing

## Step 6 — Integrity

Run a concurrency storm.

Show:

* transaction count
* ledger conservation
* zero negative balances
* zero duplicate processing
* invariant status

This sequence demonstrates the entire project without requiring the evaluator to understand the architecture beforehand.

\---

# 49\. Frontend Visual Design Direction

The consumer UI should feel modern fintech rather than academic.

Recommended visual language:

* clean cards
* restrained animations
* strong typography hierarchy
* clear amount typography
* green/success and red/error states used sparingly
* polished empty/loading states
* responsive layout

The Network Console can be more technical:

* charts
* network diagram
* status indicators
* live logs
* tree visualization

Keep the customer app and operational console visually distinct.

\---

# 50\. Suggested Screens

### Authentication

1. Login
2. Register

### Customer

3. Home
4. Pay
5. Confirm Payment
6. Payment Processing
7. Transaction Success
8. Transaction Details
9. Transactions
10. Offline Queue

### Merchant

11. Merchant Dashboard
12. Receive Payment / QR
13. Incoming Payments
14. Settlement

### Network Console

15. Network Overview
16. Bank Health
17. Routing
18. Reconciliation
19. Merkle Tree Detail
20. Integrity
21. Chaos Lab
22. Event/Activity Feed

\---

# 51\. Seed Data

The simulator should provide a deterministic seed environment.

Example entities:

```text
Banks (MVP):
BANK\_A
BANK\_B

Bank C is optional stretch capacity and should be enabled only after the two-bank system is stable.

Users:
Alice
Bob
Charlie
David

Merchants:
Campus Cafe
Book Store
Utility Merchant

Devices:
Alice Phone
Bob Phone
Merchant Terminal A
```

All seed data should be synthetic.

Provide a reset command:

```bash
make seed-reset
```

\---

# 52\. Configuration

Important environment/config values:

```text
DATABASE\_URL
JWT\_SECRET
REDIS\_URL
BANK\_A\_URL
BANK\_B\_URL
BANK\_C\_URL (stretch)
MERKLE\_BUCKET\_SIZE
ROUTING\_FAILURE\_THRESHOLD
ROUTING\_LATENCY\_THRESHOLD
CIRCUIT\_OPEN\_DURATION
OFFLINE\_REPLAY\_MAX\_RETRIES
RATE\_LIMIT\_WINDOW (stretch)
RATE\_LIMIT\_MAX\_REQUESTS (stretch)
```

All thresholds should be configurable for experiments.

\---

\---

# 54\. Service Communication Rules

Keep communication simple.

Customer → API:

```text
HTTPS/HTTP
```

API → bank adapters:

```text
HTTP/gRPC
```

Frontend ↔ API:

```text
HTTP + WebSocket
```

Workers → data store:

```text
PostgreSQL
```

Optional asynchronous events:

```text
Redis Streams
```

Avoid introducing Kafka solely to create a more impressive architecture. If a message bus is used, each event must have a clear responsibility.

\---

# 55\. Suggested Event Model

```text
PAYMENT\_CREATED
PAYMENT\_ROUTED
PAYMENT\_PROCESSING
PAYMENT\_COMPLETED
PAYMENT\_FAILED
PAYMENT\_RETRIED
BANK\_DEGRADED
BANK\_RECOVERED
CIRCUIT\_OPENED
CIRCUIT\_CLOSED
OFFLINE\_PAYMENT\_QUEUED
OFFLINE\_PAYMENT\_SYNCED
RECONCILIATION\_STARTED
RECONCILIATION\_DIVERGENCE\_FOUND
RECONCILIATION\_COMPLETED
INVARIANT\_VIOLATION
CHAOS\_STARTED
CHAOS\_STOPPED
```

Events should be immutable facts, not arbitrary commands.

\---

# 55.5 Vibe-Coding / AI-Assisted Development Boundary

AI-assisted coding is allowed and encouraged for speed, but it must be used differently across the codebase.

### AI-friendly work

* UI scaffolding
* styling and component boilerplate
* DTOs and serializers
* repetitive CRUD endpoints
* test fixtures
* documentation drafts

### Human-review-required work

No AI-generated change may be merged into the following without a teammate reading the code, understanding the control flow, and validating the relevant tests:

* payment processing path
* idempotency logic
* balance mutation
* ledger writes
* transaction boundaries
* concurrency/locking logic
* Merkle canonicalization and reconciliation
* offline replay correctness
* routing/circuit-breaker state transitions

A critical-path change is not **Done** until:

```text
Code reviewed by another member
Relevant unit/integration tests pass
Concurrency or failure test passes where applicable
Design decision is documented
```

# 56\. Team of Three — Work Division

Ownership is divided by **feature area**, and frontend work is intentionally shared across all three members. No member should become the sole frontend bottleneck.

## Member 1 — Payment Core + Customer Frontend

Own:

### Backend

* authentication
* users/accounts
* payment state machine
* idempotency
* PostgreSQL transactions
* double-entry ledger
* concurrency controls
* bank adapter contract / Bank A

### Frontend

* login/register
* customer home
* pay flow
* payment confirmation / processing / success
* transaction details
* transaction history

Deep understanding required:

* ACID
* isolation and locks
* idempotency
* concurrency
* consistency
* monetary representation

## Member 2 — Resilience + Offline + Merchant Frontend

Own:

### Backend

* adaptive routing
* bank health
* circuit breaker
* failure injection
* recovery orchestration
* offline synchronization API
* Bank B adapter

### Frontend

* merchant dashboard
* QR generation/display
* incoming payments
* settlement view
* offline queue
* connectivity/sync UX

Deep understanding required:

* failure modes
* retries and timeout semantics
* circuit breakers
* availability
* offline synchronization
* strong vs eventual consistency

## Member 3 — Reconciliation + Integrity + Network Console + Research

Own:

### Backend / Research

* incremental Merkle engine
* reconciliation API
* naive baseline
* integrity engine
* benchmark harness
* research experiment automation

### Frontend

* network overview
* bank health visualization
* routing visualization
* reconciliation dashboard
* Merkle-tree visualization
* integrity dashboard
* chaos lab UI

Deep understanding required:

* Merkle trees
* canonical hashing
* reconciliation complexity
* benchmark design
* invariant design
* visualization of distributed state

## Shared Ownership Rule

All three members must understand the entire architecture. More importantly, **all payment/ledger/idempotency/concurrency-critical changes require review by at least one other member** before merge.

# 57\. Twelve-Week Implementation Roadmap

The project is intentionally planned as a **12-week implementation and research cycle**. The calendar is designed to avoid superficial completion and to leave time for benchmark reruns, debugging, paper writing, and final demonstration polish.

## Phase 1 — Weeks 1–2: Architecture, Contracts, and Payment Foundation

Deliver:

* final system architecture
* database schema
* API contracts
* authentication and RBAC
* two simulated bank services
* seed data
* basic React shell

Exit gate:

```text
User login works
Bank services start reproducibly
Database migrations are deterministic
Customer can view account state
```

## Phase 2 — Week 3: Payment Core

Implement:

* payment state machine
* idempotency
* balance operations
* double-entry ledger
* transaction history
* payment UI

Exit gate:

```text
Online payment works end-to-end
Duplicate request cannot create duplicate logical payment
Ledger conservation holds
```

## Phase 3 — Week 4: Concurrency and Correctness

Implement and test:

* row locking / chosen concurrency strategy
* atomic debit/ledger commit
* concurrent payment storm
* invariant checks
* adversarial payment tests

Exit gate:

```text
Hot-account concurrency test passes
No negative balances
No unexpected duplicate ledger entries
```

## Phase 4 — Week 5: Incremental Merkle Infrastructure

Implement:

* canonical transaction serialization
* incremental bucketed Merkle tree
* persisted/derivable commitments
* global root construction
* root comparison
* reconciliation API

Exit gate:

```text
New ledger record updates only affected commitment path
Roots match for identical ledgers
```

## Phase 5 — Week 6: Reconciliation Research Harness

Implement:

* naive full-diff baseline
* Merkle divergence traversal
* divergent transaction localization
* timing/node/record/byte measurements
* 10K and 100K benchmark datasets
* repeatable benchmark scripts
* reconciliation visualization

Exit gate:

```text
Baseline and proposed methods produce reproducible measurements
No benchmark number is manually entered
```

## Phase 6 — Week 7: Adaptive Routing

Implement:

* bank health sampling
* deterministic health score
* routing policy
* circuit breaker
* half-open recovery
* routing history
* network-console routing visualization

Exit gate:

```text
Static and adaptive routing can be run under identical workload conditions
```

## Phase 7 — Week 8: Chaos Engineering and Self-Healing

Implement:

* bank outage
* latency injection
* transient message failure
* temporary network partition simulation
* admin-only test ledger corruption
* concurrency storm controls
* recovery event logging

Exit gate:

```text
Every chaos scenario is repeatable
Recovery behavior is observable
```

## Phase 8 — Week 9: Offline-First Application Layer

Implement:

* IndexedDB queue
* offline detection
* durable request capture
* replay worker
* retry/backoff
* synchronization UI
* merchant/offline screens

Exit gate:

```text
Offline request persists through page refresh
Reconnect causes safe replay
Duplicate replay does not duplicate payment
```

## Phase 9 — Week 10: Complete Frontend + Network Console

Finish:

* customer flow polish
* merchant flow polish
* network overview
* bank health
* routing visualization
* reconciliation visualization
* integrity dashboard
* chaos lab
* WebSocket real-time updates

Exit gate:

```text
Full demo can be executed from the browser without developer tools
```

## Phase 10 — Week 11: Experimental Campaign

Run controlled experiments:

* reconciliation at 10K and 100K
* static vs adaptive routing
* outage/latency scenarios
* offline synchronization scenarios
* concurrency and invariant tests

Store raw outputs under a versioned benchmark directory.

Exit gate:

```text
Every reported figure can be regenerated from a script and raw output
```

## Phase 11 — Week 12: Paper, Hardening, and Final Demo

Complete:

* paper results/tables/figures
* README
* architecture diagrams
* API documentation
* limitations
* demo script
* deployment rehearsal
* bug fixes
* final performance reruns

Do not add major features in this phase.

## Optional 7-Day Rapid Prototype Sprint

A separate 7-day sprint is permitted only as a **vertical-slice milestone**, not as the complete project schedule. The sprint should implement one thin path:

```text
Day 1: payment foundation
Day 2: payment correctness
Day 3: basic Merkle prototype
Day 4: basic routing
Day 5: offline queue
Day 6: minimal network console
Day 7: demo + smoke tests
```

The sprint is useful for early validation, but the full research prototype continues through the 12-week roadmap.

# 58\. Milestone Definition of Done

A feature is not considered complete merely because code exists or the UI renders. Each critical subsystem must satisfy an implementation + test + evidence gate.

## Milestone 1 — Foundation

```text
✓ Services start reproducibly
✓ Migrations are deterministic
✓ Seed data is reproducible
✓ Authentication works
```

## Milestone 2 — Payment Core

```text
✓ End-to-end payment works
✓ State machine rejects invalid transitions
✓ Ledger entries are persisted atomically
```

## Milestone 3 — Idempotency + Concurrency

```text
✓ Duplicate request returns original logical result
✓ Key reuse with changed payload is rejected
✓ Concurrent spending preserves balance correctness
✓ Concurrency tests are passing
```

## Milestone 4 — Merkle Infrastructure

```text
✓ Incremental updates work
✓ Identical ledgers produce identical roots
✓ Controlled divergence is detected
```

## Milestone 5 — Reconciliation Research

```text
✓ Naive baseline exists
✓ Merkle traversal exists
✓ Divergent transactions are localized
✓ 10K benchmark works
✓ 100K benchmark works
✓ Raw measurements are saved
```

## Milestone 6 — Offline

```text
✓ Queue persists locally
✓ Offline state is clearly shown
✓ Replay uses original request identity
✓ Replay is safe under retry
```

## Milestone 7 — Adaptive Routing

```text
✓ Bank health is measurable
✓ Routing score is deterministic
✓ Circuit breaker opens/closes correctly
✓ Traffic shifts under failure
```

## Milestone 8 — Chaos

```text
✓ Each scenario is admin-only where applicable
✓ Scenarios are repeatable
✓ Recovery behavior is observable
```

## Milestone 9 — Research Evidence

```text
✓ Baselines are fixed before final measurement
✓ Experiments are scripted
✓ No benchmark numbers are typed into code or docs
✓ Results are reproducible from raw outputs
```

## Milestone 10 — Final Application

```text
✓ Customer flow works
✓ Merchant flow works
✓ Network console works
✓ One-command local startup works
✓ Final demo runs without manual database edits
```

# 59\. Research Paper Structure

## Title

**TransactX: Fault-Tolerant Payment Switching with Offline-First Resilience and Merkle-Tree Accelerated Reconciliation**

## Abstract

Summarize:

* problem
* architecture
* innovations
* experimental setup
* results

## Introduction

Explain:

* digital payment scale
* reliability challenge
* connectivity challenge
* reconciliation challenge
* research gap

## Related Work

Cover:

* digital payment systems
* distributed transactions
* offline payment approaches
* Merkle-based reconciliation
* fault-tolerant routing
* financial consistency

## Proposed System

Include:

* architecture
* payment workflow
* offline mechanism
* routing
* reconciliation
* integrity layer

## Methodology

Describe:

* baseline
* workloads
* datasets
* fault scenarios
* benchmark methodology

## Results

Tables/graphs:

* reconciliation latency
* nodes visited
* routing success rate
* recovery time
* offline replay correctness
* invariant violations

## Discussion

Explain tradeoffs:

* computation
* storage
* complexity
* availability
* consistency

## Limitations

Explicitly acknowledge:

* simulated banks
* synthetic data
* prototype offline model
* no production NPCI connectivity
* limited scale

## Future Work

Discuss:

* cryptographic delegated offline spending
* formal verification
* multi-region deployment

\---

# 60\. Paper-Quality Metrics

The benchmark harness must be implemented by the midpoint of the project so results can be rerun before the final phase.

At minimum report:

### Reconciliation

* median runtime
* P95 runtime
* number of records scanned
* number of Merkle nodes visited
* communication volume

### Routing

* success rate
* P95/P99 latency
* mean time to recovery
* proportion of traffic shifted

### Offline

* queue persistence success
* replay success
* duplicate count
* synchronization latency

### Integrity

* invariant violation count
* recovery success
* false positive integrity alerts if applicable

\---

# 61\. Expected Experimental Claims

Do not pre-fill the paper with fabricated numbers.

The implementation must generate its own actual measurements.

Use language such as:

> “In our experimental environment, the proposed Merkle-based method reduced reconciliation runtime by X% relative to the naive full-diff baseline at the tested ledger scale.”

Do not claim a percentage before the benchmark exists.

Likewise:

> “Under the injected Bank B outage scenario, adaptive routing maintained an X% payment success rate compared with Y% for static routing.”

Only publish numbers that were actually measured and reproducibly generated.

\---

# 62\. Key Interview Topics the Team Must Master

Every member should be able to explain:

### Distributed systems

* partial failure
* retry safety
* timeouts
* circuit breakers
* idempotency
* availability
* consistency

### Databases

* ACID
* isolation
* row locks
* optimistic vs pessimistic concurrency
* indexes
* transactions

### Financial systems

* double-entry ledger
* reconciliation
* settlement vs transaction state
* balance correctness

### Algorithms

* Merkle tree
* SHA-256
* tree traversal
* complexity

### Frontend

* optimistic vs confirmed state
* offline persistence
* WebSockets
* synchronization

### System design

* scaling the switch
* bottlenecks
* database scaling
* horizontal scaling
* observability
* failure recovery

\---

# 63\. Likely Interview Questions

1. Why not simply compare two databases?
2. Why is Merkle reconciliation useful?
3. Is reconciliation really O(log n)?
4. What happens when there are multiple divergent regions?
5. What is idempotency?
6. How do you prevent duplicate payments?
7. What happens if the client times out after the bank debits the account?
8. What happens if two payments spend the same balance simultaneously?
9. Why use a double-entry ledger?
10. Why PostgreSQL instead of a NoSQL database?
11. What happens if Bank B is down?
12. How does the circuit breaker work?
13. How is bank health calculated?
14. Why adaptive routing instead of load balancing alone?
15. How does offline mode avoid duplicate payments?
16. Is the offline request actually successful before reconnecting?
17. What happens if the device crashes after queueing?
18. What happens if the same queued request is replayed twice?
19. How did you test resilience?
20. What exactly does chaos engineering prove?
21. How would you scale from two simulated banks to many participants?
22. Where is the strongest consistency boundary?
23. What would you shard first?
24. How would multi-region deployment change the design?
25. What parts of this architecture are not production-ready?

The team should prepare precise answers grounded in the implementation rather than memorized buzzwords.

\---

# 64\. What Makes This Project Novel

The novelty claim should **not** be:

> “We built a payment system.”

The novelty is the integrated experimental architecture:

> \*\*A payment-switch prototype combining offline-first transaction capture, health-aware self-healing routing, runtime financial invariant checking, and hierarchical Merkle-based cross-bank reconciliation, evaluated through controlled fault injection and quantitative baselines.\*\*

The strongest individual research contribution is the reconciliation experiment. The strongest hackathon demonstration is the combination of adaptive routing + chaos injection + live operational visualization.

\---

# 65\. What Makes It Different From a Typical Student Fintech Project

A typical student payment project:

```text
Login
 ↓
Wallet
 ↓
Transfer Money
 ↓
Transaction History
```

TransactX:

```text
User
 ↓
Payment Switch
 ↓
Idempotent Transaction Processing
 ↓
Adaptive Routing
 ↓
Multiple Independent Banks
 ↓
Double-Entry Ledger
 ↓
Runtime Integrity Checking
 ↓
Cross-Bank Merkle Reconciliation
 ↓
Offline Synchronization
 ↓
Chaos Testing + Recovery
```

The important distinction is that the project can **fail visibly and recover measurably**.

\---

# 66\. What Not to Overbuild

Avoid:

* 15+ microservices
* real bank integrations
* blockchain just for appearance
* AI features that do not solve a real payment problem
* elaborate mobile and web clients simultaneously
* huge authentication systems
* complicated message brokers before correctness works
* fake performance claims

The project should feel technically deep because its mechanisms are deep, not because the architecture diagram contains many boxes.

\---

# 67\. Recommended Build Priority

When forced to choose, prioritize exactly this order:

```text
1. Payment correctness
2. Idempotency + concurrency
3. Double-entry ledger
4. Merkle reconciliation
5. Adaptive routing
6. Chaos + recovery
7. Offline queue + sync
8. Integrity dashboard
9. Network console polish
10. Optional observability enhancements
```

Do not sacrifice correctness for visual features.

\---

# 67.5. Feature Acceptance Contract for the Coding Agent

The implementation agent must treat the following rules as binding:

1. Do not replace the agreed architecture with a simpler mock if the mock removes the behavior being measured.
2. Do not introduce major frameworks, brokers, databases, or microservices merely for appearance.
3. Do not claim a feature is complete because its endpoint or UI exists; use the milestone and Definition of Done gates.
4. Do not hard-code benchmark results, health scores, recovery times, or performance claims.
5. Do not move financial correctness into client-side code. PostgreSQL/backend logic is authoritative for monetary state.
6. Do not expose chaos controls to CUSTOMER or MERCHANT roles.
7. Do not report the offline queue as a completed payment before central processing confirms it after synchronization.
8. Preserve deterministic seed data and provide repeatable reset commands.
9. Keep critical financial operations auditable and easy for all three team members to read.
10. Prefer incremental, testable implementation over speculative abstractions.

# 68\. Final System Narrative

TransactX should ultimately be explainable in one paragraph:

> TransactX is a simulated distributed digital payment network in which a central payment switch orchestrates transactions across multiple simulated banks while maintaining an explicit double-entry ledger and runtime financial invariants. The system improves resilience through health-aware routing and circuit-breaker-based recovery, allowing traffic to move away from degraded participants under controlled failures. It supports offline-first transaction capture by durably storing payment intents on the client and replaying them using the same idempotency identity after connectivity returns. For cross-bank reconciliation, each ledger is represented using hierarchical Merkle commitments, allowing the system to compare roots first and recursively localize divergent regions rather than blindly comparing every transaction. A dedicated operations console exposes these mechanisms live, while controlled chaos experiments and quantitative benchmarks demonstrate whether the proposed architecture actually improves resilience, reconciliation efficiency, and financial correctness.

\---

# 69\. Final Architecture at a Glance

```text
                             TRANSACTX
                               │
          ┌────────────────────┼─────────────────────┐
          │                    │                     │
       Customer             Merchant          Network Console
          │                    │                     │
          └────────────────────┼─────────────────────┘
                               │
                         API Gateway
                               │
                         Payment Switch
                               │
            ┌──────────────────┼──────────────────┐
            │                  │                  │
      Idempotency         Adaptive Router     Offline Sync
            │                  │                  │
            │            ┌─────┴─────┐            │
            │            │           │            │
            │          Bank A      Bank B         │
            │            │           │            │
            └────────────┴─────┬─────┴────────────┘
                               │
                        Double-Entry Ledger
                               │
               ┌───────────────┼────────────────┐
               │               │                │
          Integrity       Reconciliation      Metrics
          Engine              Engine
                              │
                       Hierarchical
                       Merkle Trees
                              │
                       Divergence Detection

                       ┌───────────────┐
                       │ Chaos Engine  │
                       └───────┬───────┘
                               │
                         Failure Injection
                               │
                         Self-Healing
                               │
                      Adaptive Routing
```

# 70\. Definition of Success

The project is successful when all of the following are true:

* A user can perform a real simulated payment through the web application.
* Payments remain idempotent under repeated requests.
* Concurrent payment attempts preserve financial consistency.
* The ledger provides an auditable record of monetary movements.
* Offline payment intents survive connectivity loss and safely synchronize later.
* Merkle reconciliation detects and localizes controlled ledger divergence.
* Adaptive routing continues serving transactions during bank degradation.
* Chaos experiments produce visible and repeatable recovery behavior.
* Financial invariants remain satisfied under normal and adversarial tests.
* Benchmark experiments produce real quantitative comparisons.
* The entire stack can be started reproducibly.
* All three team members can explain the architecture and defend the design trade-offs in an SDE interview.

\---

# 71\. Final Project Positioning

### Academic

**Resilient distributed payment infrastructure with efficient cryptographic reconciliation.**

### Hackathon

**A live payment network that can go offline, survive bank failures, reroute traffic, detect ledger divergence, and continuously verify financial correctness.**

### Resume

**Designed and implemented a fault-tolerant inspired by challenges observed in large-scale digital payment infrastructure payment switch with idempotent transactional processing, double-entry ledgering, adaptive bank routing, offline-first synchronization, hierarchical Merkle reconciliation, runtime financial invariants, and chaos-based resilience testing.**

### Interview

The strongest story is not “we built a payment app.”

It is:

> \*\*“We wanted to study what happens when a distributed financial system stops behaving ideally, so we built the system around correctness and failure from the beginning.”\*\*

\---

# 72\. Immediate Next Build Step

The implementation should begin with these artifacts before any substantial coding:

1. `docs/architecture.md` — component responsibilities and boundaries
2. `docs/database.md` — final ER model and transaction invariants
3. `docs/api.md` — request/response contracts
4. `docs/reconciliation.md` — exact Merkle design and benchmark methodology
5. `docs/offline.md` — queue and synchronization state machine
6. `docs/routing.md` — health score, circuit breaker, and routing policy
7. `docs/chaos.md` — reproducible failure scenarios
8. `docs/invariants.md` — invariant definitions and validation points

Only after those contracts are stable should vibe coding begin.

\---

# 73\. Master Principle

**Build the simplest system that makes the research claim true, measurable, reproducible, and defensible.**

The project should never depend on the evaluator believing that a feature works because the UI says so. Every major claim should have:

```text
Mechanism
   ↓
Implementation
   ↓
Failure / baseline
   ↓
Measurement
   ↓
Evidence
```

That is the standard TransactX should be built to.

