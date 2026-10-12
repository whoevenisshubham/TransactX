# ADR-036: Read-Only Payment Investigator, Runbook Corpus, and Worker Claim Execution

## Status
**IMPLEMENTED** (Task B11 complete)

## Context
TransactX Milestone 2 requires an automated, operator-facing payment investigator to analyze stuck, pending, or ambiguous payments without requiring manual database inspections, raw SQL queries, or risky ad-hoc operational interventions.

The operational investigator must operate under strict platform, reliability, and security invariants:
1. **Zero External Spending & Zero Token Cost Default**: The platform operates in `evidence_only` mode by default. It constructs grounded factual summaries, missing evidence lists, coverage assessments, and runbook recommendations entirely in native Go without calling external paid LLM APIs or incurring external token costs.
2. **Grounded Facts, Assertions, and Citations**: Every reported fact must be verifiable against authoritative database evidence snapshots, citing stable `evidenceIds`, typed `assertions` (`central_state`, `amount_paise`, `participant_status`, `operation_id`, `bank_code`, `route_target`, `recovery_status`), and an explicit `contradiction` boolean.
3. **Approved Runbook Citations**: Recommendations must cite approved, versioned operational runbook chunks stored in the local corpus store (`runbook_corpus_chunks`), seeded directly from verified system runbooks.
4. **Active Bounds & Rate Limiting**: To prevent resource exhaustion, the system enforces a strict concurrency ceiling: at most 1 active (queued or running) job per actor, at most 2 active jobs globally, and a sliding-window rate limit of 3 investigation requests per actor per minute.
5. **Strict Idempotency & Conflict Detection**: Job creation requires an `Idempotency-Key` header. Identical requests replay the existing job with `202 Accepted`; conflicting payloads using the same key are rejected with `409 Conflict`.
6. **Distributed Worker Lease Protocol**: Background execution uses `FOR UPDATE SKIP LOCKED` claim queries with monotonic lease epochs and lease timeouts. Completion uses Compare-And-Swap (CAS) on `lease_epoch` to discard late results from superseded workers.
7. **Zero Financial Mutation Authority**: The investigator package interacts with payments and banking data exclusively through read-only interfaces (`operations.EvidenceReader`). It holds zero authorization to credit, debit, reverse, or settle funds.

## Decisions

### 1. Database Schema and Migration (Migration 000027)
Migration 000027 (`000027_investigation_store.up.sql`) establishes three durable relational tables:
- `investigation_jobs`: Stores job metadata, status (`queued`, `running`, `completed`, `failed`, `cancelled`), mode (`evidence_only`, `llm`), question, structured JSON report, tool timeline, usage info, and lease tracking (`lease_owner`, `lease_epoch`, `lease_expires_at`). Unique index on `(actor_id, idempotency_key)`.
- `runbook_corpus_chunks`: Stores approved runbook documentation chunks with unique constraint on `(corpus_version, document_path, heading, start_line)` and PostgreSQL full-text search `tsvector` index (`to_tsvector('english', heading || ' ' || content)`).
- `investigation_usage_reservations`: Tracks token reservations and commits with a daily cap check constraint.

### 2. Evidence-Only Deterministic Execution Pipeline
The investigator service (`internal/investigator/service.go`) implements a deterministic pipeline:
- **Tool 1 (`read_payment_evidence`)**: Retrieves the authoritative `operations.EvidenceBundle` via `operations.EvidenceReader`, capturing payment state, route, operations, transitions, and recovery status.
- **Tool 2 (`retrieve_runbook`)**: Performs full-text search with tag boosting against `runbook_corpus_chunks` matching the payment's current state and failure mode.
- **Fact & Assertion Construction**: Synthesizes verified facts with typed assertions (e.g. `central_state = PENDING_RECONCILIATION`, `amount_paise = ...`, participant presence status).
- **Missing Evidence & Coverage**: Evaluates deterministic evidence requirements (e.g. flagging missing participant confirmation or recovery attempts), setting coverage to `complete_for_question`, `limited`, or `insufficient`.
- **Timeline & Usage**: Emits ordered `ToolStep` records tracking tool name, arguments, execution duration, and evidence IDs, accompanied by zero-token `UsageInfo` (`evidence_only.v1`).

### 3. Seeded Runbook Corpus
System runbooks located in `docs/runbooks/` are automatically seeded into `runbook_corpus_chunks` during environment startup:
- `docs/runbooks/pending-reconciliation.md`: Handling payments in `PENDING_RECONCILIATION` and missing destination operations.
- `docs/runbooks/bank-outage-recovery.md`: Managing transient bank outages, exponential backoff, and `ATTEMPTS_EXHAUSTED` rechecks.
- `docs/runbooks/central-pending-continuation.md`: Continuation procedures for `BANK_SETTLED_CENTRAL_PENDING`.
- `docs/runbooks/operation-presence.md`: Authoritative participant operation presence verification and safe replay conditions.

### 4. Background Worker Integration
`backend/internal/app/worker.go` includes an independent background processing loop:
- Runs every 2 seconds, claiming eligible queued or expired jobs via `ClaimNextJob(ctx, workerLabel, leaseDuration)`.
- Executes `ExecuteJob(ctx, job)` in a bounded transaction, recording timeline steps and transitioning the job to `completed`.
- Emits structured operational log entries correlated with job ID, payment ID, and worker identity.

### 5. Operational REST Endpoints
Mounted in `backend/internal/http`:
- `POST /api/ops/investigations`:
  - Roles: `OPS_ADMIN`, `OPS_VIEWER`.
  - Headers: Required `Idempotency-Key`.
  - Body: `{paymentId, question}` (question bounded between 1 and 1000 characters).
  - Returns `202 Accepted` with `InvestigationAccepted` DTO: `{investigationId, status: "queued", mode: "evidence_only"}`.
  - Replays `202` on duplicate request; returns `409 Conflict` on payload mismatch; returns `429 Too Many Requests` on active or rate limit exhaustion.
- `GET /api/ops/investigations/{investigationID}`:
  - Roles: `OPS_ADMIN`, `OPS_VIEWER`.
  - Returns `200 OK` with full `Investigation` DTO adhering to `v2.schema.json` and `tx.investigation.v1`.
- `GET /api/ops/runtime`:
  - Includes `"canInvestigate": true` and `"investigatorMode": "evidence_only"`.

## Consequences
- **Positive**: Operators can diagnose complex payment delays in seconds with fully grounded, cited facts and runbook recommendations without direct database access.
- **Positive**: Zero API spending and zero external LLM dependencies in the default deployment mode.
- **Positive**: High testability and predictability: deterministic reports are verifiable against frozen JSON schema contracts in automated CI suites.
- **Safety**: The investigator cannot modify financial balances, retry money operations, or change payment state.
- **Graceful Degradation**: If the investigator service is unconfigured or stopped, the HTTP handler returns `503 SERVICE_UNAVAILABLE` on investigator routes without disrupting payment transactions, recovery scheduling, or reconciliation.
