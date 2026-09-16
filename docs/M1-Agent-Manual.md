TRANSACTX
TransactX: A Resilient Payment Infrastructure for the Next Generation of Digital Payments
A Fault-Tolerant Payment Network with Offline-First Resilience, Adaptive Routing, and Merkle-Accelerated Reconciliation
Member 1 Deep-Dive Implementation Plan — Payment Core + Customer Frontend
Owns authentication, users/accounts, payment state machine, idempotency, PostgreSQL transactions, double-entry ledger, concurrency control, Bank A/adapter contract, and the complete customer payment experience.

# 0\. How to Use This Document

This document is the member-specific execution contract derived from the TransactX master specification. Use it with the chronological project checklist. Work in order, keep scope narrow, and tick an item only after implementation, testing, and review.
Copilot rule: Use Copilot aggressively for boilerplate, wiring, UI, tests, migrations, and repetitive work; personally review critical financial/distributed-systems logic.
Source rule: The master specification and this member plan describe intended behavior. Code and docs must clearly distinguish IMPLEMENTED from PLANNED/FUTURE.
Evidence rule: No benchmark number, resilience percentage, or performance claim is valid until a reproducible experiment generates it.

# 1\. Shared System Contract

TransactX is a simulated distributed payment network. A central payment switch orchestrates multiple simulated banks, uses an explicit double-entry ledger, survives controlled participant failures through adaptive routing/circuit breakers, captures payment intents offline for later safe replay, and uses hierarchical Merkle commitments for reconciliation.

## 1.1 Non-negotiable boundaries

\[ ] No real money movement or production bank connectivity.
\[ ] Offline mode is request capture + safe replay, not unrestricted offline digital cash.
\[ ] Backend/PostgreSQL is authoritative for monetary state.
\[ ] Chaos and corruption controls are simulation/test-only and OPS\_ADMIN-only.
\[ ] Do not add large frameworks, brokers, or microservices solely for appearance.
\[ ] All three members must understand the complete architecture.

## 1.2 Shared service boundaries



## 1.3 Shared event vocabulary

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

## 1.4 Git/Copilot working loop

\[ ] Create a feature branch; do not commit directly to main.
\[ ] Ask Copilot for a plan before non-trivial code changes.
\[ ] Implement one bounded feature at a time.
\[ ] Run focused tests immediately after implementation.
\[ ] Run the broader build/test checks before PR.
\[ ] Update the relevant docs/\*.md file after behavior changes.
\[ ] Get peer review for critical cross-member changes.

# 2\. Mission and Ownership

You own the financial correctness boundary. Your implementation is upstream of routing, offline replay, reconciliation, and most of the user experience. The priority order is correctness → testability → clarity → performance → polish.

# 3\. Dependency Graph

Customer UI
-> Auth / Account API
-> Payment API
-> Idempotency
-> Payment State Machine
-> Bank Adapter
-> PostgreSQL Transaction
-> Balance + Double-Entry Ledger
-> Events / Observability

M2 consumes: BankAdapter + payment failure semantics
M3 consumes: payment/ledger schema + deterministic ordering + event IDs

# 4\. Repository Ownership

backend/internal/
auth/
users/
accounts/
payments/
ledger/
idempotency/
bank/
common/

frontend/src/pages/auth/
frontend/src/pages/customer/
frontend/src/features/payments/
frontend/src/stores/useAuthStore.ts
frontend/src/stores/usePaymentStore.ts
frontend/src/services/
frontend/src/types/

backend/tests/integration/
backend/tests/concurrency/

# 5\. Data Model

## 5.1 Users



## 5.2 Accounts



## 5.3 Payments

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

## 5.4 Ledger

ledger\_transactions
id UUID PRIMARY KEY
payment\_id UUID UNIQUE
created\_at TIMESTAMP

ledger\_entries
id UUID PRIMARY KEY
ledger\_transaction\_id UUID
account\_id UUID
entry\_type VARCHAR
amount BIGINT
created\_at TIMESTAMP
The authoritative monetary rule is conservation: total debits must equal total credits for each completed transfer. Use one sign convention consistently.

## 5.5 Idempotency records

key UNIQUE
user\_id
request\_hash
payment\_id
response\_snapshot JSONB
created\_at
expires\_at NULL

# 6\. Authentication/RBAC Implementation

## 6.1 Registration

\[ ] Validate required fields and normalize identifiers.
\[ ] Reject duplicate phone/payment identifier.
\[ ] Hash password using Argon2id or bcrypt.
\[ ] Create user and initial synthetic account according to the agreed transaction boundary.
\[ ] Never return password hashes.

## 6.2 Login

