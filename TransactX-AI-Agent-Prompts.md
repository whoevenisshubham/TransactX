# TransactX — AI Coding Agent Prompt Pack

Use these prompts directly in the VS Code coding-agent chat.

**Authoritative specification:** `docs/TransactX-Technical-Specification.md`

---

## 1. Initialize the Project + Documentation

**Use this FIRST, before serious implementation.**

```text
Read docs/TransactX-Technical-Specification.md completely and treat it as the authoritative specification for this project.

Do not implement the application yet.

First:
1. Inspect the repository and current environment.
2. Validate the proposed architecture against the actual repo/tooling.
3. Create the documentation structure:
   docs/
   ├── architecture.md
   ├── database.md
   ├── payment-flow.md
   ├── reconciliation.md
   ├── offline.md
   ├── routing.md
   ├── chaos.md
   ├── invariants.md
   ├── benchmarking.md
   └── decisions.md
4. Populate each file with an initial draft based only on the technical specification.
5. Clearly label anything not implemented yet as DESIGN, PLANNED, or FUTURE WORK. Never label planned behavior as implemented.
6. Create/update README.md with the project overview, setup, repository structure, and development workflow.
7. Create the initial repository/folder structure required by the specification.
8. Do not invent technologies, features, APIs, metrics, benchmark numbers, or completed functionality.

At the end:
- summarize the repository structure created,
- explain important architecture decisions,
- identify ambiguities that need decisions,
- give me the exact first implementation task you recommend.

Do not start that implementation task yet.
```

**When:** First session in the repository.

---

## 2. Plan the Next Feature Before Coding

**Use before every meaningful implementation task.**

```text
Using docs/TransactX-Technical-Specification.md and the current repository as the source of truth, plan the implementation of this feature:

[DESCRIBE FEATURE HERE]

Before writing code:
1. Inspect all relevant existing files and modules.
2. Explain the intended design and data flow.
3. Identify files to create/modify.
4. Identify database/schema/API changes.
5. Identify edge cases and failure modes.
6. Identify tests that must exist.
7. Identify which parts are critical financial/concurrency/reconciliation code and require careful human review.

Do not implement yet.

Finish with:
- implementation plan,
- files affected,
- acceptance criteria,
- risks/assumptions,
- smallest safe implementation sequence.
```

**When:** Before starting any non-trivial feature.

---

## 3. Implement One Feature Safely

**Use after Prompt 2.**

```text
Implement ONLY the feature we just planned.

Rules:
- Follow docs/TransactX-Technical-Specification.md.
- Do not expand scope.
- Do not refactor unrelated code unless required for correctness.
- Preserve existing working behavior.
- Prefer simple, explicit implementations over unnecessary abstractions.
- Do not fabricate functionality, metrics, benchmark results, or production guarantees.
- Add/update tests as part of the implementation.
- For payment, ledger, idempotency, concurrency, offline replay, routing, and reconciliation code, prioritize correctness over speed.

After implementation:
1. Run relevant tests.
2. Run lint/type-check/build where applicable.
3. Fix failures caused by your changes.
4. Update the relevant docs/*.md file to reflect what is actually implemented.
5. Summarize files changed and important implementation decisions.
6. Tell me exactly which parts I should personally read and understand before this feature is considered complete.

Do not claim the feature is complete if acceptance criteria are not satisfied.
```

**When:** When you are ready to let the agent code.

---

## 4. Review Critical Code / Explain It to Me

**Use especially for payment-core code that you must understand.**

```text
Act as a senior backend engineer reviewing the current implementation of:

[MODULE / FILE / FEATURE]

Do NOT modify code yet.

Explain:
1. Exact end-to-end execution flow.
2. Transaction boundaries.
3. Database consistency guarantees.
4. Concurrency behavior and possible race conditions.
5. Idempotency behavior.
6. Failure and timeout behavior.
7. What happens under retries.
8. Security/authorization implications.
9. Hidden assumptions.
10. Any places where the implementation could produce duplicate money movement, inconsistent ledger state, invalid state transitions, or data races.

Then compare the implementation with the TransactX technical specification and identify:
- correct decisions,
- questionable decisions,
- bugs,
- missing tests,
- improvements.

Be precise. Do not reassure me unless the code actually supports the claim.
```

**When:** Before personally signing off on core financial or distributed-systems code.

---

## 5. Test / Attack the Feature

**Use after implementation to deliberately try to break it.**

```text
Treat the current TransactX implementation as potentially incorrect and try to break it.

Target:
[FEATURE / MODULE]

Create and run adversarial tests for:
- duplicate requests,
- retries,
- concurrent requests,
- timeouts,
- partial failures,
- malformed inputs,
- invalid state transitions,
- insufficient balance,
- service unavailability,
- replay,
- race conditions,
- feature-specific edge cases.

For financial logic explicitly verify:
- no duplicate logical payment,
- no negative balance unless explicitly allowed,
- debit/credit conservation,
- ledger correctness,
- valid transaction state transitions,
- idempotency,
- correct recovery after failure.

Do not simply say the tests pass. Show:
- what was tested,
- what failed,
- what was fixed,
- risks that remain.

Do not modify tests merely to make them pass; fix the implementation where appropriate.
```

