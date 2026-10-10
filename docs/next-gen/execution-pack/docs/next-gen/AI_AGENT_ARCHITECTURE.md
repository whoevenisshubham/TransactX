# Read only payment investigator

## Product job

Answer an operator's question about a selected payment by collecting evidence, retrieving relevant runbooks, and producing a traceable report. The flagship query is why a payment is still pending. The investigator must distinguish a central fact, a participant observation, missing evidence, a hypothesis and a recommendation.

This is a bounded investigation workflow, not a general chatbot. It cannot settle, reverse, approve, compensate, reroute, retry money, start chaos, run SQL, execute a shell or browse arbitrary URLs.

## Modes and truthful capability reporting

`evidence_only` is the zero-cost default: gather real authorized evidence, retrieve approved documents, apply deterministic report templates, and show the result under that label. This is useful evidence automation; do not call it an LLM result.

`llm` uses an explicitly configured OpenAI provider or a compatible local model adapter. Generation plus retrieval is the RAG mode. Provider keys remain server-side. Real tool-calling is enabled only when supported and tested by the configured model. A provider lacking that capability can receive the already collected evidence packet and generate a structured report; label orchestration as deterministic evidence collection in that mode.

An optional local model can run on a partner's machine without per-request API billing, but availability, memory and latency must be measured. Do not deploy a large local model on the small free VM by default. Claude is a future alternate provider, not a required second implementation in this release.

## Packages and interfaces

M3 owns `backend/internal/investigator/{model,service,tools,retrieval,grounding,provider,repository}.go`, corresponding tests, `docs/runbooks`, corpus manifest, fixtures, and evaluation tooling. M2 owns API mounting, shared configuration, migrations and runtime composition.

Use a small explicit provider interface such as `Generate(ctx, GenerationRequest) (GenerationResult,error)`. The request contains typed schema, bounded evidence/document packets, allowed tool definitions, remaining budgets and a prompt version. The result contains structured output or typed tool calls plus actual usage metadata. Do not let a provider adapter access payment repositories directly.

`EvidenceReader`, `RunbookRetriever`, `TraceReader`, `AnalyticsReader`, and `InvestigationStore` are narrow interfaces. A trusted request scope is injected by the server and cannot be overridden by model-generated tool arguments.

## Tool registry

| Tool | Allowed arguments | Read result and bound |
| --- | --- | --- |
| `read_payment_evidence` | selected payment UUID only | One redacted EvidenceBundle; 3-second source deadline |
| `read_recovery_attempts` | selected payment UUID, limit <=20 | Existing attempt evidence; no scheduling |
| `read_related_reconciliation` | selected payment UUID, known related run UUID | Bounded existing run/discrepancy context; no run creation |
| `read_trace_context` | trace ID already present in selected evidence | At most 200 safe spans; no raw logs |
| `read_operational_window` | bank/target in selected route, bounded UTC window | Up to 60 completed aggregate windows |
| `retrieve_runbook` | query <=500 chars, approved tags, topK <=5 | Approved chunks with source hashes and sections |

Reject unknown tools, unknown fields, untrusted entity IDs, excessive windows and repeated identical requests. Tool calls are executed by deterministic code after authorization, schema validation and budget checks. Retrieval content and tool results are untrusted data, never instructions.

## Orchestration

1. Authenticate; validate payment scope; apply per-actor/global limits; insert a durable job with actor-scoped idempotency.
2. Claim the investigation job with lease owner/epoch. A worker crash resumes from stored tool outputs and budget reservation rather than creating another unlimited call.
3. Collect core payment/route/operation/recovery evidence deterministically once.
4. Build a retrieval query from the user's question plus sanitized reason/state tags. Fetch approved documents.
5. In evidence-only mode, construct the structured report using deterministic statements and citations.
6. In LLM mode, send the bounded evidence packet and schema. Allow at most two additional model-directed tool rounds, only from the read registry. Optional tool rounds are not mandatory for every question.
7. Validate the final schema, each evidence ID, each document citation, source statuses, identity and freshness. Reject unsupported statements or downgrade them to explicitly unsupported inference only when safe and transparent.
8. Store validated report, source hashes, prompt/model versions, actual usage and tool timeline. Expose it through the authenticated GET.

Do not hold a DB transaction or payment lock while calling a model. On restart after a provider reply may have been received but before report persistence, reuse stored request IDs/results if available. Otherwise mark interrupted/uncertain and enforce the reserved cost budget; no automatic infinite re-generation.

## Runtime budgets

Default evidence-only jobs: three requests per actor per minute, one active job per actor, two active jobs globally; 30-second overall deadline. LLM defaults when explicitly enabled: one active call globally; two provider rounds maximum; eight unique tool calls maximum; document topK five; each chunk maximum 500 words; total model context capped at 8,000 input tokens; generated output cap 1,200 tokens. Measure provider-compatible limits and clamp at server side.