\[ ] Validate credentials without account-enumeration leakage.
\[ ] Issue short-lived JWT/secure session.
\[ ] Include only required claims.
\[ ] Attach request/correlation ID to auth logs.

## 6.3 Authorization

\[ ] Server enforces CUSTOMER/MERCHANT/OPS\_ADMIN.
\[ ] Customer can access only owned resources.
\[ ] Merchant can access only merchant-owned incoming payments.
\[ ] OPS\_ADMIN is required for diagnostic/chaos controls.
\[ ] Frontend route hiding is not treated as authorization.

# 7\. Payment State Machine

CREATED -> VALIDATING -> LOCAL_SETTLEMENT -> COMMITTED -> COMPLETED
VALIDATING -> ROUTING -> PROCESSING -> COMMITTED -> COMPLETED
PROCESSING -> FAILED
PROCESSING -> PENDING\_RECONCILIATION
PENDING\_RECONCILIATION -> COMPLETED | REVERSED

OFFLINE\_CAPTURED -> QUEUED -> SYNCING
SYNCING -> COMPLETED | REPLAY\_FAILED

Implement a single transition function/table. Handlers must not assign arbitrary states.

# 8\. Payment API Contract

POST /api/payments
Authorization: Bearer <token>
Idempotency-Key: <UUID>

{
"recipient": "bob@transactx",
"amount": 12500,
"currency": "INR",
"note": "Lunch"
}

## 8.1 Server sequence

\[ ] Authenticate.
\[ ] Validate amount as a positive smallest-unit integer.
\[ ] Resolve/validate recipient.
\[ ] Check idempotency key for this user/scope.
\[ ] Compare request hash if key exists.
\[ ] Return prior result on identical key + identical request.
\[ ] Reject same key + different request.
\[ ] Create payment and transition state.
\[ ] Select bank through adapter/routing contract.
\[ ] Begin authoritative PostgreSQL transaction.
\[ ] Lock/revalidate sender balance.
\[ ] Perform required balance mutations and ledger writes atomically.
\[ ] Commit.
\[ ] Advance state and emit event.
\[ ] Return final or intermediate status honestly.

# 9\. Idempotency Deep Dive



## 9.1 Implementation traps to test

\[ ] Check-then-insert race on idempotency record.
\[ ] Creating payment before winning the idempotency race.
\[ ] Returning a result before the authoritative transaction commits.
\[ ] Storing a response snapshot that cannot reconstruct status correctly.
\[ ] Allowing same key across users to collide incorrectly or bypass ownership.

# 10\. Double-Entry Ledger

Payment P = 125.00
Sender:   DEBIT  125.00
Receiver: CREDIT 125.00

For every completed transfer:
sum(debits) == sum(credits)
\[ ] Do not commit balance mutation without corresponding ledger writes.
\[ ] Do not create final ledger entries for a payment that did not commit.
\[ ] Link one ledger transaction to one payment using a unique constraint.
\[ ] Provide a verification path that reconstructs balance from ledger entries.

# 11\. Concurrency Control

Start with PostgreSQL row locking or another explicitly justified strategy. The important property is that the balance check and monetary mutation occur under the same authoritative consistency boundary.
BEGIN;

SELECT balance, version
FROM accounts
WHERE id = $sender
FOR UPDATE;

\-- Re-check available balance here.

UPDATE accounts
SET balance = balance - $amount,
version = version + 1
WHERE id = $sender;

\-- Write ledger entries.

COMMIT;

## 11.1 Required stress cases



# 12\. Bank Adapter + Bank A

type BankAdapter interface {
GetBalance(ctx context.Context, accountID string) (Balance, error)
Debit(ctx context.Context, req DebitRequest) (OperationResult, error)
Credit(ctx context.Context, req CreditRequest) (OperationResult, error)
Health(ctx context.Context) (HealthResult, error)
}
\[ ] Payment logic depends on interface, not Bank A URL/details.
\[ ] Bank A has independent service.
\[ ] Health, debit, credit, and account lookup exist.
\[ ] Timeouts are typed and do not trigger unsafe blind duplication.
\[ ] Explicit rejection is distinguishable from unknown outcome.
\[ ] Request/payment IDs are correlated in logs.

# 13\. Customer Frontend Deep Dive

## Login

\[ ] Identifier/password
\[ ] Loading/error states
\[ ] Successful navigation

## Register

\[ ] Fields/validation
\[ ] Password rules
\[ ] Success navigation

## Home

\[ ] Payment identifier
\[ ] Balance
\[ ] Recent transactions
\[ ] Send/scan CTAs
\[ ] Online/offline state
\[ ] Pending sync count

## Pay

