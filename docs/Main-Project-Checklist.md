TransactX
A Resilient Payment Infrastructure for the Next Generation of Digital Payments
Chronological Implementation Checklist — 12-Week Execution
Technical subtitle: A Fault-Tolerant Payment Network with Offline-First Resilience, Adaptive Routing, and Merkle-Accelerated Reconciliation

# How to Use This Checklist

## Current repository checkpoint

The current branch completes the Member-1 routed-payment checkpoint (M1-6): durable Bank A, the frozen adapter contract, routed saga persistence, operation status, compensation, and deterministic pending recovery. The roadmap's later **Phase 6 — Offline-First Client & Safe Replay** remains intentionally unchecked; it is a separate later project phase and is outside this takeover's exit scope.

\[ ] Work strictly in phase order unless a dependency explicitly allows parallel work.
\[ ] Tick an item only when it is implemented, tested, and verified — not merely coded.
\[ ] For payment, ledger, idempotency, concurrency, offline replay, routing, and reconciliation code, human review is mandatory.
\[ ] Use the technical specification as the source of truth; this checklist is the execution order.
\[ ] Keep main stable. Each member works on a branch and opens a PR for meaningful changes.
\[ ] At each phase gate, run the listed acceptance checks before moving forward.
\[ ] Record important architecture decisions in docs/decisions.md.

# Team Ownership Legend

# Phase 0 — Kickoff \& Repository Setup

Duration: Week 1   Objective: Create a clean, reproducible working environment before application development.

\[ ] ALL — Create the GitHub repository and protect the main branch.
\[ ] ALL — Add .gitignore, .editorconfig, LICENSE, README.md, and .env.example.
\[ ] ALL — Add docs/TransactX-Technical-Specification.md as the authoritative specification.
\[ ] ALL — Give the coding agent the initialization prompt and instruct it not to implement application logic yet.
\[ ] ALL — Create the docs/ folder and initial architecture/database/payment-flow/reconciliation/offline/routing/chaos/invariants/benchmarking/decisions files.
\[ ] ALL — Agree on the exact backend stack, frontend stack, database, and containerization approach before coding.
\[ ] ALL — Create the initial monorepo structure from the technical specification.\[ ] ALL — Create the frontend skeleton and backend skeleton.
\[ ] ALL — Add a minimal health endpoint and minimal frontend shell.
\[ ] ALL — Verify every teammate can clone, install, build, and run the project.
\[ ] ALL — Make the first clean baseline commit and tag it.
PHASE EXIT GATE: A fresh clone starts the frontend, backend, database, and required local infrastructure successfully.

# Phase 1 — Architecture, Data Model \& Development Foundation

Duration: Weeks 1–2   Objective: Freeze the core financial model and build the reusable foundation.

\[ ] ALL — Finalize the two-bank MVP decision while keeping the bank adapter interface N-bank capable.
\[ ] ALL — Finalize roles: CUSTOMER, MERCHANT, OPS\_ADMIN.
\[ ] ALL — Finalize payment states and permitted state transitions.
\[ ] ALL — Finalize money representation (integer paise / smallest currency unit).
\[ ] M1 — Create PostgreSQL migrations for users, banks, accounts, payments, ledger transactions, ledger entries, idempotency records.
\[ ] M2 — Create bank service/adaptor interfaces and synthetic bank seed structure.
\[ ] M3 — Create reconciliation/integrity/benchmark data structures.
\[ ] ALL — Add deterministic seed data for users, merchants, banks, accounts, and devices.
\[ ] ALL — Document database relationships and indexes.
\[ ] ALL — Add environment validation and startup configuration checks.
\[ ] ALL — Add baseline automated test command and CI workflow if practical.
\[ ] ALL — Create API error/response conventions.
\[ ] ALL — Create request/correlation ID plumbing.
PHASE EXIT GATE: Schema, seed data, service boundaries, state machine, and local development conventions are documented and tested.

# Phase 2 — Authentication, Accounts \& Consumer App Foundation

