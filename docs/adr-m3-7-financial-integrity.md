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

## 7. Immutable State Transition Boundary & Non-Retroactive History

To guarantee verifiable state progression without reconstructing events from logs, the system maintains an immutable audit table: `payment_state_transitions`.

### Schema & Invariants
- `id` (UUID PRIMARY KEY)
- `payment_id` (UUID NOT NULL REFERENCES payments(id))
- `from_state` (VARCHAR NOT NULL)
- `to_state` (VARCHAR NOT NULL)
- `transitioned_at` (TIMESTAMPTZ NOT NULL)
- Index on `(payment_id, transitioned_at ASC)`
- Database-level trigger `prevent_payment_state_transitions_mutation` blocking all `UPDATE` and `DELETE` operations.

### Atomic Transition Recording
Every payment state transition writes one immutable history row in the **same transaction** as the payment state update (`updateState`, `settlePayment`, `settleCentral`).

### Non-Retroactive History Rule
> **Existing historical payments before this migration have no reconstructed transition history. Their current state may be checked, but historical transition validity is not retroactively claimed.**

For newly recorded transitions, `PAYMENT_STATE_VALIDITY` reads the real transition history and verifies every edge using `payments.CanTransition(from, to)`. For historical rows preceding the migration, the engine verifies current state legality without synthesizing transitions from logs.

---

## 8. Merkle Commitment Consistency & Independent Sources

The Merkle integrity check enforces a strict read boundary between two independent sources:

```
    authoritative financial records
              |
              v
      canonical Merkle calculation
              |
              v
      compare with independently
      maintained commitment state
```

### A. Authoritative Record Source
Obtained via `FinancialDataStore.GetAuthoritativeRecords(ctx, participantID, scope)`. In PostgreSQL, reads from `CentralLedgerSnapshotSource` querying core ledger transactions and entries within the requested scope interval, generating deterministic `CanonicalRecord` slices.

### B. Maintained Commitment Source
Obtained via `FinancialDataStore.GetMaintainedCommitment(ctx, participantID, scope)`. Queries the independently maintained `IncrementalCommitmentStore` or participant commitment persistence layer. The verification engine **never builds, bootstraps, or materializes commitments during verification**. Missing commitments result in deterministic `FAIL` violations.

### C. Exact Merkle Configuration Source
Extracted directly from the stored commitment:
- `BucketWidth`: Dynamic duration from stored commitment state. The engine **never hardcodes `1 * time.Hour`**.
- `Partition`: Logical partition ID.
- `CanonicalVersion`: Expected `v1`.
- `AlgorithmVersion`: Expected `merkle-v1`.
- `Generation`: Monotonic generation identifier.

The integrity engine detects:
- Tampered maintained root (bit flips)
- Stale maintained root (record count mismatch)
- Wrong generation
- Configuration mismatch (partition, bucket width, versions)
- Authoritative record mutation

---

## 9. Violation Persistence Format

Violations are preserved boundedly and deterministically:
- Column: `violations JSONB NOT NULL DEFAULT '[]'::jsonb` on table `integrity_check_results`.
- Model: Typed `[]CheckViolation` slices containing:
  - `entityId`: Unique identifier of the affected account, payment, or participant.
  - `description`: Stable human-readable description of the violation.
  - `details`: Bounded key-value mapping of diagnostic evidence (observed vs expected amounts, roots, versions).
- Unbounded internal runtime objects are excluded.
- `PostgresIntegrityRunStore.SaveRun` serializes `check.Violations` to JSONB; `GetRun` unmarshals it back into typed `[]CheckViolation`.
