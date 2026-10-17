#!/usr/bin/env bash
set -euo pipefail

backup=''
identity=''
expected_hash=''
source_sha=''
target=''
report_dir=''
expected_counts=''
allow_empty=false
validate_only=false
while (($#)); do
  case "$1" in
    --backup) backup="${2:-}"; shift 2 ;;
    --identity) identity="${2:-}"; shift 2 ;;
    --sha256) expected_hash="${2:-}"; shift 2 ;;
    --source-sha) source_sha="${2:-}"; shift 2 ;;
    --target-db) target="${2:-}"; shift 2 ;;
    --report-dir) report_dir="${2:-}"; shift 2 ;;
    --expected-counts) expected_counts="${2:-}"; shift 2 ;;
    --allow-empty) allow_empty=true; shift ;;
    --validate-only) validate_only=true; shift ;;
    *) echo "Unknown argument: $1" >&2; exit 2 ;;
  esac
done
[[ -n "$backup" && -f "$backup" ]] || { echo 'Encrypted backup file is required and must exist.' >&2; exit 2; }
[[ -n "$identity" && -f "$identity" ]] || { echo 'Private identity file is required and must exist.' >&2; exit 2; }
identity_path="$(realpath -e "$identity")" || { echo 'Private identity path cannot be resolved.' >&2; exit 2; }
[[ -f "$identity_path" ]] || { echo 'Private identity target must be a regular file.' >&2; exit 2; }
unsafe_identity="$(find "$identity_path" -perm /077 -print)" || { echo 'Cannot inspect identity permissions.' >&2; exit 2; }
[[ -z "$unsafe_identity" ]] || { echo 'Private identity file must be owner-only (mode 0400 or 0600).' >&2; exit 2; }
[[ "$expected_hash" =~ ^[0-9a-f]{64}$ ]] || { echo 'A lowercase SHA-256 hash is required.' >&2; exit 2; }
[[ "$source_sha" =~ ^[0-9a-f]{40}$ ]] || { echo 'A full source SHA is required.' >&2; exit 2; }
[[ "$target" =~ ^transactx_restore_[a-z0-9_]{4,40}$ ]] || { echo 'Target must be a disposable transactx_restore_* database.' >&2; exit 2; }
[[ "$target" != "${PGDATABASE:-}" && "$target" != 'transactx' ]] || { echo 'Refusing the live database name.' >&2; exit 2; }
[[ "${PGHOST:-}" == '127.0.0.1' || "${PGHOST:-}" == 'localhost' || "${PGHOST:-}" == /var/run/postgresql ]] || { echo 'Restore checker requires a local PostgreSQL host.' >&2; exit 2; }
[[ "$report_dir" == /* && "$report_dir" != '/' ]] || { echo 'An absolute private report directory is required.' >&2; exit 2; }

expected_counts_path=''
if [[ -n "$expected_counts" ]]; then
  [[ -f "$expected_counts" ]] || { echo 'Expected count manifest file not found.' >&2; exit 2; }
  expected_counts_path="$(realpath -e "$expected_counts")" || { echo 'Expected count manifest path cannot be resolved.' >&2; exit 2; }
  node -e '
    const fs = require("fs");
    let manifest;
    try {
      manifest = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
    } catch (e) {
      console.error("Expected count manifest is not valid JSON.");
      process.exit(2);
    }
    if (typeof manifest !== "object" || manifest === null || Array.isArray(manifest)) {
      console.error("Expected count manifest must be a JSON object.");
      process.exit(2);
    }

    const ASSERTION_KEYS = new Set([
      "payments", "minPayments",
      "accounts", "minAccounts",
      "bankAOperations", "minBankAOperations",
      "bankBOperations", "minBankBOperations",
      "idempotencyRecords", "minIdempotencyRecords"
    ]);

    let countsObj;
    if (manifest.counts !== undefined) {
      if (typeof manifest.counts !== "object" || manifest.counts === null || Array.isArray(manifest.counts)) {
        console.error("Manifest property '\''counts'\'' must be a JSON object.");
        process.exit(2);
      }
      for (const k of Object.keys(manifest)) {
        if (k === "counts" || k === "description") continue;
        if (ASSERTION_KEYS.has(k)) {
          console.error(`Ambiguous manifest: cannot specify top-level table assertion "${k}" when embedded "counts" object is present.`);
          process.exit(2);
        }
        console.error(`Unknown top-level key in manifest: "${k}"`);
        process.exit(2);
      }
      countsObj = manifest.counts;
    } else {
      countsObj = manifest;
    }

    const TABLES = [
      { id: "payments", exactKey: "payments", minKey: "minPayments" },
      { id: "accounts", exactKey: "accounts", minKey: "minAccounts" },
      { id: "bankAOperations", exactKey: "bankAOperations", minKey: "minBankAOperations" },
      { id: "bankBOperations", exactKey: "bankBOperations", minKey: "minBankBOperations" },
      { id: "idempotencyRecords", exactKey: "idempotencyRecords", minKey: "minIdempotencyRecords" }
    ];

    const ALLOWED_KEYS = new Set(ASSERTION_KEYS);
    if (countsObj === manifest) {
      ALLOWED_KEYS.add("description");
    }

    for (const k of Object.keys(countsObj)) {
      if (!ALLOWED_KEYS.has(k)) {
        if (countsObj !== manifest) {
          console.error(`Unknown key in counts object: "${k}"`);
        } else {
          console.error(`Unknown key in manifest: "${k}"`);
        }
        process.exit(2);
      }
      if (k === "description") continue;
      const val = countsObj[k];
      if (typeof val !== "number" || !Number.isSafeInteger(val) || val < 0 || Object.is(val, -0)) {
        console.error(`Invalid count for "${k}": must be a safe non-negative integer`);
        process.exit(2);
      }
    }

    let minTotalRequired = 0;
    for (const t of TABLES) {
      const hasExact = (countsObj[t.exactKey] !== undefined);
      const hasMin = (countsObj[t.minKey] !== undefined);
      if (hasExact && hasMin) {
        console.error(`Conflicting assertions for "${t.id}": cannot specify both "${t.exactKey}" and "${t.minKey}"`);
        process.exit(2);
      }
      if (!hasExact && !hasMin) {
        console.error(`Missing assertion for "${t.id}": manifest must specify either "${t.exactKey}" or "${t.minKey}"`);
        process.exit(2);
      }
      const bound = hasExact ? countsObj[t.exactKey] : countsObj[t.minKey];
      minTotalRequired += bound;
    }

    if (minTotalRequired === 0) {
      console.error("Invalid populated acceptance manifest: assertions permit an entirely empty database. At least one table must require > 0 rows (use --allow-empty for empty-state verification).");
      process.exit(2);
    }
  ' "$expected_counts_path" || { echo 'Invalid expected count manifest.' >&2; exit 2; }
elif ! $allow_empty; then
  echo 'Restore completeness contract required: provide --expected-counts <path> or explicit --allow-empty.' >&2
  exit 2
fi

for name in age pg_restore psql createdb sha256sum node realpath; do
  command -v "$name" >/dev/null || { echo "Missing required command: $name" >&2; exit 1; }
done
actual_hash="$(sha256sum "$backup" | cut -d ' ' -f1)"
[[ "$actual_hash" == "$expected_hash" ]] || { echo 'Encrypted backup hash mismatch.' >&2; exit 1; }
globals="${backup%.dump.age}.globals.sql.age"
[[ "$globals" != "$backup" && -f "$globals" && -f "$globals.sha256" ]] || { echo 'Matching encrypted global metadata is missing.' >&2; exit 1; }
(cd "$(dirname "$globals")" && sha256sum -c "$(basename "$globals.sha256")" >/dev/null)
if $validate_only; then
  echo "Target, backup, identity permissions and expected-population contract validated. No database created: $target."
  exit 0
fi

# Ensure local files and directories created by this shell process (reports, temporary files) are owner-only.
# Note: files created by the PostgreSQL server process in PGDATA are governed by the server daemon, not client umask.
umask 077

# createdb fails if the named target already exists. Never drop or overwrite it.
createdb --no-password "$target"
started="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
age --decrypt -i "$identity_path" "$backup" | pg_restore --format=custom --no-owner --no-acl --exit-on-error --no-password --dbname="$target"

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

# Completeness / expected population verification
export TX_RESTORE_PAYMENTS="$payments" TX_RESTORE_ACCOUNTS="$accounts"
export TX_RESTORE_BANK_A_OPS="$bank_a_ops" TX_RESTORE_BANK_B_OPS="$bank_b_ops"
export TX_RESTORE_IDEMPOTENCY="$idempotency"

if [[ -n "$expected_counts_path" ]]; then
  node -e '
    const fs = require("fs");
    const manifest = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
    const counts = (manifest.counts && typeof manifest.counts === "object" && !Array.isArray(manifest.counts))
      ? manifest.counts
      : manifest;
    const p = process.env;
    const actual = {
      payments: Number(p.TX_RESTORE_PAYMENTS),
      accounts: Number(p.TX_RESTORE_ACCOUNTS),
      bankAOperations: Number(p.TX_RESTORE_BANK_A_OPS),
      bankBOperations: Number(p.TX_RESTORE_BANK_B_OPS),
      idempotencyRecords: Number(p.TX_RESTORE_IDEMPOTENCY)
    };

    const TABLES = [
      { id: "payments", exactKey: "payments", minKey: "minPayments" },
      { id: "accounts", exactKey: "accounts", minKey: "minAccounts" },
      { id: "bankAOperations", exactKey: "bankAOperations", minKey: "minBankAOperations" },
      { id: "bankBOperations", exactKey: "bankBOperations", minKey: "minBankBOperations" },
      { id: "idempotencyRecords", exactKey: "idempotencyRecords", minKey: "minIdempotencyRecords" }
    ];

    const errors = [];
    for (const t of TABLES) {
      const actVal = actual[t.id];
      if (counts[t.exactKey] !== undefined) {
        const expVal = Number(counts[t.exactKey]);
        if (actVal !== expVal) {
          errors.push(`Count mismatch for ${t.id}: expected ${expVal}, found ${actVal}`);
        }
      } else if (counts[t.minKey] !== undefined) {
        const minVal = Number(counts[t.minKey]);
        if (actVal < minVal) {
          errors.push(`Count below minimum for ${t.id}: expected at least ${minVal}, found ${actVal}`);
        }
      }
    }

    const totalActual = actual.payments + actual.accounts + actual.bankAOperations + actual.bankBOperations + actual.idempotencyRecords;
    if (totalActual === 0) {
      errors.push("Populated data verification failed: observed 0 total rows across all five core tables.");
    }

    if (errors.length > 0) {
      for (const err of errors) console.error(err);
      process.exit(1);
    }
  ' "$expected_counts_path" || { echo 'Restored completeness verification failed against expected count manifest.' >&2; exit 1; }
  population_contract="manifest:$(basename "$expected_counts_path")"
  populated_verified=true
  empty_allowed=false
else
  # Verify that all five core tables in the declared empty-state scope are strictly empty (0 rows)
  if [[ "$payments" != '0' || "$accounts" != '0' || "$bank_a_ops" != '0' || "$bank_b_ops" != '0' || "$idempotency" != '0' ]]; then
    echo "Empty-state restore check failed: declared empty-state scope (public.payments, public.accounts, bank_a.operations, bank_b.operations, public.idempotency_records) contains populated rows." >&2
    echo "Found: payments=$payments, accounts=$accounts, bank_a_ops=$bank_a_ops, bank_b_ops=$bank_b_ops, idempotency_records=$idempotency." >&2
    exit 1
  fi
  population_contract="allow_empty"
  populated_verified=false
  empty_allowed=true
fi

finished="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
mkdir -p "$report_dir"
report_dir="$(realpath -e "$report_dir")"
report="$report_dir/restore-${target}-${finished//[:]/}.json"
[[ ! -e "$report" ]] || { echo 'Report already exists.' >&2; exit 1; }
export TX_RESTORE_REPORT="$report" TX_RESTORE_TARGET="$target" TX_RESTORE_SOURCE_SHA="$source_sha"
export TX_RESTORE_HASH="$actual_hash" TX_RESTORE_STARTED="$started" TX_RESTORE_FINISHED="$finished"
export TX_RESTORE_PG_VERSION="$(pg_restore --version)"
export TX_RESTORE_POPULATION_CONTRACT="$population_contract"
export TX_RESTORE_POPULATED_VERIFIED="$populated_verified"
export TX_RESTORE_EMPTY_ALLOWED="$empty_allowed"
node -e 'const fs=require("fs"); const p=process.env; fs.writeFileSync(p.TX_RESTORE_REPORT,JSON.stringify({schemaVersion:"tx.restore.v1",sourceSha:p.TX_RESTORE_SOURCE_SHA,backupSha256:p.TX_RESTORE_HASH,startedAt:p.TX_RESTORE_STARTED,finishedAt:p.TX_RESTORE_FINISHED,targetDatabase:p.TX_RESTORE_TARGET,pgRestoreVersion:p.TX_RESTORE_PG_VERSION,counts:{payments:Number(p.TX_RESTORE_PAYMENTS),accounts:Number(p.TX_RESTORE_ACCOUNTS),bankAOperations:Number(p.TX_RESTORE_BANK_A_OPS),bankBOperations:Number(p.TX_RESTORE_BANK_B_OPS),idempotencyRecords:Number(p.TX_RESTORE_IDEMPOTENCY)},checks:{threeSchemas:true,nonnegativeBalances:true,balancedCentralLedger:true,noOrphanIdempotency:true,coreTablesEmptyVerified:(p.TX_RESTORE_EMPTY_ALLOWED==="true"),populationContractSatisfied:true,populatedDataVerified:(p.TX_RESTORE_POPULATED_VERIFIED==="true"),completenessVerified:(p.TX_RESTORE_POPULATED_VERIFIED==="true")},emptyStateScope:(p.TX_RESTORE_EMPTY_ALLOWED==="true"?"public.payments,public.accounts,bank_a.operations,bank_b.operations,public.idempotency_records":"none"),populationContract:p.TX_RESTORE_POPULATION_CONTRACT,emptyStateAllowed:(p.TX_RESTORE_EMPTY_ALLOWED==="true"),workerContinuation:"not_run",projectionRebuild:"not_run",globalRoles:"not_applied_to_shared_cluster"},null,2)+"\n")'
(cd "$report_dir" && sha256sum "$(basename "$report")" > "$(basename "$report.sha256")")
echo "Isolated restore checks passed. Report: $report"
echo 'Worker continuation, projection rebuild and global-role restore remain separate release gates.'
