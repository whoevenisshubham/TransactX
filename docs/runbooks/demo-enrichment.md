# Bounded synthetic demo enrichment

`backend/cmd/demo-enrich` is a dry-run-first client for the existing initialized
Neon demo. It never launches services, reseeds a populated database, mints funds
or writes financial SQL. The historical missing-transition dataset is not an
eligible target. Local/protected-source databases, transaction poolers, mismatched
endpoints and databases without the original demo capacity trigger are rejected.
The existing acknowledged Neon database `neondb` is supported without renaming
or recreating it. Its name is not sufficient authorization: exact host, database
and owner-reviewed project acknowledgement plus the installed demo capacity
function/trigger must all match. Other accepted database names contain `demo`;
the protected source name `transactx` and administrative `postgres` remain rejected.

## Review the target first

In the [Neon Console](https://console.neon.tech), open the existing demo project:
**Settings > General** shows the permanent **Project ID** (not the display name).
Select its existing database branch, then **Postgres database > Computes** to
find the primary read-write compute's **Endpoint ID** (`ep-...`). Open **Connect**,
select `neondb`, turn connection pooling off, and copy only the full direct
hostname (`ep-....neon.tech`), not the password or connection URI. Confirm that
this project/branch is the initialized synthetic public demo. The private URI
already supplied is reused and cross-checked against these independent values;
no project/database creation, reset, rename or history repair is needed.
[Project settings](https://neon.com/docs/manage/projects#general-project-settings),
[compute/endpoint details](https://neon.com/docs/manage/computes#view-a-compute).


The owner supplies a private direct `DATABASE_URL`, exact expected database and
Neon endpoint, owner-verified project ID, actual HTTPS API origin and reviewed
tool source SHA. VCS build metadata (or the current Git checkout for `go run`)
must match that SHA; apply also requires a clean source. Build the installed
client from the reviewed Git source with VCS metadata. The project label records
owner acknowledgement; a hostname
alone does not prove Neon project ownership. The command verifies the connected
database, original capacity guard and API demo identities/payment results against
that database. No credential is passed on a command line or printed.

Run from `backend/`, with `DATABASE_URL` loaded privately:

```powershell
go run ./cmd/demo-enrich --expected-db=DEMO_DATABASE --expected-host=DIRECT_NEON_HOST --expected-project=REVIEWED_PROJECT --api-origin=https://transactx-demo-api.onrender.com --source-sha=FULL_REVIEWED_TOOL_COMMIT
```

Default dry-run reads only PostgreSQL; it does not log in, invoke an API mutation
or create a receipt file. It prints the twelve planned entries, original payment
IDs/states when present, current daily/lifetime consumption and capacity decision.
It uses the **existing** demo trigger's fifty/day and thousand/lifetime limits,
retaining at least ten new-payment slots. Changed capacity policy requires review.

## Explicit apply and interruption

After reviewing the real target, add `--apply`, the exact acknowledgement
`--ack="ENRICH SYNTHETIC DEMO REVIEWED_PROJECT/DEMO_DATABASE"` and an absolute
`--receipt-file=PRIVATE_PATH_OUTSIDE_GIT`. The append-only receipt contains
synthetic manifest IDs, tool source SHA, actual outcomes and checks; never JWTs.
The tool SHA is **not** an attestation of the deployed backend's source.

The fixed v1 manifest makes twelve small real API payments (total 1,530 paise),
including Alice→Bob, Bob→Store and Alice→Store. It starts with twelve rather than
exhausting a 24–30-record ceiling. Funding/account ownership must already be
ready; no funding endpoint or SQL balance adjustment is used. Four-second pacing
respects normal write admission. A rate limit stops with Retry-After instead of
retrying blindly. Unexpected auth/money/API errors stop the command.

Each project/database/manifest entry has a deterministic user-scoped idempotency
key. Rerun the same command to inspect/resume missing entries; source SHA changes
do not generate replacement keys. Completed entries are read, not resubmitted.
An unresolved/reserved original operation stops the run. An ambiguous transport
outcome is not recorded as failure or retried with a new key. No reset, truncate,
trigger disabling, fabricated historical dates or persistent fault is involved.

Enrichment clients share an advisory lock. Capacity is rechecked before each
new payment and at the end; normal visitor traffic can cause a safe early stop.
The tool does not reserve or block visitor traffic. It reports snapshot limits
honestly if concurrent activity prevents final convergence/capacity acceptance.

## Independent verification and remaining live gates

Read-only SQL checks central and participant conservation, per-account mirror
balances, exact two-entry debit/credit amounts/ownership and observed participant
operation multiplicity for these manifest payments. All SQL errors fail checks;
missing ledger evidence cannot pass. A mismatch is a pending investigation, not
permission to alter financial records. Script/unit SQL validation is not proof
that a real run or native recovery worked.

On 19 October 2026, the owner-confirmed existing synthetic `neondb` passed the
target/capacity dry-run. The clean `cafae17` tool then completed all twelve v1
manifest payments (1,530 paise) against the deployed `cafae17` API. Its final
checked financial verification passed, leaving 37 daily payment slots. Private
receipts retain the original identities and observed outcomes outside Git.
Those counts describe that run, not current capacity. A future invocation must
repeat the target/capacity checks and inspect the same original manifest keys;
this result does not authorize a new manifest, reseeding or history changes.
Up to four saved investigations remain a separate integration step after these
payments: confirm the deployed investigator mode/quota first, use ordinary
operator APIs and stable report request keys, and retain actual report provenance.
This command intentionally does not start provider-backed jobs implicitly.

Synthetic traffic is not a healthy ML calibration attestation. Rare/failure
stories stay in `/showcase`, visibly separate from live observations. Deployment,
automatic projection, real provider and browser acceptance are separate gates.
The earlier native API launch rejection (`blocked by policy`) remains explicit;
this command does not authorize an alternate launcher.
