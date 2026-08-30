SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

COMPOSE_BASE := docker compose -f compose.yaml
COMPOSE_DEV := docker compose -f compose.yaml -f compose.dev.yaml
GO := bash scripts/go-tool.sh
PNPM := bash scripts/pnpm-tool.sh

.PHONY: help doctor secrets bootstrap dev dev-live ps logs stop watch test test-unit test-integration test-e2e auth-smoke lint workflow-lint typecheck format generate generate-check migrate migration seed sources-verify fixtures-record eval test-dedupe test-search test-extraction test-research scheduler-tick digest-preview digest-run retention-run test-scheduler test-dst time-travel time-travel-clean backup restore-drill observability config-check demo demo-audit security-scan prepush prodlike prodlike-smoke sbom clean reset

help:
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "%-22s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

doctor: ## Validate local prerequisites and safety fuses
	bash scripts/doctor.sh

secrets: ## Create ignored local-only secret files
	bash scripts/secrets.sh

bootstrap: doctor secrets ## Build, generate, migrate, and seed the safe local stack
	$(PNPM) install --frozen-lockfile
	$(MAKE) generate
	$(COMPOSE_BASE) build
	$(COMPOSE_BASE) up -d --wait postgres minio fake-source fake-openai fake-delivery
	$(COMPOSE_BASE) run --rm minio-init
	$(COMPOSE_BASE) run --rm --build migrate up
	$(COMPOSE_BASE) run --rm seed

dev: secrets ## Start the safe hot-reload stack
	$(COMPOSE_DEV) up --build --detach --watch
	COMPOSE_FILES='-f compose.yaml -f compose.dev.yaml' bash scripts/stack-wait.sh

watch: ## Attach Compose Watch to an already-running development stack
	$(COMPOSE_DEV) watch --no-up

dev-live: ## Refuse live providers until an explicit owner rollout
	@printf 'Live providers are disabled pending a separately reviewed owner rollout.\n' >&2
	@exit 1

ps: ## Show service state and health
	$(COMPOSE_DEV) ps

logs: ## Follow structured service logs
	$(COMPOSE_DEV) logs --follow --tail=200

stop: ## Stop local containers without deleting data
	$(COMPOSE_DEV) down --remove-orphans

test: test-unit test-integration ## Run the current complete test suite

test-unit: ## Run Go and TypeScript unit tests
	$(GO) test ./...
	$(PNPM) test

test-integration: secrets ## Run database, object-storage, and worker integration checks
	$(COMPOSE_BASE) up -d --wait postgres minio fake-openai fake-delivery
	$(COMPOSE_BASE) run --rm minio-init
	$(COMPOSE_BASE) run --rm --build migrate up
	$(COMPOSE_BASE) run --rm seed
	$(COMPOSE_BASE) run --rm --build worker once
	@IFS= read -r relantern_database_secret < .local/secrets/database_password; \
		DATABASE_URL="postgres://relantern:$${relantern_database_secret}@127.0.0.1:5432/relantern?sslmode=disable" \
		$(GO) test -p 1 ./internal/controlplane/pgstore ./internal/dedupe/pgstore ./internal/digest/pgstore ./internal/discovery/pgstore ./internal/embedding/pgstore ./internal/extraction/pgstore ./internal/fetcher/pgstore ./internal/jobqueue ./internal/openaiwebhook ./internal/operability ./internal/parsing/pgstore ./internal/radar/pgstore ./internal/readingstate/pgstore ./internal/reembedding ./internal/research/pgstore ./internal/retention/pgstore ./internal/scheduler ./internal/search/pgstore ./internal/sources/pgstore ./internal/worker -count=1
	@IFS= read -r relantern_s3_access < .local/secrets/minio_access_key; \
		IFS= read -r relantern_s3_secret < .local/secrets/minio_secret_key; \
		S3_TEST_ENDPOINT=http://127.0.0.1:9000 \
		S3_TEST_BUCKET=relantern-local \
		S3_TEST_ACCESS_KEY="$${relantern_s3_access}" \
		S3_TEST_SECRET_KEY="$${relantern_s3_secret}" \
		$(GO) test ./internal/storage/s3store -count=1

