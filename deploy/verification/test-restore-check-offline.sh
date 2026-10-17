#!/usr/bin/env bash
set -euo pipefail

# Offline regression test harness for deploy/scripts/restore-check.sh
# Stubs external commands (createdb, pg_restore, age, psql, and POSIX perm simulation).
# Zero PostgreSQL connection or network access.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
RESTORE_SCRIPT="$REPO_ROOT/deploy/scripts/restore-check.sh"

TEST_TMP="$(mktemp -d)"
trap 'rm -rf -- "$TEST_TMP"' EXIT

# Set up isolated stub bin directory
STUB_BIN="$TEST_TMP/stub_bin"
mkdir -p "$STUB_BIN"

# Stub: createdb
cat << 'EOF' > "$STUB_BIN/createdb"
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$STUB_BIN/createdb"

# Stub: age
cat << 'EOF' > "$STUB_BIN/age"
#!/usr/bin/env bash
# Echo dummy byte stream for the pipe
echo "DUMMY_DUMP"
exit 0
EOF
chmod +x "$STUB_BIN/age"

# Stub: pg_restore
cat << 'EOF' > "$STUB_BIN/pg_restore"
#!/usr/bin/env bash
if [[ "${1:-}" == "--version" ]]; then
  echo "pg_restore (PostgreSQL) 18.6 (Stubbed)"
  exit 0
fi
cat >/dev/null
exit 0
EOF
chmod +x "$STUB_BIN/pg_restore"

# Stub: psql
cat << 'EOF' > "$STUB_BIN/psql"
#!/usr/bin/env bash
# Query dispatcher based on query string passed in -c "$1"
query="${*: -1}"
res="0"
case "$query" in
  *information_schema.schemata*)
    res="${MOCK_SCHEMA_COUNT:-3}"
    ;;
  *payment_id*)
    res="${MOCK_ORPHAN_IDEMPOTENCY:-0}"
    ;;
  *public.accounts*balance_paise*)
    res="${MOCK_NEGATIVE_BALANCES:-0}"
    ;;
  *bank_a.accounts*balance_paise*)
    res="${MOCK_NEGATIVE_A:-0}"
    ;;
  *bank_b.accounts*balance_paise*)
    res="${MOCK_NEGATIVE_B:-0}"
    ;;
  *public.ledger_transactions*)
    res="${MOCK_UNBALANCED:-0}"
    ;;
  *public.payments*)
    res="${MOCK_PAYMENTS:-1}"
    ;;
  *public.accounts*)
    res="${MOCK_ACCOUNTS:-2}"
    ;;
  *bank_a.operations*)
    res="${MOCK_BANK_A_OPS:-1}"
    ;;
  *bank_b.operations*)
    res="${MOCK_BANK_B_OPS:-1}"
    ;;
  *public.idempotency_records*)
    res="${MOCK_IDEMPOTENCY:-1}"
    ;;
  *)
    res="0"
    ;;
esac
echo "$res"
exit 0
EOF
chmod +x "$STUB_BIN/psql"

# Stub: find (simulates Linux POSIX permission inspection for identity file)
cat << 'EOF' > "$STUB_BIN/find"
#!/usr/bin/env bash
if [[ "${1:-}" == *".globals.sql.age" || "${1:-}" == *"globals"* ]]; then
  exec /usr/bin/find "$@"
fi
if [[ "$*" == *"-perm /077"* ]]; then
  if [[ "${MOCK_INSECURE_IDENTITY:-0}" == "1" ]]; then
    echo "$1"
    exit 0
  else
    exit 0
  fi
fi
exec /usr/bin/find "$@"
EOF
chmod +x "$STUB_BIN/find"

# Prepend stub bin to PATH
export PATH="$STUB_BIN:$PATH"
export TEST_TMP
export PGHOST="127.0.0.1"
export PGDATABASE="postgres"
export MOCK_INSECURE_IDENTITY="0"

# Create valid fake encrypted backup file and globals file with SHA-256 sidecars
FAKE_BACKUP="$TEST_TMP/transactx-20261015T000000Z.dump.age"
echo "CIPHERTEXT" > "$FAKE_BACKUP"
BACKUP_HASH="$(sha256sum "$FAKE_BACKUP" | cut -d ' ' -f1)"

