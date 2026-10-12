# Database Migration Tooling and Runbook (Task B0)

## Overview

The TransactX migration system provides deterministic, transactional, and checksum-verified schema migrations for PostgreSQL 18. It replaces raw script execution with an authoritative version ledger and exclusive advisory locking to prevent concurrent migration races and schema drift.

## Migration CLI (`backend/cmd/migrate`)

The migration CLI is located at `backend/cmd/migrate` and compiled as `migrate`.

### Commands

1. **`migrate up`**
   - Discovers all `*.up.sql` migration files in the configured migrations directory.
   - Verifies existing applied migrations against on-disk file checksums and lexical order.
   - Applies each pending migration in its own transaction (unless explicitly declared with `-- transactx:no-transaction`).
   - Records execution metrics in `schema_migrations` ledger.

2. **`migrate status`**
   - Displays the current schema version, total files, applied migrations with timestamps and durations, and any pending migrations.

3. **`migrate verify`**
   - Validates that every recorded migration in the database strictly matches the SHA-256 checksum and ordering of local migration files without making any changes.

4. **`migrate baseline [version]`**
   - Establishes a baseline on an existing populated database without re-executing `CREATE TABLE` statements.
   - Default target version: `000021`.
   - **Safety requirement**: The baseline command strictly verifies that core baseline tables (`users`, `accounts`, `banks`, `payments`, `ledger_transactions`, `ledger_entries`, `idempotency_records`, `payment_state_transitions`) actually exist before recording the baseline. If run on an empty database, the operation is refused.

### Command-line Flags & Environment Variables

| Flag | Env Variable | Default | Purpose |
| --- | --- | --- | --- |
| `-database-url` | `DATABASE_URL` | None (Required) | PostgreSQL connection DSN (sanitized in logs) |
| `-dir` | `TX_MIGRATIONS_DIR` | Auto-detected (`migrations`) | Path to SQL migrations directory |
| `-timeout` | None | `15s` | Bounded advisory lock acquisition timeout |
| `-target-db` | None | `""` | Optional database name verification to prevent migrating unintended targets |

## Concurrency and Locking Contract

- **Advisory Lock ID**: `839217491047120391` (`DefaultAdvisoryLockID`).
- **Session-bound Acquisition**: The lock is acquired on a dedicated connection checked out from `pgxpool.Pool` using `SELECT pg_try_advisory_lock($1)`.
- **Automatic Release**: The lock is guaranteed to be released on that exact same connection via `SELECT pg_advisory_unlock($1)` in a deferred block before the connection returns to the pool.
- **Mutual Exclusion**: Concurrent migration runners wait up to the bounded timeout. Exactly one runner applies migrations while concurrent runners detect a completed state upon acquiring the lock.

## Security and Observability

- **Credential Redaction**: Connection URLs, passwords, and tokens are never printed in structured logs or CLI output.
- **Audit Logging**: Each migration records `version`, `name`, `checksum`, `applied_at`, and `execution_time_ms`.
- **Target Safety**: Optional `-target-db` validates `current_database()` to prevent running migrations against wrong environments.

## B0 Verification Evidence

- Baseline Go: `1.27.0` / `1.27.1`
- Target PostgreSQL: `18.6`
- Test suite executed against disposable PostgreSQL databases:
  - `TestMigrator_EmptyDBToCurrent`: PASS (all 21 migrations applied transactionally)
  - `TestMigrator_SecondRunNoOp`: PASS (0 applied, idempotent)
  - `TestMigrator_ConcurrentMigrationAttempts`: PASS (advisory lock enforces mutual exclusion)
  - `TestMigrator_ChecksumAlterationDetected`: PASS (tampered migration fails closed)
  - `TestMigrator_FailedMigrationRollback`: PASS (transaction rollback prevents partial failure)
  - `TestMigrator_BaselineRejectionOnEmptyDB`: PASS (unverified empty database refused)
  - `TestMigrator_BaselineSuccessOnPopulatedDB`: PASS (untracked populated DB safely baselined)
  - `TestMigrator_TargetDatabaseValidation`: PASS (mismatched database rejected)
  - `TestPhase1ASchema`: PASS (all 16 financial integrity schema tests pass)
