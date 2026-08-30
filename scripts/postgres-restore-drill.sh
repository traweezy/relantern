#!/usr/bin/env bash
set -euo pipefail

compose=(docker compose -f compose.yaml)
backup_path="${1:-}"
if [[ -z "${backup_path}" ]]; then
  backup_path="$(bash scripts/postgres-backup.sh)"
fi
if [[ ! -f "${backup_path}" || "${backup_path}" != *.dump ]]; then
  printf 'A readable custom-format .dump backup is required.\n' >&2
  exit 64
fi

resolved_project="$("${compose[@]}" config | awk '$1 == "name:" { print $2; exit }')"
if [[ "${resolved_project}" != "relantern" ]]; then
  printf 'Refusing restore drill: expected Compose project relantern, resolved %s.\n' "${resolved_project}" >&2
  exit 1
fi

started_epoch="$(date -u +%s)"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
restore_database="relantern_restore_${timestamp//[^0-9A-Za-z]/_}_$$"
if [[ ! "${restore_database}" =~ ^relantern_restore_[0-9A-Za-z_]+$ ]]; then
  printf 'Refusing unexpected restore database name.\n' >&2
  exit 1
fi

drop_restore_database() {
  "${compose[@]}" exec -T postgres sh -ec \
    'export PGPASSWORD="$(cat /run/secrets/database_password)"; exec dropdb --if-exists --force --username=relantern "$1"' \
    _ "${restore_database}" >/dev/null
}
trap drop_restore_database EXIT

"${compose[@]}" exec -T postgres sh -ec \
  'export PGPASSWORD="$(cat /run/secrets/database_password)"; exec createdb --username=relantern "$1"' \
  _ "${restore_database}"
"${compose[@]}" exec -T postgres sh -ec \
  'export PGPASSWORD="$(cat /run/secrets/database_password)"; exec pg_restore --exit-on-error --no-owner --no-privileges --dbname="$1" --username=relantern' \
  _ "${restore_database}" <"${backup_path}"

verification="$("${compose[@]}" exec -T postgres sh -ec \
  'export PGPASSWORD="$(cat /run/secrets/database_password)"; exec psql --no-psqlrc --tuples-only --no-align --set=ON_ERROR_STOP=1 --username=relantern --dbname="$1" --command="select concat_ws('"'"'|'"'"', (select max(version_id) from public.goose_db_version where is_applied), (select count(*) from app.users), (select count(*) from app.sources), (select count(*) from app.schedule_definitions), (select count(*) from app.audit_events), (select count(*) from app.digests));"' \
  _ "${restore_database}")"
IFS='|' read -r migration_version user_count source_count schedule_count audit_count digest_count <<<"${verification}"
if [[ ! "${migration_version}" =~ ^[0-9]+$ || "${migration_version}" -lt 19 ]]; then
  printf 'Restored migration version %s is below the required baseline.\n' "${migration_version}" >&2
  exit 1
fi

completed_epoch="$(date -u +%s)"
rto_seconds="$((completed_epoch - started_epoch))"
backup_epoch="$(stat -c %Y "${backup_path}")"
rpo_seconds="$((started_epoch - backup_epoch))"
if (( rpo_seconds < 0 )); then
  rpo_seconds=0
fi
if (( rpo_seconds > 86400 || rto_seconds > 14400 )); then
  printf 'Restore drill missed RPO/RTO: rpo=%ss rto=%ss.\n' "${rpo_seconds}" "${rto_seconds}" >&2
  exit 1
fi

report_directory="dist/restore-drills"
report_path="${report_directory}/${timestamp}.json"
mkdir -p -- "${report_directory}"
backup_sha256="$(sha256sum "${backup_path}" | awk '{print $1}')"
git_sha="$(git rev-parse HEAD)"
verification_counts="{\"auditEvents\":${audit_count},\"digests\":${digest_count},\"schedules\":${schedule_count},\"sources\":${source_count},\"users\":${user_count}}"
printf "insert into app.restore_drills (backup_sha256, release_git_sha, state, rpo_seconds, rto_seconds, rpo_target_seconds, rto_target_seconds, restored_migration_version, verification_counts, started_at, completed_at) values (decode('%s', 'hex'), '%s', 'passed', %s, %s, 86400, 14400, %s, '%s'::jsonb, to_timestamp(%s), to_timestamp(%s)) on conflict (backup_sha256, started_at) do nothing;\n" \
  "${backup_sha256}" "${git_sha}" "${rpo_seconds}" "${rto_seconds}" "${migration_version}" \
  "${verification_counts}" "${started_epoch}" "${completed_epoch}" | \
  "${compose[@]}" exec -T postgres sh -ec \
    'export PGPASSWORD="$(cat /run/secrets/database_password)"; exec psql --no-psqlrc --set=ON_ERROR_STOP=1 --username=relantern --dbname=relantern' \
    >/dev/null
printf '{\n  "auditCount": %s,\n  "backupSha256": "%s",\n  "digestCount": %s,\n  "gitSha": "%s",\n  "migrationVersion": %s,\n  "result": "pass",\n  "rpoSeconds": %s,\n  "rpoTargetSeconds": 86400,\n  "rtoSeconds": %s,\n  "rtoTargetSeconds": 14400,\n  "scheduleCount": %s,\n  "sourceCount": %s,\n  "userCount": %s,\n  "verifiedAt": "%s"\n}\n' \
  "${audit_count}" "${backup_sha256}" "${digest_count}" "${git_sha}" \
  "${migration_version}" "${rpo_seconds}" "${rto_seconds}" "${schedule_count}" \
  "${source_count}" "${user_count}" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"${report_path}"
printf '%s\n' "${report_path}"
