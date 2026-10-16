#!/usr/bin/env bash
set -euo pipefail

# Offline regression test suite for run-cross-bank-disposable.sh
# Uses isolated command stubs for createdb, dropdb, psql, go, node.
# Zero PostgreSQL connection or network access.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
ORCHESTRATOR_SCRIPT="$REPO_ROOT/backend/scripts/run-cross-bank-disposable.sh"

TEST_TMP="$(mktemp -d)"
export TEST_TMP
trap 'rm -rf -- "$TEST_TMP"' EXIT

STUB_BIN="$TEST_TMP/stub_bin"
mkdir -p "$STUB_BIN"
LOG_FILE="$TEST_TMP/commands.log"
export LOG_FILE

# Find real node for URL parsing (zero network, pure JS)
REAL_NODE="$(command -v node)"

# Create stubs
cat << 'EOF' > "$STUB_BIN/createdb"
#!/usr/bin/env bash
echo "createdb [PGDATABASE=${PGDATABASE:-}, PGSSLMODE=${PGSSLMODE:-}] $*" >> "$LOG_FILE"
if [[ "${MOCK_CREATEDB_FAIL:-0}" == "1" ]]; then
  echo "createdb: error: database creation failed" >&2
  exit 1
fi
exit 0
EOF
chmod +x "$STUB_BIN/createdb"

cat << 'EOF' > "$STUB_BIN/dropdb"
#!/usr/bin/env bash
echo "dropdb [PGDATABASE=${PGDATABASE:-}, PGSSLMODE=${PGSSLMODE:-}] $*" >> "$LOG_FILE"
if [[ "${MOCK_DROPDB_FAIL:-0}" == "1" ]]; then
  echo "dropdb: error: database drop failed" >&2
  exit 1
fi
exit 0
EOF
chmod +x "$STUB_BIN/dropdb"

cat << EOF > "$STUB_BIN/node"
#!/usr/bin/env bash
exec "$REAL_NODE" "\$@"
EOF
chmod +x "$STUB_BIN/node"

cat << 'EOF' > "$STUB_BIN/psql"
#!/usr/bin/env bash
echo "psql [PGDATABASE=${PGDATABASE:-}, PGSSLMODE=${PGSSLMODE:-}] $*" >> "$LOG_FILE"
args="$*"
if [[ "$args" =~ SELECT[[:space:]]+count\(\*\)[[:space:]]+FROM[[:space:]]+pg_database ]]; then
  if [[ "${MOCK_MAINT_CONNECT_FAIL:-0}" == "1" ]]; then
    echo "psql: error: could not connect to server" >&2
    exit 2
  fi
  # If dropdb was already invoked, we are in the post-teardown verification phase
  if grep -q '^dropdb' "$LOG_FILE" 2>/dev/null; then
    if [[ "${MOCK_DROPDB_CATALOG_STUCK:-0}" == "1" ]]; then
      echo "1"
      exit 0
    fi
    echo "0"
    exit 0
  fi
  if [[ "${MOCK_PREEXISTING_DB:-0}" == "1" ]]; then
    echo "1"
    exit 0
  fi
  echo "0"
  exit 0
fi

if [[ "$args" =~ -f[[:space:]]+.*\.up\.sql ]]; then
  if [[ "${MOCK_MIGRATION_FAIL:-0}" == "1" ]]; then
    echo "psql: error: migration syntax error" >&2
    exit 1
  fi
  exit 0
fi

exit 0
EOF
chmod +x "$STUB_BIN/psql"

cat << 'EOF' > "$STUB_BIN/go"
#!/usr/bin/env bash
echo "go $* [DATABASE_URL=$DATABASE_URL, ORCHESTRATED=${TRANSACTX_DISPOSABLE_DB_ORCHESTRATED:-0}, CONTRACT_FILE=${TRANSACTX_DISPOSABLE_CONTRACT_FILE:-NONE}, TOKEN=${TRANSACTX_DISPOSABLE_INVOCATION_TOKEN:-NONE}]" >> "$LOG_FILE"

