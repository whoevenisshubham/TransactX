#!/usr/bin/env bash
set -euo pipefail
umask 077

backup=''
identity=''
expected_hash=''
source_sha=''
target=''
report_dir=''
validate_only=false
while (($#)); do
  case "$1" in
    --backup) backup="${2:-}"; shift 2 ;;
    --identity) identity="${2:-}"; shift 2 ;;
    --sha256) expected_hash="${2:-}"; shift 2 ;;
    --source-sha) source_sha="${2:-}"; shift 2 ;;
    --target-db) target="${2:-}"; shift 2 ;;
    --report-dir) report_dir="${2:-}"; shift 2 ;;
    --validate-only) validate_only=true; shift ;;
    *) echo "Unknown argument: $1" >&2; exit 2 ;;
  esac
done
[[ -f "$backup" && -f "$identity" ]] || { echo 'Encrypted backup and private identity files are required.' >&2; exit 2; }
[[ "$expected_hash" =~ ^[0-9a-f]{64}$ ]] || { echo 'A lowercase SHA-256 hash is required.' >&2; exit 2; }
[[ "$source_sha" =~ ^[0-9a-f]{40}$ ]] || { echo 'A full source SHA is required.' >&2; exit 2; }
[[ "$target" =~ ^transactx_restore_[a-z0-9_]{4,40}$ ]] || { echo 'Target must be a disposable transactx_restore_* database.' >&2; exit 2; }
[[ "$target" != "${PGDATABASE:-}" && "$target" != 'transactx' ]] || { echo 'Refusing the live database name.' >&2; exit 2; }
[[ "${PGHOST:-}" == '127.0.0.1' || "${PGHOST:-}" == 'localhost' || "${PGHOST:-}" == /var/run/postgresql ]] || { echo 'Restore checker requires a local PostgreSQL host.' >&2; exit 2; }
[[ "$report_dir" == /* && "$report_dir" != '/' ]] || { echo 'An absolute private report directory is required.' >&2; exit 2; }
for name in age pg_restore psql createdb sha256sum node realpath; do
  command -v "$name" >/dev/null || { echo "Missing required command: $name" >&2; exit 1; }
done
actual_hash="$(sha256sum "$backup" | cut -d ' ' -f1)"
[[ "$actual_hash" == "$expected_hash" ]] || { echo 'Encrypted backup hash mismatch.' >&2; exit 1; }
globals="${backup%.dump.age}.globals.sql.age"
[[ "$globals" != "$backup" && -f "$globals" && -f "$globals.sha256" ]] || { echo 'Matching encrypted global metadata is missing.' >&2; exit 1; }
(cd "$(dirname "$globals")" && sha256sum -c "$(basename "$globals.sha256")" >/dev/null)
if $validate_only; then
  echo "Target and backup validated. No database created: $target."
  exit 0
fi

# createdb fails if the named target already exists. Never drop or overwrite it.
createdb --no-password "$target"
started="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
age --decrypt -i "$identity" "$backup" | pg_restore --format=custom --no-owner --no-acl --exit-on-error --no-password --dbname="$target"

check() {
  PGDATABASE="$target" psql --no-password --no-psqlrc --tuples-only --no-align --set ON_ERROR_STOP=1 -c "$1"
}
schema_count="$(check "SELECT count(*) FROM information_schema.schemata WHERE schema_name IN ('public','bank_a','bank_b')")"
[[ "$schema_count" == '3' ]] || { echo 'Central and both participant schemas were not restored.' >&2; exit 1; }
payments="$(check 'SELECT count(*) FROM public.payments')"
accounts="$(check 'SELECT count(*) FROM public.accounts')"
bank_a_ops="$(check 'SELECT count(*) FROM bank_a.operations')"
bank_b_ops="$(check 'SELECT count(*) FROM bank_b.operations')"
idempotency="$(check 'SELECT count(*) FROM public.idempotency_records')"
negative_balances="$(check 'SELECT count(*) FROM public.accounts WHERE balance_paise < 0')"
negative_a="$(check 'SELECT count(*) FROM bank_a.accounts WHERE balance_paise < 0')"
negative_b="$(check 'SELECT count(*) FROM bank_b.accounts WHERE balance_paise < 0')"
unbalanced="$(check "SELECT count(*) FROM (SELECT lt.id FROM public.ledger_transactions lt LEFT JOIN public.ledger_entries le ON le.ledger_transaction_id = lt.id GROUP BY lt.id HAVING count(le.id) <> 2 OR coalesce(sum(CASE WHEN le.entry_type = 'DEBIT' THEN le.amount_paise ELSE -le.amount_paise END),0) <> 0) bad")"
orphan_idempotency="$(check 'SELECT count(*) FROM public.idempotency_records i LEFT JOIN public.payments p ON p.id = i.payment_id WHERE i.payment_id IS NOT NULL AND p.id IS NULL')"
[[ "$negative_balances" == '0' && "$negative_a" == '0' && "$negative_b" == '0' && "$unbalanced" == '0' && "$orphan_idempotency" == '0' ]] || { echo 'Restored financial invariants failed.' >&2; exit 1; }
finished="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
mkdir -p "$report_dir"
report_dir="$(realpath -e "$report_dir")"
report="$report_dir/restore-${target}-${finished//[:]/}.json"
[[ ! -e "$report" ]] || { echo 'Report already exists.' >&2; exit 1; }
export TX_RESTORE_REPORT="$report" TX_RESTORE_TARGET="$target" TX_RESTORE_SOURCE_SHA="$source_sha"
export TX_RESTORE_HASH="$actual_hash" TX_RESTORE_STARTED="$started" TX_RESTORE_FINISHED="$finished"
export TX_RESTORE_PAYMENTS="$payments" TX_RESTORE_ACCOUNTS="$accounts" TX_RESTORE_BANK_A_OPS="$bank_a_ops" TX_RESTORE_BANK_B_OPS="$bank_b_ops" TX_RESTORE_IDEMPOTENCY="$idempotency"
export TX_RESTORE_PG_VERSION="$(pg_restore --version)"
node -e 'const fs=require("fs"); const p=process.env; fs.writeFileSync(p.TX_RESTORE_REPORT,JSON.stringify({schemaVersion:"tx.restore.v1",sourceSha:p.TX_RESTORE_SOURCE_SHA,backupSha256:p.TX_RESTORE_HASH,startedAt:p.TX_RESTORE_STARTED,finishedAt:p.TX_RESTORE_FINISHED,targetDatabase:p.TX_RESTORE_TARGET,pgRestoreVersion:p.TX_RESTORE_PG_VERSION,counts:{payments:Number(p.TX_RESTORE_PAYMENTS),accounts:Number(p.TX_RESTORE_ACCOUNTS),bankAOperations:Number(p.TX_RESTORE_BANK_A_OPS),bankBOperations:Number(p.TX_RESTORE_BANK_B_OPS),idempotencyRecords:Number(p.TX_RESTORE_IDEMPOTENCY)},checks:{threeSchemas:true,nonnegativeBalances:true,balancedCentralLedger:true,noOrphanIdempotency:true},workerContinuation:"not_run",projectionRebuild:"not_run",globalRoles:"not_applied_to_shared_cluster"},null,2)+"\n")'
(cd "$report_dir" && sha256sum "$(basename "$report")" > "$(basename "$report.sha256")")
echo "Isolated restore checks passed. Report: $report"
echo 'Worker continuation, projection rebuild and global-role restore remain separate release gates.'
