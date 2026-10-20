# TransactX v2 delivery acceptance

Status: The accepted Experience V3 frontend is deployed and its hosted read-only smoke passed at f7e432a. Repository presentation cleanup is completed in the current release changes; native certification and unverified operational claims remain separate.

## Hosted deployment verification — 20 October 2026

PR #27 and PR #28 were merged normally, preserving commits and authorship.
Stable/default source is `f7e432adaa60da01f5c110ab97d20ec262fc4d03`, whose tree
matches reviewed head `8c38a7291d7c90697f93156082e71a4f400e9174`.
All five required jobs passed on the reviewed head; all three merge-triggered
jobs also passed. Both Vercel and Render track `release/transactx-v2`.

Vercel production `dpl_FokfeGfQjNmRnL4iekD3WkSs85xQ` is Ready on `f7e432a`
at https://transactx-frontend.vercel.app. Backend remains `433011c` /
`dep-dbb536g473hc73clm290`; Render Free auto-deploy remains disabled. No backend
redeploy, secret/configuration, billing or database changes were needed.
Previous frontend `cafae17` / `dpl_2NbJJjtUhHCWY5ncn9mgZ5wxif1o` remains rollback.

Actual read-only browser smoke passed the new landing and theme switch, customer
account/activity/existing 107-paise receipt, merchant incoming/actual QR, operator
worklist with 15 completed payments and connected SSE, and saved generated report
`2ed1aec5-d7a4-4c7d-a01d-9eb29a6461d4`. That report retains its older snapshot
warning. Its historical central citation is not silently rewritten to current
evidence. A retained exact transition deep link opened collapsed technical
evidence and focused the correct record. Google sign-in remains available; prior
real first/returning consent evidence is reused, not repeated. No new payment or
provider generation was submitted.

The old frontend client loaded the new design after reload and safe close/reopen.
Served worker revision changed from `1d64399ab00cd317` to `4cb84b9bc2585559` and
matches its 19-asset manifest; `/sw.js` is served no-cache and update code waits
for existing tabs. API/auth responses remain excluded. Browser cache internals
were not directly exposed; no new hosted-offline emulation is claimed. Existing
production-shell fixtures and owner Chrome replay evidence retain their scopes.

Additional current-design breakpoint checks at 768/1024/1920 passed 24 cases.
Affected landing heading correction passed four desktop/mobile/theme checks and
a focused Lighthouse heading-order audit. Mobile lab performance was 99 with
LCP 1.7s, CLS 0.001 and TBT 10ms; its initial accessibility score was 99 before
that heading correction, not a claim of complete WCAG or field CWV certification.

Repository presentation cleanup preserves the verified private archive and Git
history. Canonical contracts/fixtures now live under `docs/api/contracts/`; retained
technical references are linked from the documentation index. Redundant planning
material is removed, all active consumers and packaging references are updated,
and approved corpus content/hashes are unchanged. See [hosted release notes](../releases/v3.0.0-hosted.md). Native Linux installation,
new native restored-worker and scheduled-backup/RPO claims remain separately
unverified and explicitly deferred from this hosted-demo pass.

## Experience V3 acceptance — 19 October 2026

Deployed backend: `433011cb39ce8e996c0a2d46a333095919d7f4e4`.
Vercel production frontend: `cafae17a0366fc5b4991bc6e1ea123053d5d3576`;
frontend source is unchanged between these commits.
API: https://transactx-demo-api.onrender.com; frontend:
https://transactx-frontend.vercel.app. Render deployment:
`dep-dbb536g473hc73clm290` (confirmed Gemini configuration activation);
Vercel: `dpl_2NbJJjtUhHCWY5ncn9mgZ5wxif1o`.
Previous same-source provider-disabled deployment: `dep-dbb4su6q1p3s73evc1s0`.
Immediate backend rollback: `cafae17` / `dep-dbas8gk9v7es739op080`;
earlier application rollback `426436d780b70eaf83042df91bf8f44fcf65ba99` retained.
Documentation-only commits do not change those deployed IDs.
Private evidence and the single checkpoint stay outside Git.

