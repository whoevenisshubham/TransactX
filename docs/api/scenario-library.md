# Illustrative scenario library

`/showcase` is a public, read-only learning surface, separate from authenticated
live workspaces. A persistent source label and the fixed scenario date
(`2026-09-20`, UTC) distinguish it from collected network observations.

The versioned `frontend/src/showcase/scenarios.v1.json` contains 192 deterministic
synthetic payment records: 24 variations for each of eight stories. Stories cover
normal settlement, a lost reply, worker lease takeover, participant settlement
ahead of central completion, outbox lag, a scoped reconciliation mismatch,
account-owned offline replay and missing-evidence abstention. Payment rows reuse
the frontend's payment contract and shared amount/status presentation. Each
record has a scripted, chronological observation trail. Unknown outcomes remain
pending, and only completed examples have a completion timestamp.

The investigation paragraphs are labelled prewritten examples, never provider
results. The lease-takeover story is not native process-crash evidence, and the
integrity story is not a computed or live cryptographic proof.

The route makes no API requests, even when a session exists. It clears inherited
URL context, never sends example IDs to financial APIs, grants no role and
exposes no mutation controls. It does not replace failed live requests, populate
analytics/calibration or touch any database. Opening the live demo starts a
separate normal authenticated page load.

Regenerate the static JSON from `frontend/` with
`node scripts/generate-showcase.mjs`. The generator performs no network or
database operations. Its fixtures are covered by catalogue invariants and
browser checks for source isolation, pagination, evidence links and accessibility.
The catalogue and its styles are loaded in the deferred showcase module.