# Normalize contract file path for MSYS/Cygwin/Linux
norm_contract="${TRANSACTX_DISPOSABLE_CONTRACT_FILE:-}"
if command -v cygpath >/dev/null 2>&1 && [[ -n "$norm_contract" ]]; then
  norm_contract="$(cygpath -u "$norm_contract" 2>/dev/null || echo "$norm_contract")"
fi

# Record the contract file location and verify its existence at test execution time
if [[ -n "$norm_contract" ]]; then
  echo "$norm_contract" > "$TEST_TMP/last_observed_contract_path"
  if [[ -f "$norm_contract" ]]; then
    echo "EXISTS" >> "$TEST_TMP/last_observed_contract_path"
    cat "$norm_contract" > "$TEST_TMP/last_observed_contract_content"
  else
    echo "MISSING" >> "$TEST_TMP/last_observed_contract_path"
  fi
fi

if [[ "${MOCK_GO_TEST_FAIL:-0}" == "1" ]]; then
  echo "FAIL: simulated test failure" >&2
  exit "${MOCK_GO_TEST_EXIT_CODE:-1}"
fi

if [[ "${MOCK_GO_TEST_UNEXPECTED_CLEANUP:-0}" == "1" ]]; then
  echo "FAIL: unexpected cleanup error (status=UNEXPECTED_CLEANUP_ERROR): connection refused" >&2
  exit 1
fi

if [[ "${MOCK_GO_TEST_IMMUTABLE_HISTORY:-0}" == "1" ]]; then
  if [[ "${TRANSACTX_DISPOSABLE_DB_ORCHESTRATED:-0}" != "1" ]]; then
    echo "FAIL: in-process cleanup blocked by immutable transition history (no orchestrator)" >&2
    exit 1
  fi
  if [[ -z "$norm_contract" ]] || [[ ! -f "$norm_contract" ]]; then
    echo "FAIL: in-process cleanup blocked by immutable transition history: missing lifecycle contract" >&2
    exit 1
  fi
  if [[ -z "${TRANSACTX_DISPOSABLE_INVOCATION_TOKEN:-}" ]]; then
    echo "FAIL: in-process cleanup blocked by immutable transition history: missing invocation token in test environment" >&2
    exit 1
  fi
  if ! grep -q "\"database\":[[:space:]]*\"$PGDATABASE\"" "$norm_contract"; then
    echo "FAIL: contract database mismatch" >&2
    exit 1
  fi
  if ! grep -q "\"invocationToken\":[[:space:]]*\"$TRANSACTX_DISPOSABLE_INVOCATION_TOKEN\"" "$norm_contract"; then
    echo "FAIL: contract invocation token mismatch" >&2
    exit 1
  fi
  echo "PASS: immutable history cleanup delegated to orchestrator with valid contract and matching invocation token"
  exit 0
fi

echo "PASS"
exit 0
EOF
chmod +x "$STUB_BIN/go"

# Create isolated system path without host PostgreSQL binaries
CLEAN_SYS_PATH=$(echo "$PATH" | tr ':' '\n' | grep -v -i "PostgreSQL" | tr '\n' ':' | sed 's/:$//')

TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

run_scenario() {
  local num="$1"
  local desc="$2"
  local expected_exit="$3"
  shift 3

  TOTAL_TESTS=$((TOTAL_TESTS + 1))
  echo "------------------------------------------------------------"
  echo "Running Scenario $num: $desc"
  > "$LOG_FILE"
  rm -f "$TEST_TMP/last_observed_contract_path" "$TEST_TMP/last_observed_contract_content" 2>/dev/null || true

  set +e
  PATH="$STUB_BIN:$CLEAN_SYS_PATH" "$@" bash "$ORCHESTRATOR_SCRIPT" \
    --base-url "postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable" \
    > "$TEST_TMP/out.log" 2> "$TEST_TMP/err.log"
  local actual_exit=$?
  set -e

  if [[ $actual_exit -eq $expected_exit ]]; then
    echo "  [PASS] Exit code matches expected: $actual_exit"
    PASSED_TESTS=$((PASSED_TESTS + 1))
    return 0
  else
    echo "  [FAIL] Expected exit code $expected_exit, got $actual_exit" >&2
    echo "  Stdout:" >&2
    cat "$TEST_TMP/out.log" >&2
    echo "  Stderr:" >&2
    cat "$TEST_TMP/err.log" >&2
    FAILED_TESTS=$((FAILED_TESTS + 1))
    return 1
  fi
}

