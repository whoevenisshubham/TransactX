#!/usr/bin/env bash
set -euo pipefail

archive=''
expected_hash=''
source_sha=''
validate_only=false
activate=false
required_commands=(api bank-a bank-b worker migrate maintain-merkle corpus rebuild-worklist)
while (($#)); do
  case "$1" in
    --archive) archive="${2:-}"; shift 2 ;;
    --sha256) expected_hash="${2:-}"; shift 2 ;;
    --source-sha) source_sha="${2:-}"; shift 2 ;;
    --validate-only) validate_only=true; shift ;;
    --activate) activate=true; shift ;;
    *) echo "Unknown argument: $1" >&2; exit 2 ;;
  esac
done
[[ -f "$archive" ]] || { echo 'An existing archive is required.' >&2; exit 2; }
[[ "$expected_hash" =~ ^[0-9a-f]{64}$ ]] || { echo 'A lowercase SHA-256 archive hash is required.' >&2; exit 2; }
[[ "$source_sha" =~ ^[0-9a-f]{40}$ ]] || { echo 'A full lowercase source SHA is required.' >&2; exit 2; }
command -v sha256sum >/dev/null || { echo 'sha256sum is required.' >&2; exit 1; }
command -v tar >/dev/null || { echo 'tar is required.' >&2; exit 1; }
actual_hash="$(sha256sum "$archive" | cut -d ' ' -f1)"
[[ "$actual_hash" == "$expected_hash" ]] || { echo 'Archive SHA-256 mismatch.' >&2; exit 1; }
while IFS= read -r entry; do
  case "$entry" in
    /*|../*|*/../*|./../*) echo 'Unsafe archive path.' >&2; exit 1 ;;
  esac
done < <(tar -tzf "$archive")
metadata="$(tar -xOzf "$archive" ./metadata.json 2>/dev/null)" || { echo 'Archive metadata is missing.' >&2; exit 1; }
printf '%s\n' "$metadata" | grep -Eq "\"sourceSha\"[[:space:]]*:[[:space:]]*\"$source_sha\"" || { echo 'Source SHA mismatch.' >&2; exit 1; }
complete=false
if printf '%s\n' "$metadata" | grep -Eq '"complete"[[:space:]]*:[[:space:]]*true'; then complete=true; fi
if $complete; then
  members="$(tar -tzf "$archive")"
  for arch in linux-amd64 linux-arm64; do
    for command in "${required_commands[@]}"; do
      grep -Fxq "./bin/$arch/transactx-$command" <<< "$members" || { echo "Missing $arch binary: $command" >&2; exit 1; }
    done
  done
  grep -Fxq './docs/runbooks/corpus-manifest.json' <<< "$members" || { echo 'Approved corpus manifest is missing.' >&2; exit 1; }
fi

prefix=/opt/transactx
release="$prefix/releases/$source_sha"
if $validate_only; then
  echo "Archive hash, member paths and metadata valid for source $source_sha. Complete: $complete."
  echo 'Validation only: no release was installed.'
  exit 0
fi
if ! $complete; then echo 'Incomplete bundle cannot be installed.' >&2; exit 1; fi
[[ $EUID -eq 0 ]] || { echo 'Run as root for /opt/transactx installation.' >&2; exit 1; }
case "$(uname -m)" in
  x86_64) arch=linux-amd64 ;;
  aarch64) arch=linux-arm64 ;;
  *) echo 'Only Linux amd64 and arm64 are supported.' >&2; exit 1 ;;
esac
mkdir -p "$prefix/releases"
if [[ ! -d "$release" ]]; then
  stage="$(mktemp -d "$prefix/releases/.install.XXXXXXXX")"
  [[ "$stage" == "$prefix/releases/.install."* ]] || { echo 'Invalid staging path.' >&2; exit 1; }
  tar -C "$stage" -xzf "$archive"
  (cd "$stage" && sha256sum -c SHA256SUMS >/dev/null)
  grep -Eq '"complete"[[:space:]]*:[[:space:]]*true' "$stage/metadata.json" || { echo 'Incomplete bundle cannot be installed.' >&2; exit 1; }
  grep -Eq "\"sourceSha\"[[:space:]]*:[[:space:]]*\"$source_sha\"" "$stage/metadata.json" || { echo 'Source SHA mismatch.' >&2; exit 1; }
  find "$stage/deploy/scripts" -maxdepth 1 -type f -name '*.sh' -exec chmod 0755 {} +
  mkdir -p "$stage/run"
  for command in "${required_commands[@]}"; do
    binary="$stage/bin/$arch/transactx-$command"
    [[ -f "$binary" ]] || { echo "Missing $arch binary: $command" >&2; exit 1; }
    chmod 0755 "$binary"
    ln -s "../bin/$arch/transactx-$command" "$stage/run/transactx-$command"
  done
  chown -R transactx:transactx "$stage"
  mv "$stage" "$release"
else
  (cd "$release" && sha256sum -c SHA256SUMS >/dev/null)
  echo 'Release already installed; existing manifest verified.'
fi
if $activate; then
  [[ ! -e "$prefix/current" || -L "$prefix/current" ]] || { echo 'current exists and is not a symlink.' >&2; exit 1; }
  previous=''
  [[ ! -L "$prefix/current" ]] || previous="$(readlink -f "$prefix/current")"
  next="$prefix/.current-next-$$"
  ln -s "$release" "$next"
  mv -Tf "$next" "$prefix/current"
  if [[ -n "$previous" && "$previous" != "$release" ]]; then ln -sfn "$previous" "$prefix/previous"; fi
  echo "Activated $source_sha. Start/restart services only after migrations and environment checks."
else
  echo "Installed $source_sha. Apply reviewed migrations, then rerun with --activate."
fi
