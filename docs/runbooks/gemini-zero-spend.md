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
Production stays disabled. A later, separately authorized model-change result
is recorded below; the two original jobs/attempts were never replaced or refunded.

### Investigation of the 404 (19 October 2026)

The sanitized request reconstructed from `fec5320` is below. The key and actual
canonical prompt/evidence/schema values are deliberately redacted; this is the
wire structure, not a replayable capture or a new live result.

```http
POST https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash-lite:countTokens
Content-Type: application/json
x-goog-api-key: [REDACTED]

{"generateContentRequest":{
  "model":"models/gemini-2.5-flash-lite",
  "systemInstruction":{"parts":[{"text":"[canonical prompt redacted]"}]},
  "contents":[{"role":"user","parts":[{"text":"[JSON question/evidence/runbooks redacted]"}]}],
  "generationConfig":{
    "maxOutputTokens":600,"candidateCount":1,"responseMimeType":"application/json",
    "responseJsonSchema":"[canonical schema object redacted]",
    "thinkingConfig":{"thinkingBudget":0}
  }
}}
```

The [token API](https://ai.google.dev/api/tokens) documents this endpoint and
complete `generateContentRequest` wrapper. No top-level `contents` is sent with
that wrapper. [API version guidance](https://ai.google.dev/gemini-api/docs/api-versions)
supports `v1beta`; there is no evidence justifying a blind version switch.
Saved authenticated discovery lists both `countTokens` and `generateContent`
for this model. A focused PostgreSQL transport test checks the exact POST path,
header placement, full wrapper and equality of counted/generated content.

However, Google's current [Developer API access notice](https://ai.google.dev/gemini-api/docs/deprecations#gemini-25-flash-models)
limits Gemini 2.5 to prior active users and recommends 3.5 Flash-Lite or 3.8 Flash
for new projects. This makes restricted legacy access a concrete hypothesis;
the sanitized `NOT_FOUND` alone does not prove that specific cause. The Developer
API page says no 2.5 shutdown date is announced. Do not substitute a Vertex AI
lifecycle date or infer that billing/a different key is required.

The already-allowlisted `gemini-3.5-flash-lite` is in saved discovery with both
methods. Actual project **Free tier > All models** shows 15 RPM / 250K TPM /
500 RPD for its text row; [standard pricing](https://ai.google.dev/gemini-api/docs/pricing#gemini-3.5-flash-lite)
lists free input/output. No search/maps/cache/batch tools are requested. No
project, key, billing, production model or enabled flag changed. A new
configuration-bound review is required before a deliberate model change; an old
2.5 review must never be reused as a 3.5 review. An explicitly authorized bounded
3.5 check would test the changed legacy-access hypothesis, not repeat the same
2.5 request. Production activation still requires a real grounded saved report.

### Authorized 3.5 result (19 October 2026)

The owner authorized the prepared single canonical 3.5 verification. It ran once
under its separate fixed identity and exclusive private receipt. `countTokens`
and `generateContent` both returned HTTP200. Independent checked PostgreSQL
queries record completed counting with 4,979 input tokens and the generation
attempt with 4,979 input /584 output tokens plus a response ID. Four global
admissions are charged in total, including both earlier legacy failures. No
counter, job, reservation, receipt or lease was reset/refunded.

Invocation access for the same key/project with 3.5 is now demonstrated, without
billing or key changes. However, canonical decoding returned `provider_malformed`;
the job is failed with a persisted evidence-only report. This is not a successful
grounded generated report. Token totals alone do not establish a token-limit
finish, model-version mismatch or JSON decoding cause. The original receipt
contains HTTP metadata, not a retained response body/finish reason. Actual
project UI says logging is disabled and requires billing; leave it disabled.

The maintenance observer now records bounded structural diagnostics for a future
authorized generation: envelope validity, response-ID presence, model-match
boolean, candidate/text/thought counts, allowlisted finish enum and report JSON /
strict canonical-decode booleans. No response text, ID, arbitrary model/provider
string, prompt, key, URL or report is logged by that observer. Response bytes and
all production validation/limits are preserved. Offline tests check these
redactions and byte-preserving behavior. These diagnostics cannot reconstruct
the past result. No further request is authorized or automatically attempted;
production remains off pending diagnosis and real report acceptance.

### Offline diagnosis and proposed instrumented test

The existing receipt has HTTP200 events but **no retained generation body,
finish reason, actual model-version string, candidate/part layout or report
JSON**. The global generation ledger records `failed` with token usage and a
response ID: this failure arose inside the adapter, before citation grounding.
A citation failure after decoding would instead be `grounding_rejected`.
Neither the old failed job nor its evidence-only fallback contains the rejected
generated report. It cannot be recovered by re-reading that fallback.

Local regressions distinguish MAX_TOKENS, truncated/trailing JSON, missing
required fields, null arrays, incorrect enums, unknown properties, wrong typed
assertions, foreign evidence/runbook citations and stale snapshots. A discovered
parser defect is fixed: Go decoding alone accepted missing required fields.
The adapter now enforces the embedded schema's structural subset, failing closed
on unsupported schema keywords, before the unchanged semantic grounding stage.
This defect is not established as the cause of the past live adapter failure.
The verifier also now independently reads the persisted job and requires a real
collected fact citation; an empty-facts generated report cannot pass that gate.

The verification cap remains600 (production hard maximum1200 unchanged). A
compact one-citation candidate using the saved snapshot identifiers is722UTF-8
bytes. There is no exact local model tokenizer; byte count and the observed584
tokens do not prove that the actual required response fit, or that it truncated.
No cap, prompt, schema or citation rule is relaxed to obtain acceptance.

**Instrumented procedure, separately authorized and executed below:** use the same reviewed3.5
model/key/project, canonical evidence/store and existing600-token cap, at most
one count plus one generation with no retries. Before admission, require a new
private `--encrypted-response=PRIVATE_RESPONSE.age` and existing
`--age-recipient-file=PRIVATE_RECIPIENT_FILE` alongside the private receipt.
The installed age tool/recipient is preflighted, and the output is exclusive and
outside Git. The complete bounded response (maximum1MiB) is encrypted directly
from memory, never written as plaintext. Encryption failure fails closed; it
does not trigger another call. Keep the matching private age identity locally.

The sanitized receipt records the canonical decoder's fixed rejection code as
well as finish/model/part diagnostics. The encrypted response and that job's
saved snapshot/chunks permit exact OFFLINE replay through JSON/schema and
citation validation until the cause is understood. This covers truncation,
model identity, required fields, actual report shape and semantic citations
without a second diagnostic generation. Retain both earlier failed jobs and all
four earlier charged attempts. This is not standing permission for more calls;
any further fixed test identity requires explicit owner authorization. Production
stays disabled. Do not enable Google's paid response logging.

### Instrumented result and offline correction

The authorized provider sequence sent exactly one count and one generation:
bothHTTP200, `STOP`, one non-thought text part, configured model match,
4,979input/563output tokens. The full response is encrypted outside Git with
the matching private evidence snapshot. Canonical JSON/schema decoding passed;
semantic grounding correctly rejected it. Exact offline replay through the real
decoder and `GroundGeneratedReport` proves the model copied a known evidence ID
into `assertion.entityId`; the required value was that evidence's payment entity
ID. State/value, citation and snapshot matched. This result was not truncated;
it does not retroactively establish the cause of the earlier uncaptured failure.

The corrected request now supplies explicit collected `citationBindings` and a
per-request string enum of permitted entity IDs, using the documented structured
output enum capability. It excludes evidence IDs and null entities for published
typed assertions. The shared schema/prompt is not mutated. All exact pair/value
grounding remains mandatory; rejected reports are never rewritten or accepted.
A regression reproduces evidence-ID-as-entity rejection; request tests verify
the scoped bindings/enum and absence of cross-request schema mutation. This
request correction has local acceptance only; no further generation was sent.

Before the actual provider sequence, an initial queued verification was claimed
by the normal deployed worker and failed `provider_disabled` with zero provider
rounds; checked global attempts remained4. A wrapper-store preflight was then
rejected by the existing concrete canonical-store guard, also with zero calls.
All jobs/empty captures were retained. Maintenance now atomically creates and
leases its canonical job, and runs it under that exact owner/epoch. The concrete
Gemini store/review guard was preserved, not relaxed. Normal HTTP jobs still use
the regular queue. Real disposable PostgreSQL/race tests verify foreign-worker
denial, idempotency, unchanged lease and active limits, ordinary store conformance
and guarded generated execution. The required CI gate now includes that atomic
admission test, so it cannot silently skip.

The resulting total is6charged HTTP attempts, not six attempts in this owner
authorization. No provider retries, counter refunds, lease resets or financial
SQL were used. Grounded generated-report acceptance remains pending; production
is off. A future corrected-request check must be separately authorized, use the
same bounds/encrypted capture and keep the failed reports and provenance intact.

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
`--payment-id=EXISTING_SYNTHETIC_UUID`, `--receipt=NEW_PRIVATE_ABSOLUTE_PATH`,
`--encrypted-response=NEW_PRIVATE_RESPONSE.age`,
`--age-recipient-file=EXISTING_PRIVATE_RECIPIENT_PATH` and
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

For the separately reviewed legacy-access hypothesis, the prepared alternative
is `--diagnostic-ack="OWNER AUTHORIZED ONE GEMINI 3.5 VERIFICATION"`, requiring
exact model `gemini-3.5-flash-lite`, its own bound review and new private receipt.
It has a third fixed identity, preserving both failed legacy jobs. This option
is not permission to run it; explicit owner authorization is required first.

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