FAKE_GLOBALS="$TEST_TMP/transactx-20261015T000000Z.globals.sql.age"
echo "GLOBALS_CIPHERTEXT" > "$FAKE_GLOBALS"
(cd "$TEST_TMP" && sha256sum "$(basename "$FAKE_GLOBALS")" > "$(basename "$FAKE_GLOBALS.sha256")")

# Valid source SHA and target DB
SOURCE_SHA="dd8cd60581692fd8a10843963cb10456b40f8b79"
TARGET_DB="transactx_restore_offline_test"
REPORT_DIR="$TEST_TMP/reports"
mkdir -p "$REPORT_DIR"

# Identity files
SECURE_KEY="$TEST_TMP/secure.key"
echo "AGE-SECRET-KEY-1TESTTESTTESTTESTTESTTESTTESTTESTTESTTESTTESTTESTTEST" > "$SECURE_KEY"
INSECURE_KEY="$TEST_TMP/insecure.key"
echo "AGE-SECRET-KEY-1TESTTESTTESTTESTTESTTESTTESTTESTTESTTESTTESTTESTTEST" > "$INSECURE_KEY"

# Helper for asserting failure
assert_failure() {
  local desc="$1"
  local expected_exit="$2"
  local expected_err_pattern="$3"
  shift 3

  set +e
  local err_file="$TEST_TMP/err.log"
  "$@" >/dev/null 2>"$err_file"
  local actual_exit=$?
  set -e

  if [[ $actual_exit -ne $expected_exit ]]; then
    echo "FAIL: $desc (expected exit $expected_exit, got $actual_exit)" >&2
    cat "$err_file" >&2
    exit 1
  fi

  if ! grep -Eq "$expected_err_pattern" "$err_file"; then
    echo "FAIL: $desc (error output did not match pattern '$expected_err_pattern')" >&2
    cat "$err_file" >&2
    exit 1
  fi

  echo "PASS: $desc"
}

# Helper for asserting success
assert_success() {
  local desc="$1"
  shift

  set +e
  local err_file="$TEST_TMP/err.log"
  local out_file="$TEST_TMP/out.log"
  "$@" >"$out_file" 2>"$err_file"
  local actual_exit=$?
  set -e

  if [[ $actual_exit -ne 0 ]]; then
    echo "FAIL: $desc (expected exit 0, got $actual_exit)" >&2
    cat "$err_file" >&2
    if [[ -f "$TEST_TMP/psql.log" ]]; then
      echo "=== PSQL LOG ===" >&2
      cat "$TEST_TMP/psql.log" >&2
    fi
    exit 1
  fi

  echo "PASS: $desc"
}

echo "=== Running Offline Restore-Check Regression Tests ==="

# Create valid acceptance manifest
ACCEPTANCE_MANIFEST="$TEST_TMP/acceptance-manifest.json"
cat << 'EOF' > "$ACCEPTANCE_MANIFEST"
{
  "description": "Offline test acceptance manifest",
  "counts": {
    "payments": 1,
    "accounts": 2,
    "bankAOperations": 1,
    "bankBOperations": 1,
    "idempotencyRecords": 1
  }
}
EOF

# Test 1: Missing private identity file
assert_failure "Missing private identity file" 2 "Private identity file is required" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$TEST_TMP/nonexistent.key" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_DB" \
  --report-dir "$REPORT_DIR" \
  --allow-empty

# Test 2: Insecure private identity rejection logic (mocked find reporting mode 0644 with /077 bits set)
# Note: This tests the script's branching/rejection logic against stubbed find output;
# true POSIX inode permission enforcement depends on the host Linux kernel and filesystem.
export MOCK_INSECURE_IDENTITY="1"
assert_failure "Rejection logic on insecure identity permissions (mocked /077 bits set)" 2 "must be owner-only" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$INSECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_DB" \
  --report-dir "$REPORT_DIR" \
  --allow-empty
export MOCK_INSECURE_IDENTITY="0"

