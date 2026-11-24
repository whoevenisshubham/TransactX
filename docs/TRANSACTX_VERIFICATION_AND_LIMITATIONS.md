# TransactX: Verification Evidence, Test Taxonomy & Research Limitations

**Project:** TransactX — A Resilient Payment Infrastructure for the Next Generation of Digital Payments  
**Document ID:** `DOC-VERIF-LIMIT-01`  
**Milestone:** M20-R.6 (Final Project Freeze)  
**Classification:** Scientific Verification & Limitations Disclosure  
**Date:** November 2026  

---

## 1. Executive Summary & Epistemic Boundaries

This document provides a rigorous, transparent account of the test evidence supporting TransactX. In alignment with academic integrity principles, this document explicitly distinguishes between:
1. **Mocked Unit & Contract Tests:** Verifying component logic and HTTP serialization under simulated inputs.
2. **Browser Storage & Lifecycle Fixtures:** Verifying client-side state machines, IndexedDB persistence, and user interfaces via Playwright.
3. **Real PostgreSQL Integration Tests:** Verifying database schemas, ACID transactional boundaries, constraints, and concurrency against disposable PostgreSQL instances.
4. **Staging System Tests:** Verifying multi-process interaction through Caddy, the Go API, PostgreSQL, and bank simulator microservices.

> [!WARNING]
> **Research Prototype Classification:** TransactX is a simulated payment infrastructure prototype developed for academic research. It is **not** an operational implementation of live Indian UPI/NPCI rails, ISO 20022 banking switches, or PCI-DSS Level 1 certified payment processors. A clean build or zero-vulnerability scan indicates code hygiene, not institutional or banking-grade regulatory certification.

---

## 2. Test Evidence Taxonomy & Categorization

To avoid conflating mocked behavior with real database guarantees, all tests across the repository are categorized as follows:

| Layer | Harness / Framework | Execution Command | Scope & What It Actually Proves | Does It Use Real PostgreSQL? |
|---|---|---|---|:---:|
| **Go HTTP Contract Suite** | Go `net/http/httptest` | `go test ./internal/http` | Verifies HTTP status codes, error envelopes, and API state serialization across 48 scenarios. | **No** (Uses mocked repository hooks) |
| **Go Domain Unit Tests** | Go `testing` | `go test ./internal/...` | Verifies transition rules, Merkle leaf hashing, circuit breakers, and rate limiting logic. | **No** (Pure in-memory unit tests) |
| **PostgreSQL Integration Tests** | Go `testing` + `pgxpool` | `go test -run "TestRealPostgres|TestConcurrency" ./internal/payments` | Exercises real PostgreSQL 16 schema, 33 migrations, unique constraints, and ACID transactions in disposable databases. | **Yes** (Real PostgreSQL 16) |
| **Go Race Detector** | Go `-race` flag | `go test -race ./...` | Proves absence of data races across all concurrent goroutines, channels, and worker pools. | **Both** (In-memory & DB tests) |
| **Frontend Unit & Money** | Node.js Test Runner | `npm test; npm run test:money` | Verifies role routing, 64-bit integer paise arithmetic, and offline queue state machines across 134 subtests. | **No** (Node.js runtime) |
| **Browser Fixture Suite** | Playwright (Headless Chromium) | `npx playwright test` | Verifies real IndexedDB schema, tab isolation, reload survival, and UI states across 19 lifecycle tests and 157 total tests. | **No** (Mocked network routes, real browser IndexedDB) |
| **Offline Shell Suite** | Playwright + Preview | `npm run test:browser:offline-shell` | Verifies that production Vite bundle reloads fully offline with service worker caching. | **No** (Static web server) |
| **Local Staging Supervised Suite** | PowerShell Staging Scripts | `./scripts/test-local-staging-lifecycle.ps1` | Verifies Caddy ingress, Go API, PostgreSQL, Bank A, Bank B, and Worker processes running concurrently. | **Yes** (Real PostgreSQL + multi-process) |

---

## 3. Real PostgreSQL Integration Evidence

### 3.1 Idempotency Durability & Concurrency Verification
The test `TestRealPostgres_IdempotencyDurabilityLifecycle` (in `backend/internal/payments/idempotency_concurrency_test.go`) executes against an ephemeral PostgreSQL database created with all 33 migrations:

