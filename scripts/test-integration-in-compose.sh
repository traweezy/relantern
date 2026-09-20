#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == inside ]]; then
  if [[ -e /workspace/.local ]]; then
    printf 'Refusing test image containing local workspace state.\n' >&2
    exit 1
  fi
  PGPASSWORD="$(cat /run/secrets/database_password)"
  export PGPASSWORD
  export DATABASE_URL='postgres://relantern@postgres:5432/relantern?sslmode=disable'
  go test -race -p 1 \
    ./internal/controlplane/pgstore ./internal/dedupe/pgstore \
    ./internal/digest/pgstore ./internal/discovery/pgstore \
    ./internal/embedding/pgstore ./internal/extraction/pgstore \
    ./internal/fetcher/pgstore ./internal/ingestion/pgstore \
    ./internal/intelligence/pgstore ./internal/jobqueue \
    ./internal/openaiwebhook ./internal/operability \
    ./internal/parsing/pgstore ./internal/radar/pgstore \
    ./internal/readingstate/pgstore ./internal/reembedding \
    ./internal/research/pgstore ./internal/retention/pgstore \
    ./internal/scheduler ./internal/search/pgstore \
    ./internal/sources/pgstore ./internal/worker -count=1
  unset DATABASE_URL PGPASSWORD

  export S3_TEST_ENDPOINT=http://minio:9000
  export S3_TEST_BUCKET=relantern-local
  S3_TEST_ACCESS_KEY="$(cat /run/secrets/minio_access_key)"
  S3_TEST_SECRET_KEY="$(cat /run/secrets/minio_secret_key)"
  export S3_TEST_ACCESS_KEY S3_TEST_SECRET_KEY
  go test -race ./internal/storage/s3store -count=1
  exit
fi

if (( $# != 0 )); then
  printf 'Usage: %s\n' "${0##*/}" >&2
  exit 64
fi

script_directory="$(cd -P -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(cd -P -- "${script_directory}/.." && pwd)"
cd "${repository_root}"
compose=(docker compose -f compose.yaml)
postgres_container="$("${compose[@]}" ps -q postgres)"
minio_container="$("${compose[@]}" ps -q minio)"
if [[ -z "${postgres_container}" || -z "${minio_container}" ]]; then
  printf 'PostgreSQL and MinIO must be running before integration tests.\n' >&2
  exit 1
fi

postgres_networks="$(docker inspect --format '{{range .NetworkSettings.Networks}}{{println .NetworkID}}{{end}}' "${postgres_container}")"
minio_networks="$(docker inspect --format '{{range .NetworkSettings.Networks}}{{println .NetworkID}}{{end}}' "${minio_container}")"
network_id="$(comm -12 <(printf '%s\n' "${postgres_networks}" | sort -u) <(printf '%s\n' "${minio_networks}" | sort -u))"
if [[ ! "${network_id}" =~ ^[0-9a-f]{64}$ ]]; then
  printf 'Expected one shared Compose network for PostgreSQL and MinIO.\n' >&2
  exit 1
fi

cache_directory="${repository_root}/.local/go-integration-cache"
if [[ "$(realpath -m -- "${cache_directory}")" != "${cache_directory}" ]]; then
  printf 'Refusing integration cache outside the repository.\n' >&2
  exit 1
fi
mkdir -p -- "${cache_directory}/tmp"
chmod 700 "${cache_directory}"

test_image="$(docker build --quiet --file - "${repository_root}" <<'DOCKERFILE'
# syntax=docker/dockerfile:1.20
FROM golang:1.27.0-bookworm@sha256:ded31c68586d2e49e760acc2e65a884b23d032e9bbbed0ae0c55abd3fcaf4452
WORKDIR /workspace
COPY . .
DOCKERFILE
)"

docker run --rm --init --read-only \
  --network "${network_id}" \
  --user "$(id -u):$(id -g)" \
  --workdir /workspace \
  --tmpfs /tmp:rw,nosuid,nodev \
  --cap-drop ALL --security-opt no-new-privileges \
  --mount "type=bind,src=${cache_directory},dst=/cache" \
  --mount "type=bind,src=${repository_root}/.local/secrets/database_password,dst=/run/secrets/database_password,readonly" \
  --mount "type=bind,src=${repository_root}/.local/secrets/minio_access_key,dst=/run/secrets/minio_access_key,readonly" \
  --mount "type=bind,src=${repository_root}/.local/secrets/minio_secret_key,dst=/run/secrets/minio_secret_key,readonly" \
  --env APP_ENV=test \
  --env ALLOW_LIVE_EXTERNAL_APIS=false \
  --env ALLOW_LIVE_DELIVERY=false \
  --env GOTOOLCHAIN=local \
  --env GOFLAGS=-mod=readonly \
  --env GOMODCACHE=/cache/mod \
  --env GOCACHE=/cache/build \
  --env GOPATH=/cache/gopath \
  --env GOTMPDIR=/cache/tmp \
  --env HOME=/tmp \
  "${test_image}" \
  bash /workspace/scripts/test-integration-in-compose.sh inside
