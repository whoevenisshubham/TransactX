# Sources and adapted engineering ideas

Sources checked 11 October 2026. The repository is implementation truth; the handoff is historical context. All new architecture/task choices are recommendations in this pack, not claims that cited companies use the same exact design.

## Project sources

- [Canonical repository](https://github.com/whoevenisshubham/TransactX).
- [Planning commit](https://github.com/whoevenisshubham/TransactX/commit/2b9f6e6702d2db54651d71859f29d6cbc3eb90fc).
- [Current architecture](https://github.com/whoevenisshubham/TransactX/blob/2b9f6e6702d2db54651d71859f29d6cbc3eb90fc/docs/architecture.md), [decisions](https://github.com/whoevenisshubham/TransactX/blob/2b9f6e6702d2db54651d71859f29d6cbc3eb90fc/docs/decisions.md), [resilience evidence](https://github.com/whoevenisshubham/TransactX/blob/2b9f6e6702d2db54651d71859f29d6cbc3eb90fc/docs/resilience-experiments.md), [reconciliation evidence](https://github.com/whoevenisshubham/TransactX/blob/2b9f6e6702d2db54651d71859f29d6cbc3eb90fc/docs/reconciliation-experiments.md).
- User-supplied 30 September handoff and Cohesity FY27 DSO internship job description. Their content informed continuity and career mapping; employer requirements are not substituted for architectural justification.

## Adapted patterns

| Source | Real idea | Adaptation and deliberate difference |
| --- | --- | --- |
| [Stripe idempotent requests](https://docs.stripe.com/api/idempotent_requests) | Stable request identity and safe repeated requests | Preserve TransactX's user-scoped key and immutable monetary operation UUID; do not copy Stripe's retention/business rules blindly |
| [AWS transactional outbox guidance](https://docs.aws.amazon.com/prescriptive-guidance/latest/cloud-design-patterns/transactional-outbox.html) | Avoid dual-write loss and tolerate duplicates | Use existing PostgreSQL storage and a current-state worklist consumer; no external broker at demo scale |
| [PostgreSQL SELECT locking](https://www.postgresql.org/docs/current/sql-select.html) and [explicit locking](https://www.postgresql.org/docs/current/explicit-locking.html) | Concurrent queue claims and advisory lock behavior | Short scheduling claims plus separate shared payment serialization; claims are not money locks |
| [PostgreSQL sequence semantics](https://www.postgresql.org/docs/current/functions-sequence.html) | Sequence allocation is not transactional/gapless | Avoid global sequence-watermark-only dispatch; use durable delivery status and event IDs |
| [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/) and [exporters](https://opentelemetry.io/docs/languages/go/exporters/) | Standard instrumentation and export | Instrument all processes with bounded storage and redaction; telemetry never decides financial truth |
| [OpenAI function calling](https://developers.openai.com/api/docs/guides/function-calling) and [structured output](https://developers.openai.com/api/docs/guides/structured-outputs) | Typed tool calls and report formats | Server-authorized read registry plus deterministic grounding checks; schema shape alone cannot establish truth |
| [OpenAI model selection](https://developers.openai.com/api/docs/guides/model-selection) | Match efficient/strong models to workload | Luna for bounded routine work, Sol for core coding; recommendations depend on actual available models and checks |
| [OpenAI deployment checklist](https://developers.openai.com/api/docs/guides/deployment-checklist) | Deliberate effort, context and usage choices | Compact task context and bounded escalation; no claim of exact Work credit savings |
| [ChatGPT Work introduction](https://learn.chatgpt.com/docs/get-started-with-work) | Delegate substantial work with accessible files/tools | Starter prompts provide scope and checkpoints; account/tool availability still constrains execution |
| [IsolationForest documentation](https://scikit-learn.org/stable/modules/generated/sklearn.ensemble.IsolationForest.html) | Small unsupervised operational anomaly baseline | Evaluate synthetic scenario windows and export native scoring; never equate it to real fraud detection |
| [Cohesity DataProtect](https://www.cohesity.com/platform/dataprotect/) | Data resilience and recovery as product outcomes | Prove backup restore, integrity and evidence visibility in this simulator; do not claim Cohesity-equivalent features/scale |

## Hosting sources

- [Vercel Hobby](https://vercel.com/docs/plans/hobby): free personal/non-commercial tier conditions. Verify limits at deployment.
- [Vite on Vercel](https://vercel.com/docs/frameworks/frontend/vite): compatible static build path; preserve the Go backend architecture.
- [Oracle Always Free](https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier_topic-Always_Free_Resources.htm): free eligibility, current A1 resources, capacity and idle-reclamation caveats; backup storage must stay within actual quotas.
- [Cloudflare React Pages](https://developers.cloudflare.com/pages/framework-guides/deploy-a-react-site/): alternate static React deployment path.
- [Duck DNS](https://www.duckdns.org/about.jsp): free hostname option; owner creates/controls it.
- [Caddy automatic HTTPS](https://caddyserver.com/docs/automatic-https): certificate and HTTPS behavior; verify real DNS and issued trust.
- [Render free deployment](https://render.com/docs/free): sleeping compute and expiring free PostgreSQL make it unsuitable as the guaranteed long-term full stack.

No perpetual pricing/availability guarantee is inferred from these pages. The plan chooses a maintainable zero-budget default and records external deployment/provider prerequisites.
