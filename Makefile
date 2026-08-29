SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

COMPOSE_BASE := docker compose -f compose.yaml
COMPOSE_DEV := docker compose -f compose.yaml -f compose.dev.yaml
GO := bash scripts/go-tool.sh
PNPM := bash scripts/pnpm-tool.sh

.PHONY: help doctor secrets bootstrap dev dev-live ps logs stop watch test test-unit test-integration test-e2e lint typecheck format generate generate-check migrate migration seed sources-verify fixtures-record eval scheduler-tick digest-preview digest-run test-scheduler test-dst time-travel time-travel-clean observability config-check demo demo-audit prepush prodlike prodlike-smoke sbom clean reset

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
	$(COMPOSE_BASE) run --rm migrate up
	$(COMPOSE_BASE) run --rm seed

dev: secrets ## Start the safe hot-reload stack
	$(COMPOSE_DEV) up --build --detach --watch
	COMPOSE_FILES='-f compose.yaml -f compose.dev.yaml' bash scripts/stack-wait.sh

watch: ## Attach Compose Watch to an already-running development stack
	$(COMPOSE_DEV) watch --no-up

dev-live: ## Refuse live providers until the post-PR-0 implementation phase
	@printf 'Live providers are intentionally unavailable in PR 0.\n' >&2
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

test-integration: secrets ## Run migration and scheduler integration smoke checks
	$(COMPOSE_BASE) up -d --wait postgres fake-delivery
	$(COMPOSE_BASE) run --rm migrate up
	$(COMPOSE_BASE) run --rm seed
	$(COMPOSE_BASE) run --rm worker once

test-e2e: ## Run the PR 0 production-build smoke in lieu of product flows
	$(PNPM) --filter @relantern/web build

lint: ## Run Biome, gofmt verification, and Go vet
	$(PNPM) lint
	bash scripts/gofmt-check.sh
	$(GO) vet ./...

format: ## Format supported source files
	$(PNPM) format
	$(GO) fmt ./...

typecheck: ## Typecheck all TypeScript workspaces
	$(PNPM) typecheck

generate: ## Generate sqlc, OpenAPI, and the TypeScript API client
	$(GO) run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate
	$(GO) run ./cmd/api openapi --output contracts/openapi.yaml
	$(PNPM) --filter @relantern/api-client generate

generate-check: generate ## Fail if committed generated artifacts drift
	@git diff --exit-code -- contracts/openapi.yaml internal/database/sqlcdb packages/api-client/src/generated/schema.ts

migrate: secrets ## Apply forward local migrations
	$(COMPOSE_BASE) run --rm migrate up

migration: ## Create a timestamped empty migration (name=required)
	@test -n "$(name)" || { printf 'Usage: make migration name=short_description\n' >&2; exit 1; }
	@touch "migrations/$$(date -u +%Y%m%d%H%M%S)_$(name).sql"

seed: secrets ## Apply the idempotent local owner and schedule seed
	$(COMPOSE_BASE) run --rm seed

sources-verify: ## Verify that live ingestion remains disabled in PR 0
	@test -f sources/registry.yaml
	@rg -q '^enabled: false$$' sources/registry.yaml

fixtures-record: ## Refuse live fixture recording until ingestion is approved
	@printf 'Fixture recording is intentionally unavailable in PR 0.\n' >&2
	@exit 1

eval: ## Run deterministic zero-network evaluation fixtures
	$(GO) test ./...

scheduler-tick: secrets ## Run one schedule reconciliation and capture pass
	$(COMPOSE_BASE) run --rm worker once

digest-preview: scheduler-tick ## Capture the deterministic local placeholder digest
	@curl --fail --silent --show-error http://127.0.0.1:8092/captures

digest-run: ## Keep external delivery behind a later explicit fuse
	@printf 'PR 0 supports fake digest-preview only; external delivery is disabled.\n' >&2
	@exit 1

test-scheduler: ## Run scheduler unit and DST tests
	$(GO) test ./internal/scheduler/... -count=1

test-dst: test-scheduler ## Alias for the reviewed DST suite

time-travel: secrets ## Run one isolated fixed-clock reconciliation (at=RFC3339 required)
	@test -n "$(at)" || { printf 'Usage: make time-travel at=2026-08-29T08:00:00-04:00\n' >&2; exit 1; }
	CLOCK_MODE=fixed TEST_NOW='$(at)' COMPOSE_PROJECT_NAME=relantern-time-travel POSTGRES_PUBLISHED_PORT=15432 FAKE_DELIVERY_PUBLISHED_PORT=18092 $(COMPOSE_BASE) up --build --detach postgres fake-delivery migrate seed
	CLOCK_MODE=fixed TEST_NOW='$(at)' COMPOSE_PROJECT_NAME=relantern-time-travel POSTGRES_PUBLISHED_PORT=15432 FAKE_DELIVERY_PUBLISHED_PORT=18092 STACK_LONG_RUNNING='postgres fake-delivery' STACK_ONE_SHOTS='migrate seed' bash scripts/stack-wait.sh
	CLOCK_MODE=fixed TEST_NOW='$(at)' COMPOSE_PROJECT_NAME=relantern-time-travel POSTGRES_PUBLISHED_PORT=15432 FAKE_DELIVERY_PUBLISHED_PORT=18092 $(COMPOSE_BASE) run --rm worker once
	@capture_json="$$(curl --fail --silent --show-error http://127.0.0.1:18092/captures)"; printf '%s\n' "$$capture_json"; printf '%s' "$$capture_json" | rg -q '"count":1'

time-travel-clean: ## Remove the isolated time-travel stack after confirmation
	@printf 'Type relantern-time-travel to delete its disposable volumes: '; read -r answer; test "$$answer" = relantern-time-travel
	COMPOSE_PROJECT_NAME=relantern-time-travel $(COMPOSE_BASE) down --volumes --remove-orphans

observability: ## Start the optional local LGTM profile
	$(COMPOSE_DEV) --profile observability up -d otel-lgtm

config-check: secrets ## Validate Compose, Docker, env, and Railway parity
	bash scripts/config-check.sh

demo: dev ## Open the anonymous static demo placeholder
	@printf 'Open http://127.0.0.1:3000/demo\n'

demo-audit: ## Validate the demo's current static isolation contract
	$(PNPM) --filter @relantern/web build
	@rg -q 'noindex,nofollow' docs/demo-route-contract.md

prepush: lint typecheck test generate-check config-check ## Run required local fast release gates
	bash scripts/policy-check.sh
	$(GO) test -race ./...
	$(PNPM) build

prodlike: secrets ## Build and run the exact production Docker stages
	$(COMPOSE_BASE) up --build --detach
	bash scripts/stack-wait.sh

prodlike-smoke: prodlike ## Smoke test all production-like health surfaces
	curl --fail --silent --show-error http://127.0.0.1:3000/healthz >/dev/null
	curl --fail --silent --show-error http://127.0.0.1:8080/readyz >/dev/null
	curl --fail --silent --show-error http://127.0.0.1:8092/healthz >/dev/null

sbom: ## Require Syft before generating release SBOMs
	@command -v syft >/dev/null 2>&1 || { printf 'Install pinned Syft before generating release SBOMs.\n' >&2; exit 1; }
	@mkdir -p dist/sbom
	syft dir:. -o cyclonedx-json=dist/sbom/repository.cdx.json

clean: ## Remove build outputs while preserving local volumes
	$(PNPM) exec rimraf apps/web/.next coverage dist 2>/dev/null || true
	$(GO) clean -cache -testcache

reset: ## Confirm and remove only Relantern local containers and volumes
	bash scripts/reset.sh
