#!/usr/bin/env bash
set -euo pipefail

docker compose -f compose.yaml config --quiet
docker compose -f compose.yaml -f compose.dev.yaml config --quiet

for service in web api worker migrate; do
  rg -q "^  ${service}:$" deploy/railway/parity.yaml || {
    printf 'Railway parity is missing service %s.\n' "${service}" >&2
    exit 1
  }
done

for dockerfile in web api worker migrate; do
  rg -q "dockerfile: deploy/docker/${dockerfile}.Dockerfile" deploy/railway/parity.yaml || {
    printf 'Railway parity has an unexpected %s Dockerfile.\n' "${dockerfile}" >&2
    exit 1
  }
  rg -q "dockerfile: deploy/docker/${dockerfile}.Dockerfile" compose.yaml || {
    printf 'Compose has an unexpected %s Dockerfile.\n' "${dockerfile}" >&2
    exit 1
  }
done

rg -q 'production_mode: system' deploy/railway/parity.yaml
rg -q 'CLOCK_MODE: \$\{CLOCK_MODE:-system\}' compose.yaml

while IFS= read -r digest; do
  if ! rg -q "${digest}" docs/version-manifest.md; then
    printf 'Image digest %s is absent from the reviewed version manifest.\n' "${digest}" >&2
    exit 1
  fi
done < <(rg -o 'sha256:[0-9a-f]{64}' compose.yaml deploy/docker/*.Dockerfile | sed 's/.*://' | sort -u | sed 's/^/sha256:/')

printf 'Compose, environment, Docker, and Railway parity checks passed.\n'
