# M1 product and delivery manual

## Mission

Build a coherent customer/merchant/operator experience, validate it in a real browser, make native free-tier deployment reproducible, prove restoration and integrate the three lanes. Own frontend, deployment, browser tests, integration files and release documentation. Preserve existing payment/idempotency/IndexedDB behavior.

Default model is GPT-6 Luna medium for bounded product/tooling work. Use GPT-6.1 Sol medium for integration failures and high for a short financial/security integration review. Do not reread every technical document for each screen.

All tasks include focused validation, safe plain-English commits, actual status in M1 state and a reviewable PR. Fixture completion is separate from real API/browser/online verification.

## A0 Install the plan and establish integration

Can start immediately. Verify remote/default/SHA and repository permissions. Preserve existing changes. Create `release/transactx-v2` from current canonical; install `docs/next-gen`, manuals/prompts/contracts under an appropriate `docs/next-gen` structure and a concise compatible root AGENTS.md. Update links if moving paths; keep the pack's relative layout intact under a `docs/next-gen/execution-pack` subfolder if simplest.

Create owner branches in distinct checkouts; do not modify partner checkouts. Commit one shared planning bootstrap. Establish exact API/schema version and migration ownership with M2 through repository handoff. Do not require M2/M3 to wait for bootstrap to begin fixture work.

Tests: relative links/schema/registry parse; clean baseline preserved. Acceptance: partner prompts locate their manuals and share the same contract. Rollback: planning files removable without app changes. State records integration base and PR links.

## A1 Extract shared frontend contracts and design primitives

Depends on A0 for shared installation; fixture work can begin locally before that. Read FRONTEND_ARCHITECTURE and PRODUCT_UX_PLAN. Add one production API base resolver, ops DTOs/decoders/client, named fixture adapter, reusable status/table/timeline primitives and route support. Keep existing visual tokens and exact money parsing.

Files: frontend config/api/ops/design/types, tests and package lock only if a necessary test dependency is added. Fixture adapter is explicit dev/test only; it cannot silently activate on production API failure.

Tests: schema-compatible fixtures, decimal strings, response cancellation, existing frontend suite/typecheck/build. Security: no secrets in VITE variables, safe text rendering, role state cleared. Observability: visible last-fetch/source status, no fabricated trace UI. Acceptance: new components compile with existing app and accessible keyboard basics. Rollback: route feature flags; customer workflow stays intact.

## A2 Build searchable recovery worklist

Depends on A1; use fixtures until B8. Implement URL-backed state/recovery/bank/time filters, exact UUID search, cursor navigation, stale/lag banner, clear empty states and links to payment evidence.

Files: WorklistPage, typed client/helpers, console navigation and tests. Explain count scope; no client inference of global totals. Debounced SSE invalidation and bounded fallback refresh.

Tests: filter reset/back-forward, page changes, empty/error/lagging, viewer read access and unavailable backend. Acceptance: operator can find an attention payment and open its evidence at mobile/desktop widths. Integration gate: real B8 response and authorization. Rollback: hide worklist route; preserve server data.

## A3 Build payment journey and export

Depends on A2; fixtures first, B8/B9 for live gate. Implement authoritative central summary, original route, ordered transitions, operation/status/presence table, source availability, recovery attempts, related check context and trace panel. Read API_CONTRACTS carefully.

New admin recheck schedules only; show actual accepted status. Viewer cannot invoke it. Evidence export uses real redacted server payload with content/hash/time display.

Tests: partial banks, unknown outcome, bank-settled/central-pending, terminal, missing trace, racing navigation responses, long histories/IDs and unauthorized control. GET detail is the displayed truth even if worklist lags. Acceptance: the UI explains missing evidence without claiming financial failure. Rollback: hide new detail controls; existing customer details remain.

## A4 Add investigation and analytics experience

Depends on A3 and C0 contract fixtures; live gate B11/C4 and B10/C5 as applicable. Build question input fixed to selected payment, queued/running/failure/report display, tool timeline, evidence/runbook citation panels, mode labels and stale-report warning. No canned result in live mode.

