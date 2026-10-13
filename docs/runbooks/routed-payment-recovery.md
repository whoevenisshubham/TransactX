# Routed payment recovery

## Capability status

Stable routed operation identities, request-driven recovery, status lookup, compensation, and central-only recovery are implemented in the current release-candidate source. The v2 continuously scheduled recovery worker, authoritative participant-presence endpoint, recovery parking reason codes, and durable worklist are planned M2 capabilities until their integration evidence is available. Operators must not treat this planned procedure as proof that a worker ran.

## Unknown participant outcome

A timeout, lost reply, unavailable participant, or invalidly correlated reply is an unknown outcome. It is never proof of failure. Preserve the payment and original operation UUID, read the participant status at most once within the evidence deadline, and display the observation time and source status. If the status is still pending or unavailable, keep the payment pending and identify the missing participant evidence.

Do not create a replacement operation UUID. Do not compensate merely because a request timed out. A later recovery attempt uses the durable original identities and the same logical request. Definite participant failure can enter the tested compensation path; definite success can advance the saga. Conflicting evidence is reported rather than guessed away.

## Operation absent before dispatch

The source baseline identifies a crash boundary where a central operation row may commit before its participant request is sent. The old participant status API can report pending for both a missing operation and a real pending operation, so it cannot prove absence. Until M2 supplies the authenticated presence capability and proves participant uniqueness against delayed requests, recovery must park this case as unknown.

The planned presence read returns `present` or `absent` only after an authoritative durable participant lookup. `absent_confirmed` plus the unchanged original operation payload and the shared per-payment serialization boundary permits replay. A timeout, connection error, unsupported adapter, or untrusted model statement cannot establish absence. This is a planned v2 procedure, not an implemented current-branch claim.

## Bank-settled central-pending

`BANK_SETTLED_CENTRAL_PENDING` records that all required participant settlement steps succeeded while the final central settlement transaction did not complete. The implemented central-only recovery path rereads the central payment and completes central ledger/state work without issuing another participant monetary request. Evidence should cite the participant operation successes and the central state separately.

If central evidence is unavailable, do not claim that recovery completed. If the payment is already terminal when reread, report that current state. A bank-settled observation never authorizes an investigator or HTTP GET to mutate central state; only the serialized recovery boundary may do so.

## Missing original execution target

Recovery must use the persisted execution target selected for the payment. A currently healthy alternate target is not the route originally used and cannot be substituted. If the selected target is no longer configured, recovery fails closed and the payment remains pending for operator attention. Health and circuit snapshots can explain current availability but cannot rewrite route history.

Record the persisted target, source and destination bank identities, and the source status that prevented lookup. The planned v2 reason code should remain a stable operational classification; it is not a new financial state. Restoring the original target configuration or deploying a reviewed compatibility adapter is an operational action outside the investigator.

## Compensation boundary

Before source confirmation, a definite destination failure may allow release of the source hold and reversal of provisional destination credit. After the source hold is confirmed, there is no safe generic undo for that bank-side debit; an unresolved destination finalization stays `PENDING_RECONCILIATION`. Compensation itself uses stable operation identities and must be observed, not assumed.

The investigator may recommend reviewing this runbook and the cited operation sequence. It cannot invoke compensation, mark a payment failed, or turn an unknown result into a definite failure. The central state machine remains authoritative even when a recommendation appears obvious.