## Accepted visual system — 20 October 2026

The owner accepted corrected representative direction `c84536a` for extension.
Source `e36df2dfd6413acf3c32254f6bd389eb0b97a133` carries that DM Sans,
light/dark palette and layout system through existing login/demo/OAuth/privacy,
customer send/review/outcome/receipt/activity, merchant receive/incoming/settlement/search,
operator worklist/journey/investigation/saved reports/analytics, private admin and
labelled scenario routes. This supersedes the earlier rejected visual direction;
it does not discard the functional evidence or historical source scopes below.

Before/after full-page captures cover the representative screens in both themes
at desktop/mobile widths, with 320px, long identities/large money and actual 200%
zoom checks. The extension captures retain existing route-specific controls and
scope labels. Exact citation navigation opens collapsed evidence and focuses its
target; mobile operator navigation was corrected after two real selector failures.
The initial affected browser run had 27 passes and two mobile failures; the affected
mobile operator and actual-zoom rerun passed all four checks after the fix.
Static-only offline-shell/account-owner fixture checks passed both tests.
Frontend units, money, types/build and final-head CI passed; all five required CI
jobs succeeded on `e36df2d`. These fixture captures prove UI behavior in their
recorded scope, not new live financial/provider observations. Prior Google,
Gemini, financial and hosted-offline evidence is reused where behavior is unchanged.

Final Vercel production deployment and scoped read-only browser/static-update checks passed as recorded in the 20 October section above. The 19 October production/rollback IDs below retain their historical scopes.

Post-deploy authenticated customer/account/payment and saved generated-report
reads passed; Google remains enabled and registration disabled. Actual Render
logs show a worker heartbeat at433011c and two initial aggregate windows processed.
Gateway `/healthz` reports alive/database not-probed; authenticated reads provide
the separate database evidence. Owner confirmed Gemini activation: saved exact
provider/project/model/enable values and private review equality were checked;
same433011c configuration redeploy succeeded. Customer login/account/payment
reads still pass, Google remains enabled, durable attempts remain8 and the
shared disable latch is false. No further diagnostic generation was sent;
new hosted-worker generated execution remains unclaimed.