Duration: Week 2   Objective: Make the application usable before adding complex payment behavior.

\[ ] M1 — Implement registration.
\[ ] M1 — Implement secure password hashing.
\[ ] M1 — Implement login/session or JWT flow.
\[ ] M1 — Implement server-side role authorization.
\[ ] M1 — Implement user profile and payment-identifier resolution.
\[ ] M1 — Implement account lookup and balance read.
\[ ] M1 — Build login screen.
\[ ] M1 — Build registration screen.
\[ ] M1 — Build customer home screen.
\[ ] M1 — Build customer navigation shell.
\[ ] M2 — Build merchant navigation shell and merchant identity flow.
\[ ] M3 — Build initial network-console shell with placeholder states only.
\[ ] ALL — Add unit and integration tests for authentication and authorization.
\[ ] ALL — Verify frontend cannot access protected resources without valid authorization.
PHASE EXIT GATE: A seeded customer and merchant can authenticate and reach the correct protected application areas.

# Phase 3 — Financial Core: Payments, Ledger \& Idempotency

Duration: Week 3   Objective: Implement the financially correct heart of TransactX before resilience features.

\[ ] M1 — Implement payment request validation.
\[ ] M1 — Implement transaction creation.
\[ ] M1 — Implement explicit transaction-state transition validation.
\[ ] M1 — Implement idempotency-key persistence and uniqueness.
\[ ] M1 — Implement request-hash validation for reused idempotency keys.
\[x] M1 — Implement atomic balance mutation.
\[x] M1 — Implement double-entry ledger transaction creation.
\[x] M1 — Implement debit and credit ledger entries.
\[x] M1 — Implement balance reconstruction from ledger.
\[x] M1 — Implement insufficient-balance rejection.
\[x] M1 — Add database transaction boundaries.
\[x] M1 — Add row locking / equivalent concurrency control.
\[x] M1 — Add payment service tests.
\[ ] M1 — Build pay form.
\[ ] M1 — Build confirm-payment screen.
\[ ] M1 — Build processing screen.
\[ ] M1 — Build success/failure state screens.
\[ ] M1 — Build transaction-details screen.
\[ ] M1 — Build transaction-history screen.
\[ ] ALL — Manually review every line of the payment/ledger/idempotency critical path.
\[x] ALL — Run duplicate-request tests.
\[x] ALL — Run same-key/different-payload conflict tests.
\[x] ALL — Run insufficient-balance tests.
\[x] ALL — Run concurrent same-account payment tests.
PHASE EXIT GATE: Normal payments are correct under retries and concurrency; no negative balance, duplicate logical payment, or debit/credit conservation violation is observed.

# Phase 4 — Bank Simulation \& End-to-End Multi-Bank Flow

Duration: Week 4   Objective: Introduce actual independent simulated bank participants and routing boundaries.

\[x] M2 — Implement Bank A service.
\[ ] M2 — Implement Bank B service.
\[x] M2 — Implement bank account lookup.
\[x] M2 — Implement bank debit operation.
\[x] M2 — Implement bank credit operation.
\[x] M2 — Implement bank health endpoint.
\[x] M2 — Implement configurable bank latency.
\[x] M2 — Implement configurable bank failure mode.
\[x] M1 — Implement payment-switch bank adapter abstraction/injection boundary.
\[x] M1 — Route a normal payment through the payment switch to the selected bank.
\[x] M1 — Persist selected route on the payment.
\[x] M1 — Define timeout/error mapping between switch and bank adapters.
\[x] ALL — Run end-to-end customer → switch → bank → ledger → response flow.
\[x] ALL — Verify bank failures cannot silently create successful payments.
PHASE EXIT GATE: A real payment travels through the switch and bank adapter into the ledger, with failures surfaced safely.

# Phase 5 — Incremental Merkle Reconciliation

Duration: Weeks 5–6   Objective: Build the main research contribution correctly and benchmarkable from the start.

