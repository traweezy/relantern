# PR 19 Railway staging readiness evidence

Date: 2026-08-30
Scope: read-only GitHub and Railway control-plane inspection plus local
configuration validation. No infrastructure apply, deployment, public-domain
creation, provider request, delivery, GitHub write, push, or tag occurred.

## Local results

| Gate | Result | Evidence |
|---|---|---|
| complete local pre-push gate | PASS | lint, format, vet, unit, integration, generation drift, configuration, policy, race, and production web build |
| Railway IaC typecheck and graph tests | PASS | strict TypeScript and five Vitest topology/fuse tests |
| repository config parity | PASS | Compose, Docker, environment, image digest, and Railway parity checks |
| dependency integrity | PASS | 280 packages had verified registry signatures; audit found no known vulnerabilities |
| digest transaction contention | PASS | affected integration packages passed ten consecutive runs after bounded serializable-transaction retry coverage |
| staging plan safety | PASS | seven creates, zero destructive changes |
| clean-plan readiness | BLOCKED | the same seven resources remain unapplied |
| staging soak | NOT STARTED | no staging services, storage, deployment SHA, or daily evidence exists |

The read-only plan contains exactly:

- volume `postgres-data`;
- private bucket `bucket`;
- services `postgres`, `migrate`, `api`, `worker`, and `web`.

## Railway state

- Project: `Relantern`, ID `3bff0471-505e-47ab-a79e-a6f54e46d673`.
- Staging environment ID: `e8389ebf-6fe6-43f2-8e69-72386f8d8209`.
- Production environment ID: `890d96d9-cd7f-4493-8f13-823e4cbec105`.
- `privateNetworkDisabled` is `false` in both environments.
- Both environments contain zero service instances and zero volume instances.
- The project contains zero services and zero buckets.

Therefore no public application surface exists, no migration has run, no live
provider or delivery can be reached, and the 336-hour P9 clock cannot start.

## GitHub state and plan limitation

- The private repository's default branch is `staging`.
- GitHub reports no repository environments.
- Branch protection queries for `staging` and `master` return HTTP 403.
- Repository ruleset queries return HTTP 403.
- Merge commits and rebase merges remain enabled, and automatic head-branch
  deletion remains disabled.

This private-repository plan cannot enforce the specification's branch and
environment protections. No approval is claimed. PR20 must implement and test
the attended fallback: exact staging source/tree verification, signed tag,
full SHA, and typed `DEPLOY_PRODUCTION` confirmation from `master` before any
production secret or deployment step can run.

## Migration and rollback

PR19 adds no database migration and has not created hosted state. Rollback is a
normal revert of the configuration commit. Once durable hosted resources
contain data, they must be preserved; application rollback is to a prior
compatible SHA and database schema evolution remains forward-only.

## Decision

The local PR19 implementation and full repository validation pass. Hosted
readiness and P9 remain blocked by the intentionally unapplied seven-resource
plan, absent staging credentials/domain/backup automation, and the minimum
14-day observation window. Production deployment is not ready.
