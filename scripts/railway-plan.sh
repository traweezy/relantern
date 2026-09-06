#!/usr/bin/env bash
set -euo pipefail

expected_environment="${1:-staging}"
plan_mode="${2:-}"
case "${expected_environment}" in
  staging | production) ;;
  *)
    printf 'Usage: %s [staging|production]\n' "$0" >&2
    exit 1
    ;;
esac
case "${plan_mode}" in
  "" | --require-clean) ;;
  *)
    printf 'Usage: %s [staging|production] [--require-clean]\n' "$0" >&2
    exit 1
    ;;
esac

command -v railway >/dev/null 2>&1 || {
  printf 'Railway CLI is required for a read-only infrastructure plan.\n' >&2
  exit 1
}

runner="$(pwd)/node_modules/.bin/railway-iac-ts"
test -x "${runner}" || {
  printf 'Run pnpm install before planning Railway infrastructure.\n' >&2
  exit 1
}

plan_file="$(mktemp)"
trap 'rm -f "${plan_file}"' EXIT
railway config plan \
  --runner "${runner}" \
  --file .railway/railway.ts \
  --json >"${plan_file}"

node --input-type=module - "${plan_file}" "${expected_environment}" "${plan_mode}" <<'NODE'
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";

const [, , planPath, expectedEnvironment, planMode] = process.argv;
const plan = JSON.parse(readFileSync(planPath, "utf8"));
if (plan.ok !== true) {
  const diagnostics = Array.isArray(plan.diagnostics) ? plan.diagnostics : [];
  for (const diagnostic of diagnostics) {
    process.stderr.write(`${diagnostic.path || "plan"}: ${diagnostic.message}\n`);
  }
  process.exit(1);
}

const environment = plan.currentEnvironment?.environmentName;
if (environment !== expectedEnvironment) {
  process.stderr.write(
    `Railway is linked to ${environment ?? "an unknown environment"}; expected ${expectedEnvironment}.\n`,
  );
  process.exit(1);
}

const rawChanges = Array.isArray(plan.changeSet?.changes) ? plan.changeSet.changes : [];
const destructive = rawChanges.filter((change) => change.severity === "destructive");
if (destructive.length > 0) {
  for (const change of destructive) {
    process.stderr.write(`Destructive Railway change rejected: ${change.summary}\n`);
  }
  process.exit(1);
}

const servicesByName = (graph) =>
  new Map(
    (Array.isArray(graph?.resources) ? graph.resources : [])
      .filter((resource) => resource?.type === "service")
      .map((resource) => [resource.name, resource]),
  );
const desiredServices = servicesByName(plan.desiredGraph);
const currentServices = servicesByName(plan.currentGraph);
const verifiedImportOmissions = [];
const restartPolicySummary =
  /^Update (api|worker|web) deploy\.restartPolicyMaxRetries, deploy\.restartPolicyType$/;
const restartPolicyDetails = new Set([
  "deploy.restartPolicyMaxRetries (unset → 10)",
  'deploy.restartPolicyType (unset → "ON_FAILURE")',
]);

const isVerifiedRestartPolicyImportOmission = (change) => {
  if (change?.kind !== "resource.update" || change?.severity !== "safe") {
    return false;
  }
  const match = restartPolicySummary.exec(change.summary ?? "");
  if (match === null) {
    return false;
  }
  const serviceName = match[1];
  const details = Array.isArray(change.details) ? change.details : [];
  if (
    details.length !== restartPolicyDetails.size ||
    details.some((detail) => !restartPolicyDetails.has(detail))
  ) {
    return false;
  }

  const desiredDeploy = desiredServices.get(serviceName)?.deploy;
  const currentDeploy = currentServices.get(serviceName)?.deploy;
  if (
    desiredDeploy?.restartPolicyType !== "ON_FAILURE" ||
    desiredDeploy?.restartPolicyMaxRetries !== 10 ||
    currentDeploy?.restartPolicyType !== undefined ||
    currentDeploy?.restartPolicyMaxRetries !== undefined
  ) {
    return false;
  }

  try {
    const deployments = JSON.parse(
      execFileSync(
        "railway",
        [
          "deployment",
          "list",
          "--service",
          serviceName,
          "--environment",
          expectedEnvironment,
          "--json",
        ],
        { encoding: "utf8" },
      ),
    );
    const liveDeploy = deployments.find(
      (deployment) => deployment?.meta?.serviceManifest?.deploy !== undefined,
    )?.meta?.serviceManifest?.deploy;
    if (
      liveDeploy?.restartPolicyType !== desiredDeploy.restartPolicyType ||
      liveDeploy?.restartPolicyMaxRetries !== desiredDeploy.restartPolicyMaxRetries
    ) {
      return false;
    }
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    process.stderr.write(
      `Could not verify live restart policy for ${serviceName}; preserving plan change: ${message}\n`,
    );
    return false;
  }

  verifiedImportOmissions.push(serviceName);
  return true;
};

const changes = rawChanges.filter((change) => !isVerifiedRestartPolicyImportOmission(change));
if (planMode === "--require-clean" && changes.length > 0) {
  process.stderr.write(
    `Railway ${environment} is not ready: ${changes.length} unapplied infrastructure change(s).\n`,
  );
  process.exit(1);
}

process.stdout.write(
  `Railway ${environment} plan for ${plan.currentEnvironment.projectName}: ${changes.length} safe pending change(s).\n`,
);
for (const change of changes) {
  process.stdout.write(`- ${change.summary}\n`);
}
for (const serviceName of verifiedImportOmissions) {
  process.stdout.write(
    `- Verified live ${serviceName} restart policy; ignored Railway SDK import omission.\n`,
  );
}
NODE