1. **Atomic Insertion:** Proves that `payments` and `idempotency_records` rows are committed in the same transaction.
2. **Key Mapping:** Confirms that `(user_id, key)` maps deterministically to `payment_id` and SHA-256 payload hash.
3. **Replay Deduplication:** Confirms that replaying the identical payment returns the original payment identity (`duplicate == true`) without inserting a new payment row.
4. **Conflict Rollback:** Confirms that replaying with a conflicting payload (mismatched amount or note) is rejected with `ErrIdempotencyConflict` and causes zero database mutations.
5. **Zero Double-Debit Proof:** Inspects `accounts` balances and counts `ledger_entries` (1 debit, 1 credit). Proves that repeated duplicate replays cause **zero** secondary debits.
6. **Post-Completion Durability:** Retrieves the completed payment (`StateCompleted`) by idempotency key.
7. **Post-Failure Durability:** Simulates bank adapter failure, executes recovery transitioning the payment to `StateFailed`, and proves that the idempotency record remains retrievable in its terminal failed state.
8. **Server Restart / Connection Recovery:** Instantiates brand-new repository and service objects against the database and confirms that existing records remain retrievable.
9. **Disposability:** Ensures that test teardown drops only the isolated `tx_m20r2_*` database using `DROP DATABASE ... WITH (FORCE)`.

---

## 4. Static Analysis, Race Conditions & Vulnerabilities

### 4.1 Go Race Detector
- **Command:** `cd backend; go test -race -count=1 ./...`
- **Result:** **PASSED (0 data races detected)** across all backend packages including `internal/payments`, `internal/recovery`, `internal/bank`, and `internal/demohost`.

### 4.2 Static Code Analysis & Type Checking
- **Go Vet:** `go vet ./...` completed with exit code 0 (zero lint warnings).
- **TypeScript Compiler:** `tsc -b --pretty false` compiled with exit code 0 (zero type errors across 2,381 modules).

### 4.3 Vulnerability Audit
- **Command:** `govulncheck ./...`
- **Result:** `No vulnerabilities found` across all compiled Go dependencies.

---

## 5. Hosted GitHub Actions Continuous Integration Evidence

All verification gates are automated and proven on hosted GitHub Actions runners on the feature branch commit `5a472e248d4255004d58a330dc31f721089d4654`:

| Hosted Workflow | Run ID | Status | Conclusion | URL |
|---|---|---|---|---|
| **Release validation** | `45253972675` | `completed` | **`success`** | [Run 45253972675](https://github.com/whoevenisshubham/TransactX/actions/runs/45253972675) |
| **Demo host validation** | `45253972899` | `completed` | **`success`** | [Run 45253972899](https://github.com/whoevenisshubham/TransactX/actions/runs/45253972899) |
| **Auth & PostgreSQL acceptance** | `45253972979` | `completed` | **`success`** | [Run 45253972979](https://github.com/whoevenisshubham/TransactX/actions/runs/45253972979) |

---

## 6. Known System Limitations & Threats to Validity

To ensure an academically rigorous evaluation, the following architectural and experimental limitations are formally acknowledged:

### 6.1 Infrastructure & Deployment Topology Limitations
1. **Single-Host Topology:** In the local staging environment, Caddy, Go API, PostgreSQL, and bank simulators run on a single host. Network latency between nodes is sub-millisecond, which does not reflect cross-region WAN partition behavior.
2. **Single Point of Failure (SPOF):** The PostgreSQL database and Caddy reverse proxy represent single points of failure in the local staging topology. Multi-region Active-Active PostgreSQL clustering was outside the project scope.
3. **Vercel Static Preview:** The Vercel preview deployment hosts static frontend assets only and connects to mock/demo endpoints. It does not interface with a live, distributed backend database cluster.

### 6.2 Payment Protocol & Rail Limitations
1. **Simulated Bank Rails:** Bank A and Bank B are simulated HTTP microservices with artificial latency and error injection. Real-world ISO 8583/20022 wire formats, HSM cryptographic signing, and NPCI switch clearing protocols are simplified for research tractability.
2. **Idempotency Record Retention:** The current system does not enforce an automated time-to-live (TTL) pruning daemon for `idempotency_records`. In a multi-billion transaction production network, an archival partition strategy (e.g. 90-day retention with cold storage offload) would be required.
3. **Offline Replay Window:** Offline payment acceptance relies on trusted local key storage in IndexedDB. In a production consumer deployment, hardware-backed secure enclaves (e.g. Android StrongBox, Apple Secure Enclave) would be necessary to prevent client clock tampering and replay attacks.