| Gate | Status and exact scope |
| --- | --- |
| Visual design acceptance | Owner accepted c84536a for extension; accepted system extended across existing routes in e36df2d, with final heading correction in cb41709 and scoped rendered/keyboard/responsive checks. Hosted f7e432a verification passed; no native certification is implied |
| Real representative role flows | PASS on deployed V3: customer payment/receipt, merchant activity/QR, operator journey, real worker-completed saved evidence report and all five citations |
| Authorization / HTTPS / CORS | PASS: foreign payment 404, merchant ops 403, viewer admin 403, exact frontend origin allowed and foreign origin rejected; OAuth callback no-store/no-referrer |
| Google first and returning sign-in | PASS: owner completed real External/Testing consent/login; independent SQL proves one identity, stable customer/accounts, no duplicates and zero balances. Not published to all Google users |
| Canonical PostgreSQL / corpus | PASS: migration checksums through33, fresh/legacy-upgrade CI, durable conformance/corpus lifecycle; cloud only additive31–33, no reset/reseed |
| Durable provider controls | PASS in disposable PostgreSQL/transport tests: request/token reservations, concurrency, disable latch, no retry, configuration binding and evidence fallback; not live generation proof |
| Report parser / verifier regression checks | PASS: strict required/nested shape and grounding; scoped entity bindings/enum reject the observed citation-as-entity error. Atomic maintenance admission preserves canonical limits/fences; normal store conformance passed. Corrected request also live-accepted |
| Live Gemini | PASS bounded maintenance verification; reviewed production configuration now enabled: corrected request count+generationHTTP200; STOP,5655input/502output tokens, exact model/JSON/schema/semantic grounding and independently re-read persisted citations pass. Job2ed1aec5-d7a4-4c7d-a01d-9eb29a6461d4; encrypted exact response and matching snapshot outside Git.8durable HTTP attempts across all checks; no retries/ID repair. Hosted saved-report browser reads passed; no new enabled-worker generation performed |
| Synthetic activity / financial checks / projection | PASS: 12 API-created enrichment entries plus representative payment, original identities retained; checked balance/ledger/participant invariants; automatic worklist projection observed |
| Encrypted rollback restore | PASS: refreshed cloud-to-owner-local encrypted whole-DB snapshot through33 restored into a fresh isolated PostgreSQL DB; before/after/restored summaries equal:14payments/5accounts/1Google identity/7jobs/6provider attempts, equal1100000paise totals and zero reconstruction/unbalanced/duplicate-participant-ledger errors.11.765s dump+restore phase, not an operational RTO guarantee |
| Password compatibility | PASS scoped regression evidence: real stored-password login/authenticated reads in disposablePG plus no-password OAuth denials. Exact original public-demo password recheck unavailable; not a separately specified mandatory gate, no reset needed |
| Hosted browser offline account-isolated replay | PASS on deployedcafae17 with owner-operated Chrome: Alice107paise→Bob queued offline, cached reload retained intent; logout/switchBob/offline reload showed no foreign intent and checked SQL found0matching server payments. ReturnAlice replayed automatically: exactly1COMPLETED payment530c0de1-cfb0-4ff0-bcb6-ced13e0c71ee, one owner-scoped idempotency record, one balanced central ledger transaction,4successful bank operations, correct participant ledgers and automatic projection. Screenshots/checked SQL private. Initial local request IDs were not captured; no arbitrary replays submitted |
| Hosted SSE transport reconnect | PASS, limited scope: actual frontend subscription bundled for Node against real HTTPS API; gateway expiry produced disconnect/gap, second connect/ready and200 recovery reads in122.4s. Browser offline/visual transition remains unclaimed |
| Native V3 crash / restored worker continuation / installation | Existing historical native scenarios retained at their tested source; new native V3 installation/continuation unverified under recorded launch rejection/no Linux target. Native distribution claim remains pending; this is distinct from the working Render demo |
| Independent backup / scheduled recovery target | Cloud-host-independent encrypted owner-local copy and isolated restore verified; a third physical device is not required by A7. Daily retention/schedule/RPO and new restored-worker continuation remain unverified |
| Idle/wake | PASS limited to existing deployed child group: Render logs show idle-stop15:17:28UTC, ready15:19:34, worker heartbeat atcafae17 and two real aggregate windows processed15:20:04. Data persistence verified by subsequent reads/backup. Not platform suspension or a crash |
| Cleanup | Completed in the current presentation changes: private hash-verified backups, canonical contracts/fixtures retained, consumer and packaging paths updated, redundant planning removed; approved corpus unchanged |
| Source promotion / final archive / release tag | PR27/28 normally merged; hosted V3 is on the default branch and deployed. Hosted release notes identify the presentation release; native archive/installation certification remains separate and unverified |

Source `433011c` has all five required GitHub jobs successful (backend, frontend,
disposable demo and both PostgreSQL runs); its Vercel preview comment check also
succeeded. Subsequent documentation receives focused verification, not another unchanged broad audit. Build
prerequisite validation finds all eight required commands, the actual API origin,
17 delivery inputs (including current provider/enrichment setup guides) and six approved corpus inputs; it creates no archive and is
not an installation/runtime proof. The complete native archive remains pending
its required acceptance gates. Spending is zero; reviewed Gemini configuration is enabled, with no
paid fallback, billing changes or financial/historical-data repairs.

### Requirement scope and smallest remaining actions

This separates the authorized **V3 Render demo** from the still-unfinished
**native distribution/operational claims**. No existing deliverable is waived,
and optional inspections are not new mandatory release gates. External V3 pack
references below name the owner-supplied files/sections, kept outside Git.

