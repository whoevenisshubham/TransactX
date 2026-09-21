# Architecture

## Current routed-payment status

TransactX has two explicit execution paths:

```text
Customer -> Go API -> Payment Service -> central PostgreSQL
                                  \-> BankAdapter registry -> Bank A/B HTTP services -> bank_a / bank_b schemas
```

The local path remains the synchronous `LOCAL_SETTLEMENT` PostgreSQL transaction. When adapters are configured, the routed path creates the central payment intent and executes a durable saga across the selected source and destination participants.

Central PostgreSQL owns users, central accounts and account-to-bank-account mapping, payment state, user-scoped idempotency, the central double-entry ledger, route metadata, bank-operation tracking, and recovery state. It is not a second live copy of a participant's balance for routed settlement.

Customer payment creation selects the single active primary account on the server. A fresh central account uses local settlement unless both persisted source and destination bank IDs have configured adapters. Customer DTOs expose only payment references, names, direction, safe bank names/codes, state, timing, note, origin, and failure information.

Bank A and Bank B are separate Go processes. Each bank schema owns participant accounts, balances, account status, holds, provisional/final credits, participant operations, participant ledger entries, and status lookup. The service boundary is HTTP; `internal/bank.HTTPClient` implements the domain-only `BankAdapter` contract.

## Routed protocol

```text
CREATED -> VALIDATING -> ROUTING -> PROCESSING
  -> resolve source and destination accounts
  -> HOLD source
  -> PROVISIONAL_CREDIT destination
  -> CONFIRM source hold
  -> FINALIZE destination credit
  -> central PostgreSQL settlement
  -> COMMITTED -> COMPLETED
```

Every monetary operation has a deterministic operation ID derived from the payment and logical step. Retries reuse it. A timeout or lost response is `PENDING`, never presumed failed: the recovery entry point calls `GetOperationStatus` using the original ID. A still-unknown operation keeps the payment in `PENDING_RECONCILIATION`; definite failure follows release/reversal compensation where safe.

If bank-side settlement succeeds but central persistence fails, the payment is `BANK_SETTLED_CENTRAL_PENDING`. Its recovery path repairs only central accounts and ledger state; it never repeats bank operations.

There is no transaction spanning Bank A and central PostgreSQL, and the system makes no distributed-ACID claim.

## Health monitoring

M2-3 records explicit observational samples for stable execution-target identifiers such as `BANK-A` and `BANK-B`. Health samples are not account, balance, ledger, or payment state, and payment outcomes do not create health samples automatically. Raw samples are retained in central PostgreSQL; a bounded rolling window is used to calculate read-only snapshots.

The default 15-minute snapshot uses at most 500 samples and requires one sample. Its deterministic score is:

```text
availabilityScore = successfulChecks / totalChecks
successScore = successfulCalls / totalCalls
latencyPenalty = clamp((p95Latency - 10ms) / (1000ms - 10ms), 0, 1)
timeoutPenalty = timeoutRate
score = clamp(0.35*availabilityScore + 0.35*successScore
              - 0.20*latencyPenalty - 0.10*timeoutPenalty, 0, 1)
```

P95 uses nearest-rank over latency values sorted ascending. Samples at the window start are included; older samples are excluded.

## Deterministic routing

M2-4 selects immutable switch-level route candidates without modifying logical bank ownership:

- **Execution target vs. bank ownership**: `ExecutionTargetID` represents a switch-level route, rail, or execution endpoint path. It is never account ownership or logical participant authority. `sourceBankID` and `destinationBankID` remain strictly immutable across all candidates; routing never swaps sender and receiver bank identity.
- **Multiple execution targets**: Multiple legitimate execution targets (e.g. `direct`, `RAIL-A`, `RAIL-B`) may be configured for the same `(sourceBankID, destinationBankID)` pair, each specifying genuine `SourceAdapter` and `DestinationAdapter` instances. If only default bank adapters exist, safe single-candidate routing is preserved.
- **Selection modes**:
  - `STATIC`: Deterministically selects the configured baseline candidate ID (via `ROUTING_STATIC_BASELINE`) or falls back to lexicographical candidate order. Fails safely with `ErrNoRouteCandidate` if the configured baseline is unavailable or invalid.
  - `ADAPTIVE`: Obtains authoritative M2-3 health snapshots for each candidate's `ExecutionTargetID`, excludes unavailable (`availabilityScore == 0`) and unhealthy (`score <= 0`) targets, compares scores, and selects the highest health score.
