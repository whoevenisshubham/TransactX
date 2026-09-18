# Architecture

## Phase-6 status

TransactX has two explicit execution paths:

```text
Customer -> Go API -> Payment Service -> central PostgreSQL
                                  \-> BankAdapter -> Bank A HTTP service -> bank_a schema
```

The local path remains the synchronous `LOCAL_SETTLEMENT` PostgreSQL transaction. When adapters are configured, the routed path creates the central payment intent and executes a durable saga across the selected source and destination participants.

Central PostgreSQL owns users, central accounts and account-to-bank-account mapping, payment state, user-scoped idempotency, the central double-entry ledger, route metadata, bank-operation tracking, and recovery state. It is not a second live copy of a participant's balance for routed settlement.

Bank A is a separate Go process. Its `bank_a` schema owns participant accounts, balances, account status, holds, provisional/final credits, participant operations, participant ledger entries, and status lookup. The service boundary is HTTP; `internal/bank.HTTPClient` implements the domain-only `BankAdapter` contract.

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

## Boundaries and future work

The adapter is keyed by bank ID and contains no Bank-A-specific orchestration logic. Bank B, adaptive routing, circuit breakers, Merkle reconciliation, full chaos orchestration, and offline queue UX are later phases and are not claimed as implemented here.