echo "Starting TransactX Disposable DB Orchestrator Offline Validation..."

# Scenario 1: Successful scenario execution and successful teardown
run_scenario 1 "Successful scenario execution and successful teardown" 0 \
  env MOCK_CREATEDB_FAIL=0 MOCK_DROPDB_FAIL=0 MOCK_GO_TEST_FAIL=0
# Verify command sequence
if grep -q '^createdb' "$LOG_FILE" && \
   grep -q '^go test' "$LOG_FILE" && \
   grep -q '^dropdb' "$LOG_FILE"; then
  echo "  [PASS] Command sequence verified: createdb -> go test -> dropdb"
else
  echo "  [FAIL] Command sequence missing expected commands" >&2
  cat "$LOG_FILE" >&2
  FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# Verify created and dropped database names are exactly the same generated identifier
created_db=$(grep '^createdb' "$LOG_FILE" | head -n1 | awk '{print $NF}')
dropped_db=$(grep '^dropdb' "$LOG_FILE" | head -n1 | awk '{print $NF}')
if [[ -n "$created_db" && "$created_db" == "$dropped_db" && "$created_db" =~ ^transactx_disposable_ci_ ]]; then
  echo "  [PASS] Target created ($created_db) exactly matches target dropped ($dropped_db)"
else
  echo "  [FAIL] Created ($created_db) and dropped ($dropped_db) identifiers do not match or are invalid" >&2
  FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# Verify explicit maintenance-db and sslmode propagation on createdb, dropdb, and psql
if grep -q '^createdb.*--maintenance-db=postgres' "$LOG_FILE" && \
   grep -q '^dropdb.*--maintenance-db=postgres' "$LOG_FILE"; then
  echo "  [PASS] Explicit maintenance database '--maintenance-db=postgres' verified on createdb and dropdb"
else
  echo "  [FAIL] createdb or dropdb missing explicit --maintenance-db=postgres" >&2
  cat "$LOG_FILE" >&2
  FAILED_TESTS=$((FAILED_TESTS + 1))
fi

if grep -q '^psql.*-d postgres' "$LOG_FILE"; then
  echo "  [PASS] Explicit maintenance database '-d postgres' verified on psql existence check"
else
  echo "  [FAIL] psql missing explicit -d postgres" >&2
  cat "$LOG_FILE" >&2
  FAILED_TESTS=$((FAILED_TESTS + 1))
fi

if grep -q '^createdb.*PGSSLMODE=disable' "$LOG_FILE" && \
   grep -q '^dropdb.*PGSSLMODE=disable' "$LOG_FILE"; then
  echo "  [PASS] PGSSLMODE=disable verified on createdb and dropdb"
else
  echo "  [FAIL] createdb or dropdb missing PGSSLMODE=disable" >&2
  cat "$LOG_FILE" >&2
  FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# Scenario 2: Scenario test failure followed by successful teardown
run_scenario 2 "Scenario test failure followed by successful teardown" 1 \
  env MOCK_GO_TEST_FAIL=1 MOCK_DROPDB_FAIL=0
if grep -q '^dropdb' "$LOG_FILE"; then
  echo "  [PASS] Teardown was executed despite test failure"
else
  echo "  [FAIL] Teardown dropdb was NOT called after test failure" >&2
  FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# Scenario 3: Immutable-history cleanup classification followed by successful teardown
run_scenario 3 "Immutable-history cleanup classification followed by successful teardown" 0 \
  env MOCK_GO_TEST_IMMUTABLE_HISTORY=1 MOCK_DROPDB_FAIL=0
if grep -q 'ORCHESTRATED=1' "$LOG_FILE" && \
   grep -q 'TOKEN=tx_token_' "$LOG_FILE" && \
   grep -q '^dropdb' "$LOG_FILE"; then
  echo "  [PASS] Orchestrator contract and invocation token passed to tests and teardown succeeded"
