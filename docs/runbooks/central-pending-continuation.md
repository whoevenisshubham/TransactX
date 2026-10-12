# Runbook: Bank Settled Central Pending Continuation

## Context
Payments in `BANK_SETTLED_CENTRAL_PENDING` indicate that the participant bank settled the transaction successfully, but central ledger recording or state update was interrupted before finalization.

## Diagnosis
1. Verify the participant operation presence returns `present` with settled status.
2. Check existing double-entry ledger entries for the payment in `ledger_entries`.
3. Check if account balance reflects local debit/credit.

## Resolution Procedure
- Central-only continuation must execute under the payment's dedicated advisory lock.
- If ledger rows already exist (at least 2 entries for double-entry), complete the payment state to `COMPLETED` without duplicate ledger insertion.
- If central ledger rows are missing, record central ledger and update payment to `COMPLETED`.
- Under no circumstances should participant bank operations be repeated or re-dispatched.
