# Shared API and integration contracts

Contract version: `tx.v2.1`. M2 owns changes. M1 and M3 may propose a correction with a concrete compatibility test; they must not silently diverge. Existing APIs and error envelopes remain compatible.

## General rules

- Use existing JWT middleware. Add OPS_VIEWER for approved operational reads. All mutation endpoints remain OPS_ADMIN and obey simulation/feature gates.
- Success envelope: `{requestId, data}`. Failure envelope: `{requestId, error:{code,message}}`. Never return raw SQL, provider content, secrets or bank credentials.
- UUIDs are canonical strings; UTC timestamps are RFC3339 with subsecond precision where available. Durations are explicit `...Ms`; counters above JavaScript's exact integer range and all new money fields are decimal strings.
- New lists use cursor pagination: `limit` default 25, maximum 100; response `{items,nextCursor,asOf}`. Cursor is opaque validated base64url JSON of the documented sort keys, at most 1 KiB. It is not a database offset or a promise of snapshot isolation.
- Use stable identifier tie breakers. Reject invalid filters with 400. Return 404 for nonexistent or unauthorized entities where disclosure would be unsafe. 401 means missing/expired authentication; 403 means an authenticated role cannot use the capability.
- Read calls time out rather than mutate to make progress. Evidence endpoints do not invoke recovery, health sampling, integrity scans, reconciliation execution or monetary operations.
- Validate lengths, timestamps, maximum body size and JSON unknown fields. Every source status is `available`, `unavailable`, `not_applicable` or `not_collected`; missing evidence is never a zero measurement.

## Endpoints

| Method and route | Permission | Purpose |
| --- | --- | --- |
| GET `/api/ops/worklist` | OPS_ADMIN, OPS_VIEWER | Derived payment/recovery worklist |
| GET `/api/ops/payments/{paymentID}/evidence` | OPS_ADMIN, OPS_VIEWER | Read-only central and participant evidence |
| GET `/api/ops/payments/{paymentID}/recovery-attempts` | OPS_ADMIN, OPS_VIEWER | Bounded ordered attempt history |
| POST `/api/ops/payments/{paymentID}/recovery/recheck` | OPS_ADMIN; control enabled | Expedite a scheduler job; does not perform money work in HTTP |
| GET `/api/ops/payments/{paymentID}/evidence/export` | OPS_ADMIN, OPS_VIEWER | Redacted JSON bundle and hash manifest |
| GET `/api/ops/metrics/summary` | OPS_ADMIN, OPS_VIEWER | Windowed durable analytics and current runtime observations |
| GET `/api/ops/traces/{traceID}` | OPS_ADMIN, OPS_VIEWER | Allowlisted bounded telemetry snapshot |
| GET `/api/ops/analytics/windows` | OPS_ADMIN, OPS_VIEWER | Operational aggregate windows and advisory anomalies |
| POST `/api/ops/investigations` | OPS_ADMIN, OPS_VIEWER; limits apply | Start bounded investigator job |
| GET `/api/ops/investigations/{investigationID}` | Authorized operational role and scope | Fetch status/report/tool timeline |
| GET `/api/ops/runtime` | OPS_ADMIN, OPS_VIEWER | Existing simulation field plus optional capability fields |

Existing operational GET endpoints may allow OPS_VIEWER only after their DTOs are audited for disclosure. Existing operational POST endpoints must not grant it mutation access. A viewer cannot invoke integrity/reconciliation checks through a renamed read tool.

## Worklist contract

Filters: `state`, `recoveryStatus`, `bankCode`, `paymentId`, `createdFrom`, `createdTo`, `cursor`, `limit`. `paymentId` is exact UUID search. `state` accepts one state or a bounded comma-separated list of allowed central states; unknown states are rejected. Default scope is nonterminal/attention payments unless a terminal state filter is supplied. Free-form global search across personal data is outside this release. Default order: `updatedAt DESC, paymentId DESC`. Cursor includes normalized filter hash to reject reuse with a different filter. Projection update order may move a row between pages; the UI offers refresh and displays `asOf`.

Item fields: `paymentId`, `state`, `amountPaise` string, `currency`, `sourceBankCode|null`, `destinationBankCode|null`, `createdAt`, `updatedAt`, `ageMs`, `recoveryStatus` (`none|scheduled|running|deferred|needs_attention|resolved`), `attemptCount`, `nextAttemptAt|null`, `reasonCode|null`, `projectionVersion` string. `ageMs` is server-computed since payment creation, not time since latest attempt. Response also has `projection:{status,updatedAt,oldestPendingEventAgeMs|null}`. `status` is `ready|lagging|rebuilding|unavailable`; threshold for lagging is configurable, default 10 seconds.

## Payment evidence contract

