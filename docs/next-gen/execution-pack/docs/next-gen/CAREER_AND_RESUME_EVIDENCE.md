# Interview and resume evidence map

This is a portfolio simulator, not real-money infrastructure or a claim of regulatory compliance. Cohesity alignment emphasizes recoverability, data integrity, APIs, Go, observability and operational automation. Barclays/fintech alignment emphasizes financial invariants, authorization, idempotency, unknown outcomes and auditable operational evidence. This mapping is an engineering interpretation, not a guarantee of hiring success.

## Preferred project areas

| Area | Concrete release evidence | Limit |
| --- | --- | --- |
| AI agents and intelligent assistants | Bounded read tools, evidence collection, authorized scope and validated reports | Agent cannot change money; evidence-only mode is labelled |
| OpenAI or Claude applications | Optional real OpenAI adapter with structured reports/evals | Live claim only after actual provider verification; Claude optional |
| Full-stack applications | Role UIs, typed APIs, worklist, timelines, SSE and offline replay | Real browser evidence required |
| Cloud-native applications | Configured stateless API boundaries, worker, TLS, health/shutdown and release artifacts | Single VM deployment, not high availability |
| Kubernetes/container platforms | Future explicit container deployment path | Not part of immediate release or resume stack |
| DevOps/automation | CI, migrations, native service setup, release/rollback, restore rehearsal | Account/hosting access still required |
| Analytics/ML | Durable windows, baseline, held-out synthetic anomaly evaluation and native scorer | Operational ML, not proven real fraud detection; no forced DL |
| Infrastructure/networking/admin | Linux services, DNS/TLS/CORS, private participants, quotas and backup recovery | No production multi-region claim |

## Target technology line

Go, PostgreSQL, React, TypeScript, HTTP/REST, SSE, IndexedDB, SHA-256/Merkle trees, OpenTelemetry; PostgreSQL outbox/full-text retrieval; optional OpenAI RAG/tool calling; Python/scikit-learn for evaluated offline anomaly training.

Select only technologies actually implemented and evidenced. Do not list LangChain, Docker, Kubernetes, Kafka, deep learning or Claude merely because they were discussed.

## Five target statements to use only after gates pass

- Built JWT/RBAC payment APIs with integer double-entry ledgers, user-scoped idempotency and durable sagas across simulated bank services.
- Implemented leased recovery workers and original-operation outcome resolution; verified crash/restart and concurrent-worker safety with [MEASURED RECOVERY TIME] and zero observed invariant violations in the specified tests.
- Added a transactional PostgreSQL outbox, idempotent worklist projections and rebuild tooling; measured [P95 PROJECTION LAG] under [DOCUMENTED LOAD].
- Instrumented cross-service OpenTelemetry traces and operational metrics, and shipped role-based payment evidence workflows with [VERIFIED BROWSER/DEPLOYMENT STATUS].
- Built a read-only investigator using approved-runbook retrieval and [ACTUALLY VERIFIED GENERATOR MODE], with [MEASURED RETRIEVAL/FACTUALITY RESULTS]; evaluated an operational anomaly model against a statistical baseline on held-out synthetic scenarios.

These are target drafts with placeholders, not accurate current accomplishments. Use present status and existing RC1 claims until the corresponding results exist. A shorter four-bullet version may combine recovery/events and omit a weak or unverified ML result.

## Defense map

| Statement | Concepts to understand | Code and evidence | Failure explanation |
| --- | --- | --- | --- |
| Payment kernel | ACID, double entry, idempotency scope, integer precision, saga | payments/ledger/bankservice and existing integration tests | Why timeout is not failure; why two banks are not one DB transaction |
| Recovery | Serialization versus lease, fencing/CAS, stable operation identity, absence proof | resumer/recovery and crash matrix | Delayed original call, lease expiry, source confirmed and destination unknown |
| Outbox | Dual-write loss, at-least-once, inbox dedup, per-aggregate versions | outbox/projector/rebuild and ack-loss/late-commit tests | Consumer commits before ack; low sequence commits late; stale event arrives last |
| Observability/product | Context propagation, async links, sampling, cardinality, UI freshness | telemetry/evidence/frontend/browser artifacts | Missing trace is not missing payment; projection lag is not authority |
| Investigator/ML | RAG retrieval versus generation, tool auth, grounding, leakage, false positives | investigator eval/model card/parity tests | Injected runbook, unsupported citation, missing model, synthetic generalization |

## Cohesity and banking emphasis

Cohesity-oriented presentation highlights the restore rehearsal, consistent backup scope, integrity evidence, observable autonomous recovery, Go APIs and controlled operational automation. Banking-oriented presentation starts with deterministic monetary invariants, original operation identities, legal state transitions and audited authorization. Both can use the same code and evidence; neither needs an invented production-scale claim.
