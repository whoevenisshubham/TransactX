# Technology decisions and reservations

## Decisions

| Decision | Selected approach | Why and simpler alternative |
| --- | --- | --- |
| Application structure | Existing Go modular backend plus separate bank and worker processes | Enough real distributed failure boundaries; avoid unnecessary service proliferation |
| Recovery | Existing saga resumer, shared lock, durable scheduling leases | Adds autonomous progress while keeping one deterministic money implementation |
| Ambiguous pre-dispatch crash | Separate authoritative presence capability and original-payload replay | Fixes a demonstrated gap without replacing the nine-method BankAdapter |
| Events | PostgreSQL transactional outbox and inbox | Solves dual-write loss with existing storage; polling read tables alone cannot guarantee delivery |
| Projection | Refresh current authoritative snapshot on event | Avoids stale ordering regression; future incremental consumers need separate ordering policy |
| Observability | OpenTelemetry Go, JSON slog, bounded trace storage/export | Standards-based signals and useful free demo; no mandatory large telemetry stack |
| Retrieval | Approved PostgreSQL full-text corpus | No embedding API/vector service expense; semantic embeddings can be evaluated later |
| AI orchestration | Explicit Go typed read tools and bounded state machine | Easy to test authorization and budgets; no framework dependency is needed for agentic workflows |
| Runtime generator | Optional configured OpenAI/local adapter | Real RAG when enabled; zero-budget default remains evidence-only |
| ML | Statistical baseline plus small IsolationForest trained offline, scored in Go | Fits operational anomalies; deep learning has no justified dataset/benefit here |
| Online hosting | Static frontend plus native free-eligible Linux VM | Fits long-lived worker, HTTP banks and SSE; Vercel frontend deployment stays simple |
| Containers | Deferred | Existing CI service container stays; no new local Docker or Kubernetes requirement |
| Backups | Whole-database logical backup plus isolated restore verification | Tests recovery of the shared-server deployment; PITR/replication deferred |

## Migration numbers

M2 is the only migration committer. Before using these reservations, verify upstream has not advanced past 000021. If it has, shift the entire new sequence once, update registry/contracts together, and record the mapping. Do not create duplicate numeric prefixes.

| Reserved ID | Purpose |
| --- | --- |
| 000022 | Recovery jobs, attempts, heartbeat and indexes |
| 000023 | OPS_VIEWER, public demo/security support, control idempotency/audit |
| 000024 | Payment event version, immutable outbox and delivery state, atomic producer triggers |
| 000025 | Consumer inbox, worklist projection and maintenance metadata |
| 000026 | Safe trace spans and bounded operational observations |
| 000027 | Approved corpus versions/chunks, investigation jobs/reports/usage reservations |
| 000028 | Operational windows, anomaly versions/scores and aggregate job cursor |
| 000029 | Optional bounded maintenance/reconciliation jobs only if P2 is reached |

Do not modify old migrations to make a new database match current code. Each new migration has schema acceptance, rollback implications and checksum. DOWN scripts that would delete evidence require explicit offline use; release rollback normally disables features or restores compatible binaries.

## ADR reservations

Current last entry is ADR-031. M2 maintains the central index in `docs/decisions.md`; owner-authored complete records can live in `docs/adr/` and be referenced once.

- ADR-032 shared financial recovery serialization and participant presence.
- ADR-033 durable leased recovery scheduler and bounded attempts.
- ADR-034 transactional outbox and idempotent snapshot projections.
- ADR-035 per-aggregate versions and no global sequence watermark.
- ADR-036 read-only investigator, approved retrieval and truthful provider modes.
- ADR-037 operational anomaly model and deterministic policy separation.
- ADR-038 free-tier native deployment and single failure domain.
- ADR-039 public demo RBAC, gated controls and service authentication.
- ADR-040 bounded OpenTelemetry storage and non-authoritative telemetry.
- ADR-041 consistent backup and verified restore.

Every implementation ADR contains context, decision, alternatives, reasons, consequences, new failure modes, migration, rollback and actual implementation status. The plan's reservation is not an IMPLEMENTED ADR.

## Dependency policy

Read current official documentation only for a narrow API uncertainty. Choose a supported exact release, update the appropriate lockfile and verify compatible toolchains. M2 owns Go dependencies; M1 frontend dependencies; M3 offline Python dependencies. Do not upgrade the entire stack as part of the plan.

Prefer standard-library HTTP/JSON and existing pgx abstractions. Add OpenTelemetry instrumentation/exporters; a focused schema validator if contract tests require it; Playwright/accessibility test tooling for browser verification; scikit-learn only in offline analytics. Avoid extra SDKs where a small typed adapter is adequate, but do not hand-roll cryptography or JWT.
