#!/usr/bin/env bash
set -euo pipefail

compose_project="relantern"
resolved_project="$(docker compose -f compose.yaml config --format json | sed -n 's/.*"name":"\([^"]*\)".*/\1/p' | head -n 1)"
if [[ "${resolved_project}" != "${compose_project}" ]]; then
  printf 'Refusing reset: expected Compose project %s, resolved %s.\n' "${compose_project}" "${resolved_project}" >&2
  exit 1
fi

if [[ "${CI:-false}" != "true" ]]; then
  printf 'This removes only Relantern local containers and volumes. Type %s to continue: ' "${compose_project}"
  read -r confirmation
  if [[ "${confirmation}" != "${compose_project}" ]]; then
    printf 'Reset cancelled.\n'
    exit 1
  fi
fi

docker compose -f compose.yaml down --volumes --remove-orphans
printf 'Relantern local containers and volumes were removed.\n'
