# TransactX M20-R.5.4: Payment Reservation Semantics Reconciliation & Final Lifecycle Acceptance Report

**Milestone:** M20-R.5.4  
**Project:** TransactX — Resilient Payment Infrastructure Research Prototype  
**Date:** November 24, 2026  
**Document ID:** `DOC-M20R54-ACCEPTANCE-REPORT-01`  
**Classification:** Formal Engineering & Verification Acceptance Package  
**Target Baseline:** `feat/frontend-rebuild-v4` (Targeting `release/transactx-v2` via PR #32)

---

## 1. Executive Summary

Milestone **M20-R.5.4** was commissioned to resolve a critical semantic ambiguity identified during post-test inspection of M20-R.5.3: the conflation of central ledger capture with client-side active payment reservation retention in HTTP state contract test suites.

In TransactX's durable payment architecture:
1. **Central State Terminality (`IsTerminal`)**: Strictly limited to `COMPLETED`, `FAILED`, and `REVERSED`.
2. **Customer Active Reservation Lock (`ActiveReservationMustRemain`)**: Governs the client-side IndexedDB `active-reservations` store. All in-flight intermediate states, offline states, network timeouts, delayed commit 404s, 500 DB errors, and 401/403 auth errors **must maintain the reservation lock** to prevent double-spending and UI drift. Only terminal outcomes release this active spend lock.
3. **Server Idempotency Record Durability (`IdempotencyRecordMustRemain`)**: Governs PostgreSQL `idempotency_records`. Once created, the record must persist indefinitely across all intermediate and terminal states to ensure duplicate request protection.

Through targeted backend contract test refactoring, frontend asynchronous exception hardening, and browser end-to-end recovery verification, Milestone M20-R.5.4 has verified complete lifecycle correctness across the entire stack.

---

## 2. Remediation & Implementation Details

### 2.1 Backend Contract Test Refactoring
In `backend/internal/http/payment_handler_test.go`:
- Removed the ambiguous `wantReservationRetain` field.
- Introduced explicit boolean invariants:
  - `wantTerminal`: Asserts whether `payments.IsTerminalState(state)` is true.
  - `activeReservationMustRemain`: Asserts whether client-side active reservation lock must remain locked.
  - `idempotencyRecordMustRemain`: Asserts whether server-side PostgreSQL idempotency record remains durable.
- Updated 25 test cases for `POST /api/payments` and 23 test cases for `GET /api/payments/idempotency/:key`.
- All 48 test cases now enforce the exact TransactX reservation lifecycle invariants.

### 2.2 Frontend Exception Handling Hardening
In `frontend/src/features/customer/SendMoneyPage.tsx`:
- Audited the catch block in the payment dispatch and polling loops.
- Previously, unexpected HTTP responses (such as HTTP 401 Unauthorized due to an expired token or HTTP 403 Forbidden) fell through to a generic handler that called `unresolvedStore.resolveTerminal(requestID, "RESOLVED_FAILED")`. This prematurely released the client-side active reservation lock while the transaction was potentially executing centrally.
- Remediated by adding explicit handling for status 401 and 403:
  - The payment state is preserved as `UNCERTAIN`.
  - The active reservation in IndexedDB (`active-reservations`) **remains locked**.
  - Historical intent in `unresolved-intents` is preserved.
  - The UI presents an authentication prompt without releasing the customer's spend lock.

### 2.3 Browser End-to-End Recovery Fixture (`E2E-17`)
In `frontend/browser/unresolved-payment-lifecycle.fixture.spec.ts`:
- Added test case `E2E-17: Preserves active reservation through reload, 404 delayed commit, and 500 DB error before terminal completion`.
- Verified the complete end-to-end customer journey:
  1. `POST /api/payments` returns 202 `PROCESSING`.
  2. Browser window is reloaded mid-flight (`page.reload()`).
  3. Client boots up and rehydrates active reservation from IndexedDB; form remains disabled.
  4. Status query returns 202 `PENDING_RECONCILIATION` -> Reservation remains locked.
  5. Status query returns 404 `PAYMENT_NOT_FOUND` (simulating replica delay) -> Reservation remains locked.
  6. Status query returns 500 `INTERNAL_ERROR` (simulating DB interruption) -> Reservation remains locked.
  7. Status query returns 200 `COMPLETED` -> Client atomically releases active reservation from `active-reservations` (count = 0) and updates `unresolved-intents` to `RESOLVED_COMPLETED`.
- Playwright suite executed with 17 passed tests.

---

## 3. Comprehensive Verification & Test Results

### 3.1 Backend Test Execution
- **HTTP State Contract Suite:**
  - `TestPaymentAPI_HTTPStateContract_PostPayments`: 25/25 PASS (0.00s)
  - `TestPaymentAPI_HTTPStateContract_GetIdempotencyKey`: 23/23 PASS (0.04s)
- **Race Detector:**
  - `go test -race -count=1 ./internal/http`: PASS (1.499s, 0 data races)
  - `go test -race -count=1 ./...`: PASS (All packages clean)
- **Vulnerability Audit:**
  - `govulncheck ./...`: PASS (0 known vulnerabilities detected)

### 3.2 Frontend Test Execution
- **Unit & Component Tests:**
  - `npm test`: PASS
  - `npm run test:money`: PASS
- **Production Build:**
  - `npm run build`: PASS (Vite production bundle compiled with 0 errors)
- **Browser Playwright Lifecycle Tests:**
  - `npx playwright test browser/unresolved-payment-lifecycle.fixture.spec.ts`:
  - 17/17 tests PASS (57.2s total execution time)

---

## 4. Release Governance & Artifact Register

| Artifact Path | Description | Status |
| :--- | :--- | :--- |
| `docs/TRANSACTX_M20R5_4_RESERVATION_SEMANTICS_CORRECTION.md` | Theoretical and architectural correction of reservation retention semantics | Validated & Committed |
| `docs/TRANSACTX_M20R5_4_FINAL_PAYMENT_LIFECYCLE_MATRIX.csv` | 48-row exhaustive verification matrix across all endpoints and states | Validated & Committed |
| `docs/TRANSACTX_M20R5_4_ACCEPTANCE_REPORT.md` | Formal engineering completion and acceptance package | Validated & Committed |
| `frontend/src/features/customer/SendMoneyPage.tsx` | Hardened asynchronous error handling for 401/403 responses | Validated & Tested |
| `frontend/browser/unresolved-payment-lifecycle.fixture.spec.ts` | E2E-17 test fixture asserting reservation persistence | Validated & Tested |
| `backend/internal/http/payment_handler_test.go` | 48 Go HTTP contract subtests with decoupled reservation invariants | Validated & Tested |

**Git Governance:**
- Branch: `feat/frontend-rebuild-v4`
- Pull Request: #32 (Open, Draft, and Unmerged)
- Upstream Target: `release/transactx-v2`

---

## 5. Formal Acceptance Verdict

Milestone **M20-R.5.4** is **ACCEPTED WITHOUT RESERVATIONS**. All reservation retention ambiguities are closed, customer balance and reservation durability guarantees are proven across page reloads and backend faults, and both backend and frontend test suites pass with 100% compliance.