else
  echo "  [FAIL] Orchestrator contract/token not passed or teardown missing" >&2
  FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# Verify contract file existed at test time and was cleaned up after orchestrator completed
last_contract_file=$(head -n 1 "$TEST_TMP/last_observed_contract_path" 2>/dev/null || echo "")
last_contract_exists=$(sed -n '2p' "$TEST_TMP/last_observed_contract_path" 2>/dev/null || echo "")
if [[ -n "$last_contract_file" && "$last_contract_exists" == "EXISTS" ]]; then
  echo "  [PASS] Contract file was verified present at test execution time"
  if grep -q '"invocationToken": "tx_token_' "$TEST_TMP/last_observed_contract_content" 2>/dev/null; then
    echo "  [PASS] Contract file contained verified invocationToken field"
  else
    echo "  [FAIL] Contract file missing invocationToken field!" >&2
    FAILED_TESTS=$((FAILED_TESTS + 1))
  fi
  if [[ ! -f "$last_contract_file" ]]; then
    echo "  [PASS] Contract file was safely deleted by orchestrator cleanup"
  else
    echo "  [FAIL] Contract file still exists on disk after orchestrator cleanup!" >&2
    FAILED_TESTS=$((FAILED_TESTS + 1))
  fi
else
  echo "  [FAIL] Contract file was missing or not observed at test execution time (path: '$last_contract_file', exists: '$last_contract_exists')" >&2
  FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# Scenario 4: Unexpected cleanup error resulting in failure
run_scenario 4 "Unexpected cleanup error resulting in failure" 1 \
  env MOCK_GO_TEST_UNEXPECTED_CLEANUP=1 MOCK_DROPDB_FAIL=0
if grep -q '^dropdb' "$LOG_FILE"; then
  echo "  [PASS] Unexpected cleanup failure propagated while teardown executed"
else
  echo "  [FAIL] Teardown not executed" >&2
  FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# Scenario 5: Database creation failure
run_scenario 5 "Database creation failure" 2 \
  env MOCK_CREATEDB_FAIL=1
if grep -q '^createdb' "$LOG_FILE" && ! grep -q '^dropdb' "$LOG_FILE"; then
  echo "  [PASS] Creation failed and teardown was skipped safely"
else
  echo "  [FAIL] Unexpected commands recorded on creation failure" >&2
  FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# Scenario 6: Migration failure
run_scenario 6 "Migration failure" 1 \
  env MOCK_MIGRATION_FAIL=1
if grep -q '^createdb' "$LOG_FILE" && grep -q '^dropdb' "$LOG_FILE" && ! grep -q '^go test' "$LOG_FILE"; then
  echo "  [PASS] Migration failed, tests skipped, and teardown executed"
else
  echo "  [FAIL] Migration failure handling did not match expected lifecycle" >&2
  FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# Scenario 7: Teardown failure
run_scenario 7 "Teardown failure (dropdb command failure)" 1 \
  env MOCK_DROPDB_FAIL=1
if grep -q '^dropdb' "$LOG_FILE"; then
  echo "  [PASS] Teardown failure correctly caused non-zero exit"
else
  echo "  [FAIL] dropdb was not called" >&2
  FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# Scenario 7b: Teardown failure (catalog check shows DB still exists)
run_scenario 7 "Teardown failure (catalog check stuck)" 1 \
  env MOCK_DROPDB_CATALOG_STUCK=1
if grep -q '^dropdb' "$LOG_FILE"; then
  echo "  [PASS] Stuck catalog check correctly caused non-zero exit"
else
  echo "  [FAIL] dropdb was not called" >&2
  FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# Scenario 8: Protected or non-disposable database target rejected before mutation
run_scenario 8 "Pre-existing database target rejected before mutation" 2 \
  env MOCK_PREEXISTING_DB=1
if ! grep -q '^createdb' "$LOG_FILE"; then
  echo "  [PASS] Pre-existing DB rejected before createdb was called"