\[ ] M3 — Define the canonical transaction representation used for hashing.
\[ ] M3 — Implement canonical serialization deterministically.
\[ ] M3 — Implement SHA-256 leaf hashing.
\[ ] M3 — Implement internal-node hashing.
\[ ] M3 — Design the hierarchical time/partition structure.
\[ ] M3 — Implement append-only / incremental or touched-bucket Merkle maintenance.
\[ ] M3 — Ensure tree commitments are not rebuilt from the entire ledger on every reconciliation run.
\[ ] M3 — Implement root retrieval.
\[ ] M3 — Implement root comparison.
\[ ] M3 — Implement recursive divergence localization.
\[ ] M3 — Implement exact divergent-bucket identification.
\[ ] M3 — Implement exact divergent-transaction lookup where possible.
\[ ] M3 — Record nodes visited, records inspected, elapsed time, and bytes transferred.
\[ ] M3 — Implement naive full-diff baseline.
\[ ] M3 — Implement Merkle-vs-naive benchmark harness.
\[ ] M3 — Implement reconciliation API.
\[ ] M3 — Build reconciliation result UI.
\[ ] M3 — Build Merkle tree visualization.
\[ ] M3 — Add controlled ledger-divergence test fixture.
\[ ] ALL — Verify identical snapshots produce identical roots.
\[ ] ALL — Verify a one-record mutation changes the appropriate commitments.
\[ ] ALL — Verify the algorithm can localize the mutation.
\[ ] ALL — Verify benchmark harness runs repeatedly without manual editing.
PHASE EXIT GATE: Merkle commitments are incrementally maintained, divergence can be localized, and the naive baseline produces reproducible comparison data.

# Phase 6 — Offline-First Client \& Safe Replay

Duration: Week 7   Objective: Make the consumer app continue to function sensibly during temporary connectivity loss.

\[ ] M2 — Define the offline payment intent data model.
\[ ] M2 — Add IndexedDB persistence using Dexie or equivalent.
\[ ] M2 — Detect browser online/offline state.
\[ ] M2 — Queue payment intents locally when the API is unreachable.
\[ ] M2 — Preserve client request ID.
\[ ] M2 — Preserve original idempotency key.
\[ ] M2 — Preserve payload hash and retry metadata.
\[ ] M2 — Implement offline queue listing.
\[ ] M2 — Implement queue status transitions.
\[ ] M2 — Implement replay when connectivity returns.
\[ ] M2 — Implement bounded exponential backoff.
\[ ] M2 — Handle replay success.
\[ ] M2 — Handle replay failure without deleting the request prematurely.
\[ ] M2 — Build offline queue screen.
\[ ] M2 — Add clear ONLINE / OFFLINE / QUEUED / SYNCING / SYNCED states.
\[ ] M1 — Verify server idempotency protects replay from duplicate processing.
\[ ] ALL — Verify no offline transaction is shown as final settlement before server acceptance.
\[ ] ALL — Test multiple queued payments across reconnects.
\[ ] ALL — Test browser refresh while offline and confirm durable local queue.
\[ ] ALL — Test duplicate replay.
PHASE EXIT GATE: Offline requests persist across refresh/reconnect, replay safely, and final settlement occurs only after central acceptance.

# Phase 7 — Adaptive Routing \& Circuit Breaker

Duration: Week 8   Objective: Make the network resilient to degraded or failed bank participants.

\[ ] M2 — Implement bank health sampling.
\[ ] M2 — Define normalized health signals.
\[ ] M2 — Implement deterministic routing score.
\[ ] M2 — Implement bank selection using the routing score.
\[ ] M2 — Record routing decisions and reasons.
\[ ] M2 — Implement failure counters.
\[ ] M2 — Implement circuit CLOSED state.
\[ ] M2 — Implement OPEN state.
\[ ] M2 — Implement HALF\_OPEN state.
\[ ] M2 — Implement cooldown and probe behavior.
\[ ] M2 — Implement gradual traffic restoration after recovery.
\[ ] M3 — Build bank-health view.
\[ ] M3 — Build routing distribution visualization.
\[ ] M3 — Stream health/routing changes to the frontend.
\[ ] ALL — Define exact experimental routing thresholds in configuration.
\[ ] ALL — Run static-routing baseline.
\[ ] ALL — Run adaptive-routing condition.
\[ ] ALL — Verify deterministic behavior under the same input conditions.
PHASE EXIT GATE: The switch demonstrably avoids unhealthy banks, recovers healthy banks gradually, and records why each route was chosen.

