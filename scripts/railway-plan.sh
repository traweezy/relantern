#!/usr/bin/env bash
set -euo pipefail

expected_environment="${1:-staging}"
case "${expected_environment}" in
  staging | production) ;;
  *)
    printf 'Usage: %s [staging|production]\n' "$0" >&2
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

node --input-type=module - "${plan_file}" "${expected_environment}" <<'NODE'
import { readFileSync } from "node:fs";

const [, , planPath, expectedEnvironment] = process.argv;
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

const changes = Array.isArray(plan.changeSet?.changes) ? plan.changeSet.changes : [];
const destructive = changes.filter((change) => change.severity === "destructive");
if (destructive.length > 0) {
  for (const change of destructive) {
    process.stderr.write(`Destructive Railway change rejected: ${change.summary}\n`);
  }
  process.exit(1);
}

process.stdout.write(
  `Railway ${environment} plan for ${plan.currentEnvironment.projectName}: ${changes.length} safe pending change(s).\n`,
);
for (const change of changes) {
  process.stdout.write(`- ${change.summary}\n`);
}
NODE
