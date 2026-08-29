#!/usr/bin/env bash
set -euo pipefail

test -f pnpm-lock.yaml || { printf 'pnpm-lock.yaml is required.\n' >&2; exit 1; }
test -f go.sum || { printf 'go.sum is required.\n' >&2; exit 1; }

if rg -n '(@mui/|material-ui|eslint|prettier)' --glob '**/package.json' .; then
  printf 'Forbidden UI or formatting dependency detected.\n' >&2
  exit 1
fi

if rg --files -g '*eslint*' -g '*prettier*' | rg .; then
  printf 'ESLint and Prettier configuration files are forbidden; use Biome.\n' >&2
  exit 1
fi

if rg -n 'NEXT_PUBLIC_(?!APP_VERSION|BUILD_SHA)' --pcre2 \
  --glob '*.{ts,tsx,js,mjs,cjs,json,yaml,yml}' \
  --glob '!scripts/policy-check.sh' .; then
  printf 'Unsafe NEXT_PUBLIC variable detected.\n' >&2
  exit 1
fi

if rg -n '^\s*uses:\s*[^#[:space:]]+@(v[0-9]+|main|master|latest)\s*(#.*)?$' \
  --glob '*.{yaml,yml}' .github; then
  printf 'Floating GitHub Action reference detected.\n' >&2
  exit 1
fi

unpinned_actions="$(rg '^\s*uses:' --glob '*.{yaml,yml}' .github \
  | rg -v '^.*uses:\s*\./' \
  | rg -v '@[0-9a-f]{40}(\s|$)' || true)"
if [[ -n "${unpinned_actions}" ]]; then
  printf 'Every GitHub Action must use a full commit SHA:\n%s\n' "${unpinned_actions}" >&2
  exit 1
fi

unlabeled_actions="$(rg '^\s*uses:' --glob '*.{yaml,yml}' .github \
  | rg -v '^.*uses:\s*\./' \
  | rg -v '# v[0-9]' || true)"
if [[ -n "${unlabeled_actions}" ]]; then
  printf 'Every external action pin needs a release comment:\n%s\n' "${unlabeled_actions}" >&2
  exit 1
fi

checkout_count="$(rg '^\s*uses:\s*actions/checkout@' --glob '*.{yaml,yml}' .github | wc -l)"
nonpersistent_count="$(rg '^\s*persist-credentials:\s*false$' --glob '*.{yaml,yml}' .github | wc -l)"
if [[ "${checkout_count}" -ne "${nonpersistent_count}" ]]; then
  printf 'Every checkout must explicitly disable persisted credentials.\n' >&2
  exit 1
fi

for dockerfile in deploy/docker/api.Dockerfile deploy/docker/worker.Dockerfile deploy/docker/migrate.Dockerfile deploy/docker/web.Dockerfile; do
  rg -q '^USER nonroot:nonroot$' "${dockerfile}" || {
    printf '%s must run its production stage as nonroot.\n' "${dockerfile}" >&2
    exit 1
  }
done

if rg -n '(OPENAI_API_KEY|DISCORD_WEBHOOK|RESEND_API_KEY)=' --glob '!docs/**' --glob '!.env.local.example' .; then
  printf 'A production credential assignment is forbidden in the repository.\n' >&2
  exit 1
fi

printf 'Repository policy checks passed.\n'
