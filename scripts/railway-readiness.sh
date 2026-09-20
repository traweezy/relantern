#!/usr/bin/env bash
set -euo pipefail

expected_environment="${1:-staging}"
expected_sha="${2:-}"
case "${expected_environment}" in
  staging | production) ;;
  *)
    printf 'Usage: %s [staging|production] <full-git-sha>\n' "$0" >&2
    exit 1
    ;;
esac
if ! [[ "${expected_sha}" =~ ^[0-9a-f]{40}([0-9a-f]{24})?$ ]]; then
  printf 'Usage: %s [staging|production] <full-git-sha>\n' "$0" >&2
  exit 1
fi

command -v railway >/dev/null 2>&1 || {
  printf 'Railway CLI is required for the read-only readiness audit.\n' >&2
  exit 1
}

bash scripts/railway-plan.sh "${expected_environment}" --require-clean

mkdir -p .local
readiness_temp_dir="$(mktemp -d .local/railway-readiness.XXXXXX)"
trap 'test -n "${readiness_temp_dir:-}" && rm -rf -- "${readiness_temp_dir}"' EXIT

railway status --environment "${expected_environment}" --json >"${readiness_temp_dir}/status.json"
railway service list --environment "${expected_environment}" --json >"${readiness_temp_dir}/services.json"
railway bucket list --environment "${expected_environment}" --json >"${readiness_temp_dir}/buckets.json"
railway environment config --environment "${expected_environment}" --json \
  >"${readiness_temp_dir}/environment.json"

bash scripts/pnpm-tool.sh exec node scripts/railway-readiness.mjs \
  topology "${readiness_temp_dir}" "${expected_environment}"

for service in api migrate postgres web worker; do
  railway service status \
    --service "${service}" \
    --environment "${expected_environment}" \
    --json >"${readiness_temp_dir}/${service}-status.json"
  railway deployment list \
    --service "${service}" \
    --environment "${expected_environment}" \
    --limit 1000 \
    --json >"${readiness_temp_dir}/${service}-deployments.json"
  railway domain list \
    --service "${service}" \
    --environment "${expected_environment}" \
    --json >"${readiness_temp_dir}/${service}-domains.json"
  railway tcp-proxy list \
    --service "${service}" \
    --environment "${expected_environment}" \
    --json >"${readiness_temp_dir}/${service}-tcp.json"
done

bash scripts/pnpm-tool.sh exec node scripts/railway-readiness.mjs \
  deployments "${readiness_temp_dir}" "${expected_sha}"
