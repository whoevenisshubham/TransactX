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
Keep the private identity and PostgreSQL passfile owner-only. On Windows, use a
private NTFS DACL; Git Bash's POSIX mode display is affected by `umask`. Do not
copy either secret into the release archive or commit it. Use a local backup
directory with sufficient free space. A mounted off-VM target needs a separate
Linux validation before claiming independent persistence.

## Exercise the scripts

Set `PGHOST`, `PGPORT`, `PGUSER`, `PGDATABASE=transactx`, `PGPASSFILE`,
`BACKUP_DIR`, `BACKUP_AGE_RECIPIENT` and `BACKUP_MAX_BYTES` for the disposable
cluster. Run `deploy/scripts/backup.sh`; retain its encrypted `.dump.age` and
matching `.globals.sql.age` plus both SHA-256 sidecars privately. Use the hash
of the encrypted dump with `restore-check.sh --validate-only`, then run the
checker with a new `--target-db transactx_restore_<unique>` and a private
absolute `--report-dir`. Pass the exact source commit to `--source-sha` and the
private identity path to `--identity`. The checker creates a fresh database;
it never drops or overwrites one.

Check a deliberately tiny `BACKUP_MAX_BYTES`, a wrong backup hash, the live
`transactx` target name and an already existing restore target. Each must fail.
Also check that an incomplete release archive cannot be installed. Keep the
disposable cluster and encrypted files until the report is reviewed; stop the
cluster using its own `pg_ctl -D <disposable-data-dir> stop` command.

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
