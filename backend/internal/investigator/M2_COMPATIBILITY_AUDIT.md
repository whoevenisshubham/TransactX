# M2 B10/B11 compatibility audit

Audit target: origin/v2/m2-reliability-platform at c64a9e62870e4ed802910b9ee84e0dee73ffc9bb (13 October 2026). The branch was inspected with Git object reads only; it was not merged, rebased or cherry-picked into M3.

## Decision

The M2 branch makes B10/B11 source available, but it is not a drop-in live integration for the completed M3 implementation. It independently defines another backend/internal/investigator model, service and repository. Combining both file sets unchanged would create duplicate package declarations and, more importantly, would mount the less complete workflow instead of M3's evaluated C1-C4 state machine.

M1/M2 integration must retain M3's investigator service, public DTO validation, corpus lifecycle, grounding and evaluation implementation. M2's operations and analytics work should be consumed through adapters, and its storage/migration must be reconciled to the M3 interfaces before mounting. Do not run both investigator services and do not delete or reauthor either branch's history.

## Observed contract differences

| Area | M2 branch at audited SHA | Required integration result |
| --- | --- | --- |
| Investigator package | Independently defines model.go, service.go and repository.go; ExecuteJob has two fixed reads and no M3 checkpoint/provider state machine | Keep the M3 package implementation and replace the M2 investigator composition with adapters implementing M3 EvidenceReader, RunbookRetriever and InvestigationStore |
| Trusted evidence | operations.EvidenceReader accepts an opaque string plus UUID; the current service does not enforce actor/role scope itself | The authenticated handler constructs M3 TrustedScope, verifies selected payment before the read, and never accepts actor/role/payment scope from a body or model |
| Evidence DTO | M2 operations.EvidenceBundle serializes the shared tx.evidence.v1 shape and bounds operations to 20 | A composition-layer EvidenceJSONSource marshals that DTO and is mounted through BridgedEvidenceReader, which preflights trusted scope before strict bounded decoding and identity validation |
| Job durability | M2 investigation_jobs stores basic job/report/timeline state | Implement the full M3 InvestigationStore: request hash and role, absolute deadline, lease-fenced checkpoints, attempt counts, evidence/retrieval/provider continuation, exact hashes and report metadata |
| Reservations | M2 stores one reserved/actual token count and reserved/committed/released status | Store separate input/output allowance and actual usage, cost cap, provider/request identity and an uncertain terminal state; keep reservation and lease transitions transactional |
| Corpus | M2 runbook_corpus_chunks has an active flag and SeedRunbooks inserts four ad-hoc chunks | Use corpus_versions plus immutable runbook_chunks, stage exact tx.corpus.c1.v1 manifest content, role-filter with simple websearch_to_tsquery, activate atomically, retain retired citations, and do not call SeedRunbooks |
| B10 features | M2 emits the six tx.features.v1 named nullable fields in the correct schema | Call anomaly.EvaluateLiveFeatures so observation time, ordering, copies, ranges, finiteness and schema are checked before baseline/model scoring |
| B10 baseline | M2 flags fixed failureRate > 0.15 or timeoutRate > 0.10 | Query only independently designated healthy durable rows for the same entity, pass them through M3 FitLiveBaseline, and keep threshold 3 with an eight-window per-feature warmup |
| B10 model | M2 always records unavailable/insufficient_data | Call anomaly.EvaluateLiveFeatures with server-owned observation time. Baseline remains default; load the experimental model only through the pinned-hash registry and never use it for payment policy |
| HTTP/worker mount | M2 mounts its local service/repository | Mount M3 CreateInvestigationRequest/CreateResponse/PublicInvestigation, inject trusted scope from existing auth, run M3 Service.Run under epoch-fenced claims, and keep provider budget zero by default |

Migration 000027 has already been named on M2, so integration must reconcile applied-database history deliberately. A replacement before release or a forward migration for any already-applied environment must add the M3 fields/tables without silently treating the audited schema as complete. M3 does not own that migration and did not edit it here.

## Adapter sequence

For live evidence, a composition-layer `EvidenceJSONSource` may import both operations and investigator without creating a package cycle. It parses the already validated selected payment UUID, invokes M2's reader with server-owned scope, and serializes the returned DTO. `BridgedEvidenceReader` validates actor, allowed role and selected-payment equality before invoking that source, then performs strict decode and identity validation. Reader errors and decoder errors must be sanitized at the HTTP boundary.

For live analytics, query prior independently designated healthy B10 rows for the same entity and call anomaly.FitLiveBaseline with the scored window's start. This enforces unique IDs/window keys, exact five-minute UTC boundaries, no overlap/future leakage, the frozen feature schema and a 10,000-row safety bound; it cannot determine whether M2's healthy designation itself is correct, so that query remains an owned review gate. Map the scored window FeatureSchemaVersion, SampleCount and named Features into anomaly.LiveFeatureValues and call anomaly.EvaluateLiveFeatures with a server-owned observation time. Persist the original M3 baseline result and model status/version/score/threshold with the completed window. Never recalibrate from the window being scored or from later windows.

## Required live acceptance gates

- Canonical migration applies from release base and upgrades an audited M2-style schema without data loss; downgrade/rollback behavior is reviewed.
- The exact C1 corpus stages and activates through the canonical schema; role-filtered PostgreSQL retrieval and retired citation resolution pass.
- Concurrent actor idempotency, claim/lease loss, checkpoint resume and usage reservation tests pass against PostgreSQL.
- Authenticated POST/GET success and cross-actor/payment/role denial paths exercise the mounted M3 service.
- A real operations evidence read passes BridgedEvidenceReader; foreign identity is rejected before the source call, while unknown fields, oversized output and source timeout fail closed.
- B10 durable windows calibrate through FitLiveBaseline, map through EvaluateLiveFeatures, and persist baseline-default results. Tests must include cross-entity, duplicate, overlap/future and malformed-window rejection. Model-off, stale, missing, insufficient and out-of-distribution states remain explicit.
- M1 validates real mounted responses and browser states. Offline fixtures, the disposable C1 DDL and the synthetic anomaly evaluation do not satisfy these live gates.

No provider request, paid resource, shared migration edit or production corpus activation was performed by this audit.
