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

if rg -n '^\s*uses:\s*[^#[:space:]]+@(v[0-9]+|main|master|latest)\s*(#.*)?$' .github/workflows; then
  printf 'Floating GitHub Action reference detected.\n' >&2
  exit 1
fi

unpinned_actions="$(rg '^\s*uses:' .github/workflows | rg -v '@[0-9a-f]{40}(\s|$)' || true)"
if [[ -n "${unpinned_actions}" ]]; then
  printf 'Every GitHub Action must use a full commit SHA:\n%s\n' "${unpinned_actions}" >&2
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