- **Deterministic tie-break**: When candidates have equal health scores, ties are stably broken by `ExecutionTargetID` ascending, then `CandidateID` ascending. Identical inputs always produce identical decisions without randomness.
- **Route history & durable recovery**: Every routed payment persists an immutable `PAYMENT_ROUTED` record to `payment_route_decisions` containing payment ID, candidate ID, source/destination bank IDs, execution target ID, score, full health snapshot, reason code, selection mode, and selection timestamp. During duplicate request handling or pending payment recovery, the selected `execution_target_id` is loaded from `payment_route_decisions` and matched against configured targets for that logical bank pair to resolve the exact original `SourceAdapter` and `DestinationAdapter`. If the selected target is no longer configured, recovery fails closed and leaves the payment pending; it never blindly defaults to `targets[0]`.
- **Explicit runtime configuration**: Configured via environment variables `ROUTING_MODE` (`STATIC` or `ADAPTIVE`), `ROUTING_STATIC_BASELINE`, and `ROUTING_TARGETS` (supporting delimited format `candidate:target:sourceBank:destBank:endpoint`, pipe format, or structured JSON). Configuration strictly distinguishes:
  - `candidateID`: candidate identity
  - `executionTargetID`: switch-level execution rail/target identity
  - `sourceBank`: logical source bank
  - `destinationBank`: logical destination bank
  - `endpoint`: dedicated health probe endpoint associated with `ExecutionTargetID` (required; missing or malformed endpoints are rejected; never silently substituted with execution endpoints or fake targets)
  - `sourceEndpoint`: optional source execution adapter endpoint
  - `destinationEndpoint`: optional destination execution adapter endpoint
- **Circuit breaker hook**: Integrates with the deterministic M2-5 circuit breaker via `CircuitEligibility`. `CLOSED` targets are eligible; `OPEN` targets are excluded; HALF_OPEN targets are excluded from payment routing; the bounded probe budget applies to recovery health probes. After successful recovery to CLOSED, payment traffic resumes with gradual restoration.

The default probe timeout threshold is 2 seconds. A probe at or above that measured duration is classified as `TIMEOUT`; otherwise the explicit availability result distinguishes `SUCCESS` from `FAILURE`.

## Deterministic circuit breaker and gradual recovery

M2-5 implements a production-inspired, deterministic, thread-safe circuit breaker and gradual recovery system:

- **State Identity**: Circuit state belongs exclusively to `executionTargetID` (e.g. `RAIL-A`, `RAIL-B`). It does NOT belong to bank ownership, accounts, balances, payments, or the financial ledger. Isolating state by `executionTargetID` ensures that a failing execution rail (e.g. `RAIL-A`) never affects or opens an alternate rail (e.g. `RAIL-B`).
- **State Machine**:
  - `CLOSED`: Target is healthy and eligible for routing. Failures and timeouts within the configured rolling window are tracked.
  - `OPEN`: Target is ineligible for routing. Excluded by `SelectRoute`. Remains OPEN until cooldown expires.
  - `HALF_OPEN`: Target enters recovery probing state once cooldown has elapsed. Excluded from payment routing; the bounded probe budget applies to recovery health probes.
- **State Transitions**:
  - `CLOSED -> OPEN`: Triggered when rolling-window failure count reaches `failureThreshold` (or timeout count reaches `timeoutThreshold`).
  - `OPEN -> HALF_OPEN`: Deterministically transitions when `now.Sub(openedAt) >= openCooldown`.
  - `HALF_OPEN -> CLOSED`: Triggered when `successfulProbes >= successThreshold`. Resets probe counters and failure histories, entering gradual restoration.
  - `HALF_OPEN -> OPEN`: Immediately triggered if any trial probe fails (`probe_failed`).
