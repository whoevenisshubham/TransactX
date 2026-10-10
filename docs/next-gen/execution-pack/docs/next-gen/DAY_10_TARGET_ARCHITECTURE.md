# Ten day target and release acceptance

This is the target for the next controlled release, not a statement that these features exist today. Refer to V2_ARCHITECTURE for flows, API_CONTRACTS for DTOs, and the manuals for implementation.

## Target system

| Layer | Concrete target | Demonstration and gate |
| --- | --- | --- |
| Financial core | Existing integer-ledger/idempotency/saga model, unified safe resumer | Concurrent request/worker and genuine reply-loss tests conserve money |
| Participants | Original Bank A/B processes plus private authenticated presence read | Pre-dispatch crash and delayed original request converge on one original operation |
| Recovery | Continuous native worker, durable jobs/attempts, claims, backoff, attention states | API/worker kill and restart needs no customer resubmission for supported recovery |
| Events | Atomic payment/recovery outbox, delivery leases, inbox, latest-state projection | Ack loss, duplicates, late commit and rebuild produce no lost/regressed worklist |
| Operator product | Worklist, journey, attempts, trace context, citations and evidence export | Real browser end-to-end workflow with partial/stale sources |
| Observability | OpenTelemetry propagation, logs, bounded spans and metrics | Four-process correlated trace; exporter outage leaves payments safe |
| Investigator | Authorized read tools, approved retrieval, validated reports, optional generator | Deterministic/adversarial evals pass; real-provider verification separately recorded |
| Analytics | Durable windows, statistical baseline, evaluated anomaly model | Leakage-safe held-out comparison and native Python/Go parity |
| Delivery | Vercel/static frontend plus native free VM bundle, private services, TLS | Actual online smoke or precise external-access blocker |
| Restore | Consistent whole-DB backup and isolated rehearsal | Integrity/idempotency/participant/worker continuation verified after restore |

## Required versus conditional evidence

P0 code and correctness gates are mandatory for a stable release. P1 capabilities remain part of the full intended release; agents continue them after the first stable candidate if resources/time require. Live cloud deployment needs an available free host and connected accounts. Live LLM generation needs a real configured model and permitted budget; evidence-only mode remains the supported zero-cost behavior.

No feature is recorded as live simply because its adapter compiles or a mock returns a response. No claim of Kubernetes, Kafka, deep learning, real banking, multi-region availability or independent physical databases belongs to this target.

## Five decisive acceptance scenarios

1. Lost participant reply: bank commits, response is lost, API dies, worker resolves original operation and finishes safely. Original UUID/payload and balanced ledger are inspected.
2. Unsent original operation: central journal commits, process dies before dispatch, confirmed absence plus proven participant idempotency permits original replay. Delayed-call race creates one effect.
3. Event acknowledgement loss: consumer commits then relay dies; duplicate delivery is absorbed and current payment remains reflected correctly.
4. Evidence investigation: viewer sees partial sources, asks why a payment is pending, receives a mode-labelled report with resolvable citations and no financial mutation.
5. Restore/rebuild: isolated DB restore plus derived projection rebuild preserves existing payment and operation identities and safe continuation.

## Finish criteria

All required tests pass on the exact reported clean source SHA. New DTOs are compatible across the three lanes. Browser and deployment statuses are recorded honestly. The release archive and runbook let an unfamiliar reviewer reproduce the system. Resume claims are selected from measured features only.
