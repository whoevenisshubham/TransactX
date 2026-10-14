# Native TransactX v2 deployment bundle

This directory packages the simulated system for a Linux host with PostgreSQL
18 and Caddy. It does not provision a cloud account or enable paid services.
The combined candidate mounts the M3 investigator, durable analytics and native
worker projector. Current acceptance and remaining owner/release gates are in
`docs/next-gen/DELIVERY_REPORT.md`; integration does not imply release readiness.
The build scripts
reject a missing binary command unless `--allow-incomplete` /
`-AllowIncomplete` is used for inspection. Their `complete` metadata describes
the file set only; it does not certify release tests or deployment readiness.

Both builders validate and stage the current architecture, delivery report,
safe acceptance artifacts, owner requirements, restore guide and read-only
financial queries. Missing/empty documentation, malformed evidence JSON or
repository-escaping inputs fail validation. This staging is not an install or
runtime test. The final archive still needs all release gates and the configured
public HTTPS API origin.

## Build

On Windows, run `pwsh deploy/scripts/build-release.ps1 -ValidateOnly
-AllowIncomplete` to inspect prerequisites. On Linux, run
`deploy/scripts/build-release.sh --validate-only --allow-incomplete`.
Set `VITE_API_BASE_URL` to the owner-controlled public HTTPS API origin before
building an archive. Both scripts reject a missing, loopback, reserved example
or malformed origin, and record the normalized origin in `metadata.json`.
Validation-only reports an unset origin without writing an archive; it does
not verify DNS, TLS or ownership. The later HTTPS smoke/browser gate does.
After the combined release gates pass, run the script without the incomplete
flag. It runs frontend checks, builds Linux amd64 and arm64 Go
binaries, and writes a timestamped archive and SHA-256 inventory under
`deploy/out/`. The archive contains only explicit files: binaries, migrations,
static assets, configuration examples and public runbooks. It does not contain
environment secrets, database dumps or `node_modules`.

Use the source SHA in the archive metadata and `go version -m` on binaries to
verify build provenance. The bundle's metadata records whether its Git tree
was dirty and which planned commands were absent.

## Local real-browser gate

Start the candidate API, banks, worker, disposable PostgreSQL database and
frontend separately. Provision one payment that the operator roles can read.
Set `VITE_API_BASE_URL` to the loopback API origin when starting the frontend.
From `frontend/`, run `npm run test:browser:live` with
`TX_LIVE_FRONTEND_URL`, `TX_LIVE_PAYMENT_ID`, `TX_LIVE_PAYMENT_STATE`,
`TX_LIVE_VIEWER_ID`, `TX_LIVE_ADMIN_ID` and `TX_LIVE_PASSWORD` set in the
private test shell. The live suite does not intercept HTTP responses or
provision users. It checks login, worklist/evidence navigation, role controls,
reload and a 360px view. Missing inputs cause a failure. Use only disposable
credentials and data; a passing result is scoped to the exact candidate stack
under test. The full release gate also requires worker-driven projection and
M3 investigation/analytics responses.
The investigation/analytics gates require `TX_LIVE_API_URL` and
`TX_LIVE_DISPOSABLE_DB=1`, an active reviewed corpus, durable analytics windows
including null measurements, and a native investigator worker. Enable the
simulation's control routes in the disposable configuration to verify viewer
denials. The actor-scope case also requires `TX_LIVE_FOREIGN_VIEWER_ID`, a second
generated viewer using the operator test password, plus the generated customer
inputs below. Provision these fixtures privately; the tests perform real login
and never grant roles themselves. For the expiry case, run a separate API with
`JWT_LIFETIME` at most 10 seconds and set `TX_LIVE_EXPECT_EXPIRY=1`; otherwise
that case is skipped and no expiry acceptance is claimed. SSE reconnect and
retained-citation cases remain required even when a candidate fails them.

Run `npm run test:browser:live -- accessibility.live.spec.ts` with the real
viewer/payment/state inputs to check worklist, evidence, completed/saved
investigation citations and analytics at 360, 768 and 1440 pixels. Controls
are reached with Tab and activated with Enter; axe checks critical/serious
WCAG findings and document overflow. The separate 200% case uses a temporary
browser extension to set native tab zoom, verifies doubled device pixel ratio
and halved content width, and saves `actual-zoom.json`. It never substitutes CSS
scale for browser zoom. Persistent test profiles are closed and removed.

