# C4 investigator integration handoff

This handoff is source-compatible with shared contract `tx.v2.1`. It does not modify M1/M2-owned handlers, configuration, migrations or Go dependencies, and it does not claim live PostgreSQL, HTTP, UI or provider verification.

## M2 package and mount contract

- Construct `Service` with `NewService(DefaultServiceConfig(), durableStore, ToolDependencies{...})`. Production supplies `EvidenceReader`, `RunbookRetriever` and the optional recovery, reconciliation, trace and analytics readers; it must never use `MemoryCorpus`, `MemoryInvestigationStore` or test fixtures.
- Keep `ModeEvidenceOnly` and paid budget zero by default. Enabling `ModeLLM` requires explicit server configuration, the committed prompt version, a provider adapter and a transactional nonzero reservation policy. Client bodies cannot choose mode/provider or supply actor, role or scope.
- POST decoding accepts only `CreateInvestigationRequest`; the authenticated handler injects `TrustedScope`, passes its selected payment to `Service.Start`, and returns `CreateResponse` in the common envelope. GET loads via `InvestigationStore.Get(scope, id)` and returns `PublicInvestigation`. The worker calls `Service.Run(jobID, workerID)` under an epoch-fenced lease.
- Implement migration 000027 and repositories exactly as specified in `INTEGRATION.md`: durable jobs/checkpoints/reports/usage reservations plus staged corpus versions/chunks. Reservation creation, uncertain transition and terminal completion must reject stale lease epochs; uncertain allowance remains consumed, while completion atomically saves the validated report and actual usage. The package memory implementations are test-only.
- Stage corpus `tx.corpus.c1.v1`, verify its manifest hash/chunk count, then activate it only after migration and role-filtered retrieval checks. Retain retired chunks for saved citation drilldowns.
- Apply the existing JWT/RBAC, body-size, unknown-field, idempotency and rate-limit middleware. Never expose SQL, shell, mutation or arbitrary URL tools. Preserve the 30-second job, three-second evidence read, two core attempts, topK five, eight unique tools and two provider-round bounds.

M2 acceptance still required: canonical migration 000027 tests, trusted real evidence reads, `internal/investigator/storetest.Run` against a schema-isolated PostgreSQL factory, authenticated POST/GET denial and success paths, active-corpus ingestion through the canonical schema, persisted citation resolution and native B10 analytics windows. The conformance harness passes against the M3 memory reference, and the corpus repository passes against test-only disposable PostgreSQL DDL; neither replaces these runtime gates or the C4 memory evaluation's limitations.

The M2 branch is now source-available, but its audited head `c64a9e62870e4ed802910b9ee84e0dee73ffc9bb` does not implement the complete contract above and overlaps this package's model/service files. Follow `M2_COMPATIBILITY_AUDIT.md`: retain the M3 service, use the strict evidence and live-feature adapter seams, reconcile migration 000027 deliberately, and do not mount both investigator implementations. Availability of the branch is not a passing live gate.

## M1 response and fixture contract

`evaluation/results.json` contains `uiFixtures` in the exact package `InvestigationResponse` shape:

- `completedEvidenceOnly`: completed deterministic report.
- `partialEvidenceOnly`: completed report with `evidenceCoverage=limited` and explicit missing evidence.
- `providerFailedFallback`: sanitized provider failure with the safe deterministic report retained.
- `validatedLLMFixtureOnly`: grounded LLM-mode rendering fixture; label it fixture-only, not live-verified.

Render `mode`, job status/error, nullable usage, fact-level evidence IDs, immutable runbook chunk IDs, missing evidence, coverage and snapshot time. Do not display inference as an authoritative fact, do not call evidence-only output an LLM result, and retain the synthetic/offline qualification for anomaly windows. The statistical baseline remains the default; IsolationForest remains experimental and must not affect routing or settlement.

M1 acceptance still required: schema validation against the shared contract, saved citation drilldowns, stale snapshot treatment, mode/error/partial labels, null measurements and real browser flows after M2 mounts the endpoints.

Cross-branch audit update: M1 head `8afec7afdb2f78f43283e6c7d78c65aee28331a8` accepts all four committed C4 investigation fixtures, both matching evidence snapshots and all four anomaly windows. A disposable browser run exposed one narrow-layout defect: immutable `runbook:<hash>` references overflow at 360px. `M1_COMPATIBILITY_AUDIT.md` contains the exact one-line CSS patch; with it, all five cross-branch artifact browser tests and M1 lint pass. The patch has not been committed to M1, and routed fixtures do not replace the remaining live endpoint gate.

## Deterministic evaluation

The reviewed dataset is `evaluation/cases.json`. `cmd/evaluate` records dataset/corpus/prompt hashes, exact source commit, dirty-tree state, command, per-case safe reports, security decisions, latency and live-provider status. Its release gates require 40/40 cases, 8/8 security blocks, 8/8 missing-evidence abstentions, typed/citation agreement for every published report and retrieval recall@5 >=0.85.

The adversarial set actively covers an injected retrieved runbook, mismatched trusted/evidence payment identity, foreign payment/trace scope, unknown tool, role-field injection, excessive recovery limit and an SQL-shaped unknown retrieval field. The live-provider entry remains `pending` until an explicitly authorized provider or suitable local model runs the fixed case IDs with model identity, prompt hash, actual usage and timestamp. No paid automatic judge is part of C4.
