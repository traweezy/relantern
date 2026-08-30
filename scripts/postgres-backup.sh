#!/usr/bin/env bash
set -euo pipefail

compose=(docker compose -f compose.yaml)
backup_directory="${BACKUP_DIRECTORY:-dist/backups}"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
backup_path="${1:-${backup_directory}/relantern-${timestamp}.dump}"

if [[ "${backup_path}" != *.dump ]]; then
  printf 'Backup path must end in .dump.\n' >&2
  exit 64
fi
if [[ -e "${backup_path}" ]]; then
  printf 'Refusing to overwrite existing backup %s.\n' "${backup_path}" >&2
  exit 1
fi

resolved_project="$("${compose[@]}" config | awk '$1 == "name:" { print $2; exit }')"
if [[ "${resolved_project}" != "relantern" ]]; then
  printf 'Refusing backup: expected Compose project relantern, resolved %s.\n' "${resolved_project}" >&2
  exit 1
fi
if ! "${compose[@]}" ps --status running --services | rg -qx postgres; then
  printf 'The Relantern PostgreSQL service must be running.\n' >&2
  exit 1
fi

umask 077
mkdir -p -- "$(dirname -- "${backup_path}")"
temporary_path="${backup_path}.partial"
trap 'test ! -f "${temporary_path}" || mv -- "${temporary_path}" "${temporary_path}.incomplete"' EXIT

"${compose[@]}" exec -T postgres sh -ec \
  'export PGPASSWORD="$(cat /run/secrets/database_password)"; exec pg_dump --format=custom --compress=9 --no-owner --no-privileges --dbname=relantern --username=relantern' \
  >"${temporary_path}"
"${compose[@]}" exec -T postgres pg_restore --list <"${temporary_path}" >/dev/null
mv -- "${temporary_path}" "${backup_path}"
chmod 600 "${backup_path}"
trap - EXIT

sha256sum "${backup_path}" >"${backup_path}.sha256"
chmod 600 "${backup_path}.sha256"
printf '%s\n' "${backup_path}"
