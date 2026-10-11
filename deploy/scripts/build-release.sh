#!/usr/bin/env bash
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
output="$repo/deploy/out"
validate_only=false
allow_incomplete=false
allow_dirty=false
for arg in "$@"; do
  case "$arg" in
    --validate-only) validate_only=true ;;
    --allow-incomplete) allow_incomplete=true ;;
    --allow-dirty) allow_dirty=true ;;
    *) echo "Unknown argument: $arg" >&2; exit 2 ;;
  esac
done
command -v go >/dev/null || { echo 'Go is required.' >&2; exit 1; }
command -v npm >/dev/null || { echo 'npm is required.' >&2; exit 1; }
command -v node >/dev/null || { echo 'Node.js is required.' >&2; exit 1; }
sha="$(git -C "$repo" rev-parse HEAD)"
[[ "$sha" =~ ^[0-9a-f]{40}$ ]] || { echo 'Invalid source SHA.' >&2; exit 1; }
dirty=false
[[ -z "$(git -C "$repo" status --porcelain)" ]] || dirty=true
commands=(api bank-a bank-b worker migrate maintain-merkle)
missing=()
for command in "${commands[@]}"; do
  [[ -d "$repo/backend/cmd/$command" ]] || missing+=("$command")
done
if ((${#missing[@]})) && ! $allow_incomplete; then
  echo "Required v2 commands missing: ${missing[*]}" >&2; exit 1
fi
if $validate_only; then
  printf 'Source: %s\nDirty tree: %s\nMissing planned commands: %s\nValidation only: no archive was written.\n' "$sha" "$dirty" "${missing[*]:-none}"
  exit 0
fi
if $dirty && ! $allow_dirty; then echo 'Git tree is dirty; commit and rerun.' >&2; exit 1; fi

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
name="transactx-${sha:0:12}-$stamp"
stage="$output/$name"
[[ ! -e "$stage" ]] || { echo "Output already exists: $stage" >&2; exit 1; }
mkdir -p "$stage/bin" "$stage/migrations" "$stage/frontend" "$stage/deploy" "$stage/docs"

(cd "$repo/frontend" && npm ci && npm test && npm run lint && npm run build)
for arch in amd64 arm64; do
  mkdir -p "$stage/bin/linux-$arch"
  for command in "${commands[@]}"; do
    [[ -d "$repo/backend/cmd/$command" ]] || continue
    (cd "$repo/backend" && GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -buildvcs=true -trimpath -ldflags '-s -w' -o "$stage/bin/linux-$arch/transactx-$command" "./cmd/$command")
  done
done
cp "$repo"/backend/migrations/*.sql "$stage/migrations/"
cp -R "$repo/frontend/dist/." "$stage/frontend/"
cp -R "$repo/deploy/env" "$repo/deploy/systemd" "$repo/deploy/caddy" "$repo/deploy/scripts" "$stage/deploy/"
cp "$repo/deploy/README.md" "$stage/deploy/README.md"
cp "$repo/README.md" "$stage/docs/README.md"
cp "$repo/docs/next-gen/execution-pack/docs/next-gen/DEPLOYMENT_AND_BACKUP.md" "$stage/docs/DEPLOYMENT_AND_BACKUP.md"
(cd "$repo/backend" && go list -m all) > "$stage/go-dependencies.txt"
cp "$repo/frontend/package-lock.json" "$stage/frontend-package-lock.json"

export TX_RELEASE_SHA="$sha" TX_RELEASE_DIRTY="$dirty" TX_RELEASE_MISSING="${missing[*]:-}"
export TX_RELEASE_GO_VERSION="$(go version)" TX_RELEASE_NODE_VERSION="$(node --version)"
node -e 'const fs=require("fs"); const p=process.env; fs.writeFileSync(process.argv[1], JSON.stringify({schemaVersion:"tx.release.v1",sourceSha:p.TX_RELEASE_SHA,createdAt:new Date().toISOString(),dirtyTree:p.TX_RELEASE_DIRTY==="true",complete:!p.TX_RELEASE_MISSING,missingCommands:p.TX_RELEASE_MISSING?p.TX_RELEASE_MISSING.split(" "):[],goVersion:p.TX_RELEASE_GO_VERSION,nodeVersion:p.TX_RELEASE_NODE_VERSION,targetArchitectures:["linux-amd64","linux-arm64"]},null,2)+"\n")' "$stage/metadata.json"
(cd "$stage" && find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS)
archive="$output/$name.tar.gz"
tar -C "$stage" -czf "$archive" .
sha256sum "$archive" > "$archive.sha256"
printf 'Archive: %s\nComplete v2 bundle: %s\n' "$archive" "$([[ ${#missing[@]} -eq 0 ]] && echo true || echo false)"
