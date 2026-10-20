# Render + Neon + Vercel: free portfolio demo

This is an optional single-replica sleeping demo deployment. The native VM
deployment remains available. The gateway runs existing bank/API/worker
binaries as separate processes on one shared host. It does not replace sagas,
participant ownership, locking, outbox semantics or authorization with mocks.

## What is / is not done

The existing Free Render service and Hobby Vercel project deployed Experience V3
source `cafae17` on 19 October 2026. The existing synthetic Neon database was
upgraded with only canonical migrations 31–33; checksums verified and no pending
migrations remained. Do not repeat initial project creation or the empty-only
initializer on this deployment.

Real checks covered shared customer/merchant/operator entry, a 100-paise payment,
receipt deep link, automatic projection, a worker-completed saved evidence
summary and its five unique citation targets, role denials and exact-origin CORS.
The bounded enrichment manifest also completed with checked financial invariants.
Google is enabled for the owner's External/Testing audience after consent
configuration and migration verification. The owner completed first/returning
login; independent database checks confirmed stable customer identity and zero
central/participant balances. General Google access is not published. Gemini
remains disabled after its first token-count probe failed before generation.
These results do not prove generated reports, native crash recovery or full
release acceptance.

Preserve backend rollback source `426436d` and its prior Render deployment,
the previous Vercel production deployment and private configuration. An encrypted
pre-upgrade snapshot was restored successfully into a fresh isolated local
database. It is not an independent off-device backup or worker-continuation test.
Rollback application source only after compatibility review; never down-migrate
identity/provider tables or erase new financial activity to match an old snapshot.

Experience V3 adds optional shared demo sign-in through `POST /api/auth/demo`.
Set `TX_PUBLIC_DEMO_AUTH=true` only on the initialized synthetic demo database;
it defaults to false. It requires simulation mode and disabled registration,
checks the initializer's budget namespace and exact stored persona roles, and
uses the same gateway admission limits as password login. It exposes no seed
password and grants no administrator persona. Existing populated demo databases
must **not** be reinitialized to enable this flag. Deploy the compatible backend
before the frontend. Google sign-in is separately configured and is not enabled
by this flag.

Files and local checks can be prepared without cloud accounts. A successful
local test does not establish Render image compatibility, Neon privileges,
public TLS, provider availability, or live browser acceptance. No cloud account
is provisioned by these files. Never select a paid plan to make a step pass.

The historical 158-transition incident remains unrecovered. The source was
not changed. Never import that affected dataset as the new public demo.
Its read-only audit is `deploy/verification/historical-transitions-readonly.sql`.
The owner reports no backup or credentials, so it cannot currently be run.

## 1. Choose Neon Free

Create a new Free project near Render Singapore. Keep the default `neondb`
or update the explicit expected name consistently. Use **Direct connection**,
not the `-pooler` hostname: migration/session advisory locks require a stable
session. Require TLS; prefer `sslmode=verify-full` and test certificate trust.
Verify available PostgreSQL version/extensions/permissions against canonical
migrations before deployment. No Firebase/Auth/IdP migration is needed.

Store the URI only in a private shell environment/secret file and Render's
secret environment, not chat, Git, logs or any `VITE_*` variable. A Neon URI
is a credential. Use a new project with synthetic records only.

Why Neon: Supabase is compatible PostgreSQL but its Free project pauses after
one week of inactivity. Neon suspends compute after inactivity and wakes on
connection. Free compute, storage, transfer and provider policies still apply.
Check the provider dashboard; quotas are not an uptime guarantee.

## 2. Initialize the new empty database ONCE from a private terminal

Set `DATABASE_URL` privately in that shell. From `backend/`:

```powershell
go run ./cmd/migrate up
go run ./cmd/migrate verify
$env:TX_DEMO_EXPECTED_DATABASE = 'neondb'
$env:TX_DEMO_INITIALIZE = 'I_ACKNOWLEDGE_NEW_EMPTY_DEMO_DATABASE'
# Set TX_DEMO_PASSWORD privately to a generated password, >=12 characters.
go run ./cmd/demo-seed
Remove-Item Env:TX_DEMO_PASSWORD, Env:TX_DEMO_INITIALIZE
```