# Test 3: Owner-only private identity acceptance logic in --validate-only (mocked clean 0400/0600 mode)
# Note: Tests script acceptance logic when stubbed find reports no /077 bits.
assert_success "Acceptance logic on owner-only identity permissions (mocked /077 bits clean)" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_DB" \
  --report-dir "$REPORT_DIR" \
  --allow-empty \
  --validate-only

# Test 4: Missing expected row-count metadata (neither --expected-counts nor --allow-empty)
assert_failure "Missing completeness contract (neither --expected-counts nor --allow-empty)" 2 "Restore completeness contract required" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_DB" \
  --report-dir "$REPORT_DIR"

# Test 4b: Missing expected-count manifest file path (--expected-counts points to nonexistent file)
assert_failure "Missing expected-count manifest file path" 2 "Expected count manifest file not found" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_DB" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$TEST_TMP/nonexistent_manifest.json"

# Test 5: Malformed expected-count manifest (invalid JSON)
BAD_JSON_MANIFEST="$TEST_TMP/bad_json.json"
echo "NOT_JSON{{{" > "$BAD_JSON_MANIFEST"
assert_failure "Malformed manifest (invalid JSON)" 2 "Invalid expected count manifest" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_DB" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$BAD_JSON_MANIFEST"

# Test 6: Incomplete / empty expected-count manifest ({})
EMPTY_MANIFEST="$TEST_TMP/empty_manifest.json"
echo "{}" > "$EMPTY_MANIFEST"
assert_failure "Empty manifest with no count assertions ({})" 2 "Invalid expected count manifest" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_DB" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$EMPTY_MANIFEST"

# Test 6b: Partial manifest missing table assertions (e.g. only payments defined)
PARTIAL_MANIFEST="$TEST_TMP/partial_manifest.json"
cat << 'EOF' > "$PARTIAL_MANIFEST"
{
  "description": "Partial manifest missing 4 tables",
  "counts": {
    "payments": 1
  }
}
EOF
assert_failure "Partial manifest missing required table assertions" 2 "Missing assertion for \"accounts\"" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_DB" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$PARTIAL_MANIFEST"

# Test 6c: Manifest with unknown key in top-level shape
UNKNOWN_KEY_MANIFEST="$TEST_TMP/unknown_key_manifest.json"
cat << 'EOF' > "$UNKNOWN_KEY_MANIFEST"
{
  "description": "Manifest with unknown top-level key",
  "payments": 1,
  "accounts": 2,
  "bankAOperations": 1,
  "bankBOperations": 1,
  "idempotencyRecords": 1,
  "unknownTopLevel": 5
}
EOF
assert_failure "Manifest with unknown key in top-level shape" 2 "Unknown key in manifest: \"unknownTopLevel\"" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_DB" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$UNKNOWN_KEY_MANIFEST"

# Test 6d: Conflicting assertions for the same table (both exact and min specified)
CONFLICT_MANIFEST="$TEST_TMP/conflict_manifest.json"
cat << 'EOF' > "$CONFLICT_MANIFEST"
{
  "description": "Manifest with conflicting assertions",
  "counts": {
    "payments": 1,
    "minPayments": 1,
    "accounts": 2,
    "bankAOperations": 1,
    "bankBOperations": 1,
    "idempotencyRecords": 1
  }
}
EOF
assert_failure "Conflicting assertions for same table" 2 "Conflicting assertions for \"payments\"" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_DB" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$CONFLICT_MANIFEST"

# Test 6e: Invalid count value (negative count)
NEGATIVE_COUNT_MANIFEST="$TEST_TMP/negative_count_manifest.json"
cat << 'EOF' > "$NEGATIVE_COUNT_MANIFEST"
{
  "description": "Manifest with negative count",
  "counts": {
    "payments": -1,
    "accounts": 2,
    "bankAOperations": 1,
    "bankBOperations": 1,
    "idempotencyRecords": 1
  }
}
EOF
assert_failure "Manifest with negative count" 2 "Invalid count for \"payments\": must be a safe non-negative integer" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_DB" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$NEGATIVE_COUNT_MANIFEST"

