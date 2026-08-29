#!/usr/bin/env bash
set -euo pipefail

script_directory="$(cd -P -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(cd -P -- "${script_directory}/.." && pwd)"
tool_directory="${CI_TOOL_BIN_DIR:-${repository_root}/.local/bin}"
scope="${1:-lint}"
temporary_directory="$(mktemp -d)"
trap 'rm -rf -- "${temporary_directory}"' EXIT

if [[ "${scope}" != "lint" ]]; then
  printf 'Usage: %s lint\n' "${0##*/}" >&2
  exit 64
fi

if [[ "$(uname -s)" != "Linux" || "$(uname -m)" != "x86_64" ]]; then
  printf 'Pinned CI tool installer supports Linux x86_64 only.\n' >&2
  exit 1
fi

mkdir -p -- "${tool_directory}"

ripgrep_archive="${temporary_directory}/ripgrep.tar.gz"
curl --fail --location --proto '=https' --tlsv1.2 \
  --retry 3 --retry-all-errors \
  --output "${ripgrep_archive}" \
  'https://github.com/BurntSushi/ripgrep/releases/download/15.2.0/ripgrep-15.2.0-x86_64-unknown-linux-musl.tar.gz'
printf '%s  %s\n' \
  '33e15bcf1624b25cdd2a55813a47a2f95dbe126268203e76aa6a585d1e7b149c' \
  "${ripgrep_archive}" | sha256sum --check --status
tar -xzf "${ripgrep_archive}" -C "${temporary_directory}"
install -m 0755 \
  "${temporary_directory}/ripgrep-15.2.0-x86_64-unknown-linux-musl/rg" \
  "${tool_directory}/rg"

printf 'Installed checksum-verified CI tools in %s.\n' "${tool_directory}"
