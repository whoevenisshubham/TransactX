# Local development with disposable synthetic data

Use Go 1.27+, Node.js 22+ and PostgreSQL 18. Create a **new** local database and
load its URI privately as DATABASE_URL. The binaries do not load dotenv files.
Do not reuse a historical/source database, reset existing data or paste secrets
into commands, chat or Git. Configuration examples are in deploy/env/.

Apply the full migration set from backend/ with `go run ./cmd/migrate up`, then
`go run ./cmd/migrate verify`. Do not follow an old numbered subset of SQL files.
Keep GOOGLE_OAUTH_ENABLED=false and TX_GEMINI_ENABLED=false for this setup.

In private shell configuration set a locally generated random JWT_SECRET of
at least 32 characters, APP_DEVELOPMENT=true, TX_SIMULATION_MODE=true and
TX_PARTICIPANT_PROVISIONING=true. Use DEFAULT_BANK_CODE=BANK-A,
BANK_A_CODE=BANK-A, BANK_B_CODE=BANK-B,
BANK_A_URL=http://127.0.0.1:8081, BANK_B_URL=http://127.0.0.1:8082 and
CORS_ALLOWED_ORIGINS=http://localhost:5173. Load the same private DB/JWT/runtime
settings into every process that needs them. Never expose participant ports.

From separate terminals in backend/:

| Process | Command | Additional local configuration |
| --- | --- | --- |
| Bank A | `go run ./cmd/bank-a` | BANK_A_DATABASE_URL = disposable URI; BANK_A_ADDR=127.0.0.1:8081 |
| Bank B | `go run ./cmd/bank-b` | BANK_B_DATABASE_URL = disposable URI; BANK_B_ADDR=127.0.0.1:8082 |
| API | `go run ./cmd/api` | APP_ADDR=127.0.0.1:8080 |
| Worker | `go run ./cmd/worker` | Same canonical/participant configuration as API |

From frontend/, run `npm ci`, then `npm run dev`. Its default API origin is
http://localhost:8080; a deployment uses VITE_API_BASE_URL at build time.
The public landing page and /showcase require no running API. Authentication,
payment and operator observations require the actual backend and data.

Registration/provisioning creates zero-balance accounts; it does not fund a demo.
For intentionally funded shared roles use only the acknowledged empty-database
initializer described in [the demo runbook](../../deploy/render/README.md).
Do not rerun it on a populated database. Provider features remain separate.

## Focused verification

Run `npm test`, `npm run lint`, `npm run test:money` and `npm run build` from
frontend/ after relevant changes. Browser fixtures use intercepted responses;
`npm run test:browser:live` needs the real disposable stack and explicit inputs
listed in [deployment verification](../../deploy/README.md).

OIDC, Gemini and fresh application composition tests require explicit loopback
txv3_oidc_* databases via OIDC_TEST_DATABASE_URL, GEMINI_TEST_DATABASE_URL
and APP_TEST_DATABASE_URL. Never supply production credentials. The dedicated
[CI workflow](../../.github/workflows/auth-provider-postgres.yml) applies canonical
migrations and rejects skipped required tests. These tests do not launch a native
API or establish process-crash recovery. Run only affected packages during
development; retain one combined acceptance after stabilization.
