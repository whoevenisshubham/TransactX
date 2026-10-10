# Security and abuse boundaries

## Assets and actors

Protect authoritative financial state, original operation identities, user-scoped data, credentials, provider budget, service access, operational evidence and deployment access. The application is a simulation, but public abuse could still alter demonstrations, exhaust a free tier or leak personal information.

Actors: CUSTOMER, MERCHANT, OPS_VIEWER, OPS_ADMIN, worker process, participant processes, deployment identity and optional model provider. No frontend role check replaces backend authorization.

## Role matrix

| Action | Customer/Merchant | OPS_VIEWER | OPS_ADMIN | Worker |
| --- | --- | --- | --- | --- |
| Read own existing payment DTO | Existing ownership rules | No implicit customer identity | Existing explicitly authorized ops evidence only | Internal repository scope |
| Read redacted ops worklist/evidence | No | Yes | Yes | Internal read |
| Read investigator reports for approved ops scope | No | Yes, rate limited | Yes, rate limited | Claims configured jobs |
| Schedule recovery recheck | No | No | Yes, gate enabled and audited | Discovery/scheduler |
| Execute chaos, corruption fixture, integrity/recon runs | No | No | Existing controls plus environment gates | Only configured deterministic maintenance |
| Execute monetary continuation | Create/retry own payment through kernel | No | No arbitrary financial override | Existing safe resumer only |
| Set model provider, install corpus, retrain model | No | No | Offline administrative deployment operation | Reads approved configuration |

Add OPS_VIEWER to database/check constraints and explicit authentication roles. Public registration cannot request a privileged role. Do not default unknown roles to viewer. Read endpoints supporting viewer must return audited redacted DTOs.

## Public demonstration

Default `APP_DEVELOPMENT=false`, `TX_ALLOW_REGISTRATION=false`, `TX_CHAOS_ENABLED=false`, `TX_RECOVERY_CONTROL_ENABLED=false`, `TX_AI_PROVIDER=rules`, paid budget zero. Keep `TX_SIMULATION_MODE=true` because all money/banks are simulated. Simulation mode alone must not expose administrator actions.

Publish only a read-only viewer credential if a login is required. Keep administrator access private. If public customer/merchant interaction is enabled, use isolated demo users, caps on amount/request frequency, and no uncontrolled funding or account creation. Existing seed logic runs once with development mode enabled, then the public runtime disables it. Reset is an authenticated offline operation with a backup/checkpoint; never periodically delete active financial state under visitors.

## Network and secrets

Bind banks to `127.0.0.1:8081` and `127.0.0.1:8082`, API to `127.0.0.1:8080`, and PostgreSQL to loopback. Expose Caddy 443 and necessary 80 certificate/redirect traffic. SSH uses owner keys and restricted source access when practical. OCI security rules and the host firewall must agree.

Add separate participant service tokens, compared in constant time and required outside explicit local development. The HTTP client sends only the token for the intended participant; never forward browser Authorization or accept user-selected participant URLs. Rotation is documented and does not change monetary identities.

Use environment files outside the repository, permissions 0600/0640 with deliberate service-user ownership. JWT secret minimum remains 32 random bytes; random generation is local. Do not echo secrets into logs or paste them into PRs. `VITE_*` is public frontend configuration, so it contains only API URL and safe capability flags. Provider keys, database URLs and private service tokens never appear there.

Audit existing cross-schema simulation provisioning/reconciliation before establishing DB roles. Use dedicated runtime roles and only necessary grants; migration/backup identities are separate. Runtime roles cannot alter schema or disable event triggers. Any retained central read grant to a participant schema is documented as a simulation limitation, not physical service isolation.

## Request boundaries

Explicit production CORS origins, exact scheme/host/port; no wildcard. Preview frontends get explicit permitted origins or a separate backend. CORS is not access control for non-browser clients; JWT/RBAC remain required.

Limit JSON bodies to 64 KiB for normal APIs, question length to 1,000 characters, export size to 2 MiB, lists to specified bounds. Use route-template rate limits: login/registration, investigator create, operational control, payment create. In-memory limits are documented as single-instance demo limits. Trust forwarded client IP headers only from the local reverse proxy.

Use stable idempotency for payment and new control/job creation. Worker/operator control duplicates cannot change financial identity. Reject replay with changed payload. Do not store plaintext request idempotency keys in public evidence.

## Browser safety

Preserve existing token handling unless a separately tested authentication migration is justified. Session storage is vulnerable to script execution; add a tested Content Security Policy and avoid unsafe HTML. React renders notes/model text as text. Do not pass model output through `dangerouslySetInnerHTML`. CSP must support the actual static build and API connect source; verify it in a browser before enforcing.

Sensitive responses use `Cache-Control: no-store`. Clear role-specific state on logout and account change. IndexedDB intents remain user scoped; replay after relogin cannot submit a previous user's intent. Do not cache authenticated evidence as public static assets.

## AI threats

Prompt injection can appear in questions, tool results, runbooks or saved reports. Allowlisted tools, server-owned scope, length bounds and output checks are the enforcement layer. Instructions in data cannot grant permissions. Tool schemas omit role/user/sql/url/shell arguments entirely.

Test forged evidence IDs, fabricated success, stale source, cross-payment access, unauthorized tools, unknown schema fields, provider refusal, budget exhaustion and misleading document instructions. Rate limits and transactional budget reservations prevent parallel overspend. Provider network egress is allowlisted; arbitrary model-provided URLs are never fetched.

## Audit and evidence

Record actor UUID/role, operation, entity, safe reason code, UTC time, request/trace identity and result for administrative scheduling, quarantine replay, corpus changes, model promotion and deployment. Audit rows are append-only to runtime identities, but do not call them immutable against a database administrator. SHA-256 export hashes detect changed contents; they are not proof of who produced them.

## Required security gate

Every new endpoint has no-auth, wrong-role, permitted-role and entity-scope tests. Viewer mutation attempts return 403. Every control requires environment gates. Bank token missing/wrong/right tests pass. Export/corpus/trace/model inputs are bounded. Secrets do not appear in built frontend, fixtures, logs or Git diff. Dependency/vulnerability checks are recorded with real tool results and triaged material findings.
