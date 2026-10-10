# M2 reliability and platform manual

## Mission and boundaries

Implement the deterministic backend foundations, safe automation and shared read interfaces. Own payment/core packages, bank protocol extensions, worker, outbox, backend configuration, migrations, Go dependencies, API mounting, security and telemetry. Preserve mature reconciliation and customer contracts.

Start with AGENT_RULES, SOURCE_BASELINE, API_CONTRACTS and this manual. Read the referenced technical document only for the current task. Default model: GPT-6.1 Sol medium. Use high for the final crash/authorization review. Record progress only in your state file.

Every task's completion includes focused tests, `git diff --check`, safe committed code, documentation of actual behavior, and a state/PR checkpoint. Code complete and integration complete are separate statuses. The rollback named below is in addition to retaining a prior compatible release.

## B0 Verify baseline and build migration tooling

Objective: make subsequent SQL changes reproducible. Depends on current canonical checkout; can start before M1 installs shared docs.

Read `SOURCE_BASELINE`, `DEPLOYMENT_AND_BACKUP`, current database package, migrations and CI. Record versions and run current build/unit/frontend baseline once; run DB schema tests on a disposable PostgreSQL 18 database.

Files: `backend/cmd/migrate`, `internal/database` migration helper/tests, shared config support, migration CLI documentation. Build checksum/version ledger and exclusive migration lock. Apply each new migration transactionally where its SQL supports that; explicitly declare exceptions. Stop on checksum mismatch or unknown ordering. Bootstrap an old untracked database only through an explicit verified baseline operation.

Tests: empty DB to current; second run no-op; concurrent migration attempts; checksum alteration; failed migration remains unapplied; wrong baseline refused. Security: distinct migration role and target validation. Observability: migration ID/start/end/result without DSN. Acceptance: existing migrations work unmodified and repeat application is safe. Rollback: leave baseline app running; do not down-migrate financial tables automatically. Docs: deployment migration section and B0 evidence.

## B1 Unify serialization and continuation

Depends on B0. Read recovery document, routed repository, CreateWithResult, tests and lock call sites. Implement `Service.Resume` and private continuation dispatcher. Initial execution/duplicate request/worker recovery must share the same per-payment lock. Reread after acquiring it; resolve original persisted route before monetary work.

Files: `payments/service.go`, `routed.go`, focused new `resume.go`, tests; new shared app composition seam only as needed. No schema migration unless baseline reachability proof demands one. Do not add a second hold/credit algorithm.

Cover early routed states and central-only completion. COMMITTED continuation validates the existing ledger and completes without another transaction. Fix lock cleanup so failed unlock cannot contaminate pooled sessions. Add bounded semaphore/pool configuration with minimum query reserve.

Tests: two resumes plus duplicate create; terminal no-op; route removed; lock deadline; lock connection failure; pool constrained; state-specific continuation. Assert actual ledger/operation rows. Metrics: lock wait, resume result, overload. Security: no public arbitrary resume endpoint. Acceptance: every money continuation caller is accounted for and serialization test passes under race detector. Rollback: disable background recovery; keep the safe shared request path. ADR-032 draft and evidence.

## B2 Prove safe operation absence and replay

Depends on B1. Read participant operation transaction/idempotency code and the presence contract. Add separate `OperationPresenceReader` implementation and authenticated private presence endpoint. Retain BankAdapter's nine methods. Verify participant uniqueness and immutable original payload behavior; repair only a demonstrated flaw with focused tests.

Files: bankservice HTTP/service, HTTPClient capability, payments ensure-operation continuation and tests. Shared service-token config may be completed with B5. Presence reads owning primary and never maps errors to absence. Replay uses original UUID, payment, account, amount, hold and original-reference IDs on the original participant.

Tests: kill after central journal before HTTP; delayed original request racing replay; conflicting same UUID/payload rejected; absent/present/unavailable/mismatched presence; adapter without capability remains pending. Include real PostgreSQL participant tests, not only mocks.

Metrics: status/presence outcome and replay reason. Security: private token, identity validation, no arbitrary endpoint. Acceptance: resolvable pre-dispatch crash converges with exactly one participant effect; unresolved evidence parks safely. Rollback: disable presence-supported replay and retain conservative pending behavior. Finalize ADR-032.

## B3 Add durable recovery scheduling

Depends on B1/B2. Read scheduler schema/claim protocol. Migration 000022. Implement discovery, due claims, lease CAS/renewal, attempts, interrupted sweep, terminal resolution, backoff/jitter and operational parking.

