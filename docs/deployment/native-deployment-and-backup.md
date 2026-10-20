# Free tier deployment and restore runbook

## Supported topology

React/Vite static build on Vercel Hobby. Native Go API, Bank A, Bank B and worker plus PostgreSQL on one Linux VM. Caddy terminates HTTPS and proxies only the API. Banks and PostgreSQL listen on loopback. No Docker or Kubernetes is required on the developer computer or VM.

Current Oracle Always Free A1 allocation documented on 11 October 2026 is equivalent to 2 OCPUs and 12 GB RAM, with resource limits and idle-reclamation conditions. Capacity may be unavailable. Recheck actual console eligibility before provisioning. A budget alert does not impose a hard spending cap; do not enable paid resources or upgrade accounts automatically.

Vercel Hobby is for a personal non-commercial demonstration within its limits. A real commercial startup deployment needs a new hosting decision. No service is promised free forever. Render's expiring free PostgreSQL is not a permanent database path.

If the VM is unavailable, complete the native release bundle and deploy the static site with an explicit backend-unavailable/recorded-walkthrough state. Do not silently remove the worker or claim a static walkthrough is the running distributed system. An existing owner-controlled Linux host can be a fallback, with the same security and uptime limitations.

## Agent-created deployment files

M1 creates these during implementation; examples in this document are specifications, not installed services:

```text
deploy/
  README.md
  env/api.env.example
  env/bank-a.env.example
  env/bank-b.env.example
  env/worker.env.example
  systemd/transactx-api.service
  systemd/transactx-bank-a.service
  systemd/transactx-bank-b.service
  systemd/transactx-worker.service
  systemd/transactx-backup.service
  systemd/transactx-backup.timer
  caddy/Caddyfile.example
  scripts/build-release.ps1
  scripts/build-release.sh
  scripts/install-release.sh
  scripts/smoke.sh
  scripts/backup.sh
  scripts/restore-check.sh
```

Scripts are idempotent where possible, fail on errors, validate exact target directories, and avoid echoing secrets. Install requires explicit release archive, target host and environment. A missing required value is an error, not a localhost production fallback.

## Build and artifact

Read toolchains from the current repository. Baseline Go is 1.27.1; Node 22 matches current CI; PostgreSQL 18 matches current integration environment. Verify actual package engine support rather than blindly installing an arbitrary version. No unnecessary major upgrades.

Use `npm ci`, tests, typecheck and production build. Build Go binaries for Linux amd64 and arm64, including API, both banks, worker, migration and maintenance tools. Set build metadata for source SHA. Use CGO-disabled builds where dependencies allow; Go race tests run separately on a supported native CI platform, not through an unsupported cross-compile.

Release archive includes binaries, migrations, configs, runbooks, corpus/model artifacts if verified, SHA-256 manifest, SBOM or dependency inventory, and validation report. It never includes credentials, database dumps, node_modules or private customer data. Preserve historical experiment artifacts separately from freshly generated ones.

## VM installation

1. Select an actually free-eligible image/shape in the home region. Check combined CPU/RAM/storage and existing account resources.
2. Install PostgreSQL 18 from a verified official distribution source and Caddy from its official package source. Use a dedicated application Unix user with no interactive root runtime.
3. Store versions at `/opt/transactx/releases/<source-sha>/`; create a `current` symlink; keep at least the last working release. Environment files live at `/etc/transactx/` with restricted permissions.
4. Create DB and runtime roles, applying migrations with the migration identity. Audit shared-schema compatibility before grants are reduced. Never expose PostgreSQL publicly.
5. Install systemd units. `ExecStart` points to an absolute binary path; environment file is explicit; working directory is stable; restart uses a bounded delay. Use `NoNewPrivileges`, `PrivateTmp` and read/write directory restrictions that have actually been tested.
6. Start banks, then API, then worker. Dependencies and health waits are bounded. Bank failure degrades readiness/capabilities rather than falsifying completed state.
7. Configure host firewall and provider security list. Confirm bank/DB ports cannot be reached externally, including IPv6 if enabled.

## Environment contract

Existing API variables: `APP_ADDR`, `APP_DEVELOPMENT`, `DATABASE_URL`, `JWT_SECRET`, `JWT_ISSUER`, `JWT_LIFETIME`, `DEFAULT_BANK_CODE`, `BANK_A_CODE`, `BANK_B_CODE`, `BANK_A_URL`, `BANK_B_URL`, `TX_SIMULATION_MODE`, `TX_PARTICIPANT_PROVISIONING`, `CORS_ALLOWED_ORIGINS`. Bank variables include their DATABASE_URL and ADDR names.

New variables to implement and document once in shared config:

| Group | Variables and safe starting defaults |
| --- | --- |
| DB/concurrency | `TX_DB_MAX_CONNS=8` for API/worker, 4 for banks; `TX_PAYMENT_CONCURRENCY=2` |
| Recovery | `TX_RECOVERY_ENABLED=true`, `TX_RECOVERY_CONCURRENCY=2`, `TX_RECOVERY_STALE_AFTER=30s`, `TX_RECOVERY_SCAN_INTERVAL=10s`, `TX_RECOVERY_LEASE=60s`, `TX_RECOVERY_HEARTBEAT=15s`, `TX_RECOVERY_ATTEMPT_TIMEOUT=20s`, `TX_RECOVERY_MAX_ATTEMPTS=20` |
| Delivery | `TX_OUTBOX_ENABLED=true`, `TX_OUTBOX_BATCH_SIZE=25`, `TX_OUTBOX_MAX_ATTEMPTS=10` |
| Public controls | `TX_ALLOW_REGISTRATION=false`, `TX_CHAOS_ENABLED=false`, `TX_RECOVERY_CONTROL_ENABLED=false` |
| Service auth | `BANK_A_SERVICE_TOKEN`, `BANK_B_SERVICE_TOKEN`; distinct random secrets outside local development |
| Telemetry | standard `OTEL_SERVICE_NAME`, `OTEL_EXPORTER_OTLP_ENDPOINT` if configured; `TX_TRACE_STORE_ENABLED=true`, `TX_TRACE_RETENTION=24h`, `TX_TRACE_MAX_SPANS=20000` |
| Investigator | `TX_AI_ENABLED=true`, `TX_AI_PROVIDER=rules`, `TX_AI_MODEL` only if enabled, `TX_AI_DAILY_TOKEN_LIMIT=0`, `TX_AI_MAX_INPUT_TOKENS=8000`, `TX_AI_MAX_OUTPUT_TOKENS=1200`; `OPENAI_API_KEY` only server side |
| Analytics | `TX_ANALYTICS_ENABLED=true`, `TX_MODEL_PATH` if verified model exists, `TX_MODEL_MAX_AGE=720h` |

These new names do not exist in the baseline and must not be represented as working until implemented. Invalid values fail startup with a sanitized clear message. Zero paid budget prevents provider calls. API binds 127.0.0.1:8080; banks bind 127.0.0.1:8081/8082. Worker has no public HTTP listener; its health is heartbeat/administrative local status.

Development seed runs once using existing explicit development flags and a private administrator password. Public runtime returns to APP_DEVELOPMENT=false. Public registration/provisioning are disabled unless the bounded interaction scenario is explicitly enabled.

## HTTPS and free hostname

Use a free owner-controlled DNS hostname such as a Duck DNS subdomain if no paid domain is available. Its update token is a secret. Point DNS to the VM and verify resolution before asking Caddy to issue a certificate. A verified trusted public-IP certificate is an alternative only if the installed server/issuer/browser path supports it; do not use an untrusted self-signed certificate for the public React API.

Example Caddy site is the owner's actual API hostname with `reverse_proxy 127.0.0.1:8080`. Configure streaming so SSE frames flush and no proxy timeout/buffering silently kills the stream. Keep a long-lived SSE request out of generic short response WriteTimeout policy; normal requests still have explicit limits. Confirm actual behavior through HTTPS.

## Vercel settings

- Import the existing personal GitHub repository.
- Production branch is the validated release branch, initially `release/transactx-v2`; changing the repository's default branch is unnecessary.
- Root Directory `frontend`; Install `npm ci`; Build `npm run build`; Output `dist`.
- Set `VITE_API_BASE_URL` to the actual HTTPS API origin before building. Rebuild when it changes.
- Add a `frontend/vercel.json` SPA fallback for console deep links and appropriate security/cache headers.
- Add the exact deployed frontend origin to backend CORS. Preview deployments are explicitly allowlisted or use a separate sandbox; no wildcard host suffix.
- Verify hard refresh at a payment detail route, login, REST, SSE, expiry/logout and offline reconnect.

Cloudflare Pages can serve the same `frontend/dist` build if Vercel is unavailable, with its platform-specific SPA fallback. Keep this as a documented alternative, not a second simultaneous production stack.

## Migration safety

M2 builds a small migration CLI with schema version/checksum table, exclusive advisory migration lock, lexical numeric ordering, error stop and bounded lock timeout. Existing SQL migrations remain unchanged.

New empty database applies all migrations once. An existing database without a migration ledger requires an explicit baseline command only after schema verification through 000021; do not blindly rerun CREATE TABLE scripts or mark unknown schema current. New migration IDs are reserved in `TECHNOLOGY_DECISIONS.md`.

Take a backup before deployment. Use additive schema changes and forward repair where possible. Do not automatically run destructive down migrations on a live demo. If app rollback cannot read the new schema, stop and use the documented migration recovery path rather than resetting data.

## Backup and restore

Run one `pg_dump` custom-format backup of the complete TransactX database so central and both bank schemas share a consistent snapshot. Dump required global role/grant metadata separately with careful encryption. Do not create three unrelated snapshots and claim cross-schema consistency.

Encrypt before off-VM upload; keep the decryption key outside the backup location. Option: an owner-controlled private OCI object bucket within currently verified free quotas, or an owner's local storage copy. Keep 7 daily and 2 weekly small backups within quota; enforce size/retention caps. Do not use a public GitHub repository for dumps. Backups in the same tenancy are useful but not independent of account loss; keep an owner-controlled extra copy where feasible.

Backup schedule target is daily. Logical daily backup RPO target is up to 24 hours; no claim of continuous PITR is made. Measure actual restore time and data-loss window. RTO target under 30 minutes on the documented rehearsal environment, not a production guarantee.

Restore rehearsal creates an explicitly named disposable database, verifies the target is not the live DSN, restores using compatible PostgreSQL tools, checks migrations/counts/ledger conservation/idempotency/participant operations, runs financial integrity, and verifies the restored worker/projection can continue safely. Rebuild derived projections after restoration if needed; do not rerun historical monetary operations to reconstruct a UI.

Keep a rehearsal artifact with backup hash, source SHA, creation/restore timestamps, versions, counts and all check results. A command that produces a dump but has never been restored is not a verified backup feature.

## Release and rollback

Install archive into a new release directory, migrate with lock, restart in order, run smoke and public browser checks, then retain the prior version. If application smoke fails, stop worker claims, switch binaries to the prior compatible release, restart and verify. Do not destroy newly committed financial facts to roll back presentation code.

TLS failure, expired credential, reclaimed VM or changed free limits produce a visible unavailable state and a precise incident/runbook. Never generate artificial CPU/network load solely to evade a provider's idle policy.
