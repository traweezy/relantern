# Production release procedure

This procedure implements the P10 release boundary but does not authorize or
perform a production deployment. A release cannot begin until the P9 ledger is
complete and `make soak-validate file=<ledger>` passes.

## Signing prerequisite

Create a dedicated Ed25519 signing key outside the repository. Store only the
private key in the GitHub repository secret `RELEASE_SIGNING_PRIVATE_KEY`, and
register the public key on the release automation account as a GitHub signing
key. Set the non-secret repository variable `RELEASE_SIGNING_EMAIL` to a
verified email on that same account so GitHub can associate the tagger and
signing key. Record the rotation owner and expiry outside Git. Never reuse an
SSH login key or a Railway credential.

The signing job reads this secret only after the separate read-only source,
master-integrity, tree, evidence, and immutable-build job has passed. Its write
token can push only the resulting tag by workflow design. If the secret is
absent, the release fails before tag creation.

## Freeze and evidence

1. Freeze the staging application commit before the soak and record:

   ```sh
   approved_sha="$(git rev-parse staging)"
   approved_tree="$(git rev-parse "${approved_sha}^{tree}")"
   ```

2. Complete the 336-hour ledger against `approved_sha`. Do not amend or replace
   failed ledgers.
3. Run the restore drill, `make prepush`, `make prodlike-smoke`,
   `make security-scan`, gitleaks, pnpm audit, and signature verification
   against that commit. Preserve small redacted logs under `docs/evidence/`.
4. Create strict gate reports. A security report has `kind: "security"` and
   lists all of: `make security-scan`,
   `bash scripts/go-tool.sh run github.com/zricethezav/gitleaks/v8@v8.30.1 git --redact --no-banner`,
   `pnpm audit --audit-level high`, and `pnpm audit signatures`. A pre-push
   report has `kind: "prepush"` and lists `make prepush` and
   `make prodlike-smoke`. Both use version 1, the approved SHA, `result: "pass"`,
   canonical RFC3339 `completedAt`, and at least one hashed evidence reference.
5. Copy `docs/evidence/releases/release-template.json` to
   `docs/evidence/releases/vMAJOR.MINOR.PATCH.json`. Fill the approved SHA/tree,
   decision time, and SHA-256 references to the passing soak ledger, restore
   JSON, security report, and pre-push report.
6. Commit only new files below `docs/evidence/` after the approved SHA. The
   release tree gate rejects modified evidence and every non-evidence change.
7. Run:

   ```sh
   make release-evidence file=docs/evidence/releases/vMAJOR.MINOR.PATCH.json
   make release-tree \
     file=docs/evidence/releases/vMAJOR.MINOR.PATCH.json \
     staging_sha="$(git rev-parse staging)" \
     candidate_sha="$(git rev-parse staging)"
   make release-check
   ```

## Promotion and artifact

Open one same-repository pull request from `staging` to `master`. Do not use a
feature branch or fork. Require the normal staging checks plus
`release-source`, `release-tree`, and `release-evidence`.

After merge, `release.yml` verifies the associated PR, extracts the approved
commit with `git archive`, runs the complete gates in that extracted tree, and
publishes one bundle containing:

- deterministic source archive;
- SPDX repository SBOM;
- in-toto/SLSA provenance binding the archive, SBOM, approved SHA, and tree;
- validated release manifest and evidence report;
- strict `SHA256SUMS`.

It then creates an SSH-signed annotated stable tag on the exact master commit,
verifies the signature locally, and pushes only that tag. The matching public
key must be registered with GitHub so the tag object reports a valid verified
signature. The workflow performs no Railway apply, migration, application
deployment, provider activation, or delivery activation.

## Attended production boundary

Only after the bundle and signed tag are reviewed, manually run `Production
authorization` from `master` with:

- the exact stable tag;
- the exact full tagged master SHA;
- `DEPLOY_PRODUCTION` typed exactly.

The workflow revalidates the commit's eligible release or hotfix PR, queries
GitHub's tag object, and requires an annotated tag with a valid signature over
the supplied SHA. It then recomputes the release evidence and tree gates. The
current workflow stops at that boundary and does not read a production secret
or deploy. P10 deployment remains blocked until P9 evidence exists, production
credentials/backups/domain are separately verified, and an explicit deployment
implementation is reviewed.

Railway production application services deliberately have no connected GitHub
branch source. A reviewed deployment implementation must upload the exact
detached, signed release checkout to `migrate`, `api`, `worker`, and `web`; it
must not connect those services to mutable `master` autodeploys. The migration
must pass before application services are started, and every resulting
deployment must report the authorized release SHA before traffic or ingestion
is enabled.

## Emergency hotfix

Branch from `master` as `hotfix/<short-name>` and open a PR to `master`. Add the
`emergency-hotfix` label and a strict
`docs/evidence/hotfixes/pr-<number>.json` file with:

- `version: 1` and the PR number;
- a 20-500 character reason;
- credential-free HTTPS incident and staging-backmerge references;
- canonical RFC3339 approval time;
- at least one repository-relative hashed evidence artifact.

Immediately back-merge the fix to staging. `master-integrity` rejects direct
pushes, forks, other branch names, missing labels, and incomplete hotfix
evidence.

## Migration and rollback

The release evidence phase adds no migration. Never delete a signed tag or
rewrite an evidence file. If a later deployment fails, disable worker intake
and live delivery, select the prior compatible signed tag, preserve durable
database/object state, and use only forward-compatible migrations. Restore is
reserved for proven data corruption.