# Phase 8 — Chaos Engineering \& Self-Healing

Duration: Week 9   Objective: Turn resilience claims into controlled, repeatable experiments.

\[ ] M2 — Implement operations-only chaos controller.
\[ ] M2 — Add bank outage scenario.
\[ ] M2 — Add configurable latency scenario.
\[ ] M2 — Add transient/message-drop scenario.
\[ ] M2 — Add temporary network partition scenario.
\[ ] M3 — Add admin-only controlled ledger-corruption fixture.
\[ ] M1 — Add concurrent payment-storm trigger through the test environment.
\[ ] M2 — Ensure every chaos action is visibly marked as simulation/test mode.
\[ ] M2 — Ensure chaos endpoints require OPS\_ADMIN authorization.
\[ ] M3 — Build chaos-control UI.
\[ ] M3 — Show active faults and scenario history.
\[ ] M3 — Stream bank-health and circuit-breaker changes live.
\[ ] ALL — Run bank outage and observe rerouting.
\[ ] ALL — Run latency degradation and observe routing-score change.
\[ ] ALL — Run bank recovery and observe HALF\_OPEN then gradual restoration.
\[ ] ALL — Run message-drop scenario and verify safe pending/recovery behavior.
\[ ] ALL — Run the corruption scenario and verify Merkle/integrity detection.
\[ ] ALL — Ensure no chaos endpoint is exposed to CUSTOMER or MERCHANT roles.
PHASE EXIT GATE: At least the outage, latency, recovery, and corruption scenarios are repeatable and produce observable, correct system behavior.

# Phase 9 — Runtime Financial Integrity \& Operational Console

Duration: Week 10   Objective: Continuously verify correctness and expose the infrastructure clearly to evaluators.

\[ ] M3 — Implement debit/credit conservation check.
\[ ] M3 — Implement no-negative-balance check.
\[ ] M3 — Implement transaction uniqueness check.
\[ ] M3 — Implement idempotency invariant check.
\[ ] M3 — Implement state-transition validity check.
\[ ] M3 — Implement completed-payment ledger completeness check.
\[ ] M3 — Implement Merkle-commitment verification check.
\[ ] M3 — Implement aggregate integrity-check endpoint.
\[ ] M3 — Build integrity dashboard.
\[ ] M3 — Build event/activity feed.
\[ ] M2 — Build network overview metrics cards.
\[ ] M2 — Build bank status cards.
\[ ] M2 — Build routing view.
\[ ] M3 — Build reconciliation history table.
\[ ] M3 — Build Merkle result detail view.
\[ ] ALL — Add structured logs and request IDs.
\[ ] ALL — Decide whether Prometheus/Grafana is worth adding after core observability works.
\[ ] ALL — Keep rate limiting as stretch until core requirements are green.
\[ ] ALL — Run integrity checks after normal payments.
\[ ] ALL — Run integrity checks during chaos tests.
PHASE EXIT GATE: The Network Console can show the system's health, reconciliation, routing, and financial-integrity state in a coherent operational view.

# Phase 10 — Merchant Experience \& Product Polish

Duration: Week 10–11   Objective: Complete the application so it feels like a believable fintech product, not a backend demo.