\[ ] Recipient
\[ ] Amount
\[ ] Note
\[ ] Validation
\[ ] Submit

## Confirm

\[ ] Recipient/amount confirmation
\[ ] Explicit final action

## Processing

\[ ] Payment ID
\[ ] Current server state
\[ ] Honest pending state

## Success/Failure

\[ ] Reference ID
\[ ] Amount
\[ ] Status
\[ ] Safe retry where applicable

## Transaction Details

\[ ] Sender
\[ ] Receiver
\[ ] Amount
\[ ] Time
\[ ] Status
\[ ] Routing bank
\[ ] Duration
\[ ] Origin

## Transactions

\[ ] Chronological history
\[ ] Status
\[ ] Useful filtering if cheap

# 14\. Observability Contract

\[ ] Emit request\_id.
\[ ] Emit payment\_id when available.
\[ ] Emit bank\_id around bank operations.
\[ ] Emit state transition old/new.
\[ ] Emit latency\_ms and outcome.
\[ ] Emit ledger transaction ID on successful completion.

# 15\. Testing Matrix



# 16\. Twelve-Week Personal Execution



# 17\. Definition of Done

\[ ] A customer authenticates and resolves a recipient.
\[ ] A normal online payment completes end-to-end.
\[ ] Same idempotency key + same payload returns original logical result.
\[ ] Same idempotency key + different payload is rejected.
\[ ] Completed payment has expected balanced ledger entries.
\[ ] Concurrent payments never create negative balance.
\[ ] Authorization blocks cross-user access.
\[ ] Bank timeout/rejection semantics are documented and tested.
\[ ] Customer UI never reports pending/offline as final settlement.
\[ ] Critical changes have peer review.

# 18\. Copilot Prompts

## Plan

Inspect current payment/account code. Do not code. Plan \[FEATURE], affected files, transaction boundaries, state transitions, DB changes, failure modes, and tests.

## Implement

Implement only \[FEATURE]. Preserve behavior, add tests, run tests/build, and stop if acceptance criteria cannot be satisfied.

## Critical review

Review this code as a senior payments engineer. Find race conditions, bad transaction boundaries, idempotency gaps, timeout-after-commit hazards, and ledger inconsistencies. Do not modify yet.

## Attack

Try to break \[FEATURE] with duplicate, concurrent, timeout, insufficient-balance, malformed-state, and retry scenarios. Fix implementation issues rather than weakening tests.

# 19\. Interview Mastery

\[ ] ACID and exact transaction boundary
\[ ] Isolation and row locking
\[ ] Idempotency vs uniqueness
\[ ] Timeout after commit and unknown outcome
\[ ] Double-entry ledger rationale
\[ ] Integer money representation
\[ ] Payment state vs ledger state
\[ ] Prototype vs production security boundary

# 20\. Handoff to M2/M3

\[ ] Publish BankAdapter interface.
\[ ] Publish payment state machine.
\[ ] Publish payment/ledger schema and deterministic ordering fields.
\[ ] Publish events and error codes.
\[ ] Publish retryability/unknown-outcome semantics.
\[ ] Publish concurrency test command and seed/reset command.

# 21\. File-by-File Implementation Map



# 22\. Database Migration and Index Plan

\[ ] Create users table with unique phone and payment identifier.
\[ ] Create banks table with unique code.
\[ ] Create accounts table with foreign keys to users/banks.
\[ ] Create payments table with unique logical ID and idempotency constraint according to chosen scope.
\[ ] Create ledger\_transactions with UNIQUE(payment\_id).
\[ ] Create ledger\_entries with indexed ledger\_transaction\_id and account\_id.
\[ ] Create idempotency\_records with unique key/scope and index on user\_id.
\[ ] Add timestamps needed for deterministic ordering.
\[ ] Add indexes for transaction-history queries by account and created\_at.
\[ ] Add index/constraint that prevents duplicate payment-to-ledger transaction mapping.
\[ ] Verify migrations are deterministic on a clean PostgreSQL instance.

# 23\. API Contract Deep Dive



POST /api/payments
Headers:
Idempotency-Key: UUID
Body:
recipient
amount (integer smallest currency unit)
currency
note (optional)

Response:
success
data.paymentId
data.state
requestId
error

# 24\. Payment Failure Matrix



# 25\. Payment Orchestration Pseudocode

