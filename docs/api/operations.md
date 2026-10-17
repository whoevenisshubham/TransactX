# Operational APIs (M2 - B8)

## Overview

The operational endpoints expose read-only evidence, derived worklists, recovery attempts, gated operator controls, and verifiable evidence export bundles for operations personnel (`OPS_ADMIN`, `OPS_VIEWER`), frontend journeys (M1), and the investigator agent (M3).

All read-only endpoints adhere strictly to the principle that **GET requests never invoke recovery, health sampling, reconciliation execution, or monetary mutations**.

---

## Endpoint Matrix

| Method and Route | Allowed Roles | Description |
| --- | --- | --- |
| `GET /api/ops/worklist` | `OPS_ADMIN`, `OPS_VIEWER` | Paginated derived worklist from `ops_payment_worklist` projection. |
| `GET /api/ops/payments/{paymentID}/evidence` | `OPS_ADMIN`, `OPS_VIEWER` | Read-only assembled evidence bundle across central authority and participants. |
| `GET /api/ops/payments/{paymentID}/recovery-attempts` | `OPS_ADMIN`, `OPS_VIEWER` | Paginated attempt audit history for a payment. |
| `POST /api/ops/payments/{paymentID}/recovery/recheck` | `OPS_ADMIN` | Expedites recovery job scheduling under feature flag and rate limits. |
| `GET /api/ops/payments/{paymentID}/evidence/export` | `OPS_ADMIN`, `OPS_VIEWER` | Cryptographically signed/hashed export bundle with manifest. |

---

## 1. Worklist Projection (`GET /api/ops/worklist`)

### Query Parameters
- `limit`: Integer (default `25`, max `100`).
- `cursor`: Opaque base64url-encoded JSON string containing cursor keys and normalized filter hash.
- `state`: Single or comma-separated allowed central states (e.g., `PROCESSING,ROUTING`). Default scope excludes terminal states (`COMPLETED`, `FAILED`, `REVERSED`).
- `recoveryStatus`: Filter by recovery job status (`none`, `scheduled`, `running`, `deferred`, `needs_attention`, `resolved`).
- `bankCode`: Matches source or destination bank code.
- `paymentId`: Exact UUID search.
- `createdFrom`, `createdTo`: RFC3339 timestamps.

### Example Response
```json
{
  "requestId": "req-12345",
  "data": {
    "items": [
      {
        "paymentId": "7343e8a4-06fe-4f18-bbbe-cb6f35832ea4",
        "state": "PROCESSING",
        "amountPaise": "25000",
        "currency": "INR",
        "sourceBankCode": "BANK-A",
        "destinationBankCode": "BANK-B",
        "createdAt": "2026-10-12T08:00:00Z",
        "updatedAt": "2026-10-12T08:00:05Z",
        "ageMs": 5000,
        "recoveryStatus": "scheduled",
        "attemptCount": 1,
        "nextAttemptAt": "2026-10-12T08:00:30Z",
        "reasonCode": "PARTICIPANT_TIMEOUT",
        "projectionVersion": "3"
      }
    ],
    "nextCursor": "eyJ1IjoiMjAyNi0xMC0xMlQwODowMDowNVoiLCJwIjoiNzM0M2U4YTQtMDZmZS00ZjE4LWJiYmUtY2I2ZjM1ODMyZWE0IiwiaCI6IjEyM2FjZiJ9",
    "asOf": "2026-10-12T08:00:10Z",
    "projection": {
      "status": "ready",
      "updatedAt": "2026-10-12T08:00:05Z",
      "oldestPendingEventAgeMs": 0
    }
  }
}
```

---

## 2. Payment Evidence (`GET /api/ops/payments/{paymentID}/evidence`)

### Behaviors
- **Deadline**: 3-second overall context deadline.
- **Isolation**: Central records captured in a short read-only transaction, closed before participant calls.
- **Partial Evidence**: If participant presence check fails or times out, returns `200 OK` with `presence: "unknown"`, `sourceStatus: "unavailable"`, and an alert warning.
- **Snapshot Identity**: SHA-256 hex string over canonical redacted JSON, binding observation times and collections without circular self-reference.

