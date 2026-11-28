# TransactX M20-R.5.4: Payment Reservation Semantics & Durability Correction

**Project:** TransactX — Resilient Payment Infrastructure Research Prototype  
**Milestone:** M20-R.5.4  
**Date:** November 24, 2026  
**Document ID:** `DOC-M20R54-RESERVATION-SEMANTICS-01`  
**Target Baseline:** `feat/frontend-rebuild-v4` (Targeting `release/transactx-v2` via PR #32)

---

## 1. Executive Summary & Problem Identification

During the closure of Milestone M20-R.5.3, exhaustive HTTP response contract verification was established for both payment submission (`POST /api/payments`) and idempotency status retrieval (`GET /api/payments/idempotency/:key`).

However, an independent semantic audit identified an ambiguity in test assertion terminology:
In the M20-R.5.3 test harness (`backend/internal/http/payment_handler_test.go`), a test assertion field named `wantReservationRetain` was evaluated against `tc.state == payments.StateCompleted`. This conflated two distinct architectural layers:
1. **Server-side Ledger Capture / Final Settlement:** In which completed payment funds are permanently transferred on the central ledger.
2. **Client-side Active Reservation Lock:** In which client funds are held locked in local durable storage (`active-reservations` object store in IndexedDB) to prevent double-spending, UI drift, and conflicting re-submissions while a payment's outcome remains non-terminal or in flight.

This conflation led to an apparent contradiction where non-terminal states (`PROCESSING`, `COMMITTED`, `PENDING_RECONCILIATION`) were tagged with `wantReservationRetain: false` in test tables, despite the foundational TransactX safety invariant requiring that active reservations **must remain locked** until a terminal outcome is proven.

Milestone M20-R.5.4 formally remediates this ambiguity, disambiguating the lifecycle into three orthogonal, formally tested invariants across both the Go backend and TypeScript frontend.

---

## 2. The Three Orthogonal Lifecycle Invariants

TransactX decomposes payment state and durability into three distinct axes:

```
+----------------------------------------------------------------------------------------------------+
|                                    TRANSACTX PAYMENT LIFECYCLE                                    |
+----------------------------------------------------------------------------------------------------+
|                                                                                                    |
|  1. Central State Terminality (`IsTerminal`)                                                        |
|     - Determined solely by central state machine: `COMPLETED`, `FAILED`, `REVERSED`.              |
|     - Intermediate states: `CREATED`, `VALIDATING`, `LOCAL_SETTLEMENT`, `ROUTING`, `PROCESSING`,   |
|       `BANK_SETTLED_CENTRAL_PENDING`, `COMMITTED`, `PENDING_RECONCILIATION`.                      |
|                                                                                                    |
|  2. Customer Active Reservation Lock (`ActiveReservationMustRemain`)                               |
|     - Governs the client-side IndexedDB `active-reservations` store.                               |
|     - IN-FLIGHT / UNCERTAIN (Intermediate, 404 delayed commit, 500 DB err, 401/403):              |
|       --> Active reservation MUST REMAIN LOCKED (Prevents double spending / UI reset).             |
|     - TERMINAL (`COMPLETED`, `FAILED`, `REVERSED`):                                                |
|       --> Active reservation is SAFELY RELEASED from `active-reservations`.                        |
|       --> Historical intent in `unresolved-intents` is preserved permanently (`RESOLVED_*`).      |
|                                                                                                    |
|  3. Server Idempotency Record Durability (`IdempotencyRecordMustRemain`)                           |
|     - Governs PostgreSQL `idempotency_records` table.                                              |
|     - Once a payment is created or dispatched on the backend, the idempotency record               |
|       MUST REMAIN DURABLE FOREVER across all intermediate and terminal states.                     |
|                                                                                                    |
+----------------------------------------------------------------------------------------------------+
```

### Invariant Definitions

1. **`IsTerminal`**:
   - `true` if and only if the central payment state is `COMPLETED`, `FAILED`, or `REVERSED`.
   - All other states (central intermediate, offline lifecycle, unknown states) are strictly non-terminal (`false`).

2. **`ActiveReservationMustRemain`**:
   - `true` for all non-terminal states, network timeouts, delayed commit 404s, 500 internal errors, 401/403 authorization interruptions, and corrupt/unknown central states. The client UI remains locked in an unresolved/in-flight view, preventing double debit.
   - `false` once a terminal state (`COMPLETED`, `FAILED`, `REVERSED`) is delivered to the client, triggering the atomic deletion of the lock from `active-reservations`.
   - `false` for pre-dispatch validation failures (e.g., 400 Bad Request, 409 Insufficient Funds, 404 Source Account Not Found) where no payment was accepted or dispatched.

3. **`IdempotencyRecordMustRemain`**:
   - `true` for any payment that has been accepted or persisted by the server. Even when the payment reaches `COMPLETED`, `FAILED`, or `REVERSED`, the server-side idempotency record is permanently preserved to guarantee deduplication of replayed requests.
   - `false` only when the request failed before reaching the idempotency engine (e.g., malformed JSON syntax, missing credentials, unknown route).

---

## 3. Frontend Implementation Audit & Hardening

### Audit Findings
An audit of `frontend/src/features/customer/SendMoneyPage.tsx` revealed an edge case in asynchronous exception handling:
When an error occurred during payment submission or status query, a generic catch block checked for specific network errors. In the `else` branch, non-network exceptions triggered:
```typescript
unresolvedStore.resolveTerminal(requestID, "RESOLVED_FAILED");
```
If an API request returned HTTP 401 (e.g., JWT expired while payment was in-flight) or HTTP 403 (role check latency), the client code would treat the error as terminal failure, discarding the active reservation and releasing the client-side spend lock even though the payment was actively processing on the backend.

### Remediation Applied
In `frontend/src/features/customer/SendMoneyPage.tsx`:
- Added explicit HTTP status checking for `401 Unauthorized` and `403 Forbidden`.
- When a 401 or 403 is received during dispatch or status check, the transaction is marked as `UNCERTAIN`.
- The active reservation in IndexedDB (`active-reservations`) is **preserved** and remains locked.
- The UI directs the user to re-authenticate without discarding the unresolved transaction lock.

---

## 4. End-to-End Browser Lifecycle Verification (`E2E-17`)

A comprehensive browser recovery and reservation test was implemented in `frontend/browser/unresolved-payment-lifecycle.fixture.spec.ts`:
- **Scenario:** `E2E-17: Preserves active reservation through reload, 404 delayed commit, and 500 DB error before terminal completion`
- **Execution Flow:**
  1. POST `/api/payments` returns HTTP 202 `PROCESSING`.
  2. Browser page reload (`page.reload()`) occurs while payment is in-flight.
  3. Client boots up, hydrates from IndexedDB, and verifies that the active reservation is still present with state `PROCESSING`, and the submit form is disabled.
  4. Status polling returns HTTP 202 `PENDING_RECONCILIATION` -> Active reservation verified retained.
  5. Status polling returns HTTP 404 `PAYMENT_NOT_FOUND` (simulating delayed replica commit) -> Active reservation verified retained; error message displayed without unlocking form.
  6. Status polling returns HTTP 500 `INTERNAL_ERROR` (simulating database connection failure) -> Active reservation verified retained; error displayed without unlocking form.
  7. Status polling returns HTTP 200 `COMPLETED` -> Terminal state reached.
  8. Client atomically deletes active reservation from `active-reservations` (store count drops to 0) while historical record in `unresolved-intents` is marked `RESOLVED_COMPLETED`.
- **Result:** Playwright fixture passed with 100% compliance across all 17 browser lifecycle test cases.

---

## 5. Backend HTTP Contract Invariant Verification

The Go contract test suite in `backend/internal/http/payment_handler_test.go` was updated to test both `activeReservationMustRemain` and `idempotencyRecordMustRemain` across 48 distinct test scenarios (25 `POST /api/payments` and 23 `GET /api/payments/idempotency/:key`).

All 48 test cases passed locally under `go test -v` and `-race`:
- `TestPaymentAPI_HTTPStateContract_PostPayments`: 25 passed.
- `TestPaymentAPI_HTTPStateContract_GetIdempotencyKey`: 23 passed.
- Total HTTP contract tests: 48 passed (0 race conditions, 0 memory leaks).
