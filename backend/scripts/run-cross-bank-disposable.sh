#!/usr/bin/env bash
set -euo pipefail

# TransactX Dedicated Disposable Test Database Lifecycle Orchestrator
#
# Lifecyle stages:
# 1. Validate environment, CLI tools (createdb, dropdb, psql, go, node), and base connection.
# 2. Generate a unique disposable database name under an authorized prefix (transactx_disposable_ci_).
# 3. Strictly reject protected database names (transactx, postgres, template0, template1) and existing databases.
# 4. Create fresh isolated disposable database via createdb.
# 5. Apply migrations from backend/migrations using psql.
# 6. Execute designated cross-bank integration tests with TRANSACTX_DISPOSABLE_DB_ORCHESTRATED=1 and DATABASE_URL.
# 7. Guaranteed teardown (dropdb) in trap handler regardless of test pass/fail.
# 8. Fail-closed return status: non-zero if tests fail, migration fails, or teardown fails.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

base_url="${DATABASE_URL:-}"
test_pattern='^(TestCrossBankRecovery_|TestCrashMatrix_)'
migrations_dir="$REPO_ROOT/backend/migrations"
test_pkg='./internal/experiments'
allow_incomplete=false

while (($#)); do
  case "$1" in
    --base-url) base_url="${2:-}"; shift 2 ;;
    --test-pattern) test_pattern="${2:-}"; shift 2 ;;
    --migrations-dir) migrations_dir="${2:-}"; shift 2 ;;
    --test-pkg) test_pkg="${2:-}"; shift 2 ;;
    --help|-h)
      echo "Usage: $0 [options]"
      echo "Options:"
      echo "  --base-url <url>        Base maintenance connection URL (defaults to DATABASE_URL or postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable)"
      echo "  --test-pattern <pat>    Go test run regex (defaults to '^(TestCrossBankRecovery_|TestCrashMatrix_)')"
      echo "  --migrations-dir <dir>  Path to migrations directory (defaults to backend/migrations)"
      echo "  --test-pkg <pkg>        Go test package path (defaults to './internal/experiments')"
      exit 0
      ;;
    *) echo "Unknown option: $1" >&2; exit 2 ;;
  esac
done

if [[ -z "$base_url" ]]; then
  base_url="postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable"
fi

# 1. Validate required tools
for tool in createdb dropdb psql go node; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "Required tool '$tool' is not available on PATH." >&2
    exit 2
  }
done

# Validate migrations directory
[[ -d "$migrations_dir" ]] || {
  echo "Migrations directory '$migrations_dir' does not exist." >&2
  exit 2
}

# 1. State tracking and default initialization
db_created=false
test_exit_code=0
teardown_failed=false
in_cleanup=false
interrupted=false
db_name=""
BASE_MAINT_DB="postgres"
contract_file=""

cleanup() {
  local trap_exit=$?
  if $in_cleanup; then
    return
  fi
  in_cleanup=true
  trap - EXIT INT TERM

  if $db_created; then
    echo "[orchestrator] Tearing down disposable test database '${db_name}'..."
    if ! PGDATABASE="${BASE_MAINT_DB:-postgres}" dropdb --maintenance-db="${BASE_MAINT_DB:-postgres}" --if-exists "${db_name}"; then
      echo "[orchestrator] ERROR: Failed to drop disposable database '${db_name}'." >&2
      teardown_failed=true
    else
      # Verify removal from catalog
      local check_count
      check_count="$(PGDATABASE="${BASE_MAINT_DB:-postgres}" psql -d "${BASE_MAINT_DB:-postgres}" --no-psqlrc --tuples-only --no-align -c "SELECT count(*) FROM pg_database WHERE datname = '${db_name}'" 2>/dev/null || echo "1")"
      if [[ "${check_count//[[:space:]]/}" != "0" ]]; then
        echo "[orchestrator] ERROR: Database '${db_name}' still exists in catalog after dropdb." >&2
        teardown_failed=true
      else
        echo "[orchestrator] Successfully destroyed disposable test database '${db_name}'."
      fi
    fi
  fi

  # Clean up lifecycle contract marker file if created
  if [[ -n "${contract_file:-}" && -f "${contract_file:-}" ]]; then
    rm -f "$contract_file" 2>/dev/null || true
    contract_file=""
  fi

  if $teardown_failed; then
    echo "[orchestrator] Teardown contract violation: database destruction was incomplete." >&2
    if [[ $test_exit_code -ne 0 ]]; then
      exit "$test_exit_code"
    fi
    exit 1
  fi

  if $interrupted; then
    echo "[orchestrator] Execution interrupted by signal." >&2
    if [[ $trap_exit -ne 0 ]]; then
      exit "$trap_exit"
    fi
    exit 130
  fi

  if [[ $test_exit_code -ne 0 ]]; then
    exit "$test_exit_code"
  fi

  if [[ $trap_exit -ne 0 ]]; then
    exit "$trap_exit"
  fi

  exit 0
}

