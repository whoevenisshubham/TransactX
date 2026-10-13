# Delivery architecture

## v2 operations and delivery boundary

Combined delivery acceptance is recorded in [the delivery report](DELIVERY_REPORT.md).
The diagram describes the current mounted components, not a completed release.

```mermaid
flowchart LR
    Browser[Customer and operator browser] -->|JWT / HTTP| API[Native Go API]
    API -->|payment authority and stable operation journal| Central[(Central PostgreSQL)]
    API -->|participant HTTP| A[Native Bank A]
    API -->|participant HTTP| B[Native Bank B]
    A --> BA[(bank_a accounts / operations / ledger)]
    B --> BB[(bank_b accounts / operations / ledger)]
    Worker[Native Go worker] -->|lease and epoch fenced recovery| Central
    Worker -->|original operation identities| A
    Worker -->|original operation identities| B
    Central -->|transactional outbox| Projection[Idempotent worklist projector]
    Worker --> Projection
    Projection --> Central
    API -->|SSE invalidation hints| Browser
    Browser -->|authoritative reread| API
    Worker --> Analytics[Durable completed analytics windows]
    Analytics --> Baseline[Statistical baseline / experimental optional model]
    Baseline -->|persisted advisory observations| Central
    Worker --> Investigator[Single evaluated M3 investigator]
    API --> Operations[Trusted scoped operations evidence reader]
    Investigator -->|trusted read-only evidence| Operations
    Operations -->|read facts| Central
    Operations -->|read participant observations| A
    Operations -->|read participant observations| B
    Investigator --> Corpus[Versioned approved corpus]
    Corpus --> Central
    Investigator -->|durable jobs / retained snapshots / budgets| Central
```

Central settlement remains authoritative; each participant owns its effects.
An unknown bank outcome remains unresolved until correlated status/presence
evidence supports safe continuation. Outbox delivery is at least once;
projection is idempotent and may lag. SSE is a hint, never financial truth.
Investigator and anomaly results have no mutation edge into the payment kernel.
Providers stay disabled, baseline remains default and IsolationForest experimental.

JWT roles gate operator reads and administrator controls. Native Linux deployment
uses systemd and Caddy with private bank/database ports. There is no OAuth/IdP,
Kubernetes or paid-provider prerequisite. Recorded native proofs cover named
process/fault boundaries; broader crash-harness and calibration provenance
requirements remain open in the delivery report.