For a generated self-signed loopback certificate, set `NODE_EXTRA_CA_CERTS` to
its public PEM certificate for the API test client. When system trust is
unavailable, `TX_LIVE_TLS_SPKI` may contain the base64 SHA-256 of that certificate's
DER SubjectPublicKeyInfo. The browser accepts only that exact key; the live
configuration rejects this option unless both origins are loopback and
`TX_LIVE_DISPOSABLE_DB=1`. Non-disposable and external origins were verified
to reject it. Do not use general `ignoreHTTPSErrors`/certificate-error flags.
Keep keys/credentials private. This setup proves encrypted local simulation
transport; public-host CA/certificate acceptance remains a separate smoke gate.

The same live suite includes an offline intent case. It requires three distinct
disposable customer IDs (`TX_LIVE_SENDER_ID`, `TX_LIVE_RECEIVER_ID`,
`TX_LIVE_OTHER_ID`), their `TX_LIVE_CUSTOMER_PASSWORD`, `TX_LIVE_API_URL`, and
`TX_LIVE_DISPOSABLE_DB=1`. The sender needs at least ₹1 in its disposable
account. Use a production frontend build with its service worker active and
the exact `TX_LIVE_API_URL` baked into `VITE_API_BASE_URL`; the API must be
loopback HTTPS and the frontend must use a loopback origin. The test creates
one ₹1 simulated payment, aborts its API write, disconnects the whole browser,
reloads the cached shell, switches users, then reconnects and checks the real
API payment and ownership. Run it only against a disposable database and
simulated banks. Run this case alone with
`npm run test:browser:live -- offline.live.spec.ts` when operator credentials
are not part of the disposable stack. `npm run test:browser:offline-shell` separately checks the
production shell and read-only saved-intent view with an explicit API fixture;
that fixture test is not live payment evidence.
The service worker stores only static HTML and hashed JS/CSS. The offline view
does not show a cached balance or submit a payment until the API revalidates
the session after reconnect.
The environment examples list only options parsed by the current M2
configuration. Earlier `TX_OUTBOX_*`, `TX_AI_*`, `TX_ANALYTICS_ENABLED` and
`TX_TRACE_*` placeholders had no parser and must not be treated as enabling
those services. M2 owns any future runtime configuration for them.

## Install and operate

1. Put PostgreSQL and banks on loopback. Create restricted roles and a separate
   application Unix user. Do not expose 5432, 8081 or 8082 publicly.
2. Copy examples from `deploy/env/` to `/etc/transactx/`, fill real values there,
   and restrict ownership/permissions to the service user and administrator.
   Keep API, bank and worker credentials distinct. Public registration, chaos
   and recovery controls stay disabled unless a reviewed scenario needs them.
3. Use `install-release.sh` with the exact archive and SHA. It installs under
   `/opt/transactx/releases/<sha>` and switches `current` only after validating
   the archive. Apply migrations with the M2 migration command before starting
   the new API. Never blindly replay migration SQL on an existing database.
4. Install the matching systemd units. Start Bank A and Bank B, then API, then
   worker. Configure Caddy only for an owner-controlled hostname with working
   DNS. Copy `deploy/caddy/Caddyfile.example` to `/etc/caddy/Caddyfile` and configure
   for your domain; it proxies the API on loopback while bank/DB ports stay private.
5. Configure Vercel with Root Directory `frontend`, Build Command
   `npm run build`, Output Directory `dist`, and an actual HTTPS
   `VITE_API_BASE_URL`. Add that exact Vercel origin to backend CORS. The SPA
   rewrite is in `frontend/vercel.json`.
6. Run `smoke.sh` against the real HTTPS API and frontend origin. Verify login,
   authorization, SSE reconnect, browser deep links, and public bank/DB port
   denial before claiming an online deployment.

## Backups and restore rehearsal

