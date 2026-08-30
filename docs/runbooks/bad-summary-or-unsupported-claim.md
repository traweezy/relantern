# Bad summary or unsupported claim runbook

## Trigger

- A brief contains a factual error, unsupported material claim, invalid quote
  span, prompt-injection residue, or misleading migration/security guidance.

## Impact

Owner trust and engineering decisions may be harmed. Source evidence remains
authoritative; unsafe content must not continue to publish or deliver.

## Immediate containment

1. Suppress the affected item/story and disable its pending external delivery.
2. Preserve item, revision, AI run, prompt/model/schema, claim, span, and source
   IDs. Do not edit immutable output in place.
3. Determine whether the issue is source quality, parsing, validation,
   synthesis, or presentation.

## Exact verification commands

```sh
make test-extraction
make test-research
make digest-preview
docker compose logs --since=30m worker api
```

## Recovery

1. Correct deterministic validation or prompt/schema version with a reviewed
   fixture reproducing the failure.
2. Re-run from the immutable source revision under a new AI run identity.
3. Publish only after every material claim has attributable primary evidence;
   retain the suppressed version for audit.

## Data-integrity checks

- Original response/run provenance is unchanged and not relabeled as valid.
- New claims point to valid spans in the exact revision.
- Digest retries never substitute the corrected content into an old payload.

## Communication

Record the inaccurate statement, verified correction, affected surfaces,
exposure window, and whether delivery occurred. Use primary-source language.

## Post-incident evidence

Retain sanitized failing fixture, run/provenance IDs, validator output, corrected
fixture, regression test, and owner impact assessment.
