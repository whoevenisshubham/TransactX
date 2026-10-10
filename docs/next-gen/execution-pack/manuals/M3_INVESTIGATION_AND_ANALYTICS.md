# M3 investigator and analytics manual

## Mission

Build the read-only evidence investigator and a useful operational anomaly subsystem. Own investigator/anomaly Go packages, approved corpus, offline Python tooling, evaluation fixtures and model artifacts. Do not edit monetary code, shared handler/config/Go lockfiles or migrations concurrently with M2. Supply bounded integration patches/specifications through the handoff.

Default model GPT-6.1 Sol medium for orchestration/grounding/scoring; GPT-6 Luna medium or low for corpus/fixtures/report formatting. Sol high is reserved for a short security/leakage review. Read AGENT_RULES, SOURCE_BASELINE, API_CONTRACTS and this manual first.

## C0 Freeze independent fixtures and read interfaces

Can start immediately from the canonical baseline. Define package-local typed EvidenceReader/retrieval/provider/store/scorer interfaces matching the shared contract. Create immutable fixture cases for healthy, unknown, absent-before-dispatch, bank-settled/central-pending, missing target, partial evidence, trace missing, wrong identity and injected document instructions.

Files: investigator model/interfaces/testfixtures; analytics feature schema/fixture vectors; approved corpus manifest. Supply M2 the required investigation/corpus/usage tables and config/mounting specification, without changing their shared files.

Tests: fixtures schema-valid, IDs/citations internally consistent, no secrets/names/notes. Acceptance: package compiles and tests independently with fakes; interfaces work without a live M2 API. Rollback: fixtures only; no runtime changes. Record C0 schema hash/readiness.

## C1 Build approved runbook retrieval

Depends on C0. Read AI_AGENT_ARCHITECTURE. Write concise technical runbooks from actual invariants and planned reason codes; distinguish implemented behavior from upcoming procedures. Implement manifest-only Markdown chunking, SHA/version metadata, safe PostgreSQL full-text ranking and staged active corpus switching.

Files: docs/runbooks, corpus manifest, investigator retrieval/ingest CLI/tests. M2 commits migration 000027; until available use repository interface fixtures and a disposable test schema coordinated with M2. Never ship local test DDL as a second canonical migration.

Tests: heading/line/hash stability, duplicate ingestion, removed document, role filtering, SQL-safe query, path escape/symlink, retrieval cases and citation preservation after corpus version change. Metrics: retrieval latency/chunk count. Acceptance: approved factual retrieval recall@5 reaches reviewed target or failure is reported/repaired. Rollback: prior active corpus; no source money changes.

## C2 Implement bounded investigator orchestration

Depends on C0/C1. Implement typed read-tool registry, trusted scope, core deterministic collection, optional bounded model tool rounds, durable job/checkpoint interface, evidence-only reports and observable tool timeline. No mutation/shell/SQL/arbitrary URL tools.

Files: investigator service/tools/repository handlers with fake readers; explicit HTTP evidence adapter if needed to avoid dependency on M2 internals. Return common-compatible handler/envelope integration to M2. Persist tool output/source hashes before expensive generation and maintain lease/budget state.

Tests: scope cannot be supplied by model; foreign payment denied; unknown tool/field; bounds/deadline/cancellation; duplicate request; interrupted job resumes without repeated unlimited calls; source unavailable; evidence-only actual report. Metrics: jobs/tools/deadlines. Acceptance: useful cited evidence report with no provider key. Rollback: disable generation and keep read-only summary.

## C3 Add provider adapter and grounding checks

Depends on C2. Implement optional OpenAI adapter from current official Responses/function/structured-output docs, plus a narrow configured local-compatible adapter only if needed. Provider model is configuration, with zero paid budget default; do not assume a Work subscription supplies API calls.

Files: provider HTTP/SDK adapter, prompt version, schema/grounding validators, reservation interface/tests. M2 owns shared dependency/config changes. Avoid returning unrestricted model text as a final report.

Validate cited typed financial claims, source identity/status/time, document hash and missing-evidence behavior. Model-directed tool calls pass deterministic auth/schema/budget checks. Usage reservations are transactional in M2-mounted store; mocked local counters do not pass concurrency budget acceptance.

Tests: provider refusal/timeouts/429, malformed report, unknown evidence ID, valid citation with wrong amount/status, injection, changed snapshot, parallel budget reservation, no-call default. Live test only with configured authorized provider/local model; otherwise record pending. Acceptance: unsupported facts rejected and generator cannot mutate money. Rollback: evidence-only. ADR-036.

## C4 Evaluate investigator and hand off integration

Depends on C3. Create at least 40 reviewed fixed cases and metrics in AI_AGENT_ARCHITECTURE. Run deterministic retrieval/tool/security/grounding suite. Produce dataset hash, expected/forbidden claims and safe reports. No paid auto-evaluation loop or model-as-judge-only correctness claim.

Give M2 package/mount/store interface, flags, migration specification and test results; give M1 real schema-compatible UI fixtures including failures/modes. Tests must verify citation meaning, not only existence. Record live-provider evaluation separately if available.

Acceptance: all auth/security/citation gates pass and evaluated limitations are explicit. Rollback: generator stays disabled on failing factuality. Do not label runtime LLM/RAG live-verified without a real generation evaluation artifact.

## C5 Build anomaly baseline training and native scorer

Depends on C0; real data integration needs B10. Read ANALYTICS_AND_ML. Implement statistical baseline, seeded scenario-data pipeline, leakage-safe run/time split, small IsolationForest, threshold comparison/model card, JSON export and pure Go scoring.

Files: analytics scripts/pinned requirements, model card/feature schema, `internal/anomaly` scorer/loader/tests, safe versioned model artifacts. Model inference never imports a financial repository or mutates policy. Offline Python is optional online; no extra service.

Tests: insufficient/invalid/missing/stale model; 100 Python reference vectors parity; bounded artifact/no cycles/NaN; held-out scenario split; excluded labels/future information; baseline false alerts/detection delay. Metrics: score latency/model status/drift. Acceptance: useful held-out comparison with synthetic qualifier and native parity, or explicit data-shortage block. Do not fake training data volume/results. Rollback: prior model or statistical baseline. ADR-037.

## C6 Close integrated AI and analytics gates

Depends on C4/C5 for full lane completion, B11 for integrated HTTP/worker proof. Verify trusted real evidence adapter, corpus migration/ingestion, HTTP auth, zero-budget provider mode, saved citation drilldowns, native scorer windows and UI mode labels with M1 fixtures/real responses.

Run focused race/concurrency tests for job claims/reservations and final security/leakage review. Deliver model/corpus/prompt versions, evaluation artifacts, integration patch readiness and exact tested SHA. If live provider access is absent, state that clearly; the evidence-only release can be complete while live LLM verification remains pending. No broad restart/research at closeout.
