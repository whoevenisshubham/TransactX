# TransactX v2 delivery acceptance

Status: local acceptance advanced; release promotion and complete archive pending.
This report governs combined delivery. The root M2 release report and older
integration matrices preserve their lane/run scopes; they do not certify the release.

## Source and architecture

The finishing branch `codex/v2-final-completion` preserves integration `86e12e4`,
M1 `bc7c167`, M2 `e61ebd0` and M3 `15efc94` through normal history. Functional
acceptance ran once on clean source `bf1808dd656e8d457cce65d1d4cc29ba930428c7`.
Subsequent delivery/evidence edits require focused staging checks, not another
backend/frontend suite. PR #25 records the current integration/CI/merge status;
local test success is not peer approval or deployment. No public archive or
installation result exists without the configured origin/host.

Use JWT roles, native Go API/banks/worker, PostgreSQL and Linux systemd/Caddy.
Bank and database ports stay private. Integer paise, central payment authority,
participant ownership and user-scoped idempotency remain mandatory.
The investigator is read-only and evidence-only with provider calls disabled.
The statistical baseline is default; IsolationForest remains experimental/advisory.
No alert or investigator output authorizes settlement, routing or recovery policy.
See [architecture](DELIVERY_ARCHITECTURE.md).

## Measured evidence index

| Acceptance boundary | Result and scope | Evidence |
| --- | --- | --- |
| Final combined local acceptance | Go tests/race/build/vet; canonical store; fresh/upgrade/corpus; dedicated race recovery lifecycle and teardown; 97 frontend tests/types/money/build passed. Explicit optional skips retained | [combined evidence](state/final-completion-acceptance.json) |
| Canonical fresh/upgrade store and corpus | PostgreSQL 18.6 fresh 1–30 and canonical 29→30 conformance, all five store cases; corpus lifecycle passed | [candidate checks](state/candidate902-acceptance-m1.json) |
| Authenticated operations | Native investigator/saved citations, role/idempotency scope, SSE reconnect and actual 10-second JWT expiry passed | [candidate checks](state/candidate902-acceptance-m1.json) |
| Production offline replay | Full cached reload/account switch, original key/request identity, one logical payment and foreign 404; independent ledger/participant/projection SQL passed | [browser checks](state/production-browser-final-m1.json) |
| Accessibility and zoom | Real viewer screens at 360/768/1440 and native 200% zoom; Tab/Enter, completed/saved investigation, citation target and analytics; zero critical/serious axe findings/overflow after M1 fixes | [browser checks](state/production-browser-final-m1.json) |
| Native crash and encrypted restore | Real API kill before hold dispatch, original DB restart, encrypted isolated restore, restored worker kill/epoch takeover, automatic projection and independent financial checks passed for same-bank transfer | [native restore](state/native-crash-restore-final-m1.json) |
| Native cross-bank reply loss | Bank A committed hold, reply withheld, real API killed; native recovery settled BANK-A→BANK-B with exact ledger/participant/balance/key/projection SQL | [cross-bank recovery](state/cross-bank-native-recovery-m1.json) |

Windows native probes used Go 1.27.1, Node 22.20.0, PostgreSQL 18.6 and Edge
154.0.4258.62. Local generated TLS used an exact certificate public-key pin
and Node CA; it is not public DNS/CA acceptance. Fixture results remain fixtures.
All probe processes were stopped; credentials, private keys and dumps are outside Git.
Spending and provider calls remain zero.

The recorded encrypted restore checker took 3 seconds; restored native worker
takeover/convergence took about 11.1 seconds in its separate measured phase.
These are local phase measurements, not a hosted RTO guarantee. The quiesced
snapshot retained the checked committed facts; an operational time-based RPO
and off-machine durability remain unverified.

## Status of delivery gates and remaining owner inputs

