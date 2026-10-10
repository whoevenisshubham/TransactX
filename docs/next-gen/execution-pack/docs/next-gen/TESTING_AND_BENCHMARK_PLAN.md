# Verification and release evidence

## Baseline and environment

At implementation start record remote SHA, clean/dirty status, Go/Node/PostgreSQL versions and actual commands. Run current backend build/unit tests and frontend checks once. Database tests require a disposable PostgreSQL database with all migrations. An unset URL that skips integration tests is not acceptance.

Use existing CI PostgreSQL service to keep the release reproducible. Native local PostgreSQL is preferred for focused debugging; no local Docker requirement. Destructive experiment scripts validate the resolved DSN/database name and refuse the live database.

## Verification levels

1. Pure unit/contract tests during each slice.
2. PostgreSQL integration for locking, transaction, event, claim, inbox and ledger behavior.
3. Native process crash/restart tests for supported kill boundaries.
4. Real browser flows against local full services.
5. Online HTTPS smoke after deployment.
6. Optional real-provider/model evaluation, recorded separately from mocks.

Every feature's status names the highest level actually passed.

## Recovery matrix

| Fault boundary | Required assertion |
| --- | --- |
| Intent committed, process killed before first stage | Same payment/idempotency identity resumes on pinned route |
| Central operation journal committed, request not sent | Presence protocol can safely replay original operation; no permanent synthetic pending loop |
| Bank effect committed, reply dropped | Original status resolves; one participant effect |
| Central operation result persistence fails | Bank status lookup recovers original UUID and outcome |
| Source confirmed, destination finalization unknown | Payment remains pending until safe evidence; no blind reversal |
| Banks complete, central transaction fails | Worker completes central only; one ledger transaction and two balanced entries |
| Two workers plus duplicate browser request | Same payment lock, stable original payload, one financial outcome |
| Worker lease expires while first worker is delayed | CAS protects scheduler; resumer/participant identity protects money |
| Lock connection dies during external call | No duplicate effect; unsafe lock state is discarded/handled |
| Pool constrained and bank slow | Bounded acquisition; no deadlock/starvation from all connections held by locks |
| Original execution target removed | Park with explicit reason; no silent route replacement |
| Participant returns mismatched identity/status | Unresolved/attention; no incorrect central completion |
| Permanent business rejection before irreversible stage | Existing safe compensation/terminal logic, conservation |
| Exhausted automatic retries | Scheduler attention only; financial unknown remains unknown |

Use test hooks or a scripted HTTP proxy for real response loss after a committed participant effect. Existing pre-call transient-drop simulation is not equivalent evidence. Hooks are test/simulation gated and absent from public runtime controls.

For crash tests, start processes as child processes, wait for deterministic checkpoints, terminate them, restart with the same DB and verify durable facts. Use bounded waits and fail with last observed checkpoint. Do not rely on a fixed long sleep or only a mocked method call to prove restart behavior.

## Financial invariants

After each crash/concurrency experiment verify nonnegative balances, debit/credit conservation, one ledger transaction per completed payment, correct ledger amount and accounts, user-scoped idempotency mapping, legal state history, stable operation UUID/payload, participant effect uniqueness and ownership. Existing integrity checks are necessary but also assert targeted rows independently so the test is not merely calling the implementation's own success flag.

Fuzz/property tests vary valid amounts within safe ranges, duplicate keys, response ordering and stage interruptions. Retain seeds on failure. Use Go race detector on changed concurrent packages and full suite at release.

## Event and projection matrix

Test source rollback, committed event after producer death, consumer commit/ack crash, duplicate delivery, two dispatcher leases, late lower-ID commit, stale event order, unsupported schema quarantine, retry exhaustion, concurrent rebuild, DB outage and retention cleanup. All tests assert actual DB rows and current version, not a relay log message.

## Security and contract tests

Each endpoint: malformed JSON, too-large body, missing/expired JWT, wrong role, correct role, unauthorized entity, invalid cursor and valid partial response. Viewer cannot schedule/control/chaos/reconcile. Bank token tests include missing/wrong/right. Evidence export excludes secrets and notes. Model/corpus inputs reject path escapes, cyclic trees, NaN and oversized payloads.