**When:** After every critical feature, especially payment/ledger/concurrency/reconciliation.

---

## 6. Update Documentation + Architecture Decisions

**Use after major implementation changes or architectural decisions.**

```text
Review the latest implementation changes against the TransactX technical specification.

Update only documentation supported by the actual code:
- docs/architecture.md
- docs/database.md
- docs/payment-flow.md
- docs/reconciliation.md
- docs/offline.md
- docs/routing.md
- docs/chaos.md
- docs/invariants.md
- docs/benchmarking.md

Also update docs/decisions.md with significant Architecture Decision Records.

For each ADR include:
- Context
- Decision
- Alternatives considered
- Why we chose it
- Trade-offs
- Consequences

IMPORTANT:
- Clearly distinguish IMPLEMENTED, PARTIALLY IMPLEMENTED, PLANNED, and FUTURE WORK.
- Never invent benchmark numbers or claim a capability that the current code does not provide.
- Keep terminology consistent with the master specification.

Summarize which documentation files changed and why.
```

**When:** After a module stabilizes or an important design choice is made.

---

## 7. Run Benchmarks / Produce Research Evidence

**Use during the research/benchmarking phase.**

```text
Act as the experimental/research engineer for TransactX.

Using docs/benchmarking.md and the current implementation, inspect the benchmark harness before changing anything.

We need reproducible experiments for:
1. Naive ledger reconciliation vs Merkle reconciliation.
2. Static routing vs adaptive routing under identical failures.
3. Online operation vs offline queue + replay.
4. Concurrent payment workloads and invariant correctness.

Guaranteed benchmark tiers:
- 10,000 transactions
- 100,000 transactions

1,000,000 is OPTIONAL and must not be assumed feasible.

Before running:
- verify the methodology,
- identify uncontrolled variables,
- define exactly what is measured,
- ensure baseline and proposed system are compared fairly.

Then run experiments and save raw results.

Measure only observable values such as:
- elapsed time,
- CPU,
- memory,
- nodes visited,
- records inspected,
- bytes transferred,
- P95/P99 latency,
- success rate,
- recovery time,
- duplicate processing,
- invariant violations.

Never fabricate or smooth results.

After the run:
- summarize actual measured results,
- identify surprising or unfavorable findings,
- explain limitations,
- recommend which results are strong enough for the paper.
```

**When:** Once implementation is stable and you need real experimental evidence.

---

## 8. Final Release-Readiness Review

**Use before a major milestone, demo, paper submission, or final release.**

```text
Perform a release-readiness audit of the entire TransactX repository against:

docs/TransactX-Technical-Specification.md

Evaluate:
1. Functional completeness.
2. Financial correctness.
3. Concurrency safety.
4. Idempotency.
5. Offline replay correctness.
6. Merkle reconciliation correctness.
7. Routing/circuit-breaker behavior.
8. Chaos scenarios.
9. Runtime invariant checking.
10. Authorization/security.
11. Frontend user flows.
12. Test coverage.
13. Benchmark reproducibility.
14. Documentation accuracy.
15. Demo readiness.

For each requirement classify it as:
- COMPLETE
- PARTIAL
- NOT IMPLEMENTED
- BROKEN
- FUTURE WORK

For every PARTIAL/BROKEN item provide:
- evidence,
- severity,
- exact files/components involved,
- recommended fix,
- whether it is required before the next milestone.

Finally provide:
A. Top 5 blocking issues.
B. Top 5 highest-value improvements.
C. Features we should NOT add because they threaten scope.
D. Final demo-critical checklist.
```

**When:** At milestones and before calling the project complete.

---

# Recommended Prompt Workflow

For a normal feature:

```text
Prompt 2 → Prompt 3 → Prompt 5 → Prompt 6
```

For critical financial/concurrency code:

```text
Prompt 2 → Prompt 3 → Prompt 4 → Prompt 5 → Prompt 6
```

For research experiments:

```text
Prompt 7 → analyze results → update paper/docs
```

For finalization:

```text
Prompt 8 → fix blockers → Prompt 5 → Prompt 6
```

---

# Golden Rules for the Coding Agent

1. The technical specification is the source of truth.
2. Never silently expand scope.
3. Never fabricate benchmark numbers, performance claims, security guarantees, or implemented features.
4. Payment, ledger, idempotency, concurrency, offline replay, routing, and reconciliation code require human review.
5. Every meaningful feature needs tests before being considered complete.
6. Documentation must describe the code that actually exists, not the code we intend to have.
7. Prefer a smaller correct implementation over a larger impressive-looking implementation.
8. When uncertain, explain the design trade-off rather than inventing a decision.
9. Keep the application working after every meaningful change.
10. Do not merge AI-generated critical-path code that the team cannot explain and defend.
