# Compromised dependency runbook

## Trigger

- OSV, Trivy, CodeQL, dependency review, registry provenance, maintainer notice,
  or runtime behavior indicates a compromised dependency, action, or image.

## Impact

Build credentials, CI tokens, application secrets, evidence integrity, and
deployed services may be affected even without a conventional CVE.

## Immediate containment

1. Stop releases and disable affected external features. Revoke workflow or
   provider credentials if execution with secrets is plausible.
2. Record exact package/image/action version, lockfile or digest, introduction
   commit, build/run environments, and exposure window.
3. Preserve artifacts and logs; do not reinstall from a mutable tag.

## Exact verification commands

```sh
make security-scan
make sbom
go mod verify
pnpm install --frozen-lockfile
pnpm audit signatures
git log -S 'suspect-name' --all --oneline
```

## Recovery

1. Remove or pin to a verified unaffected exact version from the official
   source, respecting license and compatibility review.
2. Rotate every credential reachable by the compromised build/runtime.
3. Rebuild from a clean immutable archive, rescan source and images, and compare
   SBOM/provenance before promotion.
4. Reprocess only durable data whose integrity can be proven.

## Data-integrity checks

- Lockfile/module sums and base/action digests match the reviewed manifest.
- No generated artifact or image contains the compromised component.
- Claims, source revisions, and audit records were not altered out of band.

## Communication

Record affected versions, reachability, credentials rotated, environments,
fixed SHAs, and evidence. Do not speculate beyond verified scope.

## Post-incident evidence

Retain advisories, SBOM diffs, scan output, provenance, clean rebuild hashes,
rotation evidence, and the dependency-policy improvement.
