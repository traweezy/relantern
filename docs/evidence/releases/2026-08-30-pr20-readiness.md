# PR 20 release-contract readiness evidence

Date: 2026-08-30
Scope: local release tooling plus hosted workflow, policy, and control-plane
validation through implementation SHA
`cbde9c73119513378832b440b4342a0c2b12a36c`. No release pull request, tag,
release, secret, Railway change, deployment, provider call, or delivery
occurred.

## Local results

| Gate | Result | Evidence |
|---|---|---|
| release source validation | PASS | same-repository `staging` to `master` and non-draft boundary tests |
| release tree validation | PASS | approved commit/tree identity, evidence-add-only exception, exact candidate tree, and adversarial rewrite tests |
| release evidence validation | PASS | strict JSON, hashes, path/symlink bounds, passing soak, restore RPO/RTO, and typed security/pre-push reports |
| signed tag validation | PASS | annotated object, exact commit, stable tag, and GitHub `verified: true`/`reason: valid` requirements |
| master integrity | PASS | staging release and documented hotfix fixtures; direct, fork, wrong-branch, and unlabeled paths rejected |
| workflow syntax and security | PASS | actionlint, shell syntax, repository action-pin policy, and offline zizmor |
| hosted implementation CI | PASS | all 26 jobs passed in run `33297778036` for the exact implementation SHA |
| production Railway source | PASS | production application services use empty IaC sources and cannot follow mutable branch pushes |
| production side effects | NONE | production authorization stops before secrets and contains no apply/deploy command |

The release workflow builds from an immutable Git archive of the approved SHA,
generates a deterministic source archive, SPDX SBOM, in-toto/SLSA provenance,
validated evidence report, and strict checksums. A dedicated SSH signing secret
is isolated to a dependent signing job and is required only after every
validation and build gate passes.

## Current blocking state

This document is not production release evidence and is not referenced by a
passing release manifest. `docs/evidence/releases/release-template.json` is
intentionally incomplete and fails closed. Railway staging is unapplied, its
OAuth/OpenAI/delivery/backup prerequisites are absent, P9 has not started, no
336-hour passing ledger exists, and no production signing key or tagged master
release is claimed. Repository branch protection and rulesets also remain
unavailable on the current private-repository plan; the tested attended
fallback does not claim otherwise.

## Permissions and variables

- New secret: future `RELEASE_SIGNING_PRIVATE_KEY`, a dedicated Ed25519 signing
  key whose public half must be registered with GitHub as a signing key.
- New non-secret variable: future `RELEASE_SIGNING_EMAIL`, a verified email on
  the account that owns the registered signing key.
- Current repository and environment secrets and variables: none.
- Release PR checks: read-only contents.
- Master integrity: read-only contents and pull-request metadata; every
  first-parent commit in each master push range is audited.
- Post-merge build: read-only contents and pull-request metadata.
- Dependent tag job: contents write only for the verified tag.
- Production authorization: read-only contents and pull-request metadata via
  the built-in GitHub token only; no production environment secret is read.

## Migration and rollback

PR20 adds no migration. Local rollback is a normal revert before release use.
Once created, signed tags and evidence are immutable records and are not
deleted during rollback. Production remains blocked.