\[ ] M2 — Build merchant dashboard.
\[ ] M2 — Build dynamic/static QR generation flow.
\[ ] M2 — Build incoming-payment feed.
\[ ] M2 — Build settlement-status view.
\[ ] M2 — Build merchant transaction search.
\[ ] M1 — Add customer scan/pay flow.
\[ ] M1 — Improve customer transaction timeline.
\[ ] M1 — Add loading, success, failure, and empty states.
\[ ] ALL — Make customer and merchant surfaces visually consistent.
\[ ] ALL — Make Network Console visually distinct from the consumer product.
\[ ] ALL — Remove placeholder text and dead routes.
\[ ] ALL — Verify responsive behavior at common desktop/mobile widths.
\[ ] ALL — Add meaningful error messages that do not leak internal details.
\[ ] ALL — Add demo seed/reset controls outside the customer experience.
PHASE EXIT GATE: Customer, merchant, and network-console journeys are all usable end-to-end and no major screen is visibly unfinished.

# Phase 11 — Research Benchmarking \& Evidence

Duration: Week 11   Objective: Produce real measurements for the paper and avoid unsupported claims.

\[ ] ALL — Freeze the code version used for the baseline experiments.
\[ ] M3 — Benchmark naive vs Merkle reconciliation at 10K transactions.
\[ ] M3 — Repeat the 10K benchmark multiple times.
\[ ] M3 — Benchmark naive vs Merkle at 100K transactions.
\[ ] M3 — Repeat the 100K benchmark multiple times.
\[ ] M3 — Attempt 1M only if hardware and time permit.
\[ ] M2 — Benchmark static vs adaptive routing under identical bank failures.
\[ ] M2 — Measure payment success rate.
\[ ] M2 — Measure P95/P99 latency.
\[ ] M2 — Measure recovery time.
\[ ] M2 — Measure traffic redistribution.
\[ ] M2 — Benchmark offline queue/replay behavior.
\[ ] M2 — Measure duplicate-processing count.
\[ ] M3 — Run concurrency storm and record invariant violations.
\[ ] ALL — Save raw benchmark results in versioned files.
\[ ] ALL — Record hardware/software/environment used for every experiment.
\[ ] ALL — Separate baseline measurements from proposed-system measurements.
\[ ] ALL — Generate charts/tables only from actual recorded results.
\[ ] ALL — Document unfavorable results and limitations instead of hiding them.
PHASE EXIT GATE: The project has reproducible raw evidence for reconciliation, routing resilience, offline replay, and financial correctness.

# Phase 12 — Final Hardening, Paper, Demo \& Release

Duration: Week 12   Objective: Convert the working research prototype into a defensible final submission.

\[ ] ALL — Run full backend unit-test suite.
\[ ] ALL — Run integration tests.
\[ ] ALL — Run frontend build/type-check/lint.
\[ ] ALL — Run end-to-end happy path.
\[ ] ALL — Run duplicate-payment test.
\[ ] ALL — Run concurrency test.
\[ ] ALL — Run offline reconnect test.
\[ ] ALL — Run bank outage and recovery demo.
\[ ] ALL — Run Merkle divergence demo.
\[ ] ALL — Run integrity violation detection demo.
\[ ] ALL — Verify chaos controls are OPS\_ADMIN-only.
\[ ] ALL — Audit all API authorization rules.
\[ ] ALL — Check secrets are absent from the repository.
\[ ] ALL — Review critical payment/ledger/idempotency/concurrency code manually.
\[ ] ALL — Update all docs/\*.md statuses to reflect actual implementation.
\[ ] ALL — Write the final Architecture Decision Records.
\[ ] ALL — Freeze and tag the demo release.
\[ ] ALL — Re-run key benchmarks from the release commit.
\[ ] ALL — Create final architecture diagram.
\[ ] ALL — Create final payment sequence diagram.
\[ ] ALL — Create final reconciliation/Merkle diagram.
\[ ] ALL — Create final offline sequence diagram.
\[ ] ALL — Write the paper introduction/problem/objectives/methodology/results/limitations.
\[ ] ALL — Never insert unmeasured numbers into the paper or presentation.
\[ ] ALL — Write final resume bullets using only verified implementation/results.
\[ ] ALL — Rehearse the five-to-ten minute demo.
\[ ] ALL — Rehearse likely backend/system-design questions.
\[ ] ALL — Prepare a backup demo video/screenshots in case live infrastructure fails.
\[ ] ALL — Create final presentation and project README.
PHASE EXIT GATE: Fresh-clone build passes, critical tests pass, benchmark evidence is reproducible, documentation is truthful, and the full demo can be delivered without manual code intervention.

