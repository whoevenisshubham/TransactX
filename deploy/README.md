# Native TransactX v2 deployment bundle

This directory packages the simulated system for a Linux host with PostgreSQL
18 and Caddy. It does not provision a cloud account or enable paid services.
The M2 worker and migration commands now exist, but M2/M3 investigator,
analytics and projector composition still needs integration. The build scripts
reject a missing binary command unless `--allow-incomplete` /
`-AllowIncomplete` is used for inspection. Their `complete` metadata describes
the file set only; it does not certify release tests or deployment readiness.

## Build

On Windows, run `pwsh deploy/scripts/build-release.ps1 -ValidateOnly
-AllowIncomplete` to inspect prerequisites. On Linux, run
`deploy/scripts/build-release.sh --validate-only --allow-incomplete`.
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
   DNS. The Caddyfile proxies the API on loopback; bank/DB ports stay private.
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
to the shared cluster or drops an existing database. A later M1+M2 disposable
rehearsal ran a separate worker on the restored database and rebuilt the
projection with the CLI. Its continuation input was manually seeded, and the
native worker still does not deliver outbox events. A7 therefore remains open.
The private age identity is never included in the archive.

See [RESTORE_REHEARSAL.md](RESTORE_REHEARSAL.md) for the isolated local procedure,
measured fixture result, refusal checks and remaining recovery gates.

The build is not an online deployment. A free VM, Vercel access, DNS and
provider identity checks must be available to complete the online gate. See
`docs/next-gen/execution-pack/docs/next-gen/DEPLOYMENT_AND_BACKUP.md` for the
security, backup and rollback contract.