# Test 6f: Invalid count value (fractional count)
FLOAT_COUNT_MANIFEST="$TEST_TMP/float_count_manifest.json"
cat << 'EOF' > "$FLOAT_COUNT_MANIFEST"
{
  "description": "Manifest with fractional count",
  "counts": {
    "payments": 1.5,
    "accounts": 2,
    "bankAOperations": 1,
    "bankBOperations": 1,
    "idempotencyRecords": 1
  }
}
EOF
assert_failure "Manifest with fractional count" 2 "Invalid count for \"payments\": must be a safe non-negative integer" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_DB" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$FLOAT_COUNT_MANIFEST"

# Test 6g: Invalid count value (string count)
STRING_COUNT_MANIFEST="$TEST_TMP/string_count_manifest.json"
cat << 'EOF' > "$STRING_COUNT_MANIFEST"
{
  "description": "Manifest with string count",
  "counts": {
    "payments": "1",
    "accounts": 2,
    "bankAOperations": 1,
    "bankBOperations": 1,
    "idempotencyRecords": 1
  }
}
EOF
assert_failure "Manifest with string count" 2 "Invalid count for \"payments\": must be a safe non-negative integer" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_DB" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$STRING_COUNT_MANIFEST"

# Test 6h: Invalid count value (null count)
NULL_COUNT_MANIFEST="$TEST_TMP/null_count_manifest.json"
cat << 'EOF' > "$NULL_COUNT_MANIFEST"
{
  "description": "Manifest with null count",
  "counts": {
    "payments": null,
    "accounts": 2,
    "bankAOperations": 1,
    "bankBOperations": 1,
    "idempotencyRecords": 1
  }
}
EOF
assert_failure "Manifest with null count" 2 "Invalid count for \"payments\": must be a safe non-negative integer" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_DB" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$NULL_COUNT_MANIFEST"

# Test 6h2: Invalid count value (boolean count)
BOOL_COUNT_MANIFEST="$TEST_TMP/bool_count_manifest.json"
cat << 'EOF' > "$BOOL_COUNT_MANIFEST"
{
  "description": "Manifest with boolean count",
  "counts": {
    "payments": true,
    "accounts": 2,
    "bankAOperations": 1,
    "bankBOperations": 1,
    "idempotencyRecords": 1
  }
}
EOF
assert_failure "Manifest with boolean count" 2 "Invalid count for \"payments\": must be a safe non-negative integer" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_DB" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$BOOL_COUNT_MANIFEST"

# Test 6n: Invalid count value (beyond JavaScript safe integer range)
UNSAFE_INT_MANIFEST="$TEST_TMP/unsafe_int_manifest.json"
cat << 'EOF' > "$UNSAFE_INT_MANIFEST"
{
  "description": "Manifest with count exceeding safe integer range",
  "counts": {
    "payments": 9007199254740992,
    "accounts": 2,
    "bankAOperations": 1,
    "bankBOperations": 1,
    "idempotencyRecords": 1
  }
}
EOF
assert_failure "Manifest with count exceeding safe integer range" 2 "Invalid count for \"payments\": must be a safe non-negative integer" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_DB" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$UNSAFE_INT_MANIFEST"

# Test 6o: Manifest with all exact zero assertions (empty-permitting manifest)
ALL_ZERO_EXACT_MANIFEST="$TEST_TMP/all_zero_exact_manifest.json"
cat << 'EOF' > "$ALL_ZERO_EXACT_MANIFEST"
{
  "description": "All exact zeros manifest",
  "counts": {
    "payments": 0,
    "accounts": 0,
    "bankAOperations": 0,
    "bankBOperations": 0,
    "idempotencyRecords": 0
  }
}
EOF
assert_failure "Manifest with all exact zero assertions rejected" 2 "Invalid populated acceptance manifest: assertions permit an entirely empty database" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_DB" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$ALL_ZERO_EXACT_MANIFEST"

# Test 6p: Manifest with all minimum zero assertions (empty-permitting manifest)
ALL_ZERO_MIN_MANIFEST="$TEST_TMP/all_zero_min_manifest.json"
cat << 'EOF' > "$ALL_ZERO_MIN_MANIFEST"
{
  "description": "All minimum zeros manifest",
  "counts": {
    "minPayments": 0,
    "minAccounts": 0,
    "minBankAOperations": 0,
    "minBankBOperations": 0,
    "minIdempotencyRecords": 0
  }
}
EOF
assert_failure "Manifest with all minimum zero assertions rejected" 2 "Invalid populated acceptance manifest: assertions permit an entirely empty database" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_DB" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$ALL_ZERO_MIN_MANIFEST"