`EvidenceBundle` has `schemaVersion`, `paymentId`, `snapshotId`, `collectedAt`, `payment`, `route`, `operations`, `transitions`, `transitionsTruncated`, `recovery`, `reconciliation`, `integrity`, `traces`, `sources` and `warnings`.

- `payment`: authoritative central `state`, `amountPaise` string, `currency`, `createdAt`, `updatedAt`, `completedAt|null`, `eventVersion` string and `evidenceId`. Do not include a full name, identifier, note, token or request idempotency key in operational evidence by default.
- `route`: persisted `sourceBankCode|null`, `destinationBankCode|null`, `executionTargetId|null`, `selectedAt|null`, `reasonCode|null`, `status`. Preserve the original execution target; do not present an alternate current target as the route used.
- `operations[]`: `operationId`, `operationType`, `bankCode`, `centralStatus`, `participantStatus|null`, `presence` (`present|absent_confirmed|unknown|not_checked`), `lastObservedAt|null`, `sourceStatus`, `evidenceId`. Read operation status at most once per operation per evidence request, within a shared deadline. Limit to 20 operations; unexpected excess produces a warning, not silent truncation.
- `transitions[]`: `id`, `sequence` string, `fromState`, `toState`, `occurredAt`, `evidenceId`. Ordered by per-payment transition sequence; top-level `transitionsTruncated` is true if more than 200 entries.
- `recovery`: `status`, `attemptCount`, `nextAttemptAt|null`, `lastAttemptId|null`, `reasonCode|null`, `sourceStatus`. History is paginated separately.
- `reconciliation`: `{sourceStatus,runIds,discrepancyCount|null,scopeMismatch:boolean}`. Include runs only if participant and scope support a genuine relationship. A run overlapping creation time is context, not proof that a particular operation was inspected.
- `integrity`: `{sourceStatus,runId|null,occurredAt|null,violationCount|null}`. Global checks are marked global context. Never claim a global zero violations proves this specific payment's outcome.
- `traces`: `{sourceStatus,traceIds,truncated}`; telemetry is context, not financial proof.
- `sources[]`: `{name,status,observedAt|null,errorCode|null}`. Errors are stable sanitized codes.
- `warnings[]`: `{code,message}`. Messages are safe operator copy.

Snapshot identity is SHA-256 of canonicalized redacted evidence including observation times and ordered arrays, excluding the `snapshotId` field itself. Evidence IDs are assigned before hashing and do not contain the final hash recursively. It identifies exact collected contents, not authenticity or immutable storage. Every fact returned to the investigator has a stable evidence ID and source pointer within this snapshot. An export manifest lists payload hashes and is not included in its own hash.

Evidence fetch deadline: 3 seconds overall. Capture central fields in a short read-only repeatable-read transaction, close it, then perform bounded participant requests; observation times reveal cross-service time differences. It is not a globally atomic snapshot. Participant failures produce partial evidence with 200 when central authority is available; central authority unavailable produces 503. Unknown payment produces 404.

## Recovery attempt and control contracts

Attempt: `attemptId`, `paymentId`, `workerId`, `startedAt`, `finishedAt|null`, `stateBefore`, `stateAfter|null`, `result` (`running|progressed|resolved|no_progress|deferred|error|interrupted`), `reasonCode|null`, `operationIds`, `traceId|null`, `leaseEpoch` string. Public viewers do not receive hostnames or secret-like worker identifiers; expose a stable sanitized worker label.

Recheck body: `{reason}` with 1-200 safe characters. Require `Idempotency-Key`. Same actor/key/request repeats the same accepted scheduling request; different body conflicts. Respond 202 `{paymentId,scheduledAt,status:"scheduled"}`. Terminal payments return 200 `status:"already_terminal"`. Rate limit five per minute per actor. Recheck clears operational parking only after validating the job and logs an audit fact; it never marks a payment failed or completed.

## Investigator contract

Create body: `{paymentId,question}`; question 1-1000 characters. Only a selected payment is supported initially. Identity, role and provider selection are server-owned. Require an idempotency key for create; duplicate polling never starts a second provider call. Respond 202 `{investigationId,status,mode}`.

Statuses: `queued|running|completed|failed|cancelled`. Modes: `evidence_only|llm`. Report fields:

```json
{
  "schemaVersion": "tx.investigation.v1",
  "mode": "evidence_only",
  "summary": "The payment remains pending because destination evidence is unavailable.",
  "facts": [{"text": "The central payment is PENDING_RECONCILIATION.", "evidenceIds": ["payment:example:snapshot"], "assertions": [{"field": "central_state", "value": "PENDING_RECONCILIATION", "entityId": "selected-payment", "evidenceId": "payment:example:snapshot"}], "contradiction": false}],
  "inferences": [],
  "recommendations": [{"text": "Check the destination service runbook.", "runbookChunkIds": ["runbook:destination:1"]}],
  "missingEvidence": ["destination_operation_status"],
  "evidenceCoverage": "limited",
  "snapshotId": "sha256:example",
  "collectedAt": "2026-10-11T00:00:00Z"
}
```

