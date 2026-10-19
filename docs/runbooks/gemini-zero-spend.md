# Gemini: one-time reviewed free-project configuration

Generation is disabled by default. Evidence summaries stay available without
Gemini. No paid fallback, billing/plan changes or automatic provider retries.

## Owner review and limits

Inspect the actual key's project in AI Studio/Google Cloud: billing must be
disabled and the selected model must have usable free quota. A key, subscription
or application flag is not proof. The app cannot independently verify Google's
billing state, prove a key belongs to the named project, or guarantee permanent
free-tier availability. Project identity is an owner assertion bound to that
exact key fingerprint. Keep billing disabled; never upgrade to make a test pass.
Only send synthetic evidence, not personal/real financial information. Google's
free-project data handling may permit use to improve its products.

**No calendar expiry or periodic renewal.** Unchanged configuration remains
reviewed after months of sleep/redeployment. Revalidate when key, project,
provider, model or billing configuration changes; disable generation before
changing it. External billing changes cannot be detected automatically. No
renewal job or automatic attestation to unchecked account state is installed.

Quota exhaustion, expired/revoked keys, model retirement, network/timeout and
provider failure persist a visibly labelled evidence-summary fallback. They do
not block login, payments, recovery or API/worker startup.

## One-time preparation and Render configuration

Keep TX_GEMINI_ENABLED=false during preparation. After actual owner review:

```powershell
node deploy/scripts/prepare-gemini-review.mjs --key-file=C:/PRIVATE/gemini-key.txt --output=C:/PRIVATE/gemini-review.json --provider=google --project=REVIEWED_PROJECT --model=REVIEWED_ALLOWLISTED_MODEL --confirm-billing-disabled --confirm-free-model
```

Use private absolute paths outside Git. The helper binds provider/project/model
and the key fingerprint, records the review date without an expiry, refuses
overwrite and prints no key. It never contacts Google, verifies billing/quota,
activates generation or changes account settings. Its confirmation flags mean
that the owner actually performed those checks. The owner confirmed the existing
project's Free tier and disabled billing. On 19 October 2026, actual AI Studio
**Rate Limit > All models** showed Gemini 2.5 Flash Lite text-generation limits
of 10 RPM, 250K TPM and 20 RPD for that project. Model discovery also returned
the model. A configuration-bound review was prepared from this actual evidence,
not discovery alone. These quotas are an observation, not a permanent guarantee.

The first acknowledged canonical verification job persisted `provider_unavailable`
at `countTokens`, with one failed durable admission and zero `generateContent`
attempts. Its saved result is evidence-summary fallback, not generated-report
acceptance. Production generation stays off. No automatic retry or counter reset
was performed. The verification tool now records bounded HTTP status/provider
error category without keys, URLs, prompts or response bodies. The owner then
authorized exactly one additional diagnostic probe. It returned HTTP 404 with
Google status `NOT_FOUND` at `countTokens`; checked durable SQL shows two failed
count attempts in total and zero generation attempts. Both original jobs and
receipts remain intact. The endpoint/model resource construction matches the
official token-count API; the exact provider-side reason is not established.
Model discovery and visible quota do not prove that this key can invoke it.
Production stays disabled; no further probe is authorized. Next prerequisite is
owner/Google confirmation of usable token-count access for the exact selected
project/key/model, without enabling billing or changing the plan.

The exact model allowlist is gemini-3.5-flash-lite and gemini-2.5-flash-lite.
There is no default model or automatic alias/fallback. An allowlisted name does
not establish this project's current model access or free quota.