func ProcessPayment(ctx, req) Result {
authenticate(req)
validate(req)

&#x20;   idem := idempotency.Lookup(userID, key)
    if idem.exists {
        if idem.hash != hash(req) { return IDEMPOTENCY\_CONFLICT }
        return idem.snapshot
    }

    create payment + idempotency record safely

  CURRENT M1-3C LOCAL SETTLEMENT:
    transition(CREATED, VALIDATING)
    recipient := resolveRecipient(req.recipient)
    transition(VALIDATING, LOCAL_SETTLEMENT)

    BEGIN DB TX
      SELECT sender FOR UPDATE
      re-check balance
      if insufficient -> ROLLBACK + FAILED

      debit sender
      credit receiver
      insert ledger transaction
      insert debit + credit entries
    COMMIT

    transition(COMMITTED, COMPLETED)
    persist response snapshot
    emit PAYMENT\_COMPLETED
    return result

  FUTURE BANK-ROUTED SETTLEMENT:
    route := router.SelectBank(req)
    transition(VALIDATING, ROUTING)
    transition(ROUTING, PROCESSING)
    perform bank operation through BankAdapter
    continue with the same atomic ledger and completion flow

}

# 26\. Concurrency Proof Checklist

\[ ] Two payments on one sender account serialize or otherwise obey the chosen concurrency strategy.
\[ ] Balance is re-read under the lock/validation boundary.
\[ ] Idempotency record creation cannot race into two logical payments.
\[ ] Ledger entries are in the same correctness boundary as balance mutation.
\[ ] A failed transaction leaves no partial ledger.
\[ ] A successful transaction leaves no missing expected ledger entry.
\[ ] Response loss after commit converges via idempotency/status.
\[ ] Tests use actual concurrent goroutines/requests against PostgreSQL, not only mocked service calls.

# 27\. Customer Frontend Component Map



# 28\. Cross-Member Integration Sequence

M1 publishes:
Payment model
State machine
Error taxonomy
BankAdapter
Payment events
Deterministic transaction fields

M2 consumes:
BankAdapter
Typed bank outcomes
Payment retry/pending semantics

M3 consumes:
Ledger schema
Canonical transaction fields
Ordering rules
Payment events

Breaking change rule:
announce -> update contract docs -> update tests -> merge

# 29\. Member 1 Implementation Checklist — Detailed

\[ ] Create auth interfaces.
\[ ] Implement password hash + verify.
\[ ] Implement auth middleware.
\[ ] Implement role middleware.
\[ ] Create user repository.
\[ ] Create account repository.
\[ ] Create recipient-resolution query.
\[ ] Create money validation helpers.
\[ ] Create payment entity/model.
\[ ] Create payment repository.
\[ ] Create explicit state enum.
\[ ] Create transition table.
\[ ] Create typed payment errors.
\[ ] Create idempotency entity/repository.
\[ ] Create request canonical hash.
\[ ] Create atomic idempotency insertion path.
\[ ] Create payment orchestration service.
\[ ] Create bank adapter interface.
\[ ] Create Bank A client.
\[ ] Create ledger transaction service.
\[ ] Create ledger entry writes.
\[ ] Create balance reconstruction query.
\[ ] Create payment history query.
\[ ] Add PostgreSQL transaction boundaries.
\[ ] Add row locking.
\[ ] Add concurrency stress command.
\[ ] Add duplicate-request tests.
\[ ] Add key-conflict tests.
\[ ] Add insufficient-balance tests.
\[ ] Add timeout/unknown-outcome tests.
\[ ] Add authorization isolation tests.
\[ ] Add customer login/register screens.
\[ ] Add customer home.
\[ ] Add pay/confirm/processing/success screens.
\[ ] Add transaction list/detail.
\[ ] Add online/offline state indicator hooks.
\[ ] Connect WebSocket payment events.
\[ ] Update docs/payment-flow.md.
\[ ] Update docs/database.md.
\[ ] Update docs/architecture.md.
\[ ] Get peer review before merge.

# 30\. Explicitly Do Not Build

\[ ] A real UPI/NPCI integration.
\[ ] A second ledger implementation in the frontend.
\[ ] Blind automatic retries for unknown bank outcomes.
\[ ] A complex workflow engine.
\[ ] Kafka solely for payment events.
\[ ] A microservice per CRUD entity.
\[ ] Production KYC/AML systems.
\[ ] Client-side authoritative balance updates.

# 31\. Personal Learning / Interview Deep Dive

Before final submission, explain each of these without opening Copilot:
\[ ] Why FOR UPDATE/row locking solves the specific hot-account race.
\[ ] What READ COMMITTED means for your chosen transaction.
\[ ] Why idempotency and unique payment IDs solve different problems.
\[ ] Why a timeout does not prove that a bank operation failed.
\[ ] What atomicity guarantees if the process crashes during the transaction.
\[ ] Why ledger entries are an audit trail rather than simply another balance column.
\[ ] How you would scale the payment switch horizontally.
\[ ] What you would change for multi-region deployment.