test-e2e: ## Build the production web application
	$(PNPM) --filter @relantern/web build

auth-smoke: ## Verify the disconnected owner OAuth and session journey
	bash scripts/auth-smoke.sh

lint: ## Run Biome, gofmt verification, and Go vet
	$(PNPM) lint
	bash scripts/gofmt-check.sh
	$(GO) vet ./...

workflow-lint: ## Validate GitHub Actions with pinned actionlint
	@if actionlint -version >/dev/null 2>&1; then actionlint; else $(GO) run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12; fi

format: ## Format supported source files
	$(PNPM) format
	$(GO) fmt ./...

typecheck: ## Typecheck all TypeScript workspaces
	$(PNPM) typecheck

generate: ## Generate sqlc, OpenAPI, and the TypeScript API client
	$(GO) run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate
	$(GO) run ./cmd/api openapi --output contracts/openapi.yaml
	$(PNPM) --filter @relantern/api-client generate

generate-check: ## Fail if regeneration changes the current generated artifacts
	@relantern_generate_check_dir="$$(mktemp -d)"; \
		trap 'rm -r "$${relantern_generate_check_dir}"' EXIT; \
		cp contracts/openapi.yaml "$${relantern_generate_check_dir}/openapi.yaml"; \
		cp internal/database/sqlcdb/models.go "$${relantern_generate_check_dir}/models.go"; \
		cp packages/api-client/src/generated/schema.ts "$${relantern_generate_check_dir}/schema.ts"; \
		$(MAKE) generate; \
		cmp -s "$${relantern_generate_check_dir}/openapi.yaml" contracts/openapi.yaml; \
		cmp -s "$${relantern_generate_check_dir}/models.go" internal/database/sqlcdb/models.go; \
		cmp -s "$${relantern_generate_check_dir}/schema.ts" packages/api-client/src/generated/schema.ts

migrate: secrets ## Apply forward local migrations
	$(COMPOSE_BASE) run --rm --build migrate up

migration: ## Create a timestamped empty migration (name=required)
	@test -n "$(name)" || { printf 'Usage: make migration name=short_description\n' >&2; exit 1; }
	@touch "migrations/$$(date -u +%Y%m%d%H%M%S)_$(name).sql"

seed: secrets ## Apply the idempotent local owner and schedule seed
	$(COMPOSE_BASE) run --rm seed

sources-verify: ## Strictly validate the reviewed registry and connector fixtures
	$(GO) run ./cmd/sourcectl verify --registry sources/registry.yaml --fixtures sources/fixtures.yaml
	$(GO) test ./internal/parsing -run '^TestReviewedFixtureParsers$$' -count=1

fixtures-record: ## Refuse live fixture recording until separately authorized
	@printf 'Live fixture recording remains disabled and requires explicit review.\n' >&2
	@exit 1

eval: test-dedupe test-search test-extraction test-research ## Run deterministic zero-network evaluation fixtures

test-dedupe: ## Run deterministic dedupe and cluster evaluation fixtures
	$(GO) test ./internal/dedupe/... -count=1

test-search: ## Run deterministic hybrid-retrieval evaluation fixtures
	$(GO) test ./internal/embedding/... ./internal/reembedding/... ./internal/search/... -count=1

test-extraction: ## Run structured-output, grounding, and injection evaluations
	$(GO) test ./internal/extraction/... -count=1

test-research: ## Run bounded research, provenance, and webhook evaluations
	$(GO) test ./internal/research/... ./internal/openaiwebhook -count=1

scheduler-tick: secrets ## Run one schedule reconciliation and capture pass
	$(COMPOSE_BASE) run --rm worker once

digest-preview: secrets ## Render the next digest without persistence or delivery
	$(COMPOSE_BASE) up -d --wait postgres
	$(COMPOSE_BASE) build worker
	$(COMPOSE_BASE) run --rm worker digest-preview $(if $(user_id),--user-id=$(user_id)) $(if $(schedule_id),--schedule-id=$(schedule_id))

