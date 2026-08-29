#!/usr/bin/env bash
set -euo pipefail

required_version="go1.27.0"
if command -v go >/dev/null 2>&1 && [[ "$(go version | awk '{print $3}')" == "${required_version}" ]]; then
  exec go "$@"
fi

exec docker run --rm \
  --user "$(id -u):$(id -g)" \
  --env GOCACHE=/tmp/go-cache \
  --env GOMODCACHE=/tmp/go-mod-cache \
  --volume "$(pwd):/workspace" \
  --workdir /workspace \
  golang:1.27.0-bookworm@sha256:ded31c68586d2e49e760acc2e65a884b23d032e9bbbed0ae0c55abd3fcaf4452 \
  go "$@"