Seed requires an empty users/accounts/payments/participant-accounts dataset
and refuses `transactx` or `postgres`. It is atomic, not a reset command.
Never run it against a populated project, never disable transition triggers,
and never truncate immutable tables to reseed. Migrations run explicitly,
not on every cold start. The runtime image includes the migration/seed commands
for authorized local-container maintenance; Render Free has no interactive shell.

Seeded roles (the chosen password is shared ONLY by these synthetic roles):

| Identifier | Role | Opening simulated funds |
| --- | --- | --- |
| alice@transactx | CUSTOMER, Bank A | INR 10,000 |
| bob@transactx | CUSTOMER, Bank B | INR 1,000 |
| store@transactx | MERCHANT, Bank B | INR 0 |
| viewer@transactx | OPS_VIEWER | INR 0 |

No OPS_ADMIN is created or published. Share these demo credentials on your
portfolio only after authorization/limit checks. These are shared synthetic
accounts: do not enter personal information, private notes or real passwords.
Anyone using these accounts may see the synthetic activity of other visitors.
The public deployment cannot honestly advertise per-visitor private accounts.

The initializer also installs **demo-only capacity triggers**, outside canonical
migrations, on new payment and investigation inserts. Each table admits at most
50 committed rows per database UTC day and 1,000 over its lifetime. Counters
update in the same transaction, survive sleep/redeploy and serialize admission.
At capacity, new work fails; existing jobs/evidence remain intact. Owners can
inspect `demo_limits.budgets` and arrange maintenance, but no automatic reset,
deletion or bypass is included. These caps limit write amplification, not every
possible storage/compute cost. Alert on provider quotas and maintain backups.

## 3. Deploy ONE Render Free web service

After pushing this branch, connect GitHub to Render and import `render.yaml`
as a Blueprint, or create a Docker Web Service manually:

- Branch: the reviewed `codex/render-neon-demo` head (later its merge target).
- Dockerfile: `deploy/render/Dockerfile`; build context: repository root.
- Instance: **Free**, region Singapore, one instance; no disk, worker service,
  cron, managed Render database, paid add-on or paid trial dependency.
- Health path: `/healthz` (gateway liveness only; not database readiness).
- Set `DATABASE_URL`: direct Neon URI; `TX_DEMO_EXPECTED_DATABASE`: `neondb`.
- Set `CORS_ALLOWED_ORIGINS`: exact HTTPS Vercel origin, no path/trailing slash.
- JWT/bank-token secrets are generated by Blueprint. If configuring manually,
  generate three distinct random secrets of at least 32 characters privately.
- Do not add provider keys, public admin credentials, initialization flags,
  `TX_DEMO_PASSWORD` or development-mode settings to the running service.
- Review the summary: every resource must show Free / $0 before creation.

For a hard zero-spending preference, do not attach a payment method or enable
paid features. Render documents that without a payment method, exhausted
bandwidth disables free services and exhausted build minutes disable new builds,
rather than billing overages. With a payment method, even a Free service can
incur overage charges. If signup unexpectedly requires billing or a paid plan,
stop and report it; do not work around provider checks or upgrade automatically.
Source: https://render.com/docs/free and https://render.com/docs/faq .

Runtime protections are fixed in the demo gateway, even if the shell contains
unsafe development flags. Bank ports 18081/18082 and API 18080 bind loopback.
Only the gateway's `PORT` is public. Do not proxy bank endpoints publicly.

The public demo shares a Neon owner connection across its processes for simple
provisioning. Logical bank ownership is preserved, but this is **not independent
database-role isolation**. Keep all credentials server-side. A deployment needing
stronger compartmentalization should provision/test least-privilege roles first.

## 4. Configure Vercel Hobby

