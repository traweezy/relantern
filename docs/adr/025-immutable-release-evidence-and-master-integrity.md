# ADR-025: Immutable release evidence and master integrity

Status: Accepted
Date: 2026-08-30

## Context

Production promotion must prove that the code observed during the 336-hour
staging soak is the code entering `master`. The final soak ledger, restore
report, security output, and release decision necessarily become durable after
the code SHA is frozen. Putting those records in the frozen commit would create
a self-reference; allowing ordinary changes afterward would invalidate the
soak.

The private-repository plan cannot enforce branch or environment protection.
The release contract therefore must fail closed in repository code and must
not claim a human approval or protected environment that GitHub cannot supply.

## Decision

- A versioned release manifest identifies one stable `vMAJOR.MINOR.PATCH` tag,
  the approved full staging commit SHA, and its exact Git tree SHA.
- The approved commit must be an ancestor of the promotion. Every subsequent
  tree difference must be an added file under `docs/evidence/`; modification,
  deletion, rename, code change, or build change is forbidden. The pull-request
  merge candidate must have the exact staging-head tree.
- Release evidence is repository-relative, regular-file, size-bounded, and
  SHA-256 bound. New evidence must be a non-executable Git blob; manifest
  symlinks, evidence symlinks that escape the repository, and malformed or
  trailing JSON are rejected.
- The staging ledger must evaluate to `pass` for the approved SHA. The restore
  report must meet its RPO/RTO, and typed security and pre-push reports must
  record the required commands, a passing result, the approved SHA, and hashed
  supporting artifacts.
- A release pull request must originate from the same repository's `staging`
  branch and target `master`. `release-source`, `release-tree`, and
  `release-evidence` are separate required checks.
- After merge, automation re-verifies the associated pull request, evidence,
  and tree; runs the complete gates from an immutable archive of the approved
  SHA; emits a deterministic source archive, SPDX SBOM, SLSA-shaped in-toto
  provenance, checksums, and evidence report; and creates an SSH-signed tag on
  the exact master commit. It never deploys production.
- `master-integrity` verifies every master commit is associated with a merged
  same-repository staging release PR. A `hotfix/*` PR is accepted only with the
  `emergency-hotfix` label and strict, hashed `docs/evidence/hotfixes/pr-N.json`
  record containing incident and staging-backmerge references.
- Because environment review is unavailable, production authorization is a
  manual workflow from `master` requiring the exact GitHub-verified annotated
  tag, full commit SHA, and typed `DEPLOY_PRODUCTION`. This workflow deliberately
  stops before reading production secrets or deploying.
- Railway production application services have an empty source in IaC. They
  cannot follow mutable `master` pushes; a later reviewed P10 deployment job
  must upload the exact detached signed-release checkout after authorization.

## Consequences

The evidence-only commit exception solves the SHA self-reference without
allowing code drift. A fabricated Markdown statement, incomplete soak, stale
restore, lightweight tag, unknown signing key, fork release, direct master
push, or unlabelled hotfix cannot satisfy the contract.

The signing job requires a dedicated Ed25519 private key in the
`RELEASE_SIGNING_PRIVATE_KEY` repository secret and the matching public key
registered with GitHub as a signing key. The immutable build runs in a separate
read-only job; only the dependent signing job receives `contents: write` for
the tag push. Master integrity adds read-only pull-request access and audits
every first-parent commit in a master push range. No Railway, OpenAI, delivery,
OAuth, or database secret is introduced or read.

This decision adds no migration. Before a tag exists, rollback is a normal code
revert. Tags and evidence are immutable audit records and are not deleted as a
rollback. Application rollback remains a prior compatible signed release;
database evolution stays forward-only.