- **Configuration**:
  - `CIRCUIT_FAILURE_THRESHOLD` (default: 5): Positive integer for consecutive or rolling failures required to open.
  - `CIRCUIT_TIMEOUT_THRESHOLD` (default: 0): When 0, timeouts count as failures toward `failureThreshold`; when > 0, specifies an independent timeout threshold.
  - `CIRCUIT_ROLLING_WINDOW` (default: 60s): Window duration for tracking failures and timeouts.
  - `CIRCUIT_OPEN_COOLDOWN` (default: 30s): Cooldown duration before transitioning from `OPEN` to `HALF_OPEN`.
  - `CIRCUIT_HALF_OPEN_PROBE_LIMIT` (default: 2): Maximum concurrent trial probe requests permitted in `HALF_OPEN`.
  - `CIRCUIT_SUCCESS_THRESHOLD` (default: 2): Successful probes required to transition `HALF_OPEN -> CLOSED`.
  - `CIRCUIT_RESTORATION_STEPS` (default: 3): Number of discrete steps for gradual recovery.
  - `CIRCUIT_SUCCESS_POLICY` (`DECREMENT` or `RESET`): Policy for pruning failure timestamps on successful traffic.
  - Malformed or invalid configurations fail fast on startup with descriptive validation errors; no hidden magic values.
- **Concurrency & Probe Budgeting**: Mutex-synchronized per target. In `HALF_OPEN`, atomic probe budget counters ensure concurrent recovery health probes cannot exceed `halfOpenProbeLimit`. Excess recovery health probe requests are rejected deterministically (`CIRCUIT_REJECTED`). HALF_OPEN targets are excluded from payment routing; the bounded probe budget applies to recovery health probes. After successful recovery to CLOSED, payment traffic resumes with gradual restoration.
- **Gradual Restoration**: A newly recovered target does not instantly absorb all traffic when competing execution targets exist. Restoration operates in deterministic discrete steps (`1..RestorationSteps`). At step $k$ of $N$, the target admits a proportional quota ($\frac{k}{N}$) of requests and deterministically sheds the remainder (`GRADUAL_RESTORATION_SHED`). This allows M2-4 to route shed traffic to alternate healthy candidates. Consecutive successes at each step advance the restoration step until full recovery.
- **M2-4 Integration**: Connected via `payments.CircuitEligibility` hook in `SelectRoute`. The circuit breaker only evaluates eligibility; it NEVER calls monetary `BankAdapter` operations (`Reserve`, `Settle`, `HoldFunds`, `ProvisionalCredit`) and NEVER alters payment balances or accounts.
- **Observability**: Every state transition emits an immutable `TransitionEvent` recorded to the `circuit_transition_events` PostgreSQL table and in-memory audit log. Snapshots and event logs are accessible only via authenticated `OPS_ADMIN` endpoints (`/api/ops/circuit`, `/api/ops/circuit/{targetID}`, `/api/ops/circuit/{targetID}/events`). Public roles (`CUSTOMER`, `MERCHANT`) are rejected with `403 Forbidden`.

## Controlled Chaos Controller (M2-6)

M2-6 adds a controlled, reversible, deterministic fault-injection mechanism for resilience testing:

- **Purpose & Safety Guardrails**: Chaos injection is strictly operational simulation (`mode: "SIMULATION"`). It operates exclusively at the network/service decorator seam (`ChaosAdapter`) wrapping `bank.BankAdapter` and `health.HealthChecker`. It NEVER creates fake payments, fake settlements, nor alters central ledger balances, account ownership, or holds. The frozen 9-method `BankAdapter` interface remains strictly unchanged.
- **Four Supported Scenarios**:
  1. `BANK_OUTAGE`: Simulates target endpoint unavailability (`ErrBankOutage`). Downstream health checks observe failures; monetary operations fail safely with `ErrCodeBankUnavailable`.
  2. `LATENCY`: Adds configurable, deterministic delay to target operations. Parameter `latencyMs` is validated (1ms to 30s). Delay is executed safely via context-aware sleep without uncontrolled thread blocking.
  3. `TRANSIENT_DROP` (alias `TRANSIENT`, `MESSAGE_DROP`): Controlled transient/request-drop simulation. Injects a pre-call transient rejection (`ErrTransientDrop`, error code `ErrCodeTransientFailure`) before downstream adapter invocation. It does not falsely claim to prove an unconfirmed bank outcome: `UNKNOWN != FAILURE`. Genuine unknown bank outcomes remain governed exclusively by the existing M1/M2 saga recovery path (`bank.OperationPending` / `PENDING_RECONCILIATION` / `GetOperationStatus`) without blind retries.
  4. `TEMPORARY_PARTITION`: Simulates communication partition between switch and target (`ErrNetworkPartition`). Reversible upon stop, reset, or auto-expiry.