digest-run: secrets ## Queue a local run-now preview; set deliver=true to use the configured safe sink
	@test "$(deliver)" = "" || test "$(deliver)" = "false" || test "$(deliver)" = "true" || { printf 'deliver must be true or false\n' >&2; exit 1; }
	$(COMPOSE_BASE) up -d --wait postgres minio fake-openai fake-delivery
	$(COMPOSE_BASE) build worker
	@occurrence_id="$$($(COMPOSE_BASE) run --rm worker digest-run --output=occurrence-id --deliver=$(if $(deliver),$(deliver),false) $(if $(user_id),--user-id=$(user_id)) $(if $(schedule_id),--schedule-id=$(schedule_id)))"; \
		printf 'Queued digest occurrence %s\n' "$${occurrence_id}"; \
		$(COMPOSE_BASE) run --rm worker once --occurrence-id "$${occurrence_id}"

retention-run: secrets ## Run one idempotent bounded retention cycle
	$(COMPOSE_BASE) up -d --wait postgres minio
	$(COMPOSE_BASE) run --rm minio-init
	$(COMPOSE_BASE) run --rm --build migrate up
	$(COMPOSE_BASE) run --rm --build worker retention-run

test-scheduler: ## Run scheduler unit and DST tests
	$(GO) test ./internal/scheduler/... -count=1

test-dst: test-scheduler ## Alias for the reviewed DST suite

time-travel: secrets ## Run one isolated fixed-clock reconciliation (at=RFC3339 required)
	@test -n "$(at)" || { printf 'Usage: make time-travel at=2026-08-29T08:00:00-04:00\n' >&2; exit 1; }
	CLOCK_MODE=fixed TEST_NOW='$(at)' COMPOSE_PROJECT_NAME=relantern-time-travel POSTGRES_PUBLISHED_PORT=15432 MINIO_API_PUBLISHED_PORT=19000 MINIO_CONSOLE_PUBLISHED_PORT=19001 FAKE_DELIVERY_PUBLISHED_PORT=18092 $(COMPOSE_BASE) up --build --detach postgres fake-delivery migrate seed
	CLOCK_MODE=fixed TEST_NOW='$(at)' COMPOSE_PROJECT_NAME=relantern-time-travel POSTGRES_PUBLISHED_PORT=15432 MINIO_API_PUBLISHED_PORT=19000 MINIO_CONSOLE_PUBLISHED_PORT=19001 FAKE_DELIVERY_PUBLISHED_PORT=18092 STACK_LONG_RUNNING='postgres fake-delivery' STACK_ONE_SHOTS='migrate seed' bash scripts/stack-wait.sh
	CLOCK_MODE=fixed TEST_NOW='$(at)' COMPOSE_PROJECT_NAME=relantern-time-travel POSTGRES_PUBLISHED_PORT=15432 MINIO_API_PUBLISHED_PORT=19000 MINIO_CONSOLE_PUBLISHED_PORT=19001 FAKE_DELIVERY_PUBLISHED_PORT=18092 $(COMPOSE_BASE) run --rm worker once
	@capture_json="$$(curl --fail --silent --show-error http://127.0.0.1:18092/captures)"; printf '%s\n' "$$capture_json"; printf '%s' "$$capture_json" | rg -q '"count":1'

time-travel-clean: ## Remove the isolated time-travel stack after confirmation
	@printf 'Type relantern-time-travel to delete its disposable volumes: '; read -r answer; test "$$answer" = relantern-time-travel
	COMPOSE_PROJECT_NAME=relantern-time-travel $(COMPOSE_BASE) down --volumes --remove-orphans

backup: secrets ## Create a private checksum-verified logical PostgreSQL backup
	$(COMPOSE_BASE) up -d --wait postgres
	bash scripts/postgres-backup.sh $(if $(output),$(output))

restore-drill: secrets ## Restore a snapshot into an isolated database and record RPO/RTO
	$(COMPOSE_BASE) up -d --wait postgres
	bash scripts/postgres-restore-drill.sh $(if $(backup),$(backup))

