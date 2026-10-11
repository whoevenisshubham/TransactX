# Isolated backup and restore rehearsal

This procedure checks the backup and restore scripts against a disposable
database. It does not verify worker continuation, derived projection rebuild,
cloud persistence or a production recovery time objective.

## Isolation and inputs

Use a separate PostgreSQL 18 cluster bound to `127.0.0.1` on a nondefault port.
Create a new `transactx` database and an owner role only in that cluster. Apply
the repository's existing baseline migrations in number order, then insert
synthetic records covering one payment, two central accounts, a balanced pair
of ledger entries, one idempotency record and one operation in each bank schema.
Do not use an existing financial database or real account data.

The current baseline migration `000013_m3_5_recon_runs.up.sql` has a UTF-8 BOM
that direct `psql -f` rejects. For this rehearsal, apply a disposable copy with
the BOM removed; leave the tracked migration unchanged. M2's migration CLI
must be tested against this input before it is relied on for deployment.

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

Worker continuation, projection rebuild, a real financial integrity run, a
mounted off-VM copy and the online restart sequence remain pending. Rehearse
them on the combined M2/M3 implementation before marking A7 complete.
