#!/usr/bin/env bash
set -euo pipefail

docker compose -f compose.yaml config --quiet
docker compose -f compose.yaml -f compose.dev.yaml config --quiet
pnpm railway:check

for service in web api worker migrate postgres; do
  rg -q "^  ${service}:$" deploy/railway/parity.yaml || {
    printf 'Railway parity is missing service %s.\n' "${service}" >&2
    exit 1
  }
done

rg -q '^  file: .railway/railway.ts$' deploy/railway/parity.yaml
rg -q '^  sdk: railway@3.10.0$' deploy/railway/parity.yaml
rg -q '^  apply_from_automation: false$' deploy/railway/parity.yaml
rg -q '^    automatic_deploy: false$' deploy/railway/parity.yaml
rg -q '^    attended_confirmation: DEPLOY_PRODUCTION$' deploy/railway/parity.yaml
rg -q '"railway": "3.10.0"' package.json
rg -q 'dockerfilePath: "deploy/docker/api.Dockerfile"' .railway/railway.ts
rg -q 'dockerfilePath: "deploy/docker/worker.Dockerfile"' .railway/railway.ts
rg -q 'dockerfilePath: "deploy/docker/web.Dockerfile"' .railway/railway.ts
rg -q 'dockerfilePath: "deploy/docker/migrate.Dockerfile"' .railway/railway.ts
rg -q 'startCommand: "/app/migrate up"' .railway/railway.ts
rg -q 'PUBLIC_BASE_URL: railwayHTTPSOrigin\("web.RAILWAY_PUBLIC_DOMAIN"\)' .railway/railway.ts

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

test "$(sed -n '/^public_services:/,/^[^ ]/p' deploy/railway/parity.yaml | rg -c '^  - web$')" -eq 1
rg -q 'public_receiver: /api/webhooks/openai' deploy/railway/parity.yaml
rg -q 'private_receiver: /internal/v1/openai/events' deploy/railway/parity.yaml
rg -q 'reconciliation_interval: 15m' deploy/railway/parity.yaml
rg -q 'OPENAI_WEBHOOK_SECRET_FILE: /run/secrets/openai_webhook_secret' compose.yaml
test "$(rg -c 'WEB_INTERNAL_SERVICE_TOKEN_FILE: /run/secrets/web_internal_service_token' compose.yaml)" -eq 2
rg -q 'OPENAI_RESEARCH_MAX_TOOL_CALLS: "4"' compose.yaml
rg -q 'OPENAI_DAILY_WEB_SEARCH_LIMIT: "100"' compose.yaml
rg -q 'AUTH_PROVIDER_MODE: fixture' compose.yaml
rg -q 'BETTER_AUTH_URL: http://127.0.0.1:3000' compose.yaml
rg -q 'INTERNAL_API_URL: http://api:8080' compose.yaml
rg -q 'PUBLIC_BASE_URL: http://127.0.0.1:3000' compose.yaml
rg -q 'DELIVERY_MODE: log' compose.yaml
rg -q 'ALLOW_LIVE_DELIVERY: "false"' compose.yaml
rg -q 'DISCORD_ENABLED: "false"' compose.yaml
rg -q 'RESEND_ENABLED: "false"' compose.yaml
rg -q '^INTERNAL_API_URL=http://127.0.0.1:8080$' .env.local.example
rg -q '^PUBLIC_BASE_URL=http://127.0.0.1:3000$' .env.local.example
rg -q '^DELIVERY_MODE=log$' .env.local.example
rg -q '^ALLOW_LIVE_DELIVERY=false$' .env.local.example
rg -q '^DISCORD_ENABLED=false$' .env.local.example
rg -q '^RESEND_ENABLED=false$' .env.local.example
rg -q '      - INTERNAL_API_URL' deploy/railway/parity.yaml
rg -q '      - PUBLIC_BASE_URL' deploy/railway/parity.yaml
rg -q 'live_fuse: ALLOW_LIVE_DELIVERY' deploy/railway/parity.yaml
rg -q 'owner_date_channel_unique: true' deploy/railway/parity.yaml
for secret in DISCORD_WEBHOOK_URL RESEND_API_KEY; do
  rg -q "      - ${secret}" deploy/railway/parity.yaml || {
    printf 'Railway delivery parity is missing %s.\n' "${secret}" >&2
    exit 1
  }
done
rg -q 'LOCAL_OAUTH_STUB_SECRET_FILE: /run/secrets/local_oauth_stub_secret' compose.yaml
test "$(rg -c 'DATABASE_PASSWORD_FILE: /run/secrets/database_password' compose.yaml)" -ge 2
for secret in BETTER_AUTH_SECRET DATABASE_URL GITHUB_OAUTH_CLIENT_SECRET; do
  rg -q "      - ${secret}" deploy/railway/parity.yaml || {
    printf 'Railway web auth parity is missing %s.\n' "${secret}" >&2
    exit 1
  }
done

for reference in ACCESS_KEY_ID BUCKET ENDPOINT REGION SECRET_ACCESS_KEY; do
  rg -q "    [a-z_]*: ${reference}" deploy/railway/parity.yaml || {
    printf 'Railway object-storage parity is missing %s.\n' "${reference}" >&2
    exit 1
  }
done

while IFS= read -r digest; do
  if ! rg -q "${digest}" docs/version-manifest.md; then
    printf 'Image digest %s is absent from the reviewed version manifest.\n' "${digest}" >&2
    exit 1
  fi
done < <(rg -o 'sha256:[0-9a-f]{64}' compose.yaml deploy/docker/*.Dockerfile | sed 's/.*://' | sort -u | sed 's/^/sha256:/')

printf 'Compose, environment, Docker, and Railway parity checks passed.\n'
