#!/usr/bin/env bash
set -euo pipefail

if command -v gofmt >/dev/null 2>&1; then
  unformatted="$(find cmd internal -type f -name '*.go' -print0 | xargs -0 gofmt -l)"
else
  unformatted="$(docker run --rm \
    --user "$(id -u):$(id -g)" \
    --volume "$(pwd):/workspace" \
    --workdir /workspace \
    golang:1.27.0-bookworm@sha256:ded31c68586d2e49e760acc2e65a884b23d032e9bbbed0ae0c55abd3fcaf4452 \
    sh -ec "find cmd internal -type f -name '*.go' -print0 | xargs -0 gofmt -l")"
fi

if [[ -n "${unformatted}" ]]; then
  printf 'Go files require formatting:\n%s\n' "${unformatted}" >&2
  exit 1
fi
