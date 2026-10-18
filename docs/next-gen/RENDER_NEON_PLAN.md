# Free public portfolio deployment

Current owner decision (18 October 2026): prepare a Vercel Hobby frontend,
one Render Free web service, and a Neon Free PostgreSQL project. Cold starts
and paused recovery while idle are accepted. No paid resources/provider calls.
This overrides the earlier VM-only deployment choice for this demo only.

## Ordered work and acceptance

1. Preserve the existing release and QR fix on `codex/render-neon-demo`.
2. Historical incident first: investigate the reported 158 missing transitions
   using read-only access and verified originals. Never infer original history
   from current payment state, disable triggers, or overwrite the source.
   No backup exists according to the owner; a read-only connection to exactly
   `127.0.0.1:5432/transactx` failed for missing authentication. Owner has no
   private credential path. Repair is therefore blocked, not completed.
   A fresh public demo database is independent of this unrecovered dataset.
3. Use Neon instead of Supabase: both are PostgreSQL, but Supabase Free pauses
   inactive projects after a week. Neon automatic compute suspension suits
   infrequent visitors. Validate canonical migrations and direct/session
   connections; transaction pooling cannot preserve session advisory locks.
4. Package the existing API, two banks and worker as separate child processes
   in one demo container. Keep participant ports on loopback. Only the gateway
   port is public. This is shared-fate demo hosting, not independent HA.
5. Gateway protection: global sustained/burst rate budgets, strict login budget,
   bounded active requests, bounded body size, blocked registration/control
   endpoints, fixed upstream, streaming-aware proxy and CORS for errors.
   Do not trust caller-supplied forwarded IPs or arbitrary JWT claims.
6. Stop ALL database-connected child processes after idle time and no active
   requests; cap stream lifetime. Health probes must not wake them or connect
   to Neon. A real API request wakes the process group. Leases and original
   operation IDs preserve recovery across stops. No artificial keep-alive pings.
7. Explicit fresh-demo initialization: canonical migrator, then an empty-only
   seed command with an expected database name and acknowledgment. Seed no
   old transitions. Public roles: customers, merchant, OPS_VIEWER. No public
   OPS_ADMIN credentials. Private secrets never enter Git/frontend build env.
8. Run focused protection/process-lifecycle tests, build all commands, and a
   disposable PostgreSQL smoke for payment/projection/investigation/restart.
   Docker image and managed-Neon conformance require their actual runtimes;
   distinguish local process proof from a deployed container/cloud proof.
9. Owner signs into Neon and Render; configure only Free plans and secrets.
   Apply migrations/seed to a NEW demo project, deploy backend, then deploy
   Vercel with exact public HTTPS API origin and matching backend CORS.
10. Verify from another device: login, cross-bank transfer, idempotent retry,
    merchant receipt/QR, viewer investigation, SSE reconnect, limits, idle/wake
    and worker projection. Record real URLs/source; promote only after checks.

## Free-plan constraints

Low traffic does not imply zero background compute. Worker polling and health
queries keep PostgreSQL awake. The idle gateway exists specifically to close
all pools between visits. Neon allowances, Render bandwidth/build quotas and
Vercel Hobby eligibility still apply. Free is a quota-bounded configuration,
not a promise of perpetual uptime. No automated upgrade or paid add-on.

References checked 18 October 2026:
- https://render.com/docs/free
- https://neon.com/docs/introduction/plans
- https://neon.com/docs/connect/connection-pooling
- https://supabase.com/pricing

## Historical incident unblock

Provide authorized read-only database access plus original rows from a backup,
WAL archive, export or other independently verifiable source. With access but
no originals, measure/document the loss and recovery feasibility; do not claim
restoration. Changing the affected dataset's disposition is a separate owner
decision. Continue deployment preparation without modifying that dataset.
