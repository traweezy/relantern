#!/usr/bin/env bash
set -euo pipefail

manifest_path="${1:-}"
output_directory="${2:-}"
repository="${3:-}"
builder_id="${4:-}"
invocation_id="${5:-}"
if [[ -z "${manifest_path}" || -z "${output_directory}" || -z "${repository}" ||
  -z "${builder_id}" || -z "${invocation_id}" ]]; then
  printf 'Usage: %s <manifest> <output-dir> <owner/repo> <builder-id> <invocation-id>\n' "${0##*/}" >&2
  exit 64
fi

script_directory="$(cd -P -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(cd -P -- "${script_directory}/.." && pwd)"
cd "${repository_root}"

bash scripts/go-tool.sh run ./cmd/releasectl evidence \
  --manifest "${manifest_path}" \
  --repository-root . >/dev/null
tag="$(bash scripts/go-tool.sh run ./cmd/releasectl describe --manifest "${manifest_path}" --field tag)"
approved_sha="$(bash scripts/go-tool.sh run ./cmd/releasectl describe --manifest "${manifest_path}" --field approved-release-sha)"
version="${tag#v}"

mkdir -p -- "${output_directory}"
if find "${output_directory}" -mindepth 1 -maxdepth 1 -print -quit | grep -q .; then
  printf 'Release output directory must be empty: %s\n' "${output_directory}" >&2
  exit 1
fi

temporary_directory="$(mktemp -d)"
trap 'rm -rf -- "${temporary_directory}"' EXIT
source_directory="${temporary_directory}/source"
mkdir -p -- "${source_directory}"

archive_name="relantern-${version}-source.tar.gz"
sbom_name="relantern-${version}.spdx.json"
provenance_name="relantern-${version}.intoto.json"
manifest_name="relantern-${version}-release-evidence.json"
report_name="relantern-${version}-evidence-report.json"

git archive --format=tar --prefix="relantern-${version}/" "${approved_sha}" | \
  gzip -n >"${output_directory}/${archive_name}"
git archive --format=tar "${approved_sha}" | tar -xf - -C "${source_directory}"

(
  cd "${source_directory}"
  bash scripts/pnpm-tool.sh install --frozen-lockfile
  make prepush
  make security-scan
)

if [[ ! -x "${repository_root}/.local/bin/syft" ]] && ! command -v syft >/dev/null 2>&1; then
  bash scripts/install-ci-tools.sh sbom
fi
PATH="${repository_root}/.local/bin:${PATH}" \
  syft "dir:${source_directory}" -o "spdx-json=${output_directory}/${sbom_name}"

cp -- "${manifest_path}" "${output_directory}/${manifest_name}"
bash scripts/go-tool.sh run ./cmd/releasectl evidence \
  --manifest "${manifest_path}" \
  --repository-root . >"${output_directory}/${report_name}"
bash scripts/go-tool.sh run ./cmd/releasectl provenance \
  --manifest "${manifest_path}" \
  --repository-root . \
  --repository "${repository}" \
  --builder-id "${builder_id}" \
  --invocation-id "${invocation_id}" \
  --archive "${output_directory}/${archive_name}" \
  --sbom "${output_directory}/${sbom_name}" >"${output_directory}/${provenance_name}"

(
  cd "${output_directory}"
  sha256sum \
    "${archive_name}" \
    "${sbom_name}" \
    "${provenance_name}" \
    "${manifest_name}" \
    "${report_name}" >SHA256SUMS
  sha256sum --check --strict SHA256SUMS
)
