# TransactX M2-8: Resilience Experiments and Raw Benchmark Outputs

This document details the reproducibility harness, methodology, parameters, and verified raw outputs for the TransactX M2 resilience experiments.

All experiments are deterministic, execute over the production-grade M2 domain implementations (`internal/payments`, `internal/health`, `internal/circuit`, `internal/chaos`, `frontend/src/offlineQueue.ts`, `frontend/src/offlineReplay.ts`), and output raw machine-readable JSON and CSV files under `artifacts/experiments/`.

No benchmark numbers are fabricated or estimated. Every metric reported below is directly derived from executed benchmark runs.

---

## Architecture and Safety Rules

1. **Central Financial Authority**: The payment switch remains the single authoritative decision-maker for financial status and ledger invariants.
2. **BankAdapter Contract Unchanged**: All bank interactions use the frozen `bank.BankAdapter` domain interface. No mock or experiment alters method signatures or bypasses adapter boundaries.
3. **Identity & Ownership**: Sender and receiver bank ownership is never forged or mutated to achieve rerouting. Routing decisions choose among valid `RouteCandidate` execution targets (`ExecutionTargetID`) for fixed bank identities.
4. **Idempotency & Replay Semantics**: Retries of the same logical intent preserve the original `idempotencyKey` and `clientRequestId`. Duplicate executions on the authoritative adapter are tracked and strictly verified to be zero.
5. **Unknown Outcome Preservation**: Unknown outcomes remain `PENDING`/reconciliation. They are never converted into simulated successes for benchmarks.
6. **No External Infrastructure**: Experiments run without Docker, Kafka, Redis, or Kubernetes.

---

## Environment Metadata

Every benchmark run captures environmental metadata embedded directly into each JSON output:

- **Git Commit**: Full HEAD commit SHA
- **Platform / OS**: `windows/amd64` (or host platform)
- **Go Version**: `go1.27.1`
- **Node.js Version**: `v24.14.0`
- **Deterministic Seed**: Default `42`
- **Run ID**: Unique timestamped run identifier
- **Scenario Parameters**: Explicit scenario parameters map

---

## Experiment 1 — Static Routing vs. Health-Aware Routing

### Objective
Measure the resilience improvement of deterministic health-aware adaptive routing over a fixed static routing baseline when the primary execution target experiences intermittent degradation.

### Configuration
- **Baseline**: Static candidate ordering (`SelectionModeStatic`), always selecting the designated static baseline (`CANDIDATE-A` -> `RAIL-A`).
- **Proposed**: Health-aware selector (`SelectionModeAdaptive`), scoring execution targets via `health.HealthSnapshotProvider` (`Score = 0.5*LatencyScore + 0.25*AvailabilityScore + 0.25*SuccessScore`).
- **Workload**: 300 sequential requests.
  - Phase 1 (Requests 0–74): Both targets healthy (10–14 ms latency, 100% success).
  - Phase 2 (Requests 75–224): Primary target `RAIL-A` degraded (80% failure rate, 150 ms latency). Backup `RAIL-B` healthy.
  - Phase 3 (Requests 225–299): Both targets healthy.

### Measured Results (Run `routing-20260921-160028`)

| Metric | Baseline (Static) | Proposed (Adaptive) | Difference / Impact |
| :--- | :--- | :--- | :--- |
| **Total Requests** | 300 | 300 | 0 |
| **Successes** | 178 | 300 | +122 (+68.5%) |
| **Failures** | 122 | 0 | -122 (-100%) |
| **Success Rate** | 59.33% | 100.00% | +40.67 pp |
| **P50 Latency** | 14.00 ms | 15.00 ms | +1.00 ms |
| **P95 Latency** | 162.00 ms | 19.00 ms | -143.00 ms (-88.3%) |
| **P99 Latency** | 168.00 ms | 19.00 ms | -149.00 ms (-88.7%) |
| **Max Latency** | 169.00 ms | 19.00 ms | -150.00 ms |
| **Traffic Share** | RAIL-A: 100% (300) | RAIL-A: 25% (75), RAIL-B: 75% (225) | Dynamic failover |
| **Invariants Satisfied** | Yes | Yes | Zero accounting drift |

### Raw Artifact Paths
- JSON: `artifacts/experiments/routing/routing-20260921-160028.json`
- CSV: `artifacts/experiments/routing/routing-20260921-160028.csv`

---

## Experiment 2 — Bank Outage and Circuit Isolation

### Objective
Evaluate how circuit breaker integration (`internal/circuit`) detects a complete bank rail outage injected via the M2-6 Chaos Controller (`internal/chaos`), transitions states (`CLOSED` -> `OPEN` -> `HALF_OPEN`), and isolates the failing rail.