# Agent-First Operating Guide

This document is intentionally self-contained enough for GitHub Copilot in VS Code to execute the member's portion without repeatedly loading the full 70-page project specification. Keep the master specification in the repository as the global source of truth, but use this manual as the day-to-day scope, design, test, and integration contract for the owner.

## 1\. Context loading to minimize tokens

\[ ] Open the repository and this manual.
\[ ] Read only the relevant master-spec sections when a shared contract is ambiguous.
\[ ] Inspect the current implementation, tests, migrations, and callers before changing a file.
\[ ] Ask Copilot for a bounded plan before any non-trivial feature.
\[ ] Implement one slice at a time; do not ask Copilot to rebuild an entire subsystem after every change.
\[ ] After a feature works, rely on the existing code/docs/tests rather than repeatedly re-sending the whole architecture in chat.

## 2\. Standard Copilot loop

PLAN
↓
Inspect affected files + tests
↓
IMPLEMENT one bounded slice
↓
RUN focused tests
↓
REVIEW diff
↓
RUN integration/build checks
↓
UPDATE docs
↓
COMMIT
↓
HAND OFF contract/results to teammates

## 3\. Standard response contract

After every meaningful implementation:

1. Files changed
2. Behavior added
3. Tests run + result
4. Assumptions
5. Remaining risks
6. Exact next bounded task

## 4\. Stop conditions

\[ ] Stop if the agent wants to add a major dependency or service not required by the specification.
\[ ] Stop if an implementation changes a cross-member API without explicitly updating the contract and notifying the owner.
\[ ] Stop if a benchmark can only be produced by manually entering numbers.
\[ ] Stop if critical-path code cannot be explained by the human owner.
\[ ] Stop if the agent proposes an unsafe retry or hides an unknown monetary outcome.

# 5\. Member 1 — Detailed Execution Contract

Your output is the project's strongest correctness boundary. Treat the payment path as a small state machine with a small number of explicit transactional operations. Avoid architecture that spreads one logical transfer over many independent commit points.

## 5.1 Recommended package dependency graph

HTTP
↓
handler
↓
application service
├── validation/domain rules
├── idempotency repository
├── routing interface
├── bank adapter interface
└── transaction/ledger repository
↓
PostgreSQL

# 6\. Migration Order

\[ ] Create users.
\[ ] Create banks.
\[ ] Create accounts referencing users/banks.
\[ ] Create payments referencing accounts.
\[ ] Create ledger\_transactions referencing payments.
\[ ] Create ledger\_entries referencing ledger\_transactions/accounts.
\[ ] Create idempotency\_records.
\[ ] Add required unique constraints.
\[ ] Add indexes for recipient resolution, payment history, account lookup and ledger traversal.
\[ ] Seed deterministic data only after all foreign keys exist.

# 7\. Account and Balance Semantics

A balance column is current materialized state. Ledger entries provide the audit explanation. Define whether balance is authoritative current state plus ledger verification, or whether balance is reconstructed for certain operations; do not allow two competing authoritative sources.
account.balance : BIGINT  // smallest currency unit
account.version : BIGINT

Invariant:
materialized\_balance == reconstructed\_balance
for any account checked by the integrity engine.

# 8\. Payment Creation — Detailed Cases



# 9\. Idempotency Implementation Variants

Use database uniqueness as the final arbiter. A cache can accelerate lookup later, but Redis must never be the only correctness mechanism.
-- Correctness resides in PostgreSQL:
CREATE UNIQUE INDEX ux\_idempotency\_scope\_key
ON idempotency\_records(user\_id, key);

\-- Application:

1. compute request hash
2. attempt atomic insert/claim
3. if conflict -> read existing row
4. compare hash
5. return existing result OR conflict

## 9.1 Canonical request hash

{
"amount": 12500,
"currency": "INR",
"note": "Lunch",
"recipient": "bob@transactx"
}
↓
canonical bytes
↓
SHA-256
↓
request\_hash
\[ ] Use stable field ordering.
\[ ] Normalize optional/null values.
\[ ] Never hash a raw language map/object whose iteration order is not guaranteed.
\[ ] Do not include volatile headers or request IDs in the logical request hash.

# 10\. Concurrency Design — Sender/Receiver Locking

For transfers between two accounts, choose a deterministic lock order to reduce deadlocks. A simple rule is to lock accounts in ascending stable ID order, then validate the sender's available balance. This is a prototype pattern, not a universal production solution.
ids := sort(\[senderID, receiverID])

for id in ids:
SELECT id, balance
FROM accounts
WHERE id = $1
FOR UPDATE;

\-- now evaluate sender balance
-- mutate sender/receiver
-- write ledger
-- commit

