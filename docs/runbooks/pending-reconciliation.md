# Runbook: Pending Reconciliation and Missing Destination Operation

## Context
When a payment is in `PENDING_RECONCILIATION`, the central engine dispatched a debit or credit operation to the participant bank but has not yet received definitive settlement confirmation within the primary timeout window.

## Diagnosis
1. Inspect the central payment state and latest transition event in `payment_state_transitions`.
2. Check participant durable operation presence via `OperationPresenceReader` (`GET /internal/operations/{operationID}/presence?paymentId={paymentID}`).
3. Examine bank participant connectivity and response latency in `health_samples`.
4. Review open outbox deliveries and recovery jobs for the payment.

## Resolution Procedure
- **Presence Confirmed Absent (`absent_confirmed`)**: The dispatch was lost before reaching participant durable storage. Safe replay or continuation can proceed without creating duplicate participant transactions.
- **Presence Present (`present`)**: The participant processed the operation. Central recovery or reconciliation continuation will record central settlement without re-dispatching to the bank.
- **Presence Unknown / Participant Unavailable (`unknown`)**: Do not guess or execute speculative re-dispatch. Wait for participant recovery or run offline reconciliation file ingestion.