Files: `internal/recovery/{model,repository,scheduler}.go`, tests, migration. Scheduler uses a narrow resumer interface; it cannot change balances directly. Stale discovery is bounded/indexed and never continually un-parks an attention job.

Tests: two claimers, lease expiry/epoch, late result rejected, jitter bounds, attempt exhaustion, terminal cleanup, missing route, scan pagination and interrupted attempt. Security: worker identity is server generated. Observability: queue depth/age, result counts, heartbeat age. Acceptance: claims expire/recover and cannot overwrite another claim. Rollback: disable scheduler discovery/claims, preserve records. ADR-033 and schema docs.

## B4 Wire the native worker

Depends on B3. Extract shared validated participant/route wiring from API into `internal/app` or another small composition package. Add `cmd/worker` with independent bounded loops for recovery, delivery, later analytics/investigation; no public listener. Keep one API replica and avoid duplicating circuit authority.

Files: cmd API/worker, composition, config, worker lifecycle tests. Defaults and invariants match deployment environment contract. Heartbeat says which job families are enabled. Shutdown stops claims and releases/cancels bounded work.

Tests: native worker restart on unchanged DB, bank outage/restoration, DB startup unavailable, graceful shutdown and kill -9, worker absent UI health. Metrics/logs include safe worker identity/build. Acceptance: old routed payments advance without user resubmission when evidence permits. Rollback: stop worker service; request-driven safe recovery still exists. Update native start/runbook.

## B5 Harden public roles and participant access

Depends on B0; independent of B3/B4. Migration 000023. Add OPS_VIEWER, exact read/mutate role checks, explicit registration/chaos/recheck flags, audited control idempotency and participant service tokens. Validate production required secrets/config while keeping explicitly local development workable.

Files: auth/users/config, handler role mounting, bank HTTP/client, safe demo seed, security tests. Preserve existing public registration role constraints. Extend operational GETs for viewer only after DTO disclosure audit.

Tests: role matrix per new endpoint, public role escalation, viewer controls denied, simulation/control flag combinations, missing/wrong bank token, rate limits, auth expiry. Observability: safe audit facts, no tokens. Acceptance: public viewer cannot trigger financial/fault/maintenance mutation. Rollback: disable public controls and viewer login; preserve private admin workflows. ADR-039.

## B6 Produce transactional outbox facts

Depends on B0/B1. Migration 000024. Implement payment event version and atomic producer trigger, immutable events/delivery schema and typed envelope. Recovery event production shares source transactions. Read EVENT_ARCHITECTURE.

Files: migration, `internal/outbox` envelope/producer/store tests, recovery source writes. Existing payment repository paths are enumerated and exercised. No after-commit-only event hook.

Tests: local/routed create/state changes/central completion/recovery; source rollback; trigger error rollback; same-state update emits nothing; no private data; initial old-data bootstrap semantics. Observability: pending count and safe IDs. Acceptance: every supported committed source fact has one intended event and delivery; rollback has none. Rollback: stop consumer; keep events. Dropping a trigger on a live producer requires explicit controlled migration, not a flag that falsely preserves reliability. ADR-034/035.

## B7 Deliver and rebuild the worklist projection

Depends on B6. Migration 000025. Implement leased delivery, inbox transaction, current-state projection, version guard, retries/quarantine and safe derived rebuild/replay CLI.

Files: outbox dispatcher, `internal/projections`, `cmd/rebuild-worklist`, tests. Do not use a high-watermark-only relay. Snapshot projector tolerates order; it does not promise ordered arbitrary event consumers.

Tests: all event matrix cases, lower-ID late commit, ack loss, two relays, unknown version, concurrency rebuild/new state and cleanup safety. Verify current-state counts and rows independently. Metrics: pending/quarantine/lag/rebuild result. Acceptance: restart and duplicate events do not lose or regress worklist state; rebuild changes no source authority. Rollback: disable relay and serve explicit unavailable/lag state; current payment reads still work. ADR-034/035 final.

## B8 Expose read-only evidence and worklist APIs

Depends on B3/B5/B7. Implement API_CONTRACTS exactly: worklist, evidence, attempts, gated schedule recheck and redacted export. Reuse original route and explicit participant reads. Related global checks remain labelled contextual. New GETs never invoke recovery.

