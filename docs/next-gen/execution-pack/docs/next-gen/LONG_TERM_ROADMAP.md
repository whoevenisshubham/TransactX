# Long term expansion after the measured release

These are options, not immediate tasks. Finish and evaluate the preceding release first.

| Option | Concrete benefit | Technical route | Difficulty and entry condition |
| --- | --- | --- | --- |
| Independent participant databases | Stronger ownership and failure isolation | Separate DSNs/migrations, service-only read interfaces, remove cross-schema simulation shortcuts | High; first inventory provisioning/reconciliation exceptions |
| Durable shared routing control | Multiple API instances with consistent circuit/chaos observations | Explicit shared state/leader or versioned control snapshots; test replica races | High; required before claiming API horizontal scale |
| External broker | More independent consumers and sustained event volume | Outbox relay to chosen broker; preserve event IDs, schema versions, inbox and ordering policy | High; measured PostgreSQL relay bottleneck or consumer need |
| Container and Kubernetes deployment | Repeatable platform rollout and operational education | OCI images, nonroot pods, probes, resources, NetworkPolicy, Secrets, migration job and Helm | High; user first approves containers and a free-capable cluster path |
| Semantic/hybrid RAG | Better retrieval on paraphrased operational questions | Evaluate local embeddings/pgvector against lexical baseline; same approved corpus/citations | Medium; retrieval eval shows lexical limitations |
| Multi-entity incident investigator | Correlate several payments/windows safely | Scoped incident IDs, bounded evidence graph, reviewed aggregation and tool policies | Medium-high; selected-payment investigator passes grounding gates |
| Tool recommendations with approval | Safer operator automation | Deterministic action proposal, explicit approval, RBAC, audit, policy checks and dry-run | High; never permit model financial mutation |
| Temporal workflow evaluation | Reduce custom scheduler complexity at larger scope | Compare replay/state semantics and operating cost to existing worker | High; proven maintenance burden justifies migration |
| Continuous PITR and disaster recovery | Lower measured recovery loss/window | WAL archival, monitored restore points, off-account storage and regular rehearsal | High; available storage budget and ownership |
| Multi-tenant sandbox | Safe realistic demo/developer product | Tenant-scoped auth, unique keys, resource quotas and data isolation through every table/tool | Very high; cannot be added by sprinkling tenantId on DTOs |
| Developer API/webhook product | Useful integrations beyond the UI | Scoped API keys, signed webhook events, retry/inbox semantics, sandbox docs and CLI | Medium-high; event delivery and abuse controls already proven |
| Advanced ML forecasting | Capacity/latency planning from genuine history | Longer dataset, time-based evaluation, forecast intervals, monitored model fallback | High; enough representative data exists |
| Deep learning | Only a problem requiring richer signal warrants it | Evaluate against classical baseline with a defensible dataset | Very high; no current justification |
| Regional failover research | Explore RPO/latency and authority constraints | Explicit single-writer ownership, replication, fencing and failure tests | Very high; no free-demo claim of production multi-region safety |

Each option requires its own ADR, cost/maintenance analysis, migration/rollback and measured acceptance. Difficulty reflects system risk and integration, not merely how quickly an agent writes files.