else
  echo "  [FAIL] createdb was invoked on pre-existing database!" >&2
  FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# Scenario 9: Missing required tool or configuration rejected safely
TOTAL_TESTS=$((TOTAL_TESTS + 1))
echo "------------------------------------------------------------"
echo "Running Scenario 9: Missing required tool rejected safely"
> "$LOG_FILE"
set +e
# Hide createdb by running with a restricted PATH without createdb
RESTRICTED_BIN="$TEST_TMP/restricted_bin"
mkdir -p "$RESTRICTED_BIN"
cp "$STUB_BIN/dropdb" "$RESTRICTED_BIN/"
cp "$STUB_BIN/psql" "$RESTRICTED_BIN/"
cp "$STUB_BIN/go" "$RESTRICTED_BIN/"
cp "$STUB_BIN/node" "$RESTRICTED_BIN/"
PATH="$RESTRICTED_BIN:$CLEAN_SYS_PATH" bash "$ORCHESTRATOR_SCRIPT" \
  --base-url "postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable" \
  > "$TEST_TMP/out.log" 2> "$TEST_TMP/err.log"
scen9_exit=$?
set -e
if [[ $scen9_exit -eq 2 ]] && ! grep -q '^createdb' "$LOG_FILE"; then
  echo "  [PASS] Missing createdb tool rejected with exit code 2 and no mutation"
  PASSED_TESTS=$((PASSED_TESTS + 1))
else
  echo "  [FAIL] Expected exit code 2, got $scen9_exit" >&2
  cat "$TEST_TMP/err.log" >&2
  FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# Scenario 10: Teardown attempted after a simulated test failure (command order verification)
TOTAL_TESTS=$((TOTAL_TESTS + 1))
echo "------------------------------------------------------------"
echo "Running Scenario 10: Command order verification for teardown after test failure"
> "$LOG_FILE"
set +e
PATH="$STUB_BIN:$CLEAN_SYS_PATH" env MOCK_GO_TEST_FAIL=1 bash "$ORCHESTRATOR_SCRIPT" \
  --base-url "postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable" \
  > "$TEST_TMP/out.log" 2> "$TEST_TMP/err.log"
scen10_exit=$?
set -e
# Verify command sequence: createdb before go test, go test before dropdb
createdb_line=$(grep -n '^createdb' "$LOG_FILE" | head -n1 | cut -d: -f1 || echo 0)
go_line=$(grep -n '^go test' "$LOG_FILE" | head -n1 | cut -d: -f1 || echo 0)
dropdb_line=$(grep -n '^dropdb' "$LOG_FILE" | head -n1 | cut -d: -f1 || echo 0)

if [[ $scen10_exit -eq 1 ]] && \
   [[ $createdb_line -gt 0 ]] && \
   [[ $go_line -gt $createdb_line ]] && \
   [[ $dropdb_line -gt $go_line ]]; then
  echo "  [PASS] Order verified: createdb (line $createdb_line) -> go test (line $go_line) -> dropdb (line $dropdb_line)"
  PASSED_TESTS=$((PASSED_TESTS + 1))
else
  echo "  [FAIL] Command ordering failed: createdb=$createdb_line, go=$go_line, dropdb=$dropdb_line, exit=$scen10_exit" >&2
  cat "$LOG_FILE" >&2
  FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# ============================================================
# Checkpoint 8H-4: Connection-Target Safety Tests
# ============================================================

run_target_url_negative_test() {
  local num="$1"
  local desc="$2"
  local bad_url="$3"

  TOTAL_TESTS=$((TOTAL_TESTS + 1))
  echo "------------------------------------------------------------"
  echo "Running Connection Target Safety Case $num: $desc"
  > "$LOG_FILE"

  set +e
  PATH="$STUB_BIN:$CLEAN_SYS_PATH" bash "$ORCHESTRATOR_SCRIPT" \
    --base-url "$bad_url" \
    > "$TEST_TMP/out.log" 2> "$TEST_TMP/err.log"
  local actual_exit=$?
  set -e

  if [[ $actual_exit -eq 2 ]] && [[ ! -s "$LOG_FILE" ]]; then
    echo "  [PASS] Rejected with exit code 2 and zero mutations/commands attempted"
    PASSED_TESTS=$((PASSED_TESTS + 1))
    return 0
  else
    echo "  [FAIL] Expected exit code 2 and empty log, got exit $actual_exit with log contents:" >&2
    cat "$LOG_FILE" >&2
    echo "  Stderr:" >&2
    cat "$TEST_TMP/err.log" >&2
    FAILED_TESTS=$((FAILED_TESTS + 1))
    return 1
  fi
}