### Example Response
```json
{
  "requestId": "req-evidence-1",
  "data": {
    "schemaVersion": "tx.evidence.v1",
    "paymentId": "7343e8a4-06fe-4f18-bbbe-cb6f35832ea4",
    "snapshotId": "sha256:d8e8fca2dc6b1298402b85a3...",
    "collectedAt": "2026-10-12T08:00:15Z",
    "payment": {
      "state": "PENDING_RECONCILIATION",
      "amountPaise": "25000",
      "currency": "INR",
      "createdAt": "2026-10-12T08:00:00Z",
      "updatedAt": "2026-10-12T08:00:10Z",
      "completedAt": null,
      "eventVersion": "4",
      "evidenceId": "payment:7343e8a4-06fe-4f18-bbbe-cb6f35832ea4:d8e8fca2dc6b..."
    },
    "route": {
      "sourceBankCode": "BANK-A",
      "destinationBankCode": "BANK-B",
      "executionTargetId": "RAIL-PRIMARY",
      "selectedAt": "2026-10-12T08:00:01Z",
      "reasonCode": "HEALTH_ROUTED",
      "status": "available"
    },
    "operations": [
      {
        "operationId": "e1f1a1b1-1111-4111-8111-111111111111",
        "operationType": "DEBIT_HOLD",
        "bankCode": "BANK-A",
        "centralStatus": "COMMITTED",
        "participantStatus": "SUCCEEDED",
        "presence": "present",
        "lastObservedAt": "2026-10-12T08:00:15Z",
        "sourceStatus": "available",
        "evidenceId": "operation:e1f1a1b1-1111-4111-8111-111111111111:d8e8fca..."
      }
    ],
    "transitions": [
      {
        "id": "t1111111-1111-4111-8111-111111111111",
        "sequence": "1",
        "fromState": "CREATED",
        "toState": "ROUTING",
        "occurredAt": "2026-10-12T08:00:01Z",
        "evidenceId": "transition:t1111111-1111-4111-8111-111111111111"
      }
    ],
    "transitionsTruncated": false,
    "recovery": {
      "status": "deferred",
      "attemptCount": 2,
      "nextAttemptAt": "2026-10-12T08:01:00Z",
      "lastAttemptId": "a1111111-1111-4111-8111-111111111111",
      "reasonCode": "PARTICIPANT_TIMEOUT",
      "sourceStatus": "available"
    },
    "reconciliation": {
      "sourceStatus": "not_collected",
      "runIds": [],
      "discrepancyCount": null,
      "scopeMismatch": false
    },
    "integrity": {
      "sourceStatus": "available",
      "runId": "i1111111-1111-4111-8111-111111111111",
      "occurredAt": "2026-10-12T07:55:00Z",
      "violationCount": 0
    },
    "traces": {
      "sourceStatus": "available",
      "traceIds": ["4bf92f3577b34da6a3ce929d0e0e4736"],
      "truncated": false
    },
    "sources": [
      {
        "name": "central",
        "status": "available",
        "observedAt": "2026-10-12T08:00:15Z",
        "errorCode": null
      }
    ],
    "warnings": []
  }
}
```

---

## 3. Recovery Recheck Control (`POST /api/ops/payments/{paymentID}/recovery/recheck`)

### Requirements
- **Role**: `OPS_ADMIN` only (`OPS_VIEWER` receives `403 Forbidden`).
- **Feature Flag**: Rejected with `403 Forbidden` (`RECOVERY_CONTROL_DISABLED`) when `cfg.RecoveryControlEnabled == false`.
- **Headers**: Mandatory `Idempotency-Key`.
- **Body**: `{"reason": string}` (1–200 characters).
- **Rate Limit**: Maximum 5 requests per minute per actor (`429 RATE_LIMIT_EXCEEDED`).
- **Behavior**:
  - Terminal payment (`COMPLETED`, `FAILED`, `REVERSED`): returns `200 OK` with `status: "already_terminal"`.
  - Non-terminal payment: updates or inserts recovery job with `status = 'scheduled'` and `due_at = NOW()`, records an entry in `operational_audit_logs`, and returns `202 Accepted` with `status: "scheduled"`.
  - **Financial Safety**: Never executes monetary transactions or marks payments completed/failed within HTTP request!

---

## 4. Evidence Export (`GET /api/ops/payments/{paymentID}/evidence/export`)

Pairs the complete `EvidenceBundle` with a cryptographic manifest binding the canonical bundle SHA-256 hash:

```json
{
  "requestId": "req-export-1",
  "data": {
    "bundle": { ... },
    "manifest": {
      "paymentId": "7343e8a4-06fe-4f18-bbbe-cb6f35832ea4",
      "snapshotId": "sha256:d8e8fca2dc6b1298402b85a3...",
      "bundleSha256": "4a7f29b01c38e928271829...",
      "exportedAt": "2026-10-12T08:00:20Z",
      "algorithm": "SHA-256",
      "note": "Canonical redacted evidence export. No authenticity claim."
    }
  }
}
```