1. **Code corrections verified locally:** shell-safe literal credentials (41 offline lifecycle scenarios), one server-controlled healthy attestation with negative precedence and explicit errors, checked acceptance SQL/projection/acknowledgements, honest dirty/source metadata, and fail-closed restore/teardown. Statistical baseline remains default; no provider calls.
2. **Canonical storage and scoped recovery:** fresh/upgrade investigation store and corpus acceptance passed. In-process cross-bank 7/7 and crash/event 20/20 matrices passed under race with checked financial invariants. They do not prove native process death; separate source-matched M1 native artifacts above retain their actual scenarios.
3. **Combined acceptance:** the final local normal/race/build/vet and frontend pass succeeded on the source above. Five destructive lifecycle tests and canonical conformance intentionally skip in broad runs but passed in their dedicated invocations. Opt-in concurrency/scale measurements and the dedicated PostgreSQL-server restart script were not rerun; no new proof is claimed for those skips. CI and review status must be read from PR #25, not historical green-run claims.
4. **Public HTTPS API Origin (PENDING OWNER INPUT):**
   - Requires owner-controlled public HTTPS domain and TLS termination for `VITE_API_BASE_URL`.
   - Fail-closed builder guard preserved: builder outputs `unset; required for an archive` and writes no release archive until configured.
5. **Native Linux Deployment Host & Infrastructure (PENDING OWNER INPUT):**
   - Requires owner-provided native Linux target host/access, DNS and TLS configuration for archive extraction and service installation via `deploy/scripts/install-release.sh`, Caddy setup via `deploy/caddy/Caddyfile.example`, and private-port smoke testing via `deploy/scripts/smoke.sh` ($0 / ₹0 spend).
   - Off-machine backup requires an owner-controlled destination (mounted directory for `BACKUP_OFFVM_DIR` or local storage copy) using `deploy/scripts/backup.sh` and operational recovery verification via `deploy/scripts/restore-check.sh` satisfying target RPO (up to 24h) and RTO (under 30 minutes on documented rehearsal environment; $0 cloud spend).
   - Production browser smoke requires target host execution of `playwright.live.config.ts` with owner-controlled test credentials.
   - Live-provider validation remains disabled (zero spend / $0 / ₹0). OAuth/IdP and Kubernetes are not prerequisites.

## Reviewer/demo path

Before a bundle exists, use a disposable native stack and the instructions in
[deployment](../../deploy/README.md) and [restore](../../deploy/RESTORE_REHEARSAL.md).
Provision generated local identities with the development tools; opening balances
are explicitly test fixtures, never real money or proof of recovery by themselves.
Do not provision customer fixtures into an existing live database.

Run the real browser commands with private environment inputs. Observe worklist
and authoritative payment evidence, submit an evidence-only investigation, open
its saved report/citation, download server evidence, inspect advisory analytics,
and run offline replay under the correct account. The read-only SQL queries under
`deploy/verification/` allow independent checking of the recorded financial cases.
Native crash evidence includes actual process/checkpoint facts; do not recreate
it by manually setting payment/job states.

Once the required archive gates pass, follow the installer validation and native
service startup/smoke steps in deployment instructions. Keep prior compatible
archive/configuration for rollback; pause worker claims before maintenance and
review migration compatibility. Never reset financial data or downgrade a durable
payload schema to force rollback. Restore only into a new isolated database first.

No public URL, completed release, full archive or off-machine backup is claimed.

The last local HTTPS CLI smoke passed health/SPA/CORS but stopped at the owner-only
auth-header guard on Git Bash's NTFS mount; no guard was relaxed. Authenticated
native browser evidence remains separately scoped. Complete archive, actual Linux
installation, authenticated hosted smoke, private-port verification and operational
backup targets await configured free infrastructure and origins.

Historical incident: 158 source transition rows are reported missing and remain
unresolved. This takeover did not access or mutate that dataset. Disposable tests
prove no historical recovery; preserve the affected dataset outside clean-release
claims pending the owner's recovery-material and separately authorized disposition.
