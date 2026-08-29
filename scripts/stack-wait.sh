#!/usr/bin/env bash
set -euo pipefail

compose_files="${COMPOSE_FILES:--f compose.yaml}"
read -r -a compose_arguments <<<"${compose_files}"
read -r -a long_running <<<"${STACK_LONG_RUNNING:-postgres minio fake-source fake-openai fake-delivery api worker web}"
read -r -a one_shots <<<"${STACK_ONE_SHOTS:-minio-init migrate seed}"
deadline=$((SECONDS + 150))

container_id() {
  docker compose "${compose_arguments[@]}" ps --all --quiet "$1" | head -n 1
}

while (( SECONDS < deadline )); do
  ready=true

  for service_name in "${one_shots[@]}"; do
    id="$(container_id "${service_name}")"
    if [[ -z "${id}" ]]; then
      ready=false
      continue
    fi
    state="$(docker inspect --format '{{.State.Status}}' "${id}")"
    if [[ "${state}" == "exited" ]]; then
      exit_code="$(docker inspect --format '{{.State.ExitCode}}' "${id}")"
      if [[ "${exit_code}" != "0" ]]; then
        printf 'One-shot service %s exited with code %s.\n' "${service_name}" "${exit_code}" >&2
        exit 1
      fi
    else
      ready=false
    fi
  done

  for service_name in "${long_running[@]}"; do
    id="$(container_id "${service_name}")"
    if [[ -z "${id}" ]]; then
      ready=false
      continue
    fi
    state="$(docker inspect --format '{{.State.Status}}' "${id}")"
    health="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "${id}")"
    if [[ "${state}" != "running" || "${health}" != "healthy" ]]; then
      ready=false
    fi
  done

  if [[ "${ready}" == "true" ]]; then
    printf 'Long-running services are healthy and one-shot services succeeded.\n'
    exit 0
  fi
  sleep 1
done

printf 'Timed out waiting for the Relantern stack.\n' >&2
docker compose "${compose_arguments[@]}" ps --all >&2
exit 1
