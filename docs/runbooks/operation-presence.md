# Runbook: Participant Operation Presence Verification

## Context
Operation presence querying provides authoritative confirmation of whether an operation was recorded in a participant bank's durable ledger.

## Diagnosis
1. Query `GET /internal/operations/{operationID}/presence?paymentId={paymentID}` on the relevant bank adapter using participant service token.
2. An authoritative result `absent_confirmed` occurs strictly when the durable record is absent from the participant bank.
3. Network timeouts or transport errors must never be inferred as absence.

## Resolution Procedure
- **`absent_confirmed`**: Safe to replay original dispatch or fail closed without risking double processing.
- **`present`**: Do not re-dispatch. Retrieve operation status and reconcile central state with bank outcome.
- **`unknown` / connection failure**: Fail closed. Retain payment in `PENDING_RECONCILIATION` until connectivity is restored or manual intervention is performed.