Files: new HTTP handlers, `internal/operations` read DTO/service/repository, shared handler wiring and contract fixtures. M1 consumes these; M3 uses read interfaces/adapter. Export canonical ordering/hash binds exact redacted contents; no authenticity claim.

Tests: role/scope, partial sources, central unavailable, deadlines, pagination, decimal-string money, excessive ops/history, unchanged source DB after GET, export redaction/hash. Metrics: source failure and read latency. Acceptance: shared fixtures validate and frontend/M3 adapters compile. Rollback: unmount new ops endpoints; no financial schema rollback. Document actual endpoint examples.

## B9 Add OpenTelemetry and trace context

Depends on B1/B5; can overlap B6/B8. Migration 000026. Instrument four processes and bank HTTP boundaries, JSON logs, route-template metrics, span links for asynchronous recovery, safe bounded trace exporter/read API and TTL cleanup.

Files: `internal/telemetry`, HTTP/client wrappers, cmd composition, trace handler, migration, tests, Go lockfiles. Do not block payment execution on exporter/persistence.

Tests: cross-service trace context, recovery link, redaction, cardinality bounds, exporter down/queue full/drop, retention, missing trace, shutdown timeout. Metrics/source reset semantics documented. Acceptance: a real healthy/faulted payment has correlated available spans in the configured test mode while exporter failure leaves financial results intact. Rollback: disable trace storage/export; instrumentation becomes no-op or safe local metrics. ADR-040.

## B10 Build operational analytics windows

Depends on B3/B7; final telemetry measurements may use B9. Migration 000028. Implement bounded five-minute completed windows and API; durable sources define numerator/denominator. Capture historical backlog observations before window end if used; never read a later queue snapshot as a historical feature.

Files: `internal/analytics`, worker loop, API DTOs/tests. Enforce one window identity, half-open time range, minimum sample handling and missing data. M3 supplies scorer/baseline interface. Start with simple documented source-backed features.

Tests: event/transition cutoff at window end, insufficient samples, late terminal changes not leaked into earlier features, duplicate window retry, empty/unavailable source. Metrics: aggregation age/duration. Acceptance: deterministic fixtures reproduce and UI shows provenance/sample count; ML integration waits for C5. Rollback: disable analytics loop; preserve prior windows. ADR-037 collaboration.

## B11 Mount investigator and anomaly packages

Depends on B8/B9 and C4 for investigator. Mount M3 package through trusted read interfaces, common auth/envelopes, shared config and migration 000027. M3 supplies store/corpus SQL specification; M2 commits migration and shared Go dependencies once. Add durable investigator claim/usage reservation integration to worker.

After C5, wire pure anomaly scorer into B10. Keep provider default rules, daily paid token cap zero and model absent fallback. No direct financial repository writes from M3 packages.

Tests: real HTTP scope/roles, duplicate job create, queued restart, reservations, provider unavailable, default zero-cost mode, anomaly absent/invalid/stale. Observability: job/tool timings and actual usage. Acceptance: M1 integrated UI uses real evidence/retrieval and correct mode labels. Rollback: switch evidence-only or disable investigator/model loop; money continues. ADR-036/037.

## B12 Prove crash safety and event restart behavior

Depends on B2/B4/B7. Add native process harness for the recovery/event matrices in TESTING_AND_BENCHMARK_PLAN. Reuse proven fixtures and separate genuine reply loss from pre-call chaos.

Files: integration/experiment harnesses and raw safe artifacts. Disposable DB target enforced. Run two worker processes and duplicate requests. Include pool exhaustion/deadlock checks and claim timing. Do not make tests rely solely on production success flags.

Acceptance: conservation/uniqueness/original-target invariants hold across all supported scenarios; unresolved scenarios remain visibly pending. Record source SHA, seed, toolchain, faults, timings and failures. Rollback: worker stays disabled until failures repaired. Sol high bounded final concurrency review.

## B13 Close backend release gates

Depends on B8/B9/B10/B11/B12; P1 scoring integration may remain explicitly pending until C5/C6. Run complete PostgreSQL suite, race, vet/build, API security/contract tests, targeted vulnerability check and secret/diff review. Verify skips and exact tested SHA.

Update schema/API/ADR/runbooks to final implementation. Supply M1 binaries/test artifact references and unresolved limitations. Acceptance: all P0 backend gates pass, contracts match, no invented results, PR head equals tested head. Rollback instructions are reviewed against actual config/schema. Do not tag the release; M1 owns integration and delivery.
