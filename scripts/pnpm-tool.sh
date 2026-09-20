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

workspace_root="$(pwd -P)"
if ! command -v git >/dev/null 2>&1; then
  printf 'Pinned pnpm container requires host Git for source provenance.\n' >&2
  exit 1
fi
if [[ "$(git -C "${workspace_root}" rev-parse --show-toplevel)" != "${workspace_root}" ]]; then
  printf 'Pinned pnpm container must run from the repository root.\n' >&2
  exit 1
fi
source_commit="$(git -C "${workspace_root}" rev-parse --verify HEAD)"
if [[ ! "${source_commit}" =~ ^([0-9a-f]{40}|[0-9a-f]{64})$ ]]; then
  printf 'Git did not return a complete source commit.\n' >&2
  exit 1
fi
source_status="$(git -C "${workspace_root}" status --porcelain --untracked-files=all)"
source_dirty=false
if [[ -n "${source_status}" ]]; then
  source_dirty=true
fi

exec docker run --rm \
  --user "$(id -u):$(id -g)" \
  --env CI=true \
  --env HOME=/tmp \
  --env "RELANTERN_SOURCE_COMMIT=${source_commit}" \
  --env "RELANTERN_SOURCE_DIRTY=${source_dirty}" \
  --volume "${workspace_root}:/workspace" \
  --workdir /workspace \
  node:26.8.1-bookworm-slim@sha256:367679cf9792759492a486e4aa4b421764d71a9546a6dae8aab81a99eb797b3e \
  sh -c 'node scripts/install-pnpm.mjs /tmp/relantern-pnpm && export PATH="/tmp/relantern-pnpm:$PATH" && exec pnpm "$@"' sh "$@"
