# Runbook: Bank Outage and Scheduled Recovery

## Context
When a participant bank experiences a transient outage or network disruption, payments enter the durable recovery scheduler with exponential backoff and jitter.

## Diagnosis
1. Inspect the recovery job status in `recovery_jobs`.
2. Inspect the latest attempt records in `recovery_attempts` to identify the failure reason code (e.g. `BANK_OUTAGE`, `TIMEOUT`, `CONNECTION_REFUSED`).
3. If attempt count reaches the limit (20), the job transitions to `needs_attention` with reason code `ATTEMPTS_EXHAUSTED`.
4. Check whether health monitoring indicates bank availability restoration.

## Resolution Procedure
- When the participant bank is confirmed healthy and responsive, an authorized operator (`OPS_ADMIN`) can trigger an expedited recovery check via `POST /api/ops/payments/{paymentID}/recovery/recheck`.
- The recheck clears operational parking, reschedules the recovery job for immediate worker claim, and logs an audit record in `operational_audit_logs`.
- The background recovery worker claims the job, verifies participant presence, and converges the payment to `COMPLETED` or `FAILED` without double debiting.