## 10.1 Concurrency proof cases



# 11\. Timeout-After-Commit Design

This is the critical distributed-systems edge case. The client can time out after the server has committed. A retry must query/reuse the existing logical operation instead of creating a new one.
Request P
|
+--> payment committed
|
+--> HTTP response lost
|
retry
|
v
idempotency key
|
+--> existing completed result

# 12\. Bank Adapter — Typed Result Contract

type OperationOutcome int

const (
OutcomeCommitted OperationOutcome = iota
OutcomeRejected
OutcomeUnavailable
OutcomeUnknown
)

type OperationResult struct {
Outcome OperationOutcome
ExternalReference string
Message string
}
The exact names may change, but the semantic distinction must remain: rejected, unavailable-before-execution, and unknown-outcome are not the same thing.

# 13\. Customer Frontend — Network State Integration

const paymentLabels = {
PROCESSING: "Processing",
COMPLETED: "Payment successful",
FAILED: "Payment failed",
PENDING\_RECONCILIATION: "Awaiting confirmation",
OFFLINE\_QUEUED: "Payment queued offline"
};
\[ ] Customer sees a final success badge only for server-confirmed completion.
\[ ] PROCESSING and PENDING\_RECONCILIATION remain visually distinct from failure.
\[ ] Offline queue is visibly local/pending.
\[ ] Payment details are refreshed from the server after reconnect.
\[ ] Client never writes authoritative balance.

# 14\. API Test Cases — Concrete Examples

Case: same key twice
1st POST /payments -> 201/200 with paymentId=P
2nd POST /payments same key + body -> same paymentId=P
DB count(payment logical ID) == 1

Case: same key, different amount
1st POST -> paymentId=P
2nd POST same key amount=9999
Expected: IDEMPOTENCY\_CONFLICT
DB count logical payments == 1

Case: hot balance
initial balance = 10000
100 concurrent requests × 200
Expected:
successful debit total <= 10000
minimum balance >= 0
ledger conservation == true

# 15\. Security Hardening Checklist

\[ ] Password hashing uses a slow password hash.
\[ ] JWT/session expiry is enforced.
\[ ] Object-level authorization is server-side.
\[ ] SQL uses parameterized queries/ORM-safe parameters.
\[ ] Request body size is bounded.
\[ ] Sensitive values are not logged.
\[ ] Chaos/admin endpoints are not accessible to CUSTOMER/MERCHANT.
\[ ] Secrets are loaded from environment/config, never source-controlled.

# 16\. Implementation Milestones



# 17\. Copilot Prompt — Build Payment Core

Read this Member 1 manual and inspect the existing repository.
Do not implement the entire payment system.
Implement only the next missing milestone: \[MILESTONE].

First inspect:

* current schema/migrations
* payment service/repository
* tests
* BankAdapter contract
* existing state/event definitions

Then propose the smallest implementation plan.
After approval, implement it, add focused tests, run them, and report:
files changed, transaction boundary, invariants protected, tests, assumptions, risks.
Do not add unrelated dependencies or features.

# 18\. Copilot Prompt — Review Financial Correctness

Audit \[FILE/MODULE] as if reviewing production financial code.
Do not modify.
Find:

* race conditions,
* check-then-act bugs,
* broken transaction boundaries,
* duplicate-payment paths,
* unsafe retries,
* timeout-after-commit mistakes,
* ledger/balance mismatch,
* invalid state transitions,
* authorization bypasses.
For each issue, give a concrete reproduction and a test case.

# 19\. Copilot Prompt — Finish a Feature

The implementation of \[FEATURE] exists.
Do not expand scope.

1. inspect diff
2. run focused tests
3. add missing adversarial tests
4. fix real defects
5. run package + integration tests
6. update payment-flow/database docs
7. summarize exact evidence that the feature is done
Do not claim completion without test evidence.

# 20\. Handoff Package to Team

\[ ] Push the feature branch.
\[ ] Open PR with schema/API/test notes.
\[ ] Attach any seed/reset instructions.
\[ ] Document breaking changes.
\[ ] Give M2 the bank adapter + error contract.
\[ ] Give M3 the canonical ledger fields + ordering contract.
\[ ] Record architecture decisions in docs/decisions.md.

# 21\. Member 1 Final Mastery Questions

Q. Where exactly does financial atomicity begin and end?
Q. Why cannot a SELECT balance followed by UPDATE be considered safe concurrency?
Q. How does a unique idempotency constraint interact with two simultaneous requests?
Q. Why is the ledger more auditable than a mutable balance alone?
Q. How do you handle a timeout after a bank may already have executed?
Q. Why does the client never become the source of truth for balance?
Q. Which parts of the implementation would change when scaling horizontally?

