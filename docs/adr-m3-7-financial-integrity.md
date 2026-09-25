# Architectural Decision Record: Runtime Financial Integrity Engine (M3-7)

**Status**: Implemented  
**Package**: `backend/internal/reconciliation`  
**Date**: September 2026  

---

## 1. Overview and Problem Statement

While two-sided reconciliation (M3-5) verifies consistency between TransactX canonical records and participant bank ledgers, and cryptographic Merkle proofs authenticate record inclusion against global roots, the system requires a continuous, observational financial integrity verification layer.

The **Runtime Financial Integrity Engine** evaluates internal financial and structural invariants across core ledger, payment, account, idempotency, and commitment models.

---

## 2. Check Registry

All checks are typed, identified by stable string constants, and assigned authoritative severities and descriptions:

| Check Code | Severity | Description |
|---|---|---|
| `DEBIT_CREDIT_CONSERVATION` | `CRITICAL` | Sum of debits must equal sum of credits for all ledger transactions within scope. |
| `NON_NEGATIVE_BALANCES` | `CRITICAL` | Materialized and spendable account balances must never be negative. |
| `TRANSACTION_UNIQUENESS` | `CRITICAL` | Payment and bank operation identities must be strictly unique without duplication. |
| `IDEMPOTENCY_MAPPING` | `HIGH` | Idempotency records must map uniquely and consistently to their originating user and payment. |
| `PAYMENT_STATE_VALIDITY` | `HIGH` | Payment states and transitions must strictly conform to the authoritative state machine. |
| `COMPLETED_PAYMENT_LEDGER_COMPLETENESS` | `CRITICAL` | Every payment in `COMPLETED` state must have a corresponding, balanced ledger transaction with matching debit and credit entries. |
| `MERKLE_COMMITMENT_CONSISTENCY` | `CRITICAL` | Maintained Merkle commitment roots must match the canonical calculation of underlying records for the scope. |

---

## 3. Invariant Definitions

1. **Debit/Credit Conservation**:
   For every ledger transaction, $\sum \text{Debit} = \sum \text{Credit}$. Across the aggregate requested scope, aggregate debits must equal aggregate credits.
2. **Non-Negative Balances**:
   Every account in `accounts` must have `balance_paise >= 0`. Reconstructed ledger balances must also be non-negative.
3. **Transaction Uniqueness**:
   Payment IDs (`payments.id`) must be unique. Bank operations `(bank_id, operation_id)` must be unique.
4. **Idempotency Mapping**:
   Idempotency records `(user_id, key)` must link to exactly one payment whose initiator is that user.
5. **Payment State Validity**:
   Payment `state` must belong to the permitted state set. Explicit transitions must be valid according to `payments.CanTransition(from, to)`.
6. **Completed-Payment Ledger Completeness**:
   Every payment with `state = 'COMPLETED'` must have an associated row in `ledger_transactions` with debit and credit `ledger_entries` matching the sender, receiver, and payment amount.
7. **Merkle Commitment Consistency**:
   Recomputing the Merkle tree over the underlying records using `IncrementalMerkleLedger` must yield the exact root stored in the commitment.

---

## 4. Observational & Read-Only Guarantee

The engine is strictly observational:
- It **never** mutates account balances.
- It **never** alters payment states.
- It **never** creates missing ledger rows.
- It **never** retries payments or releases holds.
- It **never** silently repairs corrupted data.

Violations are preserved as structured evidence for operational visibility and alerting.

---

## 5. Result and Status Semantics

Each check produces an `IntegrityCheckResult`:
- **`PASS`**: Invariant verified successfully.
- **`FAIL`**: Financial or logical invariant violated. Structured violations are recorded in `Violations`.
- **`ERROR`**: Operational failure during check execution (database error, network drop). An operational error is **never** reported as `PASS`.
- **`NOT_APPLICABLE`**: Check requires parameters (e.g. participant ID, scope) not supplied in the request.

---

## 6. Execution Model

The entry point is `RuntimeIntegrityEngine.Run(ctx, req)`:
- Executes registered checks in canonical order.
- Collects all results and **never stops after the first failure**.
- Aggregates metrics into `IntegrityRunSummary`.
- Persists the run via `IntegrityRunStore` (`MemoryIntegrityRunStore` or `PostgresIntegrityRunStore`).
- Supports bounded listing and retrieval APIs.

---

## 7. State History Limitations

The core schema does not maintain an immutable state transition log table in PostgreSQL. The engine therefore validates current states and any explicitly recorded transition events without synthesizing or inventing historical transitions from log strings.

---

## 8. Merkle Consistency Integration

The engine reuses the accepted Merkle implementation (`NewIncrementalMerkleLedger`, `Bootstrap`, canonical records). It does not duplicate canonical serialization, domain prefixes, or hashing logic.
