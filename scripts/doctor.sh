#!/usr/bin/env bash
set -euo pipefail

failures=0

pass() {
  printf 'PASS  %s\n' "$1"
}

fail() {
  printf 'FAIL  %s\n' "$1" >&2
  failures=$((failures + 1))
}

for command_name in docker git make setfacl getfacl; do
  if command -v "${command_name}" >/dev/null 2>&1; then
    pass "${command_name} is installed"
  else
    fail "${command_name} is required"
  fi
done

if ! command -v docker >/dev/null 2>&1; then
  exit 1
fi

if docker info >/dev/null 2>&1; then
  pass "Docker daemon is reachable"
else
  fail "Docker daemon is not reachable"
fi

if docker compose version >/dev/null 2>&1; then
  pass "Docker Compose is available: $(docker compose version --short)"
else
  fail "Docker Compose v2 is required"
fi

if docker compose watch --help >/dev/null 2>&1; then
  pass "Docker Compose Watch is available"
else
  fail "Docker Compose Watch is required"
fi

architecture="$(uname -m)"
case "${architecture}" in
  x86_64|aarch64|arm64) pass "supported architecture: ${architecture}" ;;
  *) fail "unsupported architecture: ${architecture}" ;;
esac

available_kib="$(df -Pk . | awk 'NR == 2 {print $4}')"
if [[ "${available_kib}" =~ ^[0-9]+$ ]] && (( available_kib >= 10485760 )); then
  pass "at least 10 GiB of workspace disk is available"
else
  fail "at least 10 GiB of workspace disk is required"
fi

if [[ "${ALLOW_LIVE_EXTERNAL_APIS:-false}" == "true" || "${ALLOW_LIVE_DELIVERY:-false}" == "true" ]]; then
  fail "live-provider fuses must be false for the default local profile"
else
  pass "live-provider and live-delivery fuses are disabled"
fi

for optional_command in go node pnpm; do
  if command -v "${optional_command}" >/dev/null 2>&1; then
    case "${optional_command}" in
      go) optional_version="$(go version)" ;;
      node) optional_version="$(node --version)" ;;
      pnpm) optional_version="$(pnpm --version)" ;;
    esac
    pass "optional host tool ${optional_command}: ${optional_version}"
  else
    printf 'INFO  optional host tool %s is absent; pinned containers will be used\n' "${optional_command}"
  fi
done

if [[ -d .local/secrets ]]; then
  if [[ -L .local/secrets || "$(stat -c '%u %a' -- .local/secrets)" != "$(id -u) 700" ]]; then
    fail "local secret directory must be owned by the current user with mode 0700"
  fi
  expected_acl=$'user::rw-\nuser:65532:r--\ngroup::---\nmask::r--\nother::---'
  secrets_valid=true
  for secret_path in .local/secrets/*; do
    [[ -e "${secret_path}" || -L "${secret_path}" ]] || continue
    if [[ ! -f "${secret_path}" || -L "${secret_path}" ]] ||
       [[ "$(stat -c '%u %a' -- "${secret_path}")" != "$(id -u) 640" ]] ||
       [[ "$(getfacl -cpn -- "${secret_path}")" != "${expected_acl}" ]]; then
      fail "local secret ${secret_path} must be readable only by its owner and container UID 65532"
      secrets_valid=false
    fi
  done
  if [[ "${secrets_valid}" == true ]]; then
    pass "checked local secret ownership and container read ACLs"
  fi
else
  printf 'INFO  local secrets do not exist yet; make secrets will create them\n'
fi

if (( failures > 0 )); then
  printf '\nDoctor found %d blocking issue(s).\n' "${failures}" >&2
  exit 1
fi

printf '\nRelantern local prerequisites are ready.\n'