### Configuration
- **Fault Injection**: M2-6 `ChaosController` scenario `BANK_OUTAGE` applied to target `RAIL-A` via `ChaosAdapter` from request 100 to 199 (100 requests).
- **Circuit Breaker Configuration**:
  - `FailureThreshold`: 3
  - `SuccessThreshold`: 2
  - `OpenCooldown`: 3.0s
  - `HalfOpenProbeLimit`: 1
  - `RestorationSteps`: 2
- **Baseline**: Static routing without circuit breaker, repeatedly attempting `RAIL-A`.
- **Proposed**: Adaptive routing with circuit eligibility hook (`cb.EligibilityHook`).

### Measured Results (Run `outage-20260921-160028`)

| Metric | Baseline (Static) | Proposed (Circuit-Aware) | Notes |
| :--- | :--- | :--- | :--- |
| **Total Requests** | 300 | 300 | 100 requests during outage |
| **Successes** | 200 | 297 | Only 3 failures before trip |
| **Failures** | 100 | 3 | Bounded by `FailureThreshold=3` |
| **Success Rate** | 66.67% | 99.00% | +32.33 pp |
| **Target Failures (RAIL-A)** | 100 | 3 | Immediate isolation |
| **Isolation Time** | N/A (never isolated) | 200.00 ms | 3 requests (2 ticks) to trip |
| **Traffic Share (During Outage)** | RAIL-A: 100, RAIL-B: 0 | RAIL-A: 3, RAIL-B: 97 | 97% redirected to healthy rail |
| **Traffic Share (After Outage)** | RAIL-A: 100, RAIL-B: 0 | RAIL-A: 0, RAIL-B: 100 | Half-open probe gating |
| **Circuit Transitions** | None | CLOSED -> OPEN -> HALF_OPEN | Logged and audited |

### Automated Invariants
- `Request Accounting`: `total == successes + failures` (Verified)
- `Circuit Isolation`: Rail-A isolated within 200ms of outage start (Verified)
- `Eligibility Conformance`: Zero requests routed to OPEN circuit target (Verified)

### Raw Artifact Paths
- JSON: `artifacts/experiments/outage/outage-20260921-160028.json`
- CSV: `artifacts/experiments/outage/outage-20260921-160028.csv`

---

## Experiment 3 — Latency Degradation and Traffic Shift

### Objective
Measure route selector responsiveness to transient latency degradation injected via M2-6 `LATENCY` chaos scenario on `RAIL-A` (+180ms delay), comparing static routing vs. health-aware adaptive scoring.

### Configuration
- **Workload**: 300 requests.
  - Phase 1 (0–99): Healthy baseline (`RAIL-A`: 10–14ms, `RAIL-B`: 15–19ms).
  - Phase 2 (100–199): Injected latency on `RAIL-A` (+180ms -> 190–194ms).
  - Phase 3 (200–299): Recovery back to healthy baseline.
- **Health Configuration**: `LatencyLowerBound=10ms`, `LatencyUpperBound=150ms`, `LatencyWeight=0.50`.

### Measured Results (Run `latency-20260921-160028`)

| Metric | Baseline (Static) | Proposed (Health-Aware) | Impact |
| :--- | :--- | :--- | :--- |
| **Total Requests** | 300 | 300 | |
| **Success Rate** | 100.00% | 100.00% | Both succeed |
| **P50 Latency** | 12.00 ms | 15.00 ms | Shift to RAIL-B baseline |
| **P95 Latency** | 193.00 ms | 18.00 ms | -175.00 ms (-90.7%) |
| **P99 Latency** | 193.00 ms | 191.00 ms | Initial detection window |
| **Max Latency** | 193.00 ms | 193.00 ms | First degraded sample |
| **Traffic Share** | RAIL-A: 300 (100.0%) | RAIL-A: 105 (35.0%), RAIL-B: 195 (65.0%) | 65% shifted to RAIL-B |

### Raw Artifact Paths
- JSON: `artifacts/experiments/latency/latency-20260921-160028.json`
- CSV: `artifacts/experiments/latency/latency-20260921-160028.csv`

---

## Experiment 4 — Offline Queue and Deterministic Replay

### Objective
Exercise the client-side durable `OfflineIntentQueue` (IndexedDB via `fake-indexeddb`) and `OfflineReplayWorker` across network restoration, transient failures, pending statuses, and permanent rejection.

### Configuration
- **Workload**: 50 durable offline payment intents.
  - 30 Immediate Successes: Return 201 `COMPLETED` on first replay pass.
  - 10 Transient Retries: Fail with HTTP 503 `BANK_UNAVAILABLE` on pass 1; succeed with 201 on pass 2.
  - 5 Pending Statuses: Return HTTP 202 `PROCESSING` on pass 1; resolved to `COMPLETED` via `getPayment` status poll on pass 2.
  - 5 Permanent Failures: Return HTTP 400 `INVALID_ACCOUNT`; transition immediately to `FAILED`.