Analytics shows window boundaries, samples, missing data, baseline reasons and model score/version/status, with table alternative for charts. Link to related evidence/worklist. No model probability, proven root cause or fraud label.

Tests: duplicate submit uses same attempt key; provider failure leaves evidence available; evidence-only labelled; invalid citation handled; stale report; insufficient sample/model fallback; keyboard citation navigation. Acceptance: a real integrated investigation can be reproduced and every displayed citation resolves. Rollback: switch to evidence-only/hide optional model panel.

## A5 Verify full browser workflows

Fixture tests can begin after A2/A3/A4; full gate requires B8/B11. Add real Playwright flows against API/banks/worker/PostgreSQL, plus accessibility checks. Test deep-link refresh, roles, worklist->detail->investigation->export, worker-driven update, SSE reconnect and expired JWT.

Close the known offline gap: create IndexedDB intent under one user, reload while offline, reconnect, reuse the same key and verify one backend logical payment. Logout/relogin under another user cannot replay the previous user's intent.

Tests: mobile/tablet/desktop, 200 percent zoom, keyboard-only actions, critical/serious accessibility, normal/error paths. Record actual browser/runtime versions and failures. Acceptance: real browser evidence, not helper-only simulation. Rollback: affected new route stays gated until repaired; do not weaken test assertions.

## A6 Package and deploy native online demo

Can develop scripts after A0; deployment gate requires B0/B4/B5 and passing combined release. Implement DEPLOYMENT_AND_BACKUP files, release archive, Linux arm64/amd64 builds, systemd/Caddy, validated environment examples and static-host SPA configuration.

No serverless conversion, new local Docker prerequisite or automatic paid provisioning. Keep banks/DB private, admin credentials secret, viewer read-only and provider paid budget zero. Connect Vercel with frontend root and actual HTTPS API URL. If access/capacity missing, finish runnable archive and report the exact external blocker.

Tests: script dry run/target validation, native service start/restart, HTTPS/REST/SSE, CORS allowed/denied origin, deep-link refresh, public bank/DB port denial, secrets absent from dist. Acceptance: frontend and backend plus worker actually run online, or release is explicitly deployment-ready with unresolved external access. Rollback: previous compatible archive/symlink, worker claims paused, no financial reset.

## A7 Implement backup and restore rehearsal

Depends on A6 packaging and B0 migration tools; can rehearse locally without cloud access. Implement encrypted whole-DB backup, bounded retention, configured off-VM target, timer and isolated restore checker. Local/private storage is acceptable when object credentials are absent; report which persistence path was actually verified.

Tests: restore the dump, schema/ledger/idempotency/participant checks, worker continuation and projection rebuild, wrong/live DSN rejection, backup failure/space limits. Store safe measured restore artifact/hash and no dump in Git.

Acceptance: recovery procedure restores a real consistent disposable database; measured RTO/RPO recorded. Rollback: disable backup timer only if broken; existing backups retained. ADR-041 and restore runbook.

## A8 Integrate and ship the measured release

Integrate compatible task sets under EXECUTION_AND_GIT. Verify exact PR head/tested SHA and branch protection. Resolve ownership conflicts explicitly. Run combined PostgreSQL/race/build/frontend/browser/security gates once for the final changed state, and rerun only affected gates if further fixes occur.

Create release report, architecture diagrams reflecting actual boundaries, demo instructions, feature status, evidence index, runbooks, startup/rollback guide and honest resume statements. Optional benchmarks/LLM claims require their own results. Preserve historical RC1 artifact provenance.

Tag only after required gates pass, with a clear candidate tag if live deployment/provider verification is pending. Push release artifacts through legitimate configured GitHub access. Do not merge broken code to manufacture a finished-looking history. Acceptance: an unfamiliar reviewer can run the release, observe a crash/recovery story, inspect evidence and understand every remaining limitation. Final report names what is live and what still needs external access.
