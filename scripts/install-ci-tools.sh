#!/usr/bin/env bash
set -euo pipefail

script_directory="$(cd -P -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(cd -P -- "${script_directory}/.." && pwd)"
tool_directory="${CI_TOOL_BIN_DIR:-${repository_root}/.local/bin}"
scope="${1:-lint}"
temporary_directory="$(mktemp -d)"
trap 'rm -rf -- "${temporary_directory}"' EXIT

if [[ "$(uname -s)" != "Linux" || "$(uname -m)" != "x86_64" ]]; then
  printf 'Pinned CI tool installer supports Linux x86_64 only.\n' >&2
  exit 1
fi

mkdir -p -- "${tool_directory}"

download_checked() {
  local url="$1"
  local checksum="$2"
  local destination="$3"
  curl --fail --location --proto '=https' --tlsv1.2 \
    --retry 3 --retry-all-errors \
    --output "${destination}" \
    "${url}"
  printf '%s  %s\n' "${checksum}" "${destination}" | sha256sum --check --status
}

install_lint_tools() {
  local archive="${temporary_directory}/ripgrep.tar.gz"
  download_checked \
    'https://github.com/BurntSushi/ripgrep/releases/download/15.2.0/ripgrep-15.2.0-x86_64-unknown-linux-musl.tar.gz' \
    '33e15bcf1624b25cdd2a55813a47a2f95dbe126268203e76aa6a585d1e7b149c' \
    "${archive}"
  tar -xzf "${archive}" -C "${temporary_directory}"
  install -m 0755 \
    "${temporary_directory}/ripgrep-15.2.0-x86_64-unknown-linux-musl/rg" \
    "${tool_directory}/rg"
}

install_security_tools() {
  local osv_binary="${temporary_directory}/osv-scanner"
  local trivy_archive="${temporary_directory}/trivy.tar.gz"
  local zizmor_archive="${temporary_directory}/zizmor.tar.gz"
  download_checked \
    'https://github.com/google/osv-scanner/releases/download/v2.5.1/osv-scanner_linux_amd64' \
    'f9f25499a2c8cc367b3af45df2ea7eeca7fbccceab9c35079968f4b3652194be' \
    "${osv_binary}"
  install -m 0755 "${osv_binary}" "${tool_directory}/osv-scanner"
  download_checked \
    'https://github.com/aquasecurity/trivy/releases/download/v0.74.0/trivy_0.74.0_Linux-64bit.tar.gz' \
    '2ae6fe3ee734b7fdf11335663e18c75ea12dccc76062f09f164a3b0f8be4371a' \
    "${trivy_archive}"
  tar -xzf "${trivy_archive}" -C "${temporary_directory}" trivy
  install -m 0755 "${temporary_directory}/trivy" "${tool_directory}/trivy"
  download_checked \
    'https://github.com/zizmorcore/zizmor/releases/download/v1.29.0/zizmor-x86_64-unknown-linux-gnu.tar.gz' \
    'dd96df044a6e8538d5f423790f453bdd03d49e5b2bcc38214acc41a2f1297839' \
    "${zizmor_archive}"
  tar -xzf "${zizmor_archive}" -C "${temporary_directory}" zizmor
  install -m 0755 "${temporary_directory}/zizmor" "${tool_directory}/zizmor"
}

install_sbom_tools() {
  local archive="${temporary_directory}/syft.tar.gz"
  download_checked \
    'https://github.com/anchore/syft/releases/download/v1.51.0/syft_1.51.0_linux_amd64.tar.gz' \
    '2a2e837a2c8d59ec9af5472ee22d3b04ee463c4e44476ecf993fd1e5ab6ebc7f' \
    "${archive}"
  tar -xzf "${archive}" -C "${temporary_directory}" syft
  install -m 0755 "${temporary_directory}/syft" "${tool_directory}/syft"
}

case "${scope}" in
  lint) install_lint_tools ;;
  security) install_security_tools ;;
  sbom) install_sbom_tools ;;
  supply-chain)
    install_security_tools
    install_sbom_tools
    ;;
  *)
    printf 'Usage: %s {lint|security|sbom|supply-chain}\n' "${0##*/}" >&2
    exit 64
    ;;
esac

printf 'Installed checksum-verified %s tools in %s.\n' "${scope}" "${tool_directory}"
