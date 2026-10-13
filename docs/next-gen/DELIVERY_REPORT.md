# TransactX v2 delivery acceptance

Status: local acceptance advanced; release promotion and complete archive pending.
This report governs combined delivery. The root M2 release report and older
integration matrices preserve their lane/run scopes; they do not certify the release.

## Source and architecture

Current M1 integration includes candidate `72383d0` and preserves all lane history.
The last fetched partner heads are M2 `c9da5fb` and M3 `99f3e14`; release remains
`6ad42bf`. M1 verifies in its isolated checkout and pushes its owner branch only.
Exact tested source/file hashes are in the evidence below. No final tested release
SHA, archive hash or installation result exists yet.

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

## Remaining merge and delivery gates

1. M2 must correct healthy-calibration provenance and checked error outcomes,
   and the crash harness's ignored SQL/unconditional participant claims.
   Exact files and requirements: [owner requirements](state/M1_INTEGRATION_REQUIREMENTS.md).
   Historical seeded/in-process tests and Gate 7's untracked recovery command
   are not native process proof. M1's native cases prove their listed boundaries.
2. Review migration 29 checksum variants and migration 30 rollback policy;
   current upgrade proof covers canonical 29, not every historical deployed variant.
3. Run the final exact-source combined PostgreSQL/race/build/vet, frontend,
   contract/auth and real browser/security gates after required corrections.
   Reuse only evidence matching the relevant source and boundary.
4. Supply the actual owner-controlled public HTTPS API origin for the builders.
   Create the complete clean-source amd64/arm64 archive with source SHA,
   member SHA256SUMS and archive hash, then perform real install/start/smoke checks.
5. Public deployment requires configured free host/access and DNS/TLS.
   Off-machine backup requires an independently mounted/configured destination.
   Optional live-provider validation requires separately authorized configuration;
   provider calls remain disabled. OAuth/IdP and Kubernetes are not prerequisites.

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

Current handoff: independent M1 work is complete at the recorded scopes. The
last real HTTPS CLI smoke passed its health/SPA/CORS prefix but stopped at the
owner-only auth-header guard on Git Bash's NTFS `noacl` mount; no guard was
relaxed. See `state/https-smoke-platform-m1.json`. Full authenticated Linux smoke,
the M2 corrections, actual public API origin and final combined/archive/install
gates above remain required. Work stops at these inputs without repeated polling.