on_signal() {
  interrupted=true
  cleanup
}

trap cleanup EXIT
trap on_signal INT TERM

# 2. Parse and strictly validate base connection URL
parsed_exports="$(node -e '
  const raw = process.argv[1];
  try {
    const u = new URL(raw);

    // 1. Protocol validation: must be postgres: or postgresql:
    if (u.protocol !== "postgres:" && u.protocol !== "postgresql:") {
      console.error("Invalid database URL scheme " + JSON.stringify(u.protocol) + ": only postgres: or postgresql: is authorized.");
      process.exit(2);
    }

    // 2. Host allowlist: strictly loopback hosts for test isolation
    const allowedHosts = new Set(["127.0.0.1", "localhost", "::1", "[::1]"]);
    const host = u.hostname || "";
    if (!allowedHosts.has(host)) {
      console.error("Refusing unapproved non-loopback host " + JSON.stringify(host) + ": only loopback test services (127.0.0.1, localhost, ::1) are authorized.");
      process.exit(2);
    }

    // 3. Port validation: valid TCP port
    const port = u.port || "5432";
    const portNum = parseInt(port, 10);
    if (isNaN(portNum) || portNum < 1 || portNum > 65535) {
      console.error("Invalid port " + JSON.stringify(port) + " in database URL.");
      process.exit(2);
    }

    // 4. Maintenance database validation:
    // Pathname must be either empty, "/", or strictly "/postgres"
    const rawPath = u.pathname.replace(/^\/+/, "");
    if (rawPath.includes("/")) {
      console.error("Invalid database path " + JSON.stringify(u.pathname) + ": embedded path segments are not allowed.");
      process.exit(2);
    }
    const maintDB = (rawPath || "postgres").toLowerCase();
    if (maintDB !== "postgres") {
      console.error("Refusing maintenance database " + JSON.stringify(rawPath) + ": only the dedicated maintenance database (\"postgres\") is authorized. Targeting protected databases like \"transactx\" or \"transactx_ci\" is strictly forbidden.");
      process.exit(2);
    }

    // 5. Query parameter validation: only allow a single sslmode parameter;
    // reject duplicate query parameter names (including percent-encoded forms and identical values)
    if (u.search && u.search.length > 1) {
      const rawPairs = u.search.slice(1).split("&");
      const seenRaw = new Set();
      for (const pair of rawPairs) {
        if (!pair) continue;
        const rawKey = pair.split("=")[0];
        let decKey = rawKey;
        try {
          decKey = decodeURIComponent(rawKey);
        } catch (e) {}
        const normKey = decKey.toLowerCase();
        if (normKey !== "sslmode") {
          console.error("Unexpected query parameter " + JSON.stringify(rawKey) + " in base database URL.");
          process.exit(2);
        }
        if (seenRaw.has(normKey)) {
          console.error("Duplicate query parameter " + JSON.stringify(rawKey) + " in base database URL: repeated parameters are strictly forbidden.");
          process.exit(2);
        }
        seenRaw.add(normKey);
      }
    }

    const seenParams = new Set();
    for (const [k, v] of u.searchParams.entries()) {
      let decK = k;
      try {
        decK = decodeURIComponent(k);
      } catch (e) {}
      const normK = decK.toLowerCase();
      if (normK !== "sslmode") {
        console.error("Unexpected query parameter " + JSON.stringify(k) + " in base database URL.");
        process.exit(2);
      }
      if (seenParams.has(normK)) {
        console.error("Duplicate query parameter " + JSON.stringify(k) + " in base database URL: repeated parameters are strictly forbidden.");
        process.exit(2);
      }
      seenParams.add(normK);
    }

    const sslmodeValues = u.searchParams.getAll("sslmode");
    if (sslmodeValues.length > 1) {
      console.error("Duplicate query parameter \"sslmode\" in base database URL: repeated parameters are strictly forbidden.");
      process.exit(2);
    }

    const sslmode = sslmodeValues.length === 1 ? sslmodeValues[0] : null;
    if (sslmode !== null) {
      const allowedSslModes = new Set(["disable", "allow", "prefer", "require", "verify-ca", "verify-full"]);
      if (!allowedSslModes.has(sslmode.toLowerCase())) {
        console.error("Invalid sslmode " + JSON.stringify(sslmode) + " in database URL: only disable, allow, prefer, require, verify-ca, verify-full are permitted.");
        process.exit(2);
      }
      console.log("export PGSSLMODE=" + JSON.stringify(sslmode.toLowerCase()));
    }

    console.log("export PGHOST=" + JSON.stringify(host));
    console.log("export PGPORT=" + JSON.stringify(port));
    console.log("export PGUSER=" + JSON.stringify(decodeURIComponent(u.username || "postgres")));
    if (u.password) {
      console.log("export PGPASSWORD=" + JSON.stringify(decodeURIComponent(u.password)));
    }
    console.log("export BASE_MAINT_DB=" + JSON.stringify("postgres"));
    console.log("export PGDATABASE=" + JSON.stringify("postgres"));
  } catch (e) {
    console.error("Invalid base database URL: " + e.message);
    process.exit(2);
  }