- **Target Isolation & Validation**: All scenarios are strictly target-scoped by `executionTargetID` (e.g. `RAIL-A`). Injected faults on target $X$ never impact target $Y$, unrelated source/destination banks, or unrelated payment flows. Target IDs are validated via `BuildTargetValidator` exclusively against configured execution rails and health targets; logical bank codes (`sourceBankID`, `destinationBankID`) are explicitly excluded to prevent conflation with switch-level execution rail identities. Nonexistent or unconfigured target IDs are rejected (`ErrTargetNotFound`).
- **Lifecycle & Exact Expiry**:
  - `START`: Explicit creation with a mandatory duration (100ms to 10m). Requires active-state verification in PostgreSQL; if the database is unavailable or returns an error, `Start` fails fast and refuses fault activation. Scenario state and `CHAOS_STARTED` event are persisted atomically before in-memory activation.
  - `INSPECT`: Query active scenarios or inspect specific scenario by ID. Excludes expired scenarios (`expires_at <= now`).
  - `STOP`: Explicitly deactivates scenario; persisted atomically with `CHAOS_STOPPED` event. Repeated stops are safe and idempotent. Never resurrects an expired scenario.
  - `RESET`: Clears active scenario for a specified target or resets all active scenarios across the system in both memory and PostgreSQL (`CHAOS_RESET`).
  - `AUTO-EXPIRY`: Evaluated deterministically against injected clock (`now.After(expiresAt)`). Expired scenarios transition to inactive in PostgreSQL via atomic check-and-update and emit `CHAOS_EXPIRED` exactly once. If persistence fails during expiry, the error is surfaced and in-memory state is NOT silently deactivated, preserving memory/database consistency.
- **Restart Safety & Hydration**: Active chaos state survives process restarts. On startup, `Hydrate()` reloads all unexpired active scenarios from PostgreSQL; if `Hydrate()` fails, startup logs an error and terminates with non-zero exit to avoid running with unhydrated chaos state. If an unexpired active scenario exists in the database but is absent from memory cache, `GetActiveFault` loads, validates, and caches it on demand.
- **Fail-Closed Seam**: If chaos state cannot be determined because the chaos repository is unavailable, `GetActiveFault` returns an observable error and `ChaosAdapter` fails closed with `bank.ErrCodeBankUnavailable` rather than silently bypassing active faults or falsely claiming healthy status.
- **Authorization Boundary**: All mutation and inspection controls require `OPS_ADMIN` role via JWT middleware. Public roles (`CUSTOMER`, `MERCHANT`) are rejected with `403 Forbidden`. Unauthenticated requests are rejected with `401 Unauthorized`.
- **Durable Persistence & Audit**: Scenario state changes and lifecycle audit events (`CHAOS_STARTED`, `CHAOS_STOPPED`, `CHAOS_RESET`, `CHAOS_EXPIRED`) are persisted atomically within PostgreSQL transactions. If durable persistence fails, API requests fail and in-memory faults are not activated.
- **M2-3 / M2-4 / M2-5 System Integration**:
  - M2-3 Health Monitoring samples targets through `ChaosAdapter.GetHealth()`, capturing degraded availability, latency penalties, or timeouts.
  - M2-5 Circuit Breaker observes health failure samples and trips `CLOSED -> OPEN` once the failure threshold is crossed. `HALF_OPEN` remains recovery-health-probe-only and excludes payment routing.
  - M2-4 Adaptive Routing naturally avoids targets with low health scores or open circuits, redistributing eligible traffic to alternate valid execution rails. Routing never fabricates fake reroutes or alters bank ownership.

## Boundaries and future work

The adapter registry is keyed by persisted central bank ID and contains no bank-specific orchestration logic. Merkle reconciliation, console UI visualization, and offline queue UX are later phases.
