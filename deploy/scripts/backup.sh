#!/usr/bin/env bash
set -euo pipefail
umask 077

for name in pg_dump pg_dumpall age sha256sum stat realpath; do
  command -v "$name" >/dev/null || { echo "Missing required command: $name" >&2; exit 1; }
done
[[ "${PGDATABASE:-}" == 'transactx' ]] || { echo 'PGDATABASE must name the intended TransactX database.' >&2; exit 2; }
[[ -n "${PGHOST:-}" && -n "${PGUSER:-}" ]] || { echo 'PGHOST and PGUSER are required.' >&2; exit 2; }
[[ -n "${PGPASSFILE:-}" && -f "$PGPASSFILE" ]] || { echo 'PGPASSFILE must exist and be owner-only.' >&2; exit 2; }
unsafe_passfile="$(find "$PGPASSFILE" -perm /077 -print)" || { echo 'Cannot inspect PGPASSFILE permissions.' >&2; exit 2; }
[[ -z "$unsafe_passfile" ]] || { echo 'PGPASSFILE must be owner-only.' >&2; exit 2; }
[[ "${BACKUP_AGE_RECIPIENT:-}" =~ ^age1[a-z0-9]{20,}$ ]] || { echo 'A public age recipient is required.' >&2; exit 2; }
[[ "${BACKUP_DIR:-}" == /* && "${BACKUP_DIR:-}" != '/' ]] || { echo 'An absolute private BACKUP_DIR is required.' >&2; exit 2; }
[[ "${BACKUP_MAX_BYTES:-2147483648}" =~ ^[1-9][0-9]*$ ]] || { echo 'BACKUP_MAX_BYTES must be positive.' >&2; exit 2; }
max_bytes="${BACKUP_MAX_BYTES:-2147483648}"
mkdir -p "$BACKUP_DIR"
backup_dir="$(realpath -e "$BACKUP_DIR")"
[[ "$backup_dir" != '/' ]] || { echo 'Unsafe backup directory.' >&2; exit 2; }
if [[ -n "${BACKUP_OFFVM_DIR:-}" ]]; then
  [[ "$BACKUP_OFFVM_DIR" == /* && "$BACKUP_OFFVM_DIR" != "$backup_dir" ]] || { echo 'Invalid off-VM target.' >&2; exit 2; }
  offvm_dir="$(realpath -e "$BACKUP_OFFVM_DIR")"
  [[ "$offvm_dir" != "$backup_dir" ]] || { echo 'Off-VM target resolves to the local backup directory.' >&2; exit 2; }
  mountpoint -q "$offvm_dir" || { echo 'Off-VM target is not mounted.' >&2; exit 1; }
else
  offvm_dir=''
fi

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
temp="$(mktemp "$backup_dir/.transactx-$stamp.XXXXXXXX.dump.age")"
case "$temp" in "$backup_dir"/.transactx-*.dump.age) ;; *) echo 'Unsafe temporary path.' >&2; exit 1 ;; esac
globals_temp="$(mktemp "$backup_dir/.transactx-$stamp.XXXXXXXX.globals.sql.age")"
case "$globals_temp" in "$backup_dir"/.transactx-*.globals.sql.age) ;; *) echo 'Unsafe temporary path.' >&2; exit 1 ;; esac
trap '[[ -z "${temp:-}" || ! -f "$temp" ]] || rm -f -- "$temp"; [[ -z "${globals_temp:-}" || ! -f "$globals_temp" ]] || rm -f -- "$globals_temp"' EXIT
final="$backup_dir/transactx-$stamp.dump.age"
globals_final="$backup_dir/transactx-$stamp.globals.sql.age"
[[ ! -e "$final" && ! -e "$globals_final" ]] || { echo 'Backup name already exists.' >&2; exit 1; }

# A single pg_dump snapshot covers central, bank_a and bank_b schemas.
pg_dumpall --globals-only --no-role-passwords --no-password -l "$PGDATABASE" | age -r "$BACKUP_AGE_RECIPIENT" -o "$globals_temp"
pg_dump --format=custom --no-owner --no-acl --no-password | age -r "$BACKUP_AGE_RECIPIENT" -o "$temp"
bytes="$(stat -c %s "$temp")"
globals_bytes="$(stat -c %s "$globals_temp")"
(( bytes > 0 && globals_bytes > 0 && bytes + globals_bytes <= max_bytes )) || { echo 'Encrypted backup is empty or above its size cap.' >&2; exit 1; }
mv -- "$temp" "$final"
temp=''
mv -- "$globals_temp" "$globals_final"
globals_temp=''
(cd "$backup_dir" && sha256sum "$(basename "$final")" > "$(basename "$final.sha256")")
(cd "$backup_dir" && sha256sum "$(basename "$globals_final")" > "$(basename "$globals_final.sha256")")
if [[ -n "$offvm_dir" ]]; then
  install -m 0600 "$final" "$offvm_dir/$(basename "$final")"
  install -m 0600 "$final.sha256" "$offvm_dir/$(basename "$final.sha256")"
  install -m 0600 "$globals_final" "$offvm_dir/$(basename "$globals_final")"
  install -m 0600 "$globals_final.sha256" "$offvm_dir/$(basename "$globals_final.sha256")"
  echo 'Encrypted backup copied to configured mounted target.'
else
  echo 'Encrypted backup stored locally only; independent off-VM copy is pending.'
fi

# Keep seven daily files and two weekly Sunday copies. Prune only our exact
# encrypted naming pattern after a successful backup and target copy.
shopt -s nullglob
if [[ "$(date -u +%u)" == '7' ]]; then
  mkdir -p "$backup_dir/weekly"
  install -m 0600 "$final" "$backup_dir/weekly/$(basename "$final")"
  install -m 0600 "$final.sha256" "$backup_dir/weekly/$(basename "$final.sha256")"
  install -m 0600 "$globals_final" "$backup_dir/weekly/$(basename "$globals_final")"
  install -m 0600 "$globals_final.sha256" "$backup_dir/weekly/$(basename "$globals_final.sha256")"
fi
prune() {
  local dir="$1" keep="$2" files=() index=0 file
  files=("$dir"/transactx-*.dump.age)
  ((${#files[@]} > 0)) || return 0
  mapfile -t files < <(printf '%s\n' "$dir"/transactx-*.dump.age | sort -r)
  for file in "${files[@]}"; do
    ((index+=1))
    if ((index <= keep)); then continue; fi
    [[ "$(realpath -m "$(dirname "$file")")" == "$dir" ]] || { echo 'Unsafe retention path.' >&2; exit 1; }
    local related="${file%.dump.age}.globals.sql.age"
    rm -f -- "$file" "$file.sha256" "$related" "$related.sha256"
  done
}
prune "$backup_dir" 7
if [[ -d "$backup_dir/weekly" ]]; then prune "$backup_dir/weekly" 2; fi
if [[ -n "$offvm_dir" ]]; then prune "$offvm_dir" 7; fi
echo "Encrypted backup complete: $(basename "$final") ($bytes bytes)."
