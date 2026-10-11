#!/usr/bin/env bash
set -euo pipefail

api=''
frontend=''
auth_header_file=''
while (($#)); do
  case "$1" in
    --api-origin) api="${2:-}"; shift 2 ;;
    --frontend-origin) frontend="${2:-}"; shift 2 ;;
    --auth-header-file) auth_header_file="${2:-}"; shift 2 ;;
    *) echo "Unknown argument: $1" >&2; exit 2 ;;
  esac
done
[[ "$api" =~ ^https://[A-Za-z0-9.-]+(:[0-9]+)?$ ]] || { echo 'Exact HTTPS API origin required.' >&2; exit 2; }
[[ "$frontend" =~ ^https://[A-Za-z0-9.-]+(:[0-9]+)?$ ]] || { echo 'Exact HTTPS frontend origin required.' >&2; exit 2; }
command -v curl >/dev/null || { echo 'curl is required.' >&2; exit 1; }
headers="$(mktemp)"
body="$(mktemp)"
trap 'rm -f -- "$headers" "$body"' EXIT

curl --fail --silent --show-error --max-time 10 "$api/health" > "$body"
curl --fail --silent --show-error --max-time 10 "$api/health/db" > "$body"
curl --fail --silent --show-error --max-time 10 "$frontend/console/worklist" > "$body"
grep -q '<div id="root"' "$body" || { echo 'Frontend deep link did not serve the SPA.' >&2; exit 1; }

curl --silent --show-error --max-time 10 --request OPTIONS \
  --header "Origin: $frontend" --header 'Access-Control-Request-Method: GET' \
  --dump-header "$headers" --output /dev/null "$api/api/ops/worklist"
grep -Fiq "Access-Control-Allow-Origin: $frontend" "$headers" || { echo 'Allowed CORS origin was not returned.' >&2; exit 1; }
curl --silent --show-error --max-time 10 --request OPTIONS \
  --header 'Origin: https://blocked.example.invalid' --header 'Access-Control-Request-Method: GET' \
  --dump-header "$headers" --output /dev/null "$api/api/ops/worklist"
if grep -Fiq 'Access-Control-Allow-Origin: https://blocked.example.invalid' "$headers"; then
  echo 'Disallowed CORS origin was accepted.' >&2; exit 1
fi

if [[ -n "$auth_header_file" ]]; then
  [[ -f "$auth_header_file" ]] || { echo 'Auth header file not found.' >&2; exit 2; }
  if [[ -n "$(find "$auth_header_file" -perm /077 -print)" ]]; then echo 'Auth header file must be owner-only.' >&2; exit 2; fi
  curl --fail --silent --show-error --max-time 10 --header "@$auth_header_file" "$api/api/ops/worklist" > "$body"
  grep -q '"data"' "$body" || { echo 'Authenticated worklist response is not the API envelope.' >&2; exit 1; }
  set +e
  timeout 12s curl --silent --show-error --no-buffer --header "@$auth_header_file" "$api/api/ops/events/stream" > "$body"
  sse_status=$?
  set -e
  [[ $sse_status -eq 0 || $sse_status -eq 124 ]] || { echo "SSE request failed: $sse_status" >&2; exit 1; }
  grep -Eq '^event: (ready|invalidate|stale)' "$body" || { echo 'No SSE event was observed.' >&2; exit 1; }
  echo 'HTTPS health, SPA refresh, CORS, authenticated worklist and SSE passed.'
else
  echo 'HTTPS health, SPA refresh and CORS passed. Authenticated worklist/SSE not run (no header file).'
fi
echo 'Confirm public bank and database port denial from an external host before declaring deployment complete.'