- **Replay Multi-Pass Timeline**:
  - Pass 1: `T + 10s` (Initial replay of all queued claims).
  - Pass 2: `T + 45s` (Clock advanced by 35s to expire backoff timers).
  - Pass 3: `T + 80s` (Final convergence verification).

### Measured Results (Run `offline-20260921160147`)

| Metric | Measured Value | Target / Requirement |
| :--- | :--- | :--- |
| **Total Queued** | 50 | 50 |
| **Replay Attempts** | 65 | Authoritative API exercised |
| **Successful Syncs** | 45 | 30 immediate + 10 retried + 5 polled |
| **Permanent Failures** | 5 | 5 validation errors |
| **Duplicate Processing Count** | **0** | **Strict Invariant: 0 duplicates** |
| **Same-Key Replays** | 10 | 100% same-key idempotency reuse |
| **Sync Delay P50** | 9,100 ms | Pass 1 resolution |
| **Sync Delay P95** | 41,800 ms | Pass 2 backoff resolution |
| **Sync Delay Max** | 42,000 ms | Bounded recovery |
| **Invariants Satisfied** | **true (4/4)** | Complete pass |

### Raw Artifact Paths
- JSON: `artifacts/experiments/offline/offline-20260921160147.json`
- CSV: `artifacts/experiments/offline/offline-20260921160147.csv`

---

## Experiment 5 — Concurrency Storm and Financial Invariants

### Objective
Subject the adaptive routing and payment execution pipeline to a heavy concurrent workload, verifying that financial integrity, idempotency, and circuit state invariants hold under concurrent contention.

### Configuration
- **Workload**: 300 requested operations across 10 concurrent worker goroutines.
- **Idempotency Pool**: 179 unique keys (with deliberate duplicate submissions to test concurrent duplicate deduplication).
- **Execution Pipeline**: `payments.SelectRoute` with circuit eligibility hook -> `HoldFunds` -> `ConfirmHold` -> `cb.RecordSuccess`.

### Measured Results (Run `concurrency-20260921-160028`)

| Metric | Measured Value | Invariant Condition |
| :--- | :--- | :--- |
| **Requested Operations** | 300 | Input workload |
| **Concurrency Level** | 10 workers | Concurrent goroutines |
| **Completed Operations** | 300 | No dropped operations |
| **Failed Operations** | 0 | |
| **Pending Operations** | 0 | |
| **Unique Idempotency Keys** | 179 | |
| **Duplicate Submissions Deduplicated** | 121 | Idempotent duplicate reuse |
| **Duplicate Financial Processing** | **0** | **Invariant: Exactly zero duplicate executions** |
| **Money-State Violations** | **0** | **Invariant: Only valid terminal states** |
| **Circuit Violations** | **0** | **Invariant: Zero ineligible routes selected** |

### Raw Artifact Paths
- JSON: `artifacts/experiments/concurrency/concurrency-20260921-160028.json`
- CSV: `artifacts/experiments/concurrency/concurrency-20260921-160028.csv`

---

## How to Reproduce

### 1. Backend Experiments (Experiments 1, 2, 3, 5)

Run the unified Go runner from the `backend/` directory:

```bash
cd backend

# Run all backend experiments (1, 2, 3, 5)
go run ./cmd/experiments -experiment=all -requests=300

# Or run individual experiments
go run ./cmd/experiments -experiment=routing
go run ./cmd/experiments -experiment=outage
go run ./cmd/experiments -experiment=latency
go run ./cmd/experiments -experiment=concurrency

# Run automated tests
go test -v -count=1 ./internal/experiments/...
```

### 2. Frontend Offline Experiment (Experiment 4)

Run the offline queue/replay benchmark from the `frontend/` directory:

```bash
cd frontend

# Run Experiment 4
npm run experiment:offline

# Run unit tests
npm run test:offline-queue
npm run test:offline-replay
```

---

## Infrastructure and Environment Limitations

- **Race Detector on Windows**: Running `go test -race` in this Windows environment produces `cc1.exe: sorry, unimplemented: 64-bit mode not compiled in` due to the local 32-bit MinGW Cgo compiler. Standard execution without race detection (`go test -count=1 ./...` and `go build ./...`) builds and passes 100% of tests.
- **Durable Storage Simulation**: In-memory database repositories (`memoryChaosRepo`, `fake-indexeddb`) were used to isolate resilience mechanics from external database setup while faithfully executing all domain contracts.
