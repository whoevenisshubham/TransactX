# Native TransactX v2 deployment bundle

This directory packages the simulated system for a Linux host with PostgreSQL
18 and Caddy. It does not provision a cloud account or enable paid services.
The current v2 plan requires the M2 worker and migration commands before a
complete v2 archive can be built. The build scripts reject an incomplete
archive unless `--allow-incomplete` / `-AllowIncomplete` is used for inspection.
Such an archive is labelled incomplete and is not a v2 deployment candidate.

## Build

On Windows, run `pwsh deploy/scripts/build-release.ps1 -ValidateOnly
-AllowIncomplete` to inspect prerequisites. On Linux, run
`deploy/scripts/build-release.sh --validate-only --allow-incomplete`.
After M2 commands and the combined release gates exist, run the script without
the incomplete flag. It runs frontend checks, builds Linux amd64 and arm64 Go
binaries, and writes a timestamped archive and SHA-256 inventory under
`deploy/out/`. The archive contains only explicit files: binaries, migrations,
static assets, configuration examples and public runbooks. It does not contain
environment secrets, database dumps or `node_modules`.

Use the source SHA in the archive metadata and `go version -m` on binaries to
verify build provenance. The bundle's metadata records whether its Git tree
was dirty and which planned commands were absent.

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
to the shared cluster or drops an existing database. Worker continuation and
projection rebuild must be run after M2 delivers those commands before A7 is
verified. The private age identity is never included in the archive.

The build is not an online deployment. A free VM, Vercel access, DNS and
provider identity checks must be available to complete the online gate. See
`docs/next-gen/execution-pack/docs/next-gen/DEPLOYMENT_AND_BACKUP.md` for the
security, backup and rollback contract.
