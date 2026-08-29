#!/usr/bin/env bash
set -euo pipefail

required_version="11.24.0"
if command -v pnpm >/dev/null 2>&1 && [[ "$(pnpm --version)" == "${required_version}" ]]; then
  exec pnpm "$@"
fi

exec docker run --rm \
  --user "$(id -u):$(id -g)" \
  --env HOME=/tmp \
  --volume "$(pwd):/workspace" \
  --workdir /workspace \
  node:24.20.0-bookworm-slim@sha256:ba849c60be29959425b8734d57b8b4b7d56f98edd9504c9af091d5281095a71e \
  npx --yes pnpm@11.24.0 "$@"