|Boundary|Contract|
|-|-|
|Frontend → API|HTTP/HTTPS JSON|
|Frontend ↔ live state|WebSocket for relevant events|
|Payment switch → banks|BankAdapter interface over HTTP or gRPC|
|Workers → DB|PostgreSQL|
|Optional events|In-process abstraction first; Redis Streams only when justified|



|Area|You own|Exit evidence|
|-|-|-|
|Auth/RBAC|Registration, login, hashing, token/session validation, role checks|Protected APIs enforce access rules.|
|Users/accounts|Identity, recipient resolution, balances, account state|Seeded accounts are queryable and protected.|
|Payment lifecycle|Creation, validation, state machine, completion/failure|Invalid transitions are impossible.|
|Idempotency|Key uniqueness, request hash, result reuse|Retries cannot duplicate logical payment.|
|Ledger|Double-entry transaction + entries|Completed transfer is auditable and balanced.|
|Concurrency|Locks/versioning/transaction scope|Hot-account stress never overspends.|
|Bank A|First participant + adapter contract|Payment path crosses a real participant.|
|Customer UI|Authentication through transaction history|Browser-only normal payment works.|



|Field|Rule|
|-|-|
|id|UUID primary key|
|name|Required|
|phone|Unique synthetic identity|
|payment identifier|Unique recipient identifier|
|password\_hash|Argon2id/bcrypt; never plaintext|
|role|CUSTOMER / MERCHANT / OPS\_ADMIN|
|timestamps|created\_at / updated\_at|



|Field|Rule|
|-|-|
|id|UUID|
|user\_id|FK users|
|bank\_id|FK banks|
|account\_number|Unique|
|balance|Integer smallest currency unit|
|version|Increment on mutation if optimistic versioning is used|
|status|Explicit active/frozen policy|



|Transition|Rule|
|-|-|
|CREATED → VALIDATING|Request exists and can be processed|
|VALIDATING → LOCAL_SETTLEMENT|Local synchronous settlement is selected|
|LOCAL_SETTLEMENT → COMMITTED|Atomic local monetary commit succeeded|
|VALIDATING → ROUTING|Input/recipient validation passed|
|ROUTING → PROCESSING|A route is selected|
|PROCESSING → COMMITTED|Atomic monetary commit succeeded|
|COMMITTED → COMPLETED|Finalization/eventing succeeds|
|PROCESSING → FAILED|Safe terminal rejection before commit|
|PROCESSING → PENDING\_RECONCILIATION|Outcome cannot be proven safely|
|PENDING\_RECONCILIATION → REVERSED|Safe compensating path is available|



|Scenario|Expected behavior|
|-|-|
|First request|Create key/payment atomically.|
|Same key/same payload|Return original logical result.|
|Same key/different payload|IDEMPOTENCY\_CONFLICT.|
|Two callers race on same key|Only one logical payment.|
|Client timeout after server commit|Retry converges on original result.|
|Repeated queued replay|No second logical payment.|



|Case|Expected|
|-|-|
|Seed 1,000; 100×20 concurrent|At most 50 successes; never negative.|
|Exact-balance race|One success; later attempts reject.|
|Same key parallel race|One logical payment.|
|Different keys overspend|No negative balance.|
|Response lost after commit|Retry returns original operation.|



|Layer|Required tests|
|-|-|
|Unit|Amount validation, state transitions, request hashing, ledger math|
|Integration|PostgreSQL payment transaction, Bank A adapter|
|Concurrency|Hot account, same idempotency key race, retry-after-timeout|
|Security|Cross-user resource access, role enforcement|
|E2E|Login → pay → success → history → details|



|Week|Primary work|Gate|
|-|-|-|
|1|Repo/auth/schema foundation|Clone/build/login skeleton|
|2|Users/accounts/recipient resolution|Protected account view|
|3|Payment API/state machine/idempotency|Normal payment|
|4|Ledger/locking/concurrency/retry semantics|Stress tests green|
|5|BankAdapter + Bank A|Switch→Bank A flow|
|6|Stabilize + M3 data contract|Stable ledger data|
|7|Integrate M2 routing/health calls|Routing-safe payment path|
|8|Adversarial failures/observability|Timeout/failure cases reproducible|
|9|Offline state integration|Queued state correctly represented|
|10|Customer UX polish|Browser-only customer demo|
|11|Correctness reruns/paper evidence|Raw stress output|
|12|Hardening/review/fresh-clone run|Release green|