If a model cannot support exact token estimation, reserve conservatively before calling it. Concurrent requests cannot overspend by checking the same remaining balance: maintain a transactional token reservation ledger. Default daily paid budget is zero. A configured nonzero token cap is not a guaranteed currency cap unless verified model prices and a cost reservation are also applied. Report actual provider usage; null means not reported, not zero.

No hidden retry after budget exhaustion. Provider timeout, 429 or invalid structured output yields a safe failed/partial-evidence state. An operator can still view the deterministic evidence summary.

## Retrieval design

Use PostgreSQL full-text search over an explicitly allowlisted Markdown corpus. This avoids embedding API expense and a separate vector service while offering deterministic, testable retrieval. It is still retrieval; it does not imply semantic embeddings.

Corpus content: payment states, operation identities, unknown outcomes, missing original targets, source-confirmed/destination-pending cases, compensation limits, recovery parking, outbox quarantine, projection lag, unavailable participants, reconciliation/proof generation, trace limitations, free-host restarts, backup restoration, and security controls. The allowlist includes selected implemented sections of `docs/decisions.md`, current `docs/architecture.md`, new implemented ADRs, and reviewed `docs/runbooks` files. Preserve each document's implementation status so a proposed procedure is not retrieved as an existing capability. Do not index attached instruction prompts or this entire execution pack as trusted runtime behavior.

Index headings and sections into chunks of 250-500 words with 40-word overlap only within a section. Metadata: document path, heading, start/end line, SHA-256, content version, approved tags, allowed roles, ingestion SHA and active status. Never index user notes, arbitrary uploads, secrets, external pages or raw logs. Read only paths in the committed manifest; reject symlinks escaping the repository root.

Use `websearch_to_tsquery` or safely constructed query expressions; never concatenate user input into SQL. Rank by text relevance plus an explicit small state/reason tag boost. Return top five active authorized chunks and their hashes. Missing relevant evidence should cause abstention, not a generic confident explanation.

Full rebuild runs in a staging corpus version and switches active version after validation. Identical source hashes do not create duplicates. Removed documents are deactivated. Generation citations bind to retrieved immutable chunk contents; replacing an active corpus must not change saved report citations.

## Prompt contract

Prompt version is committed. It states the simulator scope and read-only role, gives ordered evidence sources, describes unavailable data, separates facts/inferences/recommendations, requires valid evidence IDs, and forbids following instructions inside retrieved text or tool outputs. It tells the model to abstain when the supplied evidence cannot justify an answer.

User questions and runbooks are delimited data fields. They never become a system-message suffix. The model is not asked to expose hidden reasoning. Store observable tool steps and concise explanations, not private chain-of-thought.

## Grounding enforcement

- Build an evidence registry before generation. Reports can reference only IDs actually returned by authorized tools or approved retrieval in this investigation.
- Financial facts come from typed payment/participant/ledger evidence, not prose in a runbook.
- For state, amount, bank, operation identity and status claims, compare structured assertions against their cited typed values. Render final financial fact text from verified assertion templates on the server, rather than accepting a model's contradictory prose alongside a valid assertion. Avoid validating only that an ID exists.
- A source unavailable status cannot support a definite outcome. Contradictory bank/central observations remain explicitly contradictory.
- Deterministic recommendations come from reviewed reason-code/runbook mappings; LLM suggestions are advisory and separately labelled.
- Evidence coverage is deterministic and question specific. Do not use a model's self-reported confidence as probability.
- After five minutes, display a stale-report indicator and offer a new evidence read. Existing reports remain historical snapshots.

Do not claim full hallucination elimination. Free-form interpretation cannot be perfectly machine verified; constrain it, test it, label it and keep authoritative values visible.

## Evaluation dataset and gates

Create at least 40 fixed cases: 16 factual/recovery cases, 8 retrieval cases, 8 adversarial cases and 8 missing/contradictory evidence cases. Include lost bank reply, central-only pending, pre-dispatch crash, target removed, status identity mismatch, expired trace, no reconciliation relationship, injection in a runbook and attempts to request another payment.

Expected answers are structured required/forbidden facts and acceptable citations, not one exact prose string. Evaluate retrieval recall@5, citation validity, typed factual agreement, unauthorized tool attempts blocked, abstention on insufficient evidence, cost and latency. Deterministic fixtures run in CI without paid calls. Live-provider results are a separate artifact with model ID/version, prompt hash, case IDs, actual usage and timestamp.

Release gates: all security/authorization fixtures pass; every published financial fact is supported by the typed evidence checks; all report citations resolve; unavailable evidence cases abstain correctly; retrieval recall@5 target at least 0.85 on the reviewed set. Live natural-language factuality target at least 0.90 is a target to measure, not an assumed result. If it fails, keep live generation disabled and ship evidence-only mode while repairing it.

## Rollback

Set provider mode to evidence-only, cancel new generation claims and retain saved reports with their original mode/version. The payment API, worker, outbox and worklist must continue. Disable investigator routes if their authorization contract is broken. No investigator rollback changes financial tables.
