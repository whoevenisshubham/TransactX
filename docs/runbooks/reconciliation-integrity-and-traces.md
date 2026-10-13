# Reconciliation, integrity, and trace evidence

## Capability status

Read-only reconciliation, maintained Merkle commitments, proof verification, runtime financial integrity checks, and authenticated operational trace-like activity surfaces exist in the release candidate. The v2 bounded trace store and combined payment evidence API are planned until M2 provides live integration. Every report must preserve the distinction between a current source observation and a procedure described here.

## Reconciliation interpretation

A reconciliation run compares a closed participant/time scope using maintained central and participant commitments. Equal roots can avoid record inspection. Divergent roots lead to bounded bucket and record comparison. A run is relevant to a selected payment only when its participant and scope genuinely relate to the payment; temporal overlap alone does not prove the operation was inspected.

A mismatch is evidence of disagreement within the run scope, not automatic proof of which side is correct. An equal root for a related scope supports equality of the canonical records represented by that commitment generation. It does not prove unrelated payment state, availability of a later operation, or absence of all system defects. Missing or stale maintained commitments are an explicit unavailable condition; reads never rebuild them as a fallback.

## Integrity interpretation

Runtime integrity checks observe debit-credit conservation, nonnegative balances, transaction uniqueness, idempotency mappings, valid payment state, completed-payment ledger completeness, and maintained commitment consistency. They do not repair data. `PASS` means the bounded check held for its recorded scope and observation time. `FAIL` records a detected invariant violation. `ERROR` is an operational failure and must never be rendered as pass.

Some integrity results are global context. A global result with zero violations does not prove the outcome of one selected payment, and it cannot replace missing participant operation evidence. Historical payments may lack append-only transition history from before the transition migration; their current state can be checked without claiming retroactive history validity.

## Trace interpretation

Traces describe diagnostic execution context and are not financial authority. A trace may show that a request reached a service, timed out, or emitted a bounded span, but it cannot by itself prove participant settlement or central commit. Trace IDs must already be present in the selected evidence, use the allowlisted format, and return only safe span names and attributes.

A missing, expired, truncated, or disabled trace is `not_collected` or otherwise unavailable, not a failed payment. The report should cite the typed operation and state evidence first and list trace absence as missing diagnostic context. Never expose raw logs, request bodies, tokens, customer details, SQL, or host secrets as a substitute.

## Conflicting evidence

When a participant reports success while central state remains pending, publish both observations with their identities and times. Mark the report fact as contradictory where appropriate and use `BANK_SETTLED_CENTRAL_PENDING` only when the central typed state actually has that value. When central state is pending and the participant is unavailable, abstain from a participant outcome.

Reconciliation, integrity, and trace context may narrow a hypothesis or guide an operator to the correct next read. They never authorize mutation. A fresh evidence read after the report staleness interval is preferable to presenting an old snapshot as current.