observability: ## Start the optional local LGTM profile
	$(COMPOSE_DEV) --profile observability up -d otel-lgtm

config-check: secrets ## Validate Compose, Docker, env, and Railway parity
	bash scripts/config-check.sh

railway-check: ## Validate the pinned Railway IaC graph without cloud access
	$(PNPM) railway:check

railway-plan: ## Preview safe Railway drift without applying it
	bash scripts/railway-plan.sh $(if $(environment),$(environment),staging)

railway-readiness: ## Audit live Railway readiness without printing variables
	@test -n "$(release_sha)" || { printf 'Usage: make railway-readiness environment=staging release_sha=<full-sha>\n' >&2; exit 1; }
	bash scripts/railway-readiness.sh "$(if $(environment),$(environment),staging)" "$(release_sha)"

soak-status: ## Report a staging soak ledger without requiring PASS
	$(GO) run ./cmd/soakctl --file "$(if $(file),$(file),docs/evidence/staging/soak-template.json)"

soak-validate: ## Require a completed PASS staging soak ledger
	@test -n "$(file)" || { printf 'Usage: make soak-validate file=<ledger.json>\n' >&2; exit 1; }
	$(GO) run ./cmd/soakctl --file "$(file)" --require-pass

demo: dev ## Open the isolated anonymous fixture demonstration
	@printf 'Open http://127.0.0.1:3000/demo\n'

demo-audit: ## Validate the demo fixture and emitted isolation contract
	$(PNPM) --filter @relantern/web test -- demo-sanitizer.test.ts route-policy.test.ts
	$(PNPM) --filter @relantern/web build
	@rg -q 'noindex,nofollow' docs/demo-route-contract.md
	@test ! -s apps/web/.next/server/app/demo.html
	@test ! -s 'apps/web/.next/server/app/demo/story/[fixtureId].html'

prepush: lint workflow-lint typecheck test generate-check config-check sources-verify ## Run required local fast release gates
	bash scripts/policy-check.sh
	$(GO) test -race ./...
	$(PNPM) build

security-scan: ## Run pinned zero-write repository security scanners
	@test -x "$(CURDIR)/.local/bin/osv-scanner" || command -v osv-scanner >/dev/null 2>&1 || bash scripts/install-ci-tools.sh security
	@PATH="$(CURDIR)/.local/bin:$${PATH}" osv-scanner scan source --no-call-analysis=go --recursive .
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...
	@PATH="$(CURDIR)/.local/bin:$${PATH}" trivy filesystem --exit-code 1 --ignore-unfixed --scanners vuln,misconfig --severity HIGH,CRITICAL --skip-dirs .git .
	@PATH="$(CURDIR)/.local/bin:$${PATH}" zizmor --persona=regular --offline .github

prodlike: secrets ## Build and run the exact production Docker stages
	$(COMPOSE_BASE) up --build --detach
	bash scripts/stack-wait.sh

prodlike-smoke: prodlike ## Smoke test all production-like health surfaces
	curl --fail --silent --show-error http://127.0.0.1:3000/healthz >/dev/null
	curl --fail --silent --show-error http://127.0.0.1:8080/readyz >/dev/null
	curl --fail --silent --show-error http://127.0.0.1:8092/healthz >/dev/null
	$(MAKE) auth-smoke

sbom: ## Generate the pinned SPDX repository SBOM
	@test -x "$(CURDIR)/.local/bin/syft" || command -v syft >/dev/null 2>&1 || bash scripts/install-ci-tools.sh sbom
	@mkdir -p dist/sbom
	@PATH="$(CURDIR)/.local/bin:$${PATH}" syft dir:. -o spdx-json=dist/sbom/repository.spdx.json

clean: ## Remove build outputs while preserving local volumes
	$(PNPM) exec rimraf apps/web/.next coverage dist 2>/dev/null || true
	$(GO) clean -cache -testcache

reset: ## Confirm and remove only Relantern local containers and volumes
	bash scripts/reset.sh