Frozen fixtures cover exact enums/types and nullable missing data. Backend and frontend validate schema version. API GET tests compare state/operation/ledger rows before and after reads to prove no financial side effects.

## Frontend and browser tests

Run current `npm test`, `npm run test:money`, `npm run lint`, `npm run build`. Add new component cases for partial/stale/empty/error evidence and request cancellation. Real browser suite covers login/role routes, worklist filters, deep-link refresh, recovery view update, citation drilldown/export, denied viewer action, SSE disconnect/reconnect, token expiry, and IndexedDB reload/replay under correct user scope.

Check keyboard-only use and automated critical/serious accessibility findings on new screens; inspect responsive behavior at 360, 768 and 1440 pixels and 200 percent zoom. A helper-only IndexedDB test does not close the live browser reconnect gap.

## Load experiments

Controlled disposable environment; healthy and faulted conditions separate. Start with 1,000 simulated logical payments at concurrency 1/5/10/25, then choose a safe extended run. Report accepted/rejected/pending/terminal counts separately, throughput, HTTP p50/p95/p99, completion latency, worker queue age and projection lag. Do not overrun a public free instance just to create impressive metrics.

Run at least three repeated seeded trials for outage/static versus adaptive route comparisons. Keep scenario duration and bank/target mapping fixed across comparisons. The historical 297/300 result is preserved as RC1 evidence; new numbers get new source SHAs and artifacts.

Recovery benchmark uses known resolvable faults and measures restoration-to-convergence time. Report unresolved parked scenarios separately instead of removing them from the results. Outbox benchmark measures commit-to-projection lag through timestamps under controlled clock assumptions; use monotonic duration locally where possible and disclose cross-process clock limits.

Reconciliation repeats equal-root, one-mismatch and dispersed-mismatch cases at 10K/100K. Record work counters and runtime; do not rename bytes examined as network bytes or assume all cases speed up.

## AI and ML evaluation

AI fixed cases and security gates are specified in `AI_AGENT_ARCHITECTURE.md`. CI uses recorded typed fixtures only. Live results include provider/model, prompt/corpus hash, actual usage and failed cases. If no key/local model exists, record live verification pending.

ML evaluates entire held-out scenario runs, compares statistical baseline, tests Python/Go scoring parity, checks training leakage and reports false positives. Scoring correctness alone is not model usefulness. A synthetic dataset is labelled in UI/model card and resume evidence.

## Restore evidence

Actually restore a redacted disposable snapshot. Assert schema/version/counts/integrity and safe worker continuation, and report measured RTO/RPO. Backup creation without restore verification does not pass.

## Commands and release gates

Backend from `backend`: `go test -count=1 ./...`, `go test -race -count=1 ./...`, `go vet ./...`, `go build ./...`; add current govulncheck if available and record material results. Focused slices use exact package/test patterns first. Final database gate sets a real disposable DATABASE_URL and records skipped-test count.

Frontend from `frontend`: commands above plus the implemented browser test command. Pin dependencies and use lockfiles. No arbitrary reformat or dependency upgrade is necessary to satisfy checks.

Final release requires P0 acceptance, combined PostgreSQL suite, race/build/typecheck, contract/auth tests, supported crash scenarios, real browser workflows, clean source SHA, secret/diff review and online smoke if deployment credentials are available. P1 feature claims require their specific gates; otherwise mark them pending and continue after resources reset.

## Artifact format

Every new result records schema version, UTC time, source SHA, dirty-tree flag, toolchain/platform, dataset/seed/scenario, exact command, sample counts, failures, exclusions, measured values and SHA-256 of raw inputs/output. Write uniquely named files; never overwrite historical evidence. Store safe reports under `artifacts/v2/<experiment>/<run-id>/`.

Final `RELEASE_REPORT.md` has feature status, test evidence links, open limitations, deployment URL/status, provider mode and rollback instructions. No future benchmark placeholder becomes a numeric claim without its raw result.