# Test 6i: Non-object counts property (string)
NON_OBJECT_STRING_MANIFEST="$TEST_TMP/non_object_string_manifest.json"
cat << 'EOF' > "$NON_OBJECT_STRING_MANIFEST"
{
  "description": "Non-object counts property (string)",
  "counts": "not-an-object"
}
EOF
assert_failure "Non-object counts property (string)" 2 "Manifest property 'counts' must be a JSON object" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_DB" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$NON_OBJECT_STRING_MANIFEST"

# Test 6j: Non-object counts property (null)
NON_OBJECT_NULL_MANIFEST="$TEST_TMP/non_object_null_manifest.json"
cat << 'EOF' > "$NON_OBJECT_NULL_MANIFEST"
{
  "description": "Non-object counts property (null)",
  "counts": null
}
EOF
assert_failure "Non-object counts property (null)" 2 "Manifest property 'counts' must be a JSON object" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_DB" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$NON_OBJECT_NULL_MANIFEST"

# Test 6k: Non-object counts property (array)
NON_OBJECT_ARRAY_MANIFEST="$TEST_TMP/non_object_array_manifest.json"
cat << 'EOF' > "$NON_OBJECT_ARRAY_MANIFEST"
{
  "description": "Non-object counts property (array)",
  "counts": [1, 2, 3]
}
EOF
assert_failure "Non-object counts property (array)" 2 "Manifest property 'counts' must be a JSON object" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_DB" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$NON_OBJECT_ARRAY_MANIFEST"

# Test 6l: Ambiguous manifest with both top-level and embedded table assertions
AMBIGUOUS_MANIFEST="$TEST_TMP/ambiguous_manifest.json"
cat << 'EOF' > "$AMBIGUOUS_MANIFEST"
{
  "description": "Ambiguous manifest with both shapes",
  "payments": 1,
  "counts": {
    "payments": 1,
    "accounts": 2,
    "bankAOperations": 1,
    "bankBOperations": 1,
    "idempotencyRecords": 1
  }
}
EOF
assert_failure "Ambiguous manifest with top-level and embedded assertions" 2 "Ambiguous manifest: cannot specify top-level table assertion \"payments\" when embedded \"counts\" object is present" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_DB" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$AMBIGUOUS_MANIFEST"

# Test 6m: Unknown key inside embedded counts object
UNKNOWN_EMBEDDED_KEY_MANIFEST="$TEST_TMP/unknown_embedded_key_manifest.json"
cat << 'EOF' > "$UNKNOWN_EMBEDDED_KEY_MANIFEST"
{
  "description": "Manifest with unknown key inside counts object",
  "counts": {
    "payments": 1,
    "accounts": 2,
    "bankAOperations": 1,
    "bankBOperations": 1,
    "idempotencyRecords": 1,
    "unrecognizedTable": 1
  }
}
EOF
assert_failure "Manifest with unknown key inside counts object" 2 "Unknown key in counts object: \"unrecognizedTable\"" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_DB" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$UNKNOWN_EMBEDDED_KEY_MANIFEST"

# Test 7: Required tables or expected population absent (exact-count mismatch: expected 1 payment, found 0)
export MOCK_PAYMENTS="0"
export MOCK_ACCOUNTS="2"
export MOCK_BANK_A_OPS="1"
export MOCK_BANK_B_OPS="1"
export MOCK_IDEMPOTENCY="1"
export MOCK_UNBALANCED="0"
export MOCK_NEGATIVE_BALANCES="0"
assert_failure "Exact-count mismatch (payments: expected 1, found 0)" 1 "Count mismatch for payments: expected 1, found 0" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "transactx_restore_case7" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$ACCEPTANCE_MANIFEST"