Use the same reviewed branch, Root Directory `frontend`, Vite, build
`npm run build`, output `dist`. Existing `frontend/vercel.json` handles SPA
deep links. Set `VITE_API_BASE_URL` to the exact Render HTTPS origin, then
redeploy. Only this PUBLIC URL enters the frontend; no DB/JWT/service secret.
Copy the resulting exact Vercel origin into backend CORS and redeploy Render.
Prefer one stable production origin; do not wildcard every preview deployment.

## 5. Request/abuse protections

| Control | Fixed demo limit |
| --- | --- |
| All API requests | Burst 60; refill 4 requests/second |
| POST requests | Burst 6; refill 20/minute |
| Login requests | Burst 3; refill 6/minute; one hash at a time |
| Concurrent proxied requests | 12, including SSE |
| Request body | 32 KiB, including chunked bodies |
| Header size | 16 KiB at gateway server |
| Proxy request/stream lifetime | 2 minutes, then client reconnects |
| Header/body read timeout | 5s / 15s |
| Registration / chaos | Disabled at gateway and backend |
| Recovery-control writes | Disabled by forced backend config |
| Payment/investigation persistence | Separate database caps described above |
| LLM calls | Evidence summaries while `TX_GEMINI_ENABLED=false`; any private key alone cannot enable generation |

Budgets are **global to the one replica**, not per IP. This prevents forged
`X-Forwarded-For` and invented tokens bypassing quotas, but a malicious visitor
can consume the shared allowance and temporarily deny other visitors. They are
application limits, not a DDoS shield or a guarantee against provider bandwidth
exhaustion. Health/preflight do not hit PostgreSQL; Render still accounts for
their network traffic. Limit responses carry `Retry-After` and readable CORS.

## 6. Sleep / restart behaviour and quotas

The gateway stops the entire bank/API/worker group after 90 seconds with no
active requests. It closes all pools, so worker polling, bank health queries
and pool health checks cannot keep Neon awake. `/healthz` only reports gateway
liveness and never wakes the group. A genuine API request starts the group;
readiness waits for bank/API listeners with a bounded deadline. Child failure
stops the group; a subsequent request can restart after cooldown. SIGTERM is
forwarded; unresponsive children are killed after six seconds. No public
database/worker listener or artificial keep-alive traffic is added.

Render independently sleeps its free web service after 15 minutes of inactivity;
visitors can wait about a minute on the next visit. Neon has its own compute
wake-up. Leases expire normally, and workers resume pending work after startup.
No work runs during sleep. Open dashboards/SSE count as active work and consume
allowance; close them when finished. The idle design is not a usage guarantee.

## 7. Focused acceptance on the ACTUAL cloud deployment

1. Check HTTPS frontend and `/healthz`. Do not treat liveness as database proof.
2. Login as Alice; send INR 1 to Bob, then to store. Retry the SAME request key:
   one payment and one financial effect. Confirm balances, transition history,
   bank operations and automatic worklist projection.
3. Merchant QR renders; incoming payment appears. Viewer can investigate and
   retain citations but cannot use admin/chaos/recovery controls.
4. Check production frontend deep links, SSE disconnect/reconnect after the
   two-minute stream bound, and account-isolated offline replay.
5. Exercise limits with a small bounded probe, verify 429/413/CORS and that
   a client-supplied forwarded IP does not bypass the global budget.
6. Close all clients. Verify child pools disappear after idle and Neon compute
   can sleep. Visit after Render sleep; login/payment/recovery still work.
7. Check a controlled restart during pending work on synthetic data, then verify
   original IDs, participant effects, conservation and projection. Do not call
   process packaging a full independent-service failure-isolation proof.
8. Record source SHA, real URLs, actual limits, results, skips and date in the
   checkpoint. Keep optional providers disabled until separately verified; a
   stored private key is not proof of provider readiness.

If a free quota is exhausted, accept temporary unavailability or reduce usage;
do not enable billing or switch to a paid plan. Database backups are separate
private owner operations; free hosting is not an off-machine recovery guarantee.
