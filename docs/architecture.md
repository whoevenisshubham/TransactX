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

M2-4 selects immutable switch-level route candidates. A candidate keeps source and destination bank ownership separate from its execution target and adapters; selection never rewrites account ownership. The current registry provides one legitimate candidate for a configured source/destination pair, using the source participant's stable health target as its execution endpoint. `STATIC` chooses a fixed candidate ID; `ADAPTIVE` excludes observed-unavailable candidates, prefers the highest existing health score, and breaks ties by execution target ID then candidate ID. Each selection persists a `PAYMENT_ROUTED` route-decision fact. Circuit eligibility is an optional hook only; M2-5 owns circuit state and transitions.

The default probe timeout threshold is 2 seconds. A probe at or above that measured duration is classified as `TIMEOUT`; otherwise the explicit availability result distinguishes `SUCCESS` from `FAILURE`.

## Boundaries and future work

The adapter registry is keyed by persisted central bank ID and contains no bank-specific orchestration logic. Adaptive routing, circuit breakers, Merkle reconciliation, full chaos orchestration, and offline queue UX are later phases.
