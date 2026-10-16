# Isolated backup and restore rehearsal

This procedure checks the backup and restore scripts against a disposable
database. The initial rehearsal did not include worker continuation or
projection rebuild; the later M1+M2 partial result is recorded below. Neither
run establishes cloud persistence or a production recovery time objective.

## Isolation and inputs

Use a separate PostgreSQL 18 cluster bound to `127.0.0.1` on a nondefault port.
Create a new `transactx` database and an owner role only in that cluster. Apply
the repository's existing baseline migrations in number order, then insert
synthetic records covering one payment, two central accounts, a balanced pair
of ledger entries, one idempotency record and one operation in each bank schema.
Do not use an existing financial database or real account data.

The current baseline migration `000013_m3_5_recon_runs.up.sql` has a UTF-8 BOM
that direct `psql -f` rejects. For the initial rehearsal, a disposable copy had
the BOM removed; the tracked migration was left unchanged. M2's migration CLI
subsequently applied and verified this exact input from an empty disposable
database on 13 October 2026.

Generate a fresh age identity with `age-keygen` outside the backup directory.
The private identity and PostgreSQL passfile must be owner-only (mode 0400 or 0600).
The restore checker resolves the identity to its canonical path via `realpath -e` (ensuring
symbolic links cannot bypass inspection), verifies it is a regular file, and enforces that
the private identity excludes group and other read/write/execute permissions
(`find "$identity_path" -perm /077 -print` must be empty) before target database creation
or decryption begins. Missing files or permission-inspection failures fail closed.
Setting `umask 077` inside `restore-check.sh` establishes the file-creation mask for local
files created directly by the shell process (such as the JSON report and temporary files);
it does not govern database files created by the PostgreSQL server process in `PGDATA` (which
are controlled by the server daemon's umask), nor does it alter the inode mode bits of an
already existing identity file. True POSIX inode permission enforcement by the OS kernel
and filesystem remains an unverified runtime requirement on non-Linux hosts (e.g. Windows
NTFS mounts). Do not claim that `umask 077` secures PostgreSQL's underlying database files.
Do not copy either secret into the release archive or commit it. Use a local backup directory
with sufficient free space. A mounted off-VM target needs a separate Linux validation before
claiming independent persistence.

## Exercise the scripts

Set `PGHOST`, `PGPORT`, `PGUSER`, `PGDATABASE=transactx`, `PGPASSFILE`,
`BACKUP_DIR`, `BACKUP_AGE_RECIPIENT` and `BACKUP_MAX_BYTES` for the disposable
cluster. Run `deploy/scripts/backup.sh`; retain its encrypted `.dump.age` and
matching `.globals.sql.age` plus both SHA-256 sidecars privately. Use the hash
of the encrypted dump with `restore-check.sh --validate-only`, then run the
checker with a new `--target-db transactx_restore_<unique>` and a private
absolute `--report-dir`. Pass the exact source commit to `--source-sha`, the
private identity path to `--identity`, and an explicit completeness contract via
`--expected-counts deploy/verification/acceptance-expected-counts.json` (or
`--allow-empty` for an unpopulated schema-only restore). The checker creates a
fresh database; it never drops or overwrites one. Omitting both flags causes
the checker to fail closed with exit code 2.

The completeness check enforces that a populated acceptance manifest must explicitly cover
all five core application tables: `public.payments`, `public.accounts`, `bank_a.operations`,
`bank_b.operations`, and `public.idempotency_records`. For each table, an explicit assertion
is required: either an exact count (`payments`, `accounts`, `bankAOperations`, `bankBOperations`,
`idempotencyRecords`) or a minimum bound (`minPayments`, `minAccounts`, `minBankAOperations`,
`minBankBOperations`, `minIdempotencyRecords`). Specifying both exact and minimum bounds for
the same table, omitting any of the five tables, providing unknown keys, specifying recognized
table assertions at both the top level and inside an embedded `counts` object (ambiguous manifest),
providing non-object `counts` properties (such as strings, arrays, or `null`), or specifying
non-integer, negative, fractional, `null`, boolean, string counts, or integers exceeding JavaScript's
safe integer range (`Number.MAX_SAFE_INTEGER`, 9007199254740991) strictly fails validation (exit code 2).
Both top-level keys and embedded `counts: { ... }` object shapes are supported.

Furthermore, a populated acceptance manifest must not permit an entirely empty database. If all five
core table assertions specify exact zeros or minimum bounds of zero (requiring zero total rows across
all five tables), the manifest is rejected fail-closed during preflight with exit code 2
(`Invalid populated acceptance manifest: assertions permit an entirely empty database. At least one table must require > 0 rows (use --allow-empty for empty-state verification)`).
During post-restore verification, the script also verifies that at least one row exists across the
declared five-table scope (`totalActual > 0`); observing zero total rows fails with exit code 1.

The bundled `deploy/verification/acceptance-expected-counts.json` is a fixed fixture manifest
specifically scoped to validate the single-payment rehearsal fixture in this document; its row-count
scope covers strictly the five core application tables: `public.payments`, `public.accounts`,
`bank_a.operations`, `bank_b.operations`, and `public.idempotency_records`. It does not validate
row counts for any other tables in the database, including `public.ledgers`, `public.ledger_entries`,
`public.ledger_transactions`, `payment_state_transitions`, `outbox_events`, `outbox_deliveries`,
`inbox_records`, `recon_runs`, participant accounts (`bank_a.accounts`, `bank_b.accounts`), or
migration ledgers. While `public.ledger_transactions` and `public.ledger_entries` participate in
financial-invariant queries (verifying two-legged balanced debits and credits per transaction),
and `public.accounts`, `bank_a.accounts`, and `bank_b.accounts` participate in non-negative balance
invariant queries, their total row counts are not asserted by the acceptance manifest. Other tables,
such as `payment_state_transitions`, are neither count-verified nor checked by invariant queries in
this restore checker.

Report semantics distinguish between schema/invariant verification, population contract satisfaction,
empty-state core checks, and populated data verification:
- For a validated populated restore manifest, the `tx.restore.v1` evidence report records
  `checks.populationContractSatisfied: true`, `checks.populatedDataVerified: true`,
  `checks.completenessVerified: true` (retained for backward compatibility; does not equate 5-table
  assertions with database-wide completeness), `checks.coreTablesEmptyVerified: false`,
  `emptyStateScope: "none"`, `emptyStateAllowed: false`, and `populationContract: "manifest:<filename>"`.
- When `--allow-empty` is selected, it executes an explicit fail-closed check verifying that
  the declared empty-state scope (`public.payments`, `public.accounts`, `bank_a.operations`,
  `bank_b.operations`, `public.idempotency_records`) contains strictly zero rows. If any checked
  table has nonzero rows, the restore check immediately fails with exit code 1. The resulting
  evidence report records `checks.populationContractSatisfied: true`, `checks.coreTablesEmptyVerified: true`,
  `emptyStateScope: "public.payments,public.accounts,bank_a.operations,bank_b.operations,public.idempotency_records"`,
  `checks.populatedDataVerified: false`, `checks.completenessVerified: false`, `emptyStateAllowed: true`,
  and `populationContract: "allow_empty"`. Structural schema checks (`threeSchemas`, `nonnegativeBalances`,
  `balancedCentralLedger`, `noOrphanIdempotency`) verify schema and balance conservation. This empty-state
  verification is strictly bounded to the declared core tables and does not imply that uncounted tables
  across the entire database are empty.

Furthermore, passing a static or stubbed offline test harness confirms script branching and error
handling, but does not prove that a real encrypted archive restores successfully.

If an operational scenario requires comprehensive database completeness across all
tables, authoritative snapshot table inventories or dump table-of-contents catalog
manifests would be required.

Check a deliberately tiny `BACKUP_MAX_BYTES`, a wrong backup hash, an identity with
group/other permissions (e.g. 0644), missing completeness contract, a count mismatch,
a count below minimum, the live `transactx` target name, and an already existing
restore target. Each must fail. Also check that an incomplete release archive cannot be
installed. Keep the disposable cluster and encrypted files until the report is reviewed;
stop the cluster using its own `pg_ctl -D <disposable-data-dir> stop` command.

## Measured fixture result

The local Windows rehearsal on 11 October 2026 used PostgreSQL 18.6 and age
1.3.2. The clean-head encrypted snapshot restored into a new database in 4.12 seconds
wall time. The report found three schemas, one synthetic payment, two central
accounts, one operation per bank, one idempotency record, nonnegative balances,
a balanced central ledger and no orphan idempotency reference. The backup was
stored locally only. No writes occurred after the snapshot, so this rehearsal
does not measure an operational RPO. Its safe report and measured limitations
are in `docs/next-gen/state/restore-rehearsal-local.json` and `M1.md`.

Real process-crash continuation, worker-driven projection, a real financial
integrity run, a mounted off-VM copy and the online restart sequence remain
pending. Rehearse them on the combined M2/M3 implementation before marking A7
complete.

## M1+M2 partial repeat on 13 October 2026

A clean M1+M2 review checkout applied and verified 28 migrations on a fresh
PostgreSQL 18.6 cluster. The age-encrypted backup restored into a new database
with the checker passing its schema, balance, ledger and idempotency checks.
A separate native worker process then completed one manually seeded VALIDATING
payment in the restored database. Independent SQL found one net-zero central
ledger transaction and four distinct Bank A operation IDs. The rebuild CLI
repaired the worklist projection from 37/36 to 37/37 records.

The [supplemental safe report](../docs/next-gen/state/restore-rehearsal-m1m2.json)
records hashes, counts and limits. The seeded payment does not prove a native
process crash, and the M2 worker did not itself run the outbox projector. The
backup was local only; M3 integration, off-VM copy, RPO and online restart
remain unverified.

## Combined canonical-schema repeat on 13 October 2026

Clean isolated review `e3b1abc` ran both scripts on a fresh PostgreSQL 18.6
cluster with generated local credentials and an age identity. All 29 canonical
migrations, the active 35-chunk corpus and a completed evidence-only job were
backed up and restored. Restored migration checksums verified; corpus and job
payload fingerprints matched the source. One outbox event and delivery were
preserved; the inbox and worklist were empty. Safe evidence and encrypted
artifact hashes are in `docs/next-gen/state/restore-rehearsal-combined-m1.json`.

This dataset had no settled ledger or participant operations. The completed job
came from the in-process HTTP test. Native worker continuation, projection
rebuild, crash recovery, off-VM persistence and global-role restoration remain
required. The private rehearsal files are outside the repository; the isolated
cluster is stopped.

## Current native rehearsal

`docs/next-gen/state/native-crash-restore-final-m1.json` supersedes the earlier
absence of native continuation for its tested same-bank boundary. A real API
was killed after its journal committed. Actual age-encrypted backup/isolated
restore preserved that pending state, canonical migration checksums and the
active 35-chunk corpus. A restored native worker was killed during its running
lease; replacement advanced epoch 1→2 and settled the original payment with
checked central/participant financial invariants and automatic outbox projection.
Manual rebuild was checked only after automatic delivery passed.

Restore checker phase: 3 seconds. Separate restored startup/takeover/convergence
phase: about 11.1 seconds. These are local measurements, not a hosted RTO
guarantee or operational RPO. The quiesced snapshot retained the checked facts;
off-machine durability, production global-role restoration and scheduling RPO
remain pending. Keys/dumps remain private and all probe processes were stopped.
The separate cross-bank reply-loss proof did not repeat encrypted restore;
do not widen this rehearsal's scope to that snapshot or every crash boundary.
