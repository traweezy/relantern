#!/usr/bin/env bash
set -euo pipefail

required_node_version="v26.8.1"
required_pnpm_version="11.24.0"
if command -v node >/dev/null 2>&1 \
  && command -v pnpm >/dev/null 2>&1 \
  && [[ "$(node --version)" == "${required_node_version}" ]] \
  && [[ "$(pnpm --version)" == "${required_pnpm_version}" ]]; then
  exec pnpm "$@"
fi

exec docker run --rm \
  --user "$(id -u):$(id -g)" \
  --env CI=true \
  --env HOME=/tmp \
  --volume "$(pwd):/workspace" \
  --workdir /workspace \
  node:26.8.1-bookworm-slim@sha256:367679cf9792759492a486e4aa4b421764d71a9546a6dae8aab81a99eb797b3e \
  npx --yes pnpm@11.24.0 "$@"