' "$base_url")" || {
  echo "Failed to validate base PostgreSQL connection URL." >&2
  exit 2
}
eval "$parsed_exports"

# 3. Generate unique disposable database name
unique_suffix="$(date +%s)_$$"
readonly db_name="transactx_disposable_ci_${unique_suffix}"

# Safety validation on database name
[[ "$db_name" =~ ^transactx_disposable_ci_[0-9_]+$ ]] || {
  echo "Safety violation: database name '$db_name' does not match authorized prefix pattern." >&2
  exit 2
}

lower_name="$(echo "$db_name" | tr '[:upper:]' '[:lower:]')"
case "$lower_name" in
  transactx|postgres|template0|template1)
    echo "Safety violation: refusing protected database name '$db_name'." >&2
    exit 2
    ;;
esac

# Validate base database connection and ensure target database does not already exist
existing_count="$(PGDATABASE="$BASE_MAINT_DB" psql -d "$BASE_MAINT_DB" --no-psqlrc --tuples-only --no-align -c "SELECT count(*) FROM pg_database WHERE datname = '$db_name'" 2>&1)" || {
  echo "Failed to connect to PostgreSQL maintenance database '$BASE_MAINT_DB': $existing_count" >&2
  exit 2
}

if [[ "${existing_count//[[:space:]]/}" != "0" ]]; then
  echo "Refusing to target already existing database '$db_name'." >&2
  exit 2
fi

# Prepare target database URL
target_url="$(node -e '
  const u = new URL(process.argv[1]);
  u.pathname = "/" + process.argv[2];
  console.log(u.toString());
' "$base_url" "$db_name")"

# 5. Create fresh disposable database
echo "[orchestrator] Creating fresh disposable test database '$db_name'..."
PGDATABASE="$BASE_MAINT_DB" createdb --maintenance-db="$BASE_MAINT_DB" "$db_name" || {
  echo "[orchestrator] Failed to create database '$db_name'." >&2
  exit 2
}
db_created=true

# 6. Apply migrations
echo "[orchestrator] Applying schema migrations to '$db_name'..."
migration_count=0
for f in "$migrations_dir"/*.up.sql; do
  [[ -f "$f" ]] || continue
  psql "$target_url" -v ON_ERROR_STOP=1 -f "$f" >/dev/null 2>&1 || {
    echo "[orchestrator] Migration failed on file $(basename "$f")." >&2
    exit 1
  }
  migration_count=$((migration_count + 1))
done
if [[ $migration_count -eq 0 ]]; then
  echo "[orchestrator] ERROR: No migration files found in '$migrations_dir'." >&2
  exit 1
fi
echo "[orchestrator] Applied $migration_count migrations successfully."

# 7. Establish orchestrator lifecycle ownership contract marker
# Merely setting TRANSACTX_DISPOSABLE_DB_ORCHESTRATED=1 without this verifiable contract marker
# will cause Go test cleanup validation to fail-closed, preventing unauthorized delegation.
# The unique per-invocation token ensures the contract cannot be replayed or inherited across runs.
readonly invocation_token="tx_token_${unique_suffix}_$RANDOM"
contract_file="$(mktemp "${TMPDIR:-/tmp}/tx_disposable_contract_${db_name}_XXXXXX.json" 2>/dev/null || mktemp -t "tx_disposable_contract_${db_name}.XXXXXX")"
cat << EOF > "$contract_file"
{
  "version": "tx.orchestrator.contract.v1",
  "database": "$db_name",
  "invocationToken": "$invocation_token",
  "orchestratorPid": $$,
  "createdAt": "$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date +%Y-%m-%dT%H:%M:%SZ)"
}
EOF
echo "[orchestrator] Established lifecycle ownership contract marker at '$contract_file'."

# 8. Execute designated disposable integration tests
echo "[orchestrator] Executing dedicated disposable integration tests (pattern: $test_pattern)..."
set +e
(
  cd "$REPO_ROOT/backend"
  export PGDATABASE="$db_name"
  export DATABASE_URL="$target_url"
  export TRANSACTX_DISPOSABLE_DB_ORCHESTRATED=1
  export TRANSACTX_DISPOSABLE_CONTRACT_FILE="$contract_file"
  export TRANSACTX_DISPOSABLE_INVOCATION_TOKEN="$invocation_token"
  if command -v cygpath >/dev/null 2>&1; then
    export TRANSACTX_DISPOSABLE_CONTRACT_FILE="$(cygpath -w "$contract_file")"
  fi
  go test -v -count=1 "$test_pkg" -run "$test_pattern"
)
test_exit_code=$?
set -e

if [[ $test_exit_code -ne 0 ]]; then
  echo "[orchestrator] Test execution failed with exit code $test_exit_code." >&2
else
  echo "[orchestrator] Test execution completed successfully."
fi

# The EXIT trap will now execute teardown and set the final exit status