# Definition of Done — Use This Before Ticking Any Non-Trivial Feature

\[ ] Code is implemented in the intended module.
\[ ] Relevant unit/integration tests exist.
\[ ] Edge cases are tested.
\[ ] Build/type-check/lint passes.
\[ ] Existing features still work.
\[ ] Critical-path code was read and reviewed by a human.
\[ ] Documentation reflects actual implementation state.
\[ ] No fabricated metric or claim was introduced.
\[ ] Feature has a reproducible way to demonstrate it.

# Team Git Workflow

\[ ] Each member works on a feature branch, never directly on main.
\[ ] Branch naming is consistent: feat/<area>-<feature>, fix/<area>-<bug>, docs/<topic>.
\[ ] Pull requests describe what changed, why, tests run, and any design decisions.
\[ ] At least one teammate reviews meaningful PRs; critical financial code gets deeper review.
\[ ] Merge only after tests pass.
\[ ] Keep commits small enough to understand and revert.
\[ ] Tag stable milestones such as v0.1-foundation, v0.2-financial-core, v0.3-merkle, v0.4-resilience, v1.0-demo.

# AI Coding Rules

\[ ] Use the coding agent heavily for boilerplate, UI, tests, migrations, documentation, and repetitive code.
\[ ] Do not blindly accept AI-generated payment/ledger/idempotency/concurrency/reconciliation code.
\[ ] Before asking the agent to implement a non-trivial feature, ask it for a plan and affected files.
\[ ] After implementation, ask it to run tests and explain what changed.
\[ ] Personally review the critical-path implementation before merging.
\[ ] Never let the agent invent benchmark results or mark planned features as implemented.
\[ ] If the agent proposes scope expansion, treat it as a separate decision rather than silently accepting it.

# Final Demo Order

1. \[ ] Show the polished customer app and complete a normal payment.
2. \[ ] Open the transaction details and show the selected bank route.
3. \[ ] Switch to Network Console and show live bank health/routing.
4. \[ ] Inject bank degradation and show adaptive rerouting.
5. \[ ] Kill the bank and show circuit-breaker/self-healing behavior.
6. \[ ] Go offline, queue a payment, reconnect, and show safe replay.
7. \[ ] Inject controlled ledger divergence in the test environment.
8. \[ ] Run Merkle reconciliation and show the root mismatch + tree drill-down.
9. \[ ] Run integrity checks and show the financial invariants.
10. \[ ] Run the benchmark view and present actual measured results.

# Final Release Checklist

\[ ] No known blocker remains in the critical payment path.
\[ ] No real-money or real-bank integration is implied by the product.
\[ ] All demo data is synthetic.
\[ ] Chaos/corruption controls are isolated from normal users.
\[ ] README provides exact setup instructions.
\[ ] The paper's claims match implemented features and measured evidence.
\[ ] The team can explain the payment state machine, idempotency, concurrency model, ledger, Merkle reconciliation, offline replay, routing, circuit breaker, and invariant checks without relying on the AI agent.

|Owner|Primary Area|Typical Responsibilities|
|-|-|-|
|M1|Payment Core|Auth, accounts, payments, ledger, idempotency, concurrency, customer frontend|
|M2|Resilience|Offline queue, bank adapters, health, routing, circuit breaker, chaos, merchant frontend|
|M3|Research / Console|Merkle engine, reconciliation, integrity, benchmarks, network console|
|ALL|Shared|Architecture, reviews, integration, testing, documentation, paper, demo|