run_scenario_with_url() {
  local num="$1"
  local desc="$2"
  local expected_exit="$3"
  local custom_url="$4"
  shift 4

  TOTAL_TESTS=$((TOTAL_TESTS + 1))
  echo "------------------------------------------------------------"
  echo "Running Scenario $num: $desc"
  > "$LOG_FILE"

  set +e
  PATH="$STUB_BIN:$CLEAN_SYS_PATH" "$@" bash "$ORCHESTRATOR_SCRIPT" \
    --base-url "$custom_url" \
    > "$TEST_TMP/out.log" 2> "$TEST_TMP/err.log"
  local actual_exit=$?
  set -e

  if [[ $actual_exit -eq $expected_exit ]]; then
    echo "  [PASS] Exit code matches expected: $actual_exit"
    PASSED_TESTS=$((PASSED_TESTS + 1))
    return 0
  else
    echo "  [FAIL] Expected exit code $expected_exit, got $actual_exit" >&2
    echo "  Stdout:" >&2
    cat "$TEST_TMP/out.log" >&2
    echo "  Stderr:" >&2
    cat "$TEST_TMP/err.log" >&2
    FAILED_TESTS=$((FAILED_TESTS + 1))
    return 1
  fi
}

# Negative Case 1: Protected database 'transactx' rejected
run_target_url_negative_test "CT-1" "Base URL targeting protected 'transactx' database rejected before mutation" \
  "postgres://postgres:postgres@127.0.0.1:5432/transactx"

# Negative Case 2: Protected database 'transactx_ci' rejected
run_target_url_negative_test "CT-2" "Base URL targeting protected 'transactx_ci' database rejected before mutation" \
  "postgres://postgres:postgres@127.0.0.1:5432/transactx_ci"

# Negative Case 3: Unapproved remote host rejected
run_target_url_negative_test "CT-3" "Unapproved remote database host rejected before mutation" \
  "postgres://postgres:postgres@db.production.example.com:5432/postgres"

# Negative Case 4: Non-loopback IP host rejected
run_target_url_negative_test "CT-4" "Non-loopback IP host rejected before mutation" \
  "postgres://postgres:postgres@192.168.1.100:5432/postgres"

# Negative Case 5: Unsupported scheme rejected
run_target_url_negative_test "CT-5" "Unsupported URL scheme (http:) rejected before mutation" \
  "http://127.0.0.1:5432/postgres"

# Negative Case 6: Malformed URL rejected
run_target_url_negative_test "CT-6" "Malformed connection URL rejected before mutation" \
  "://not-a-valid-url"

# Negative Case 7: Embedded path segments rejected
run_target_url_negative_test "CT-7" "Embedded path segments rejected before mutation" \
  "postgres://127.0.0.1:5432/postgres/extra"

# Negative Case 8: Unexpected query parameters rejected
run_target_url_negative_test "CT-8" "Unexpected query parameters rejected before mutation" \
  "postgres://127.0.0.1:5432/postgres?sslmode=disable&unexpected_param=1"

# Positive Case 9: Supported loopback host 'localhost' accepted
run_scenario_with_url "CT-9" "Supported loopback host 'localhost' accepted" 0 \
  "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable" \
  env MOCK_CREATEDB_FAIL=0 MOCK_DROPDB_FAIL=0 MOCK_GO_TEST_FAIL=0

# Positive Case 10: Supported loopback host '[::1]' accepted
run_scenario_with_url "CT-10" "Supported loopback host '[::1]' accepted" 0 \
  "postgres://postgres:postgres@[::1]:5432/postgres?sslmode=disable" \
  env MOCK_CREATEDB_FAIL=0 MOCK_DROPDB_FAIL=0 MOCK_GO_TEST_FAIL=0

