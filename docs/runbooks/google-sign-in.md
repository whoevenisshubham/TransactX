# Google sign-in

Google OIDC is off by default. Existing password JWT authentication and bounded
shared demos work without provider discovery. Google identities are separate
private customer accounts; they never select a merchant/operator role or link by
email. Generated public aliases hide the Google name/email. New central and
participant balances are both zero.

## Configuration (server only)

| Variable | Value/purpose |
| --- | --- |
| `GOOGLE_OAUTH_ENABLED` | `false` until configuration and consent are reviewed |
| `GOOGLE_OAUTH_CLIENT_ID` | Web OAuth client ID |
| `GOOGLE_OAUTH_CLIENT_SECRET` | Private server secret; never a VITE variable |
| `GOOGLE_OAUTH_REDIRECT_URI` | `https://transactx-demo-api.onrender.com/api/auth/oidc/google/callback` |
| `OAUTH_FRONTEND_ORIGIN` | `https://transactx-frontend.vercel.app` |

The origin must appear in exact `CORS_ALLOWED_ORIGINS`. Native configuration also
requires `TX_SIMULATION_MODE=true`, `TX_PARTICIPANT_PROVISIONING=true` and
`TX_ALLOW_REGISTRATION=false`. The demo host enables only atomic participant
provisioning when Google is explicitly enabled; registration and chaos remain
blocked. Do not substitute the Gemini API key for OAuth credentials.

Apply canonical forward migration `000031_google_oidc` before enabling. Existing
password data stays intact; external-only phone/password fields become nullable.
The downgrade refuses to delete or rewrite external users. Preserve the prior
release and private environment configuration for rollback; disable the Google
flag first and keep the forward schema if identities already exist.

## Protocol and limits

`GET /api/auth/oidc/google/start?app_challenge=…` stores a ten-minute hashed-state,
browser-cookie-bound flow with a separate provider PKCE verifier and nonce hash.
The host-only `__Host-transactx_oidc` cookie is Secure/HttpOnly/SameSite=Lax.
The fixed Google discovery/JWKS uses maintained `go-oidc` signature verification;
nonce, verified email, issued time and authorized party are additionally checked.
No provider access/refresh tokens are persisted. Redirects never contain JWTs.

The callback consumes the flow once, then redirects only to the configured
frontend `/auth/callback` with a hashed-at-rest one-time application code valid
for sixty seconds. That page strips the query, clears temporary session data and
exchanges the code with its tab's independent verifier. The exchange POST requires
the exact frontend Origin. No cross-origin credentialed cookie fetch is needed.
Both callback responses and frontend callback route use no-store/no-referrer.

Global durable limits are six starts and twelve callbacks/exchanges per minute,
one in-flight HTTP auth operation, at most 100 live flows/codes, five new Google
identities per UTC day and fifty lifetime. Existing identities remain eligible
after the new-account cap. Failed provisioning rolls back identity and counters.
Gateway auth admission is shared with password/demo sign-in. Restarting the API
does not reset durable counters or permit ambiguous code reuse.

## Owner setup and live verification (pending)

1. Sign in to [Google Auth Platform](https://console.cloud.google.com/auth/overview)
   and select your owner-controlled project. Do not enable billing or paid APIs.
2. Open **Branding**. App name: **TransactX**; support/developer contact: your
   monitored email. Homepage: `https://transactx-frontend.vercel.app`;
   privacy: `https://transactx-frontend.vercel.app/privacy`. Verify the privacy
   page is actually deployed before publishing consent. Follow any required
   Google domain/ownership checks; do not invent verified ownership.
3. Open **Audience**. For initial verification use **External / Testing**, adding
   your intended Google test account under **Test users**. Public sign-in requires
   the appropriate published audience and any Google-required verification.
4. Under **Data Access**, request only `openid` and
   `https://www.googleapis.com/auth/userinfo.email` (email). No profile, Drive,
   payment permissions or paid services are needed.
5. Open [Clients](https://console.cloud.google.com/auth/clients), **Create client**,
   type **Web application**, name **TransactX Web**. Authorized redirect URI:
   `https://transactx-demo-api.onrender.com/api/auth/oidc/google/callback`.
   Leave JavaScript origins empty: this is server authorization-code flow, not
   a browser Google SDK. The separate server OAUTH_FRONTEND_ORIGIN is
   `https://transactx-frontend.vercel.app`.
6. Download the client JSON into a private folder outside Git. Share only its
   absolute file path and the audience/test-user status; never paste its secret.
   Keep GOOGLE_OAUTH_ENABLED=false until configuration/deployment verification.

[Google's setup instructions](https://developers.google.com/identity/openid-connect/openid-connect#appsetup).

Store values in private environment files/Render secret configuration. Share
configuration file paths and consent/audience status, never secret values in chat
or Git. Full-page redirects do not need embedded Google scripts or wildcard CSP.

Focused local tests use `OIDC_TEST_DATABASE_URL` only with a disposable loopback
database named `txv3_oidc_*`. Run the affected packages with `go test -race -p 1`
because these explicit integration fixtures share that isolated namespace.
Signed-token fixtures, direct HTTP tests and intercepted browser exchange tests
are not live Google acceptance. Verify real consent, HTTPS cookies/CORS,
cancel/replay cases, first and returning customer and zero balances on the
deployed reviewed source before calling Google sign-in live.

The earlier automatic rejection of a native API launch (`blocked by policy`)
remains explicit. No alternate native launcher is authorized by this runbook.

Protocol references: [Google OIDC](https://developers.google.com/identity/openid-connect/openid-connect),
[go-oidc](https://github.com/coreos/go-oidc).