|Path / module|Build contents|Do not put here|
|-|-|-|
|backend/internal/auth|handlers, service, password hashing, auth middleware, role checks|Payment business logic|
|backend/internal/users|user repository/service, recipient resolution|Bank routing|
|backend/internal/accounts|account repository, balance operations, lock/version strategy|Frontend-only calculations|
|backend/internal/payments|request validation, state machine, orchestration, typed errors|Direct SQL scattered across handlers|
|backend/internal/idempotency|record lookup/insert, request hash, response snapshot|Generic cache semantics|
|backend/internal/ledger|ledger transaction + entries, balance reconstruction|Bank health|
|backend/internal/bank|BankAdapter, client, Bank A integration|Routing policy|
|backend/migrations|schema, constraints, indexes|Runtime schema mutation|
|frontend/src/pages/customer|customer screens|Admin-only diagnostics|
|frontend/src/features/payments|payment form/state mapping|Bank implementation|
|backend/tests/concurrency|hot-account and idempotency race tests|UI snapshot tests|



|Endpoint|Success|Important failures|Auth|
|-|-|-|-|
|POST /api/auth/register|User/session created|validation, duplicate identity|Public|
|POST /api/auth/login|Token/session|invalid credentials|Public|
|GET /api/auth/me|Current identity|401|Authenticated|
|GET /api/users/resolve/{id}|Recipient summary|not found|Authenticated|
|GET /api/accounts/me|Account/balance|403/404|Customer/Merchant as applicable|
|POST /api/payments|Payment result/state|idempotency conflict, insufficient balance, bank errors|Customer|
|GET /api/payments/{id}|Payment detail|403/404|Owner|
|GET /api/payments|History|403|Owner|



|Point of failure|Possible money effect|Required behavior|
|-|-|-|
|Validation|None|Reject before monetary transaction.|
|Idempotency race|None before winner|Exactly one logical operation wins.|
|Before balance lock|None|Safe retry.|
|After balance lock, before mutation|None|Rollback transaction.|
|After debit + before ledger write|Must roll back|Never commit partial monetary state.|
|After ledger write + before commit|Must roll back|No visible completion.|
|After DB commit, response lost|Committed|Retry returns original logical result.|
|Bank timeout / unknown outcome|Possibly unknown|Do not blindly reapply monetary effect.|



|Screen|Components/state|Backend dependency|
|-|-|-|
|Login|Form, validation, auth store|POST /auth/login|
|Register|Form, validation|POST /auth/register|
|Home|Balance card, recent list, status badge|/accounts/me + /payments|
|Pay|Recipient input, amount input, validation|resolve recipient|
|Confirm|Summary card + submit|POST /payments|
|Processing|State timeline|GET/WebSocket payment update|
|Success|Reference, amount, recipient|payment detail|
|Transactions|List/status filters|GET /payments|
|Details|Transaction timeline + route|GET /payments/{id}|



|Layer|Should know|Should not know|
|-|-|-|
|HTTP handler|HTTP status, DTO validation, auth context|SQL details, row locks, ledger math|
|Payment service|business workflow, states, idempotency|React/browser state|
|Repository|SQL, constraints, transactions|HTTP request objects|
|Bank adapter|bank protocol + typed outcomes|customer UI|
|Ledger|entries, conservation, balance reconstruction|routing score|



|Stage|Read/Write|Failure rule|
|-|-|-|
|Validate|read recipient/account state|reject invalid request; no monetary effect|
|Idempotency claim|insert/read idempotency row|race resolves through DB uniqueness|
|Payment create|insert payment|must not create second logical payment|
|Route|read-only bank health/routing|no monetary mutation|
|Process|lock required accounts|re-read balances after lock|
|Ledger|insert transaction + entries|same DB transaction as balance mutation|
|Commit|commit DB transaction|only after all expected writes succeed|
|Response|read committed result|return final or explicit pending state|



|Case|Requests|Expected|
|-|-|-|
|Same sender, enough for one|2 parallel payments|one success, one rejection if combined > balance|
|Same sender, enough for many|N parallel payments|sum successful debits <= initial balance|
|A→B and B→A|parallel opposite transfers|no persistent deadlock/partial state|
|Same idempotency key|N parallel identical requests|one logical payment|
|Same key, changed payload|parallel conflicting requests|conflict; no second operation|



|Milestone|Completion test|
|-|-|
|M1-A Auth|Customer can register/login and access own account only|
|M1-B State Machine|Invalid transition test fails correctly|
|M1-C Idempotency|Same key does not create second logical payment|
|M1-D Ledger|Every completed payment has balanced entries|
|M1-E Concurrency|Hot-account stress produces no negative balance|
|M1-F Bank Adapter|Payment switch calls Bank A through interface|
|M1-G Customer UI|Browser payment journey is complete|