In the existing Render service, open **Environment > Environment Variables**.
Retain its private GEMINI_API_KEY. Add the settings below and paste the generated
JSON as one server-only TX_GEMINI_OWNER_REVIEW value. This is not a file path:
there is no secret-file mount or recurring file replacement. Apply the same
settings to a separately hosted worker if used; the demo supervisor inherits
server configuration for its child processes. Preserve the working source/config
rollback and existing release/review gates before deploying changed settings.
[Render environment variables](https://render.com/docs/configure-environment-variables#setting-environment-variables).

| Server-only setting | Purpose |
| --- | --- |
| TX_GEMINI_ENABLED=false | Default off; true only after verified configuration and authorized integration |
| GEMINI_API_KEY | Existing private server key; never chat, Git or VITE_* |
| GEMINI_PROVIDER | Exactly google |
| GEMINI_PROJECT_ID | Owner-verified actual key project ID |
| GEMINI_MODEL | Exact reviewed allowlisted model |
| TX_GEMINI_OWNER_REVIEW | Helper JSON bound to provider/project/model/key, owner confirmations and review date |

Apply migrations through 000033_gemini_disable_switch first. Migration32 retains
durable attempts;33 adds the shared emergency stop. TX_GEMINI_APPROVAL_FILE is
no longer read. Old expiring files are not automatically converted into new
confirmations. Missing/mismatched review fails closed to evidence summaries
without preventing core startup. Configuration loads on deployment/startup.

## Immediate shared disable

### One controlled verification while production stays disabled

After actual project/model quota review, `backend/cmd/verify-gemini` can run one
synthetic investigation without enabling the production provider. Supply private
`DATABASE_URL`, `GEMINI_API_KEY`, `GEMINI_MODEL`, `GEMINI_PROJECT_ID`, the helper's
`TX_GEMINI_OWNER_REVIEW`, and a normal authenticated read-only demo operator JWT
in `TX_VERIFY_OPS_TOKEN`. Never put these values on a command line or in Git.
Keep `TX_GEMINI_ENABLED=false`.

From `backend/`, pass `--expected-host=OWNER_CONFIRMED_DIRECT_NEON_HOST`,
`--expected-db=neondb`, `--api-origin=https://transactx-demo-api.onrender.com`,
`--payment-id=EXISTING_SYNTHETIC_UUID`, `--receipt=NEW_PRIVATE_ABSOLUTE_PATH` and
`--ack="VERIFY ONE SYNTHETIC GEMINI INVESTIGATION"` to `go run ./cmd/verify-gemini`.
The command checks the exact connected target and actual API operator against
the stored demo identity. Evidence comes from the real authenticated evidence
API, and retrieval uses the existing approved PostgreSQL corpus.

It uses the canonical investigator/store/grounding, existing UTC token limits
(80,000 input/12,000 output), durable ten-request admission and shared disable
latch. One generation round has a 600-token output cap and no automatic retry.
The canonical report/provenance and a private exclusive receipt are retained.
The stable verification request key and refusal to overwrite a receipt prevent
automatic resubmission. If another worker wins the normal lease or any gate
fails, inspect the original job rather than submitting another key. No API,
worker or alternate native service launcher is started. No financial SQL is
written, production configuration stays unchanged, and evidence-summary fallback
is not a successful generated-report test.

Only after separate explicit owner authorization, the single diagnostic identity
is selected with `--diagnostic-ack="OWNER AUTHORIZED ONE ADDITIONAL DIAGNOSTIC PROBE"`
and a new private receipt. It does not reset or replace the original identity;
repeating it reuses that same diagnostic identity and refuses resubmission.
There is no arbitrary attempt number or automatic retry loop.

After checking the existing synthetic demo target, use its Neon SQL Editor
with owner access to stop **new admissions** without a deploy:

```sql
UPDATE gemini_free_admission SET disabled=TRUE WHERE singleton RETURNING disabled;
```

Expect one row with true. Every countTokens/generateContent admission locks and
checks this row before charging/sending. It applies across API/worker processes,
survives sleep/redeployment and does not reset counters or financial records.
Already admitted/in-flight operations are not retroactively canceled; every
HTTP operation retains its ten-second timeout. Startup never clears this latch.
TX_GEMINI_ENABLED=false remains the normal startup/deployment switch.

To resume, complete any required configuration/billing revalidation, deploy
matching configuration if changed, then deliberately set disabled=FALSE using
the same target-checked owner SQL procedure. Never clear attempt/token counters
or busy leases to resume early. No public administrative endpoint is introduced.

## Hard bounds and provenance

Each outbound HTTP attempt is durably charged **before** sending: token counting,
generation, failures and interrupted/unknown attempts all consume the global
ten-request UTC-day cap. One global sixty-second lease covers the count/generate
sequence; each HTTP operation has a ten-second timeout. A network-uncertain
attempt keeps its lease until expiry. Restart does not refund attempts or clear
the lease. Native process-crash proof remains a separate acceptance gate.

The normal preflight count plus one generation consumes two requests, allowing
at most five investigations/day, fewer after failures. Google countTokens checks
the complete generation request before its input is admitted (max 8,000 tokens).
Generation is one candidate/one report round, max 1,200 output tokens including
thinking. There are no schema-repair calls, hidden continuation or SDK retries.
The retained canonical job reservation additionally uses a UTC daily token
window (80,000 input/12,000 output, zero monetary allowance). Existing paid
provider nonzero currency requirements remain enforced; no fake dollar budget
or generic paid-provider bypass was added.

The fixed HTTPS Google endpoint receives the key only in its request header;
redirect following is disabled. Bounded JSON uses the existing report schema,
payment-scoped evidence and retrieved runbooks. Existing deterministic grounding
replaces model-controlled financial prose, validates citations and persists the
report. No model-selected mutation tool is exposed. Refusal, malformed output,
quota, mismatched review and unavailable generation retain evidence with a reason.

Persisted attempt data records kind/model/status, actual response ID and nullable
token usage. Thinking is included when reported. No currency charge is invented
from token counts. No key, raw prompt/response or provider exception body is
written into that ledger. Synthetic transport fixtures/green tests do not prove
a real provider response or owner project entitlement.

References: [billing](https://ai.google.dev/gemini-api/docs/billing/),
[project rate limits](https://ai.google.dev/gemini-api/docs/rate-limits),
[pricing](https://ai.google.dev/gemini-api/docs/pricing),
[token counting](https://ai.google.dev/api/tokens),
[structured output](https://ai.google.dev/gemini-api/docs/structured-output).