`backup.sh` uses one custom-format `pg_dump` snapshot for the central and both
bank schemas. It separately encrypts global role/tablespace metadata without
password values. It requires a private PostgreSQL passfile, an `age` public
recipient, and a private destination. If an off-VM path is configured, that
path must be mounted; the script stops instead of silently writing to the VM
when the mount disappears. Local retention keeps seven daily and two Sunday
copies. The backup timer does not run until the owner installs its unit and
configures the backup identity and destination.

`restore-check.sh` refuses a live database name, creates a new disposable
`transactx_restore_*` database, decrypts directly into `pg_restore`, checks
central and participant schemas, balances, central ledger conservation and
idempotency references, and writes a safe report. It never applies global roles
to the shared cluster or drops an existing database. The current native M1
rehearsal at `4d8f2af` killed the real API after its operation journal committed,
encrypted/restored that pending snapshot, and verified worker continuation.
A native restored worker was also killed during a running lease; its replacement
took over at a higher epoch. Automatic worker projection and acknowledged outbox
delivery were proven before running the independent rebuild CLI. See
`docs/next-gen/state/native-crash-restore-final-m1.json` for exact scope/hashes.
This was a local same-bank transfer, not every crash boundary or off-machine
disaster recovery. The historical manually seeded rehearsal remains narrower.
The private age identity is never included in the archive.

For independent read-only SQL proof, run `deploy/verification/native-financial-proof.sql`
against the disposable database with psql `ON_ERROR_STOP=1` and variables
`payment_id`, `operation_id` (original hold UUID), `sender_user_id`,
`receiver_user_id`, `idempotency_key`, and `payment_note`. Keep database credentials
in the private passfile. The script uses quoted psql variables and returns the
checked counts, balances, payload correlation, corpus and projection state.
For the recorded 100-paise transfer from 10,000/0 opening balances, require one
payment/key/transaction, two correctly owned central entries, 9,900/100 balances
in both authorities, four unique participant operations, three correctly owned
participant entries, zero mismatch/duplicate/error counts, one matching projection,
all outbox events acknowledged/inboxed, and a resolved recovery job. Nonzero SQL
exit or a differing count is a failed gate. These values describe this fixture;
they are not universal production balance expectations. Git Bash callers should
pass paths with forward slashes so sha256sum does not escape Windows backslashes.

The separate `cross-bank-financial-proof.sql` covers the recorded 100-paise
BANK-A→BANK-B native hold-commit/reply-loss interruption. Supply psql variables
`payment_id`, `operation_id`, `sender_id`, `receiver_id`, `key`, and `note`.
Require the same central conservation checks, two unique operations in each
bank, one original source hold effect, three correctly owned participant entries,
zero payload/journal mismatches or duplicate effects, and automatic acknowledged
projection. Exact observed checkpoint/process/source evidence is in
`docs/next-gen/state/cross-bank-native-recovery-m1.json`. This query is read-only;
it does not create or advance recovery state.

The release includes `bin/linux-*/transactx-corpus`, the approved corpus manifest
at `docs/runbooks/corpus-manifest.json`, and its six document inputs at their
original relative paths. Both builders reject an input hash mismatch and stage
the reviewed documents with LF encoding. After canonical migrations and the
corpus integration gates pass, use the matching architecture's corpus command
with `-root <bundle-root> -manifest <bundle-root>/docs/runbooks/corpus-manifest.json`
and the configured local database connection to stage it; `-activate` explicitly
activates the version. `-dry-run` validates the packaged inputs without database
writes. The corpus command does not call a model provider.
It also includes `transactx-rebuild-worklist` for the reviewed maintenance
rehearsal. Installation links both tools under `run/`; complete-bundle
validation requires all eight commands for both architectures and the corpus
manifest. Rebuild acceptance still requires the canonical backend lock and
worker integration gates; shipping the command does not prove those gates.

See [RESTORE_REHEARSAL.md](RESTORE_REHEARSAL.md) for the isolated local procedure,
measured fixture result, refusal checks and remaining recovery gates.

The build is not an online deployment. A free VM, Vercel access, DNS and
provider identity checks must be available to complete the online gate. See
`docs/next-gen/execution-pack/docs/next-gen/DEPLOYMENT_AND_BACKUP.md` for the
security, backup and rollback contract.
