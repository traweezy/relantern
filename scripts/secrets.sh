#!/usr/bin/env bash
set -euo pipefail

secret_directory=".local/secrets"
case "$(realpath -m "${secret_directory}")" in
  "$(pwd)/.local/secrets") ;;
  *) printf 'Refusing unexpected secret directory.\n' >&2; exit 1 ;;
esac

umask 077
if ! command -v setfacl >/dev/null 2>&1; then
  printf 'setfacl is required to grant the nonroot local containers read access to secrets.\n' >&2
  exit 1
fi
mkdir -p "${secret_directory}"
chmod 700 "${secret_directory}"

generate_secret() {
  local secret_name="$1"
  local secret_path="${secret_directory}/${secret_name}"
  if [[ -L "${secret_path}" || ( -e "${secret_path}" && ! -f "${secret_path}" ) ]]; then
    printf 'Refusing unexpected secret path %s.\n' "${secret_path}" >&2
    exit 1
  fi
  if [[ ! -f "${secret_path}" ]]; then
    od -An -N32 -tx1 /dev/urandom | tr -d ' \n' >"${secret_path}"
  fi
  chmod 600 "${secret_path}"
  setfacl -b "${secret_path}"
  setfacl -m u:65532:r-- "${secret_path}"
}

generate_secret database_password
generate_secret better_auth_secret
generate_secret local_oauth_stub_secret
generate_secret minio_access_key
generate_secret minio_secret_key
generate_secret fake_openai_secret
generate_secret fake_delivery_secret
generate_secret openai_webhook_secret
generate_secret web_internal_service_token

printf 'Local secrets are present under %s (values not displayed).\n' "${secret_directory}"