# Test 7b: Minimum-count failure (restored accounts count 2 below minAccounts threshold 5)
MIN_FAIL_MANIFEST="$TEST_TMP/min_fail_manifest.json"
cat << 'EOF' > "$MIN_FAIL_MANIFEST"
{
  "description": "Minimum count failure manifest with all 5 tables",
  "counts": {
    "payments": 1,
    "minAccounts": 5,
    "bankAOperations": 1,
    "bankBOperations": 1,
    "idempotencyRecords": 1
  }
}
EOF
assert_failure "Minimum-count failure (accounts count 2 below minAccounts 5)" 1 "Count below minimum for accounts: expected at least 5, found 2" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "transactx_restore_case7b" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$MIN_FAIL_MANIFEST"

# Test 7c: Populated restore fails when all observed core-table counts are zero
export MOCK_PAYMENTS="0"
export MOCK_ACCOUNTS="0"
export MOCK_BANK_A_OPS="0"
export MOCK_BANK_B_OPS="0"
export MOCK_IDEMPOTENCY="0"
export MOCK_UNBALANCED="0"
export MOCK_NEGATIVE_BALANCES="0"
assert_failure "Populated restore fails when all observed core-table counts are zero" 1 "Populated data verification failed: observed 0 total rows across all five core tables" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "transactx_restore_case7c" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$ACCEPTANCE_MANIFEST"

# Reset mock counts to valid populated baseline
export MOCK_PAYMENTS="1"
export MOCK_ACCOUNTS="2"
export MOCK_BANK_A_OPS="1"
export MOCK_BANK_B_OPS="1"
export MOCK_IDEMPOTENCY="1"

# Test 8: Existing financial invariant failure remains rejected (e.g. unbalanced ledger)
export MOCK_UNBALANCED="1"
assert_failure "Financial invariant failure remains rejected (unbalanced ledger)" 1 "Restored financial invariants failed" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "transactx_restore_case8" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$ACCEPTANCE_MANIFEST"
export MOCK_UNBALANCED="0"

# Test 9: Valid populated restore matching embedded-counts manifest passes and records report semantics
TARGET_HAPPY="transactx_restore_happy"
assert_success "Valid populated restore matching embedded-counts manifest passes" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_HAPPY" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$ACCEPTANCE_MANIFEST"

# Verify generated report contents for populated restore
REPORT_FILE="$(ls -1 "$REPORT_DIR"/restore-"$TARGET_HAPPY"-*.json | head -n1)"
node -e '
  const fs = require("fs");
  const r = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
  if (r.checks.threeSchemas !== true) throw new Error("threeSchemas should be true");
  if (r.checks.nonnegativeBalances !== true) throw new Error("nonnegativeBalances should be true");
  if (r.checks.balancedCentralLedger !== true) throw new Error("balancedCentralLedger should be true");
  if (r.checks.noOrphanIdempotency !== true) throw new Error("noOrphanIdempotency should be true");
  if (r.checks.coreTablesEmptyVerified !== false) throw new Error("coreTablesEmptyVerified should be false for populated restore");
  if (r.checks.populationContractSatisfied !== true) throw new Error("populationContractSatisfied should be true for populated restore");
  if (r.checks.populatedDataVerified !== true) throw new Error("populatedDataVerified should be true");
  if (r.checks.completenessVerified !== true) throw new Error("completenessVerified should be true");
  if (r.emptyStateAllowed !== false) throw new Error("emptyStateAllowed should be false");
  if (r.emptyStateScope !== "none") throw new Error("emptyStateScope should be none for populated restore");
  if (r.counts.payments !== 1) throw new Error("payments count incorrect");
  if (r.counts.accounts !== 2) throw new Error("accounts count incorrect");
  if (!r.populationContract.startsWith("manifest:")) throw new Error("populationContract incorrect: " + r.populationContract);
  console.log("Verified populated report JSON structure and completeness flags.");
' "$REPORT_FILE"

# Test 9b: Valid populated restore matching top-level manifest shape passes
TOPLEVEL_MANIFEST="$TEST_TMP/toplevel_manifest.json"
cat << 'EOF' > "$TOPLEVEL_MANIFEST"
{
  "description": "Top-level manifest shape",
  "minPayments": 1,
  "accounts": 2,
  "bankAOperations": 1,
  "bankBOperations": 1,
  "idempotencyRecords": 1
}
EOF
TARGET_TOPLEVEL="transactx_restore_toplevel"
assert_success "Valid populated restore matching top-level manifest shape passes" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_TOPLEVEL" \
  --report-dir "$REPORT_DIR" \
  --expected-counts "$TOPLEVEL_MANIFEST"

