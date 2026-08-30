# PR 19 Railway staging readiness evidence

Date: 2026-08-30
Scope: current GitHub and Railway control-plane inspection plus local and
hosted configuration validation through implementation SHA
`cbde9c73119513378832b440b4342a0c2b12a36c`. No Railway infrastructure
apply, deployment, public-domain creation, provider request, delivery, tag, or
production change occurred.

## Local results

| Gate | Result | Evidence |
|---|---|---|
| complete local pre-push gate | PASS | lint, format, vet, unit, integration, generation drift, configuration, policy, race, and production web build |
| Railway IaC typecheck and graph tests | PASS | strict TypeScript and seven Vitest topology, source, and fuse tests |
| repository config parity | PASS | Compose, Docker, environment, image digest, and Railway parity checks |
| dependency integrity | PASS | 280 packages had verified registry signatures; audit found no known vulnerabilities |
| transaction contention | PASS | affected integration packages passed ten consecutive runs with bounded serializable retry and worker-isolated River queues |
| hosted CI | PASS | all 26 jobs passed in GitHub Actions run `33297778036` for the exact implementation SHA |
| hosted CodeQL | SKIPPED | run `33297778043`; the private-repository plan does not provide the configured CodeQL gate |
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
- Environments `staging` and `production` exist. Their custom deployment
  policies admit only `staging` and `master`, respectively.
- Merge commits and rebase merges are disabled. Squash merge is the only merge
  mode, and merged head branches are deleted automatically.
- Repository and staging environment secrets and variables are absent. No
  provider credential is stored in GitHub.
- All 26 jobs in
  `https://github.com/traweezy/relantern/actions/runs/33297778036` passed for
  `cbde9c73119513378832b440b4342a0c2b12a36c`.
- Branch protection queries for `staging` and `master` return HTTP 403.
- Repository ruleset queries return HTTP 403.

This private-repository plan cannot enforce the specification's branch and
ruleset protections. Environment branch restrictions are active, but no human
reviewer or protected-branch capability is claimed. PR20 implements and tests
the attended fallback: exact staging source/tree verification, a
GitHub-verified signed tag, full SHA, and typed `DEPLOY_PRODUCTION`
confirmation from `master` before any production secret or deployment step
can run.

## Remaining staging prerequisites

- Apply the reviewed seven-resource Railway plan only after its cost is
  accepted.
- Create and enter a staging-only GitHub OAuth app out of band.
- Create a low-cap OpenAI staging project, enter its project ID and key out of
  band, and confirm the provider hard cap.
- Enter a staging-only Discord webhook; do not use a production recipient.
- Configure the encrypted off-platform backup destination and automation.
- Generate a Railway domain for `web` only after the services are healthy.

Secret values must not be added to this evidence or pasted into an agent
session.

## Migration and rollback

PR19 adds no database migration and has not created hosted state. Rollback is a
normal revert of the configuration commit. Once durable hosted resources
contain data, they must be preserved; application rollback is to a prior
compatible SHA and database schema evolution remains forward-only.

## Decision

The PR19 implementation, local gate, and hosted CI pass. Hosted readiness and
P9 remain blocked by the intentionally unapplied seven-resource plan, absent
staging credentials/domain/backup automation, and the minimum 14-day
observation window. Production deployment is not ready.
