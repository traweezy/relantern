#!/usr/bin/env bash
set -euo pipefail

base_sha="${1:-}"
head_sha="${2:-}"
full_sha='^[0-9a-f]{40}$|^[0-9a-f]{64}$'
if [[ ! "${base_sha}" =~ ${full_sha} || ! "${head_sha}" =~ ${full_sha} ]]; then
  printf 'Usage: %s <full-base-sha> <full-head-sha>\n' "${0##*/}" >&2
  exit 64
fi
git cat-file -e "${base_sha}^{commit}"
git cat-file -e "${head_sha}^{commit}"

manifests=()
while IFS= read -r -d '' path; do
  if [[ "${path}" =~ ^docs/evidence/releases/v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.json$ ]]; then
    manifests+=("${path}")
  fi
done < <(git diff --diff-filter=A --name-only -z "${base_sha}" "${head_sha}" -- docs/evidence/releases)

if [[ "${#manifests[@]}" -ne 1 ]]; then
  printf 'Exactly one stable release manifest must be added; found %d.\n' "${#manifests[@]}" >&2
  exit 1
fi
test -f "${manifests[0]}"
printf '%s\n' "${manifests[0]}"
