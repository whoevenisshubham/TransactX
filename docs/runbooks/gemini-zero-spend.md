# Gemini: reviewed free-project path

Provider generation is disabled by default. The existing evidence-only
investigator stays available. The adapter is separate from OpenAI parsing and
does not select a paid fallback, use web/search tools or automatically retry.

## Owner review before enabling

Review the **actual API key's project** in AI Studio/Cloud: billing must be
disabled, and the chosen text model must have usable free quota in that project.
A consumer subscription, a key's existence or a local free-tier flag is not proof
of this. Free-project data may be used by Google to improve products; only send
the demo's synthetic evidence, never personal information. Keep billing disabled
and disable this integration before changing project billing/key access.

Current documentation lists free text tiers for `gemini-3.5-flash-lite` and
`gemini-2.5-flash-lite`; access to 2.5 is restricted to prior users. These are the
adapter's exact allowlist, not an assertion of this owner's current entitlement.
There is no default model. Recheck the actual project and current documentation
before choosing. No provider request has been made for this implementation.

After review, prepare a private approval using `prepare-gemini-approval.mjs`.
It reads a private raw key file, binds its SHA-256 fingerprint to the project and
model, and writes a new private file outside the checkout. It refuses overwrite,
prints no key and makes no network/billing changes. The two confirmation flags
mean the owner actually inspected billing/quota; the script does not verify
Google's account state. Default approval lasts 24 hours; the backend rejects any
approval longer than seven days and checks expiry before token counting and
generation. Expired approval produces an explicit evidence-only fallback.

Server-only settings:

| Setting | Purpose |
| --- | --- |
| `TX_GEMINI_ENABLED=false` | Keep off until owner review and release approval |
| `GEMINI_API_KEY` | Existing private server key; never VITE/browser configuration |
| `GEMINI_MODEL` | Exact reviewed allowlisted model |
| `TX_GEMINI_APPROVAL_FILE` | Absolute private approval JSON path |

Apply forward migration `000032_gemini_free_admission` first. The downgrade
refuses to erase attempt history. API and worker share the same PostgreSQL
admission. Preserve source/config rollback and never reset production counters
to make a demo run pass.

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
quota, expired review and unavailable generation retain evidence with a reason.

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