# Positive Case 11: Non-default database username explicitly receives approved maintenance database
TOTAL_TESTS=$((TOTAL_TESTS + 1))
echo "------------------------------------------------------------"
echo "Running Scenario CT-11: Non-default database username ('custom_ci_user') uses approved maintenance database"
> "$LOG_FILE"
set +e
PATH="$STUB_BIN:$CLEAN_SYS_PATH" env MOCK_CREATEDB_FAIL=0 MOCK_DROPDB_FAIL=0 MOCK_GO_TEST_FAIL=0 \
  bash "$ORCHESTRATOR_SCRIPT" \
  --base-url "postgres://custom_ci_user:custom_pass@127.0.0.1:5432/postgres?sslmode=disable" \
  > "$TEST_TMP/out.log" 2> "$TEST_TMP/err.log"
scen_ct11_exit=$?
set -e

ct11_created_db=$(grep '^createdb' "$LOG_FILE" | head -n1 | awk '{print $NF}')
ct11_dropped_db=$(grep '^dropdb' "$LOG_FILE" | head -n1 | awk '{print $NF}')

if [[ $scen_ct11_exit -eq 0 ]] && \
   grep -q '^createdb.*--maintenance-db=postgres' "$LOG_FILE" && \
   grep -q '^dropdb.*--maintenance-db=postgres' "$LOG_FILE" && \
   grep -q '^psql.*-d postgres.*SELECT count' "$LOG_FILE" && \
   [[ -n "$ct11_created_db" && "$ct11_created_db" == "$ct11_dropped_db" && "$ct11_created_db" =~ ^transactx_disposable_ci_ ]]; then
  echo "  [PASS] Non-default user 'custom_ci_user' correctly used --maintenance-db=postgres and -d postgres"
  PASSED_TESTS=$((PASSED_TESTS + 1))
else
  echo "  [FAIL] Non-default user failed maintenance-db verification: exit=$scen_ct11_exit" >&2
  cat "$LOG_FILE" >&2
  FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# Negative Case 12: Invalid sslmode rejected before mutation
run_target_url_negative_test "CT-12" "Invalid sslmode value rejected before mutation" \
  "postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=invalid_mode"

# Positive Case 13: Non-disable sslmode ('require') propagates to tools
run_scenario_with_url "CT-13" "Supported non-disable sslmode ('require') propagates PGSSLMODE" 0 \
  "postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=require" \
  env MOCK_CREATEDB_FAIL=0 MOCK_DROPDB_FAIL=0 MOCK_GO_TEST_FAIL=0
if grep -q 'PGSSLMODE=require' "$LOG_FILE"; then
  echo "  [PASS] PGSSLMODE=require verified across tool invocations"
else
  echo "  [FAIL] PGSSLMODE=require missing from log" >&2
  cat "$LOG_FILE" >&2
  FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# ============================================================
# Checkpoint 8H-7: Duplicate Query Parameter Safety Tests
# ============================================================

# Negative Case 14: Duplicate identical sslmode parameters rejected before mutation
run_target_url_negative_test "CT-14" "Duplicate identical sslmode parameters rejected before mutation" \
  "postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable&sslmode=disable"

# Negative Case 15: Duplicate conflicting sslmode parameters rejected before mutation
run_target_url_negative_test "CT-15" "Duplicate conflicting sslmode parameters rejected before mutation" \
  "postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable&sslmode=require"

# Negative Case 16: Percent-encoded duplicate parameter names rejected before mutation
run_target_url_negative_test "CT-16" "Percent-encoded duplicate sslmode parameter rejected before mutation" \
  "postgres://postgres:postgres@127.0.0.1:5432/postgres?%73%73%6c%6d%6f%64%65=disable&sslmode=disable"

# ============================================================
# Checkpoint 8H-11 & 8H-13: Lifecycle Teardown Outcomes Regression Tests
# ============================================================

