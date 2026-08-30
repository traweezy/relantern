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
command -v node >/dev/null 2>&1 || {
  printf 'Node is required for the read-only readiness audit.\n' >&2
  exit 1
}

bash scripts/railway-plan.sh "${expected_environment}" --require-clean

readiness_temp_dir="$(mktemp -d)"
trap 'test -n "${readiness_temp_dir:-}" && rm -rf -- "${readiness_temp_dir}"' EXIT

railway status --environment "${expected_environment}" --json >"${readiness_temp_dir}/status.json"
railway service list --environment "${expected_environment}" --json >"${readiness_temp_dir}/services.json"
railway bucket list --environment "${expected_environment}" --json >"${readiness_temp_dir}/buckets.json"
railway environment config --environment "${expected_environment}" --json \
  >"${readiness_temp_dir}/environment.json"

node --input-type=module - \
  "${readiness_temp_dir}/status.json" \
  "${readiness_temp_dir}/services.json" \
  "${readiness_temp_dir}/buckets.json" \
  "${readiness_temp_dir}/environment.json" \
  "${expected_environment}" <<'NODE'
import { readFileSync } from "node:fs";

const [, , statusPath, servicesPath, bucketsPath, environmentPath, expectedEnvironment] =
  process.argv;
const readJSON = (file) => JSON.parse(readFileSync(file, "utf8"));
const status = readJSON(statusPath);
const services = readJSON(servicesPath);
const buckets = readJSON(bucketsPath);
const environment = readJSON(environmentPath);
const serviceNames = Array.isArray(services)
  ? services.map((service) => service.name).sort()
  : [];
const expectedServices = ["api", "migrate", "postgres", "web", "worker"];
if (status.name !== "Relantern" || typeof status.id !== "string") {
  throw new Error("Railway readiness is linked to an unexpected project");
}
const environments = status.environments?.edges ?? [];
if (
  environments.length !== 1 ||
  environments[0]?.node?.name !== expectedEnvironment ||
  environments[0]?.node?.canAccess !== true
) {
  throw new Error(`Railway readiness did not resolve only ${expectedEnvironment}`);
}
if (JSON.stringify(serviceNames) !== JSON.stringify(expectedServices)) {
  throw new Error(
    `Railway ${expectedEnvironment} services are ${JSON.stringify(serviceNames)}; expected ${JSON.stringify(expectedServices)}`,
  );
}
const bucketNames = Array.isArray(buckets) ? buckets.map((bucket) => bucket.name).sort() : [];
if (bucketNames.length !== 1 || bucketNames[0] !== "bucket") {
  throw new Error(`Railway ${expectedEnvironment} must contain only the private bucket resource`);
}
if (environment.privateNetworkDisabled !== false) {
  throw new Error(`Railway ${expectedEnvironment} private networking is not enabled`);
}
process.stdout.write(
  `Railway ${expectedEnvironment} project, service, bucket, and private-network topology passed.\n`,
);
NODE

for service in api migrate postgres web worker; do
  railway service status \
    --service "${service}" \
    --environment "${expected_environment}" \
    --json >"${readiness_temp_dir}/${service}-status.json"
  railway domain list \
    --service "${service}" \
    --environment "${expected_environment}" \
    --json >"${readiness_temp_dir}/${service}-domains.json"
  railway tcp-proxy list \
    --service "${service}" \
    --environment "${expected_environment}" \
    --json >"${readiness_temp_dir}/${service}-tcp.json"
done

node --input-type=module - "${readiness_temp_dir}" "${expected_sha}" <<'NODE'
import { readFileSync } from "node:fs";
import { join } from "node:path";

const [, , evidenceDirectory, expectedSHA] = process.argv;
const services = ["api", "migrate", "postgres", "web", "worker"];
const gitServices = new Set(["api", "migrate", "web", "worker"]);
const readJSON = (file) => JSON.parse(readFileSync(file, "utf8"));
const walk = (value, visit) => {
  if (Array.isArray(value)) {
    for (const entry of value) walk(entry, visit);
    return;
  }
  if (value && typeof value === "object") {
    for (const [key, entry] of Object.entries(value)) {
      visit(key, entry);
      walk(entry, visit);
    }
  }
};
const collectionSize = (value) => {
  if (Array.isArray(value)) return value.length;
  if (Array.isArray(value?.edges)) return value.edges.length;
  if (Array.isArray(value?.domains)) return value.domains.length;
  if (Array.isArray(value?.domains?.edges)) return value.domains.edges.length;
  if (Array.isArray(value?.tcpProxies)) return value.tcpProxies.length;
  if (Array.isArray(value?.tcpProxies?.edges)) return value.tcpProxies.edges.length;
  return -1;
};

for (const service of services) {
  const deployment = readJSON(join(evidenceDirectory, `${service}-status.json`));
  const statuses = [];
  const revisions = [];
  walk(deployment, (key, value) => {
    if (/status/i.test(key) && typeof value === "string") statuses.push(value.toUpperCase());
    if (/(commit.*(?:hash|sha)|git.*sha|revision)/i.test(key) && typeof value === "string") {
      revisions.push(value.toLowerCase());
    }
  });
  const successful = statuses.some((status) =>
    ["ACTIVE", "RUNNING", "SUCCESS"].some(
      (accepted) => status === accepted || status.endsWith(`_${accepted}`),
    ),
  );
  if (!successful) {
    throw new Error(`Railway ${service} has no successful active deployment`);
  }
  if (gitServices.has(service) && !revisions.includes(expectedSHA)) {
    throw new Error(`Railway ${service} is not running the frozen release SHA`);
  }

  const domains = readJSON(join(evidenceDirectory, `${service}-domains.json`));
  const domainCount = collectionSize(domains);
  if (domainCount < 0 || (service === "web" ? domainCount < 1 : domainCount !== 0)) {
    throw new Error(`Railway ${service} violates the web-only public-domain boundary`);
  }
  const tcpProxies = readJSON(join(evidenceDirectory, `${service}-tcp.json`));
  if (collectionSize(tcpProxies) !== 0) {
    throw new Error(`Railway ${service} must not expose a public TCP proxy`);
  }
}
process.stdout.write("Railway deployment SHA, health, domain, and TCP exposure checks passed.\n");
NODE