| Item / requirement source | Mandatory scope | Existing evidence / actual gap | Smallest action / who can do it |
| --- | --- | --- | --- |
| Original password: V3 `03_OAUTH_DATA_AND_AI.md`, identity constraints and PostgreSQL tests | Preserve password compatibility, not possession of one historical account's password | Real stored-password login/authorized reads pass in `TestGeminiColdCompositionWithOwnerReview`; OAuth-only password denial tests pass. That original cloud password itself is unavailable | No password/reset input is needed for this requirement. Agent reuses valid regression evidence; exact original-account live recheck stays unclaimed |
| Browser offline/replay: V3 QA§4.6 and [A5](../reference/testing-and-benchmarks.md) | Mandatory current V3 browser acceptance | PASS: owner-operated actual Chrome offline queue/cache reload/Bob isolation/Alice automatic replay screenshots plus independent checkedSQL;15payments total, conserved1100000paise, zero reconstruction/unbalanced/duplicate participant-ledger errors | Completed with owner network/UI handoff; no further toggle or synthetic payment needed for this gate. Evidence applies to deployedcafae17 frontend, not native installation |
| Gemini: V3 P8 / QA§8 | Mandatory before enabling/claiming generated reports; pack allows a clearly disabled optional provider | Corrected request passed full canonical grounding/persisted citations;433011c CI, deployment, authenticated API/browser saved-report reads, connected SSE and actual worker heartbeat passed. Owner-confirmed configuration is now enabled; normal hosted-worker generation not yet claimed | Activation completed and core-service checks passed. The completed maintenance verification authorizes no further diagnostic call |
| Native Linux installation: [A6](../../deploy/README.md) / native deployment guide; V3 P10 preserves Render/Vercel/Neon | Required for certified native bundle installation, not an additional hosting requirement for the already-working Render V3 demo | Builders/configs/conformance and prior scoped native recovery exist; no actual Linux installer/systemd/Caddy run on an owner target | Owner provides an existing free Linux host/OS/architecture and authorized SSH/sudo/DNS/TLS paths if completing that native deliverable. Agent can then install/test; no paid provisioning. Until then preserve pending status |
| Independent backup: [A7](../../deploy/RESTORE_REHEARSAL.md) / [backup plan](../deployment/native-deployment-and-backup.md#backup-and-restore) | Real encrypted restore for backup-feature claim; off-VM owner-local storage explicitly allowed. A7 is P1 in registry | Current Neon→owner-PC encrypted snapshot/isolated restore verified. Same-machine-as-source backup is not claimed; source is Neon. No third-device prerequisite. Daily schedule/retention/RPO and restored worker execution still pending | No new storage input for the verified off-host copy. For unattended daily-target claims, owner must supply/authorize an available free scheduler/host; agent cannot promise24h backups while an owner's PC is off |
| Native crash/restored worker: testing-plan recovery gates / A7 | Required for those exact native/recovery claims; changed areas need affected checks, not automatic repetition of every old experiment | Historical genuine process-kill/restore/projection artifacts retained; current V3 native continuation not claimed. Idle/wake is distinct | Hidden native API launch was automatically rejected (`blocked by policy`); agent will not retry or use an alternate launcher. Policy resolution or an authorized existing target is needed for new native proof |
| Cleanup: V3 QA§5 mandatory dependency/archive procedure | Required presentation cleanup | Verified original private archive plus fresh pre-edit snapshot; retained canonical contracts and engineering references; removed redundant planning copies; corpus unchanged | Current source and focused checks demonstrate cleanup; no historical evidence or contributor history erased |
| Final source/branch/archive/tag: V3 P10, A8 and testing-plan release gates | Hosted source/default branch/deployment verified | PR27/28 normally merged; Vercel f7e432a, backend433011c and rollback preserved; presentation cleanup retains unchanged application source | Tag and published release identify the final reviewed presentation tree; native installation and scheduled recovery certification remain separately deferred |

## Historical native source and architecture

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