# Test 10a: --allow-empty fails when payments table contains populated rows
TARGET_EMPTY_FAIL_PAYMENTS="transactx_restore_empty_fail_payments"
export MOCK_PAYMENTS="1"
export MOCK_ACCOUNTS="0"
export MOCK_BANK_A_OPS="0"
export MOCK_BANK_B_OPS="0"
export MOCK_IDEMPOTENCY="0"
assert_failure "--allow-empty fails when payments table contains populated rows" 1 "Empty-state restore check failed: declared empty-state scope" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_EMPTY_FAIL_PAYMENTS" \
  --report-dir "$REPORT_DIR" \
  --allow-empty

# Test 10b: --allow-empty fails when accounts table contains populated rows
TARGET_EMPTY_FAIL_ACCOUNTS="transactx_restore_empty_fail_accounts"
export MOCK_PAYMENTS="0"
export MOCK_ACCOUNTS="2"
export MOCK_BANK_A_OPS="0"
export MOCK_BANK_B_OPS="0"
export MOCK_IDEMPOTENCY="0"
assert_failure "--allow-empty fails when accounts table contains populated rows" 1 "Empty-state restore check failed: declared empty-state scope" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_EMPTY_FAIL_ACCOUNTS" \
  --report-dir "$REPORT_DIR" \
  --allow-empty

# Test 10c: Explicit --allow-empty passes for strictly unpopulated database
TARGET_EMPTY="transactx_restore_empty"
export MOCK_PAYMENTS="0"
export MOCK_ACCOUNTS="0"
export MOCK_BANK_A_OPS="0"
export MOCK_BANK_B_OPS="0"
export MOCK_IDEMPOTENCY="0"
assert_success "Explicit --allow-empty passes for strictly unpopulated database" \
  "$RESTORE_SCRIPT" \
  --backup "$FAKE_BACKUP" \
  --identity "$SECURE_KEY" \
  --sha256 "$BACKUP_HASH" \
  --source-sha "$SOURCE_SHA" \
  --target-db "$TARGET_EMPTY" \
  --report-dir "$REPORT_DIR" \
  --allow-empty

EMPTY_REPORT_FILE="$(ls -1 "$REPORT_DIR"/restore-"$TARGET_EMPTY"-*.json | head -n1)"
node -e '
  const fs = require("fs");
  const r = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
  if (r.checks.threeSchemas !== true) throw new Error("threeSchemas should be true");
  if (r.checks.nonnegativeBalances !== true) throw new Error("nonnegativeBalances should be true");
  if (r.checks.balancedCentralLedger !== true) throw new Error("balancedCentralLedger should be true");
  if (r.checks.noOrphanIdempotency !== true) throw new Error("noOrphanIdempotency should be true");
  if (r.checks.coreTablesEmptyVerified !== true) throw new Error("coreTablesEmptyVerified must be true for allow-empty");
  if (r.checks.populationContractSatisfied !== true) throw new Error("populationContractSatisfied should be true for allow-empty");
  if (r.checks.populatedDataVerified !== false) throw new Error("populatedDataVerified must be false for allow-empty");
  if (r.checks.completenessVerified !== false) throw new Error("completenessVerified must be false for allow-empty");
  if (r.emptyStateAllowed !== true) throw new Error("emptyStateAllowed must be true for allow-empty");
  if (r.emptyStateScope !== "public.payments,public.accounts,bank_a.operations,bank_b.operations,public.idempotency_records") {
    throw new Error("emptyStateScope incorrect: " + r.emptyStateScope);
  }
  if (r.populationContract !== "allow_empty") throw new Error("populationContract incorrect: " + r.populationContract);
  console.log("Verified allow-empty report structure, core-empty scope and negative completeness flags.");
' "$EMPTY_REPORT_FILE"

echo "=== All 32 Offline Regression Tests Passed Successfully ==="