The values above illustrate shape, not real evidence; complete fixtures in the contracts folder are schema-valid. `evidenceCoverage` is `complete_for_question|limited|insufficient`, computed by deterministic evidence requirements, never a model probability. Facts have typed `assertions` and a required `contradiction` boolean; inferences have `evidenceIds` and `contradiction`; recommendations have runbook chunk citations. Assertion fields are `central_state|amount_paise|participant_status|operation_id|bank_code|route_target|recovery_status`, with string value, nullable entityId and evidenceId. Tool timeline records `toolName`, sanitized arguments, start/end times, result status and evidence IDs. Usage records actual token counts if returned by a provider, otherwise null. Provider failures never expose partial unvalidated reports as completed.

Return report at GET with `id,status,mode,report|null,toolTimeline,error|null,createdAt,completedAt|null,usage`. Limit active jobs globally and per actor. Evidence-only mode completes without a provider request.

## Metrics, trace and analytics contracts

Summary: `windowFrom`, `windowTo`, `asOf`, `payments:{attempted,completed,failed,pending}`, `recovery:{due,running,needsAttention,oldestDueAgeMs}`, `outbox:{pending,quarantined,oldestPendingAgeMs}`, `runtime:{workerLastHeartbeatAt|null,poolWaitMs|null}`, `sources`. Counters are decimal strings; ratios are nullable floats with explicit denominator and minimum sample policy.

Trace: lowercase hex trace ID (32 chars), `sourceStatus`, `collectedAt`, `truncated`, `spans[]` with span ID, parent ID/null, service, allowlisted name, start/end, status and safe attributes. Maximum 200 spans. Reject malformed trace ID. A missing or expired trace is `not_collected`, not a payment failure.

Analytics window: `id`, `entityType` (`bank|execution_target`), `entityId`, `from`, `to`, `sampleCount`, `featureSchemaVersion`, `features`, `baselineAlert|null`, `model:{version|null,status,score|null,threshold|null}`, `advisory:true`. Model status is `ready|insufficient_data|stale|unavailable|out_of_distribution`. No action recommendation in this response can authorize money or reroute a live operation.

Runtime capability fields extend the existing response: `simulationMode`, `canControlRecovery`, `canRunChaos`, `canInvestigate`, `investigatorMode`, `traceCollectionEnabled`, `analyticsEnabled`, `modelStatus`. Control booleans are computed from both current role and environment flags. Existing clients may ignore new fields. Recovery attempts and analytics windows use the standard list shape. Investigation create returns only the accepted identity/status/mode, not the complete GET result.

## Participant presence capability

Private GET `/internal/operations/{operationID}/presence?paymentId={paymentID}` returns `{paymentId,operationId,presence:"present"|"absent",observedAt,status|null}`. Require the participant service token and validate identities. `present` uses the durable participant operations table. `absent` means an authoritative successful read found no retained operation; it is not inferred from an error or timeout.

This is a separate `OperationPresenceReader` interface, not an added BankAdapter method. The original participant execution method must enforce uniqueness and payload identity against concurrent/delayed requests. Absence plus that proven execution contract permits replay of the original immutable operation; absence alone is insufficient. Adapters lacking this capability remain parked on unknown status.

## Shared integration seams

- `RecoveryResumer.Resume(ctx,paymentID) -> ResumeResult` lives in payments. It owns serialization and rereads state. All request/worker paths use it; low-level continuation helpers assume the lock is already held.
- `EvidenceReader.ReadPaymentEvidence(ctx, trustedScope, paymentID) -> EvidenceBundle` lives at the read boundary. M3 owns a matching local interface; M2 supplies the implementation/HTTP adapter.
- `Investigator.RegisterRoutes` returns handlers or mounts on a provided authenticated group; it must not bypass common envelopes/authentication. M2 mounts it in the shared API composition.
- `AnomalyScorer.Score(FeatureVector) -> ScoreResult` is pure, bounded Go. M3 owns it; M2 wires it into the worker's aggregate job.
- M1 uses `frontend/src/ops/contracts.ts`, `client.ts`, and fixture adapters. No duplicated API-base lookup in page files.

## Contract verification

Commit named fixtures for available, partial, terminal, unknown, unauthorized, empty and lagging responses. Backend serializer tests and frontend decoder/render tests use the same fixture shapes. New DTOs are explicit types, not unrestricted maps. Reject schema drift in CI by validating required fields/types and enums against `contracts/v2.schema.json`. Freeze incompatible changes until all three consumers have matching tests.
