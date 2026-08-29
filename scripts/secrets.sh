#!/usr/bin/env bash
set -euo pipefail

secret_directory=".local/secrets"
case "$(realpath -m "${secret_directory}")" in
  "$(pwd)/.local/secrets") ;;
  *) printf 'Refusing unexpected secret directory.\n' >&2; exit 1 ;;
esac

umask 077
mkdir -p "${secret_directory}"

generate_secret() {
  local secret_name="$1"
  local secret_path="${secret_directory}/${secret_name}"
  if [[ ! -f "${secret_path}" ]]; then
    od -An -N32 -tx1 /dev/urandom | tr -d ' \n' >"${secret_path}"
  fi
  chmod 600 "${secret_path}"
}

generate_secret database_password
generate_secret better_auth_secret
generate_secret local_oauth_stub_secret
generate_secret minio_access_key
generate_secret minio_secret_key
generate_secret fake_openai_secret
generate_secret fake_delivery_secret

printf 'Local secrets are present under %s (values not displayed).\n' "${secret_directory}"