# Outcome 7: Test failure (exit 1) combined with failed teardown retains failure and reports violation
run_scenario 17 "Test failure combined with failed teardown retains failure and reports violation" 1 \
  env MOCK_GO_TEST_FAIL=1 MOCK_DROPDB_FAIL=1
if grep -q '^dropdb' "$LOG_FILE" && grep -q 'Teardown contract violation' "$TEST_TMP/err.log"; then
  echo "  [PASS] Teardown was attempted after test failure, and teardown failure reported"
else
  echo "  [FAIL] Dropdb was not attempted or teardown failure was not reported" >&2
  cat "$LOG_FILE" >&2
  cat "$TEST_TMP/err.log" >&2
  FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# Outcome 7b: Test failure with distinct exit code (42) combined with failed teardown preserves exit 42 and reports violation
run_scenario "17b" "Test failure (exit 42) combined with failed teardown preserves nonzero exit 42 and reports violation" 42 \
  env MOCK_GO_TEST_FAIL=1 MOCK_GO_TEST_EXIT_CODE=42 MOCK_DROPDB_FAIL=1
if grep -q '^dropdb' "$LOG_FILE" && grep -q 'Teardown contract violation' "$TEST_TMP/err.log"; then
  echo "  [PASS] Nonzero test exit 42 preserved and teardown failure reported"
else
  echo "  [FAIL] Exit 42 not preserved or teardown failure was not reported" >&2
  cat "$LOG_FILE" >&2
  cat "$TEST_TMP/err.log" >&2
  FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# Outcome 6: Test failure with distinct exit code (42) followed by successful teardown preserves exit 42
run_scenario 18 "Test failure (exit 42) followed by successful teardown preserves nonzero exit 42" 42 \
  env MOCK_GO_TEST_FAIL=1 MOCK_GO_TEST_EXIT_CODE=42 MOCK_DROPDB_FAIL=0
if grep -q '^dropdb' "$LOG_FILE"; then
  echo "  [PASS] Teardown was executed and nonzero test exit 42 preserved"
else
  echo "  [FAIL] Teardown dropdb was NOT called after test failure" >&2
  FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# Outcome 8a: Successful tests followed by failed teardown (dropdb failure) returns nonzero (1)
run_scenario 19 "Successful tests followed by failed teardown (dropdb failure) returns nonzero (1)" 1 \
  env MOCK_GO_TEST_FAIL=0 MOCK_DROPDB_FAIL=1
if grep -q '^dropdb' "$LOG_FILE" && grep -q 'Teardown contract violation' "$TEST_TMP/err.log"; then
  echo "  [PASS] Successful test followed by dropdb failure returned nonzero (1) and reported violation"
else
  echo "  [FAIL] Teardown failure not reported or dropdb not called" >&2
  cat "$LOG_FILE" >&2
  cat "$TEST_TMP/err.log" >&2
  FAILED_TESTS=$((FAILED_TESTS + 1))
fi

# Outcome 8b: Successful tests followed by failed teardown (catalog check stuck) returns nonzero (1)
run_scenario 20 "Successful tests followed by failed teardown (catalog check stuck) returns nonzero (1)" 1 \
  env MOCK_GO_TEST_FAIL=0 MOCK_DROPDB_CATALOG_STUCK=1
if grep -q '^dropdb' "$LOG_FILE" && grep -q 'Teardown contract violation' "$TEST_TMP/err.log"; then
  echo "  [PASS] Successful test followed by catalog stuck check returned nonzero (1) and reported violation"
else
  echo "  [FAIL] Teardown failure not reported or dropdb not called" >&2
  cat "$LOG_FILE" >&2
  cat "$TEST_TMP/err.log" >&2
  FAILED_TESTS=$((FAILED_TESTS + 1))
fi

echo "============================================================"
echo "Offline Validation Summary:"
echo "  Total Scenarios: $TOTAL_TESTS"
echo "  Passed:          $PASSED_TESTS"
echo "  Failed:          $FAILED_TESTS"
echo "============================================================"

if [[ $FAILED_TESTS -gt 0 ]]; then
  exit 1
fi
exit 0
