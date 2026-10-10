# TransactX next release master plan

## Product direction

TransactX is a distributed payment reliability and operations platform for simulated bank services. It demonstrates how a payment system remains financially correct, recovers from interrupted work, delivers operational updates reliably, and lets an operator explain failures from evidence.

The demonstration has three connected experiences: customers send simulated payments safely; merchants receive and inspect them; operators investigate and recover the network. The new release builds on the existing financial core rather than replacing it.

The flagship story is one interrupted payment: a bank commits while its reply is lost, the API restarts, the worker resolves the original operation, the operator sees the evidence and recovery timeline, and the investigator produces a cited explanation. A second story rebuilds the operator projection and restores a backup without duplicating financial effects.

## Scope and priority

| Priority | Deliverable | Release meaning |
| --- | --- | --- |
| P0 | Shared contracts and baseline verification | All lanes compile against stable DTOs and preserve the existing release |
| P0 | Serialized recovery and operation absence protocol | Every supported crash boundary is either safely resumed or explicitly parked with evidence |
| P0 | Autonomous worker | Discovery, claims, bounded retries, leases, restart continuation and operator visibility |
| P0 | Payment and recovery outbox | Atomic production, at-least-once delivery, inbox deduplication, latest-state worklist projection |
| P0 | Evidence API and operator product | Searchable worklist, payment journey, recovery attempts, missing/stale evidence and redacted export |
| P0 | Security and online delivery | Read-only demo role, private banks, exact CORS, HTTPS, safe runtime configuration, free-tier release bundle |
| P0 | Observability | Propagated traces, operational metrics, JSON logs and correlation identifiers |
| P0 | Investigator foundation | Typed read tools, approved-document retrieval, evidence-only mode, optional LLM adapter, citation validation and adversarial tests |
| P1 | Trace explorer and incident workspace | A bounded cross-service trace view and saved redacted investigation reports |
| P1 | Analytics and evaluated anomaly model | Windowed operational analytics, statistical baseline, offline-trained model, native scoring and drift/fallback |
| P1 | Backup and restore rehearsal | Consistent PostgreSQL backup, off-VM storage option, actual isolated restore test and integrity verification |
| P1 | Browser quality and release evidence | Real offline reconnect, accessible new workflows, performance/chaos/AI/model reports |
| P2 | Scheduled closed-scope reconciliation | Only maintained compatible commitments, bounded jobs and explicit missing-state results |
| P2 | Rich incident grouping and topology | Derived correlation and useful visual exploration after core workflows are complete |
| P3 | Containers, Kubernetes, larger event broker, regional deployment | Future changes with measured justification and separate approval |

P0 means the release cannot claim that feature complete without its gate. P1 is part of the intended full next release, but may be delivered after the first stable demo if time or credits run short. Cut P2 and P3 first. Never cut financial safety tests to preserve a cosmetic feature.

## Technology choices

Keep Go, PostgreSQL, React, TypeScript, Vite, HTTP, SSE, IndexedDB and existing SHA-256/Merkle code. Add OpenTelemetry Go packages, a PostgreSQL outbox and inbox, PostgreSQL full-text retrieval, an explicit provider interface, and a small Python/scikit-learn offline training workflow. Production anomaly scoring stays in Go.

Use no Kafka, Redis, vector database, LangChain, workflow engine or extra AI service in the immediate release. The present traffic and budget do not justify their operational cost. Lexical retrieval is valid retrieval; call the complete system RAG only when an actual generator uses the retrieved content. LangChain is a possible orchestration library, not a prerequisite for agentic behavior.

Classical anomaly ML has a concrete target: unusual service failure and recovery windows. Deep learning, fraud scoring on fabricated customer behavior, and learned payment routing are deferred. Models must not become monetary policy.

## Team workstreams

| Owner | Main responsibility | Integration boundary |
| --- | --- | --- |
| M1 | Frontend product, accessible workflows, browser verification, native deployment, backup delivery, release integration and final evidence | Consumes frozen HTTP contracts; sole owner of frontend and deployment files |
| M2 | Recovery correctness, worker, outbox/projections, evidence APIs, security, telemetry, API composition and shared backend plumbing | Owns migrations, shared Go configuration, core source and API contracts |
| M3 | Investigator, retrieval corpus, model adapter, grounding evaluation, analytics model training and native anomaly scoring | Implements independent packages against typed read interfaces; never changes financial code |

The manuals specify exact tasks, file boundaries and acceptance criteria. Parallel work is supported through real schema-compatible fixtures. End-to-end integration is dependency ordered.

## Financial guarantees

- One central financial authority and one participant authority per simulated bank.
- Integer paise throughout. New API money and large counters use decimal strings to avoid browser precision loss; legacy DTOs remain backward compatible and retain existing safe-number checks.
- User-scoped idempotency maps one request identity to one payment; mismatch is a conflict.
- A stable original operation identity and immutable payload survive retries and restarts.
- Unknown outcomes stay unresolved until authoritative evidence establishes the next safe step.
- Central completion is atomic with the central ledger and its outbox event.
- Recovery claims expire; money operations still serialize through a payment lock and participant idempotency.
- Reconciliation, projections, telemetry, ML, AI and exports cannot mutate balances.

## Deployment target

React runs on Vercel Hobby for the personal demonstration, with a static-host fallback. The API, two bank participants, worker and PostgreSQL run as native Linux processes on one available free-eligible VM. HTTPS terminates at Caddy. Bank and database ports are private. An optional telemetry exporter is bounded and fails independently.

A single VM is a cost-conscious deployment with one physical failure domain. The system is still distributed across independently committing service boundaries; do not claim high availability, multi-region service or independently provisioned databases. Free hosting can sleep, reclaim resources, change limits or be unavailable. Reproducible installation and backup restoration are required.

The public demonstration allows read-only operational viewing and tightly bounded simulated customer interaction. It does not expose administrator credentials, recovery control, fault injection, training jobs, provider keys, or a public LLM budget.

## Completion and autonomy

Agents continue the tasks in their lane until every reachable acceptance gate passes. They maintain checkpoint files instead of repeating broad audits. M1 integrates only task sets with recorded validation, reruns required combined gates, then deploys if preconfigured access exists.

There is no instruction to hide missing evidence. Final release notes distinguish implemented, verified locally, verified in PostgreSQL, verified in a browser, verified online and live-provider-verified. A feature that lacks its required environment remains explicitly pending.

The intended sprint is ten working days with parallel lanes. A few calendar days may be enough for a narrower first release, but this is not a completion guarantee. Resource limits determine continuation pace, not the correctness standard.
