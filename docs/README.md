# Documentation

## Start here

- [Product overview and live demo](../README.md)
- [Architecture](architecture.md) and [design decisions](decisions.md)
- [Local development](runbooks/local-development.md)
- [Hosted deployment](../deploy/render/README.md)
- [Current delivery evidence and limitations](next-gen/DELIVERY_REPORT.md)
- [Contribution guidelines](../CONTRIBUTING.md)

## Interfaces and operations

- [Customer accounts](api/customer-accounts.md), [operational APIs](api/operations.md)
  and [shared contracts](api/shared-contracts.md)
- [Canonical JSON schema and test fixtures](api/contracts/v2.schema.json)
- [Google sign-in](runbooks/google-sign-in.md), [Gemini configuration](runbooks/gemini-zero-spend.md)
  and [guarded demo enrichment](runbooks/demo-enrichment.md)
- [Native deployment and backup design](deployment/native-deployment-and-backup.md)
  and [restore rehearsal](../deploy/RESTORE_REHEARSAL.md)

## Engineering reference

- [Payment kernel](reference/payment-kernel.md)
- [Distributed architecture](reference/distributed-architecture.md),
  [events](reference/event-architecture.md) and [frontend](reference/frontend-architecture.md)
- [Investigation](reference/investigation-architecture.md),
  [analytics](reference/analytics-and-ml.md) and [observability](reference/observability-and-reliability.md)
- [Security and threat model](reference/security-and-threat-model.md),
  [risks](reference/risk-register.md) and [technology decisions](reference/technology-decisions.md)
- [Testing and benchmarks](reference/testing-and-benchmarks.md),
  [reconciliation experiments](reconciliation-experiments.md) and [resilience experiments](resilience-experiments.md)

Design references retain their original specification context; the delivery report
distinguishes implementation, measured evidence and deferred operational claims.
Historical evidence remains under `next-gen/state/` and the
[backend release report](reference/backend-release-report.md). Historical source-time
paths in those records are not current setup instructions. Redundant execution
prompts and planning copies are not required to build or run this repository.
