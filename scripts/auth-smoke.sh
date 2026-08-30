#!/usr/bin/env bash
set -euo pipefail

base_url="http://127.0.0.1:3000"
auth_test_directory="$(mktemp -d)"
trap 'rm -r "${auth_test_directory}"' EXIT

expect_status() {
  local expected="$1"
  local url="$2"
  local actual
  actual="$(curl --silent --output /dev/null --write-out '%{http_code}' "${url}")"
  if [[ "${actual}" != "${expected}" ]]; then
    printf 'Expected %s from %s, received %s.\n' "${expected}" "${url}" "${actual}" >&2
    exit 1
  fi
}

expect_status 307 "${base_url}/"
expect_status 200 "${base_url}/demo"
expect_status 401 "${base_url}/api/private"

curl --fail --silent --show-error \
  --dump-header "${auth_test_directory}/login-headers" \
  --output "${auth_test_directory}/login.html" \
  "${base_url}/login"

node --input-type=module - \
  "${auth_test_directory}/login-headers" \
  "${auth_test_directory}/login.html" <<'NODE'
import { readFile } from "node:fs/promises";

const [headersPath, htmlPath] = process.argv.slice(2);
const [headers, html] = await Promise.all([
  readFile(headersPath, "utf8"),
  readFile(htmlPath, "utf8"),
]);
const nonce = headers.match(/content-security-policy:.*'nonce-([^']+)'/i)?.[1];
if (nonce === undefined) {
  throw new Error("Login response is missing a CSP nonce");
}
const scripts = [...html.matchAll(/<script\b([^>]*)>/gi)];
if (scripts.length === 0) {
  throw new Error("Login response did not render any scripts");
}
const expectedAttribute = `nonce="${nonce}"`;
for (const script of scripts) {
  if (!script[1]?.includes(expectedAttribute)) {
    throw new Error(`Login script is missing the request CSP nonce: ${script[0]}`);
  }
}
NODE

curl --fail --silent --show-error \
  --cookie-jar "${auth_test_directory}/cookies" \
  --header 'Content-Type: application/json' \
  --header "Origin: ${base_url}" \
  --data '{"provider":"github","callbackURL":"/","errorCallbackURL":"/login"}' \
  "${base_url}/api/auth/sign-in/social" > "${auth_test_directory}/signin.json"

bash scripts/pnpm-tool.sh exec node -e \
  'let body=""; process.stdin.on("data", chunk => body += chunk); process.stdin.on("end", () => process.stdout.write(JSON.parse(body).url));' \
  < "${auth_test_directory}/signin.json" > "${auth_test_directory}/authorize-url"

curl --fail --silent --show-error \
  --cookie "${auth_test_directory}/cookies" \
  --dump-header "${auth_test_directory}/authorize-headers" \
  --output /dev/null \
  "$(sed -n '1p' "${auth_test_directory}/authorize-url")"

sed -n 's/^[Ll]ocation: //p' "${auth_test_directory}/authorize-headers" \
  | tr -d '\r' > "${auth_test_directory}/callback-url"
test -s "${auth_test_directory}/callback-url"

curl --fail --silent --show-error \
  --cookie "${auth_test_directory}/cookies" \
  --cookie-jar "${auth_test_directory}/cookies" \
  --dump-header "${auth_test_directory}/callback-headers" \
  --output /dev/null \
  "$(sed -n '1p' "${auth_test_directory}/callback-url")"

rg -qi '^set-cookie: relantern\.session_token=.*httponly.*samesite=lax' \
  "${auth_test_directory}/callback-headers"

authenticated_status="$(curl --silent --show-error \
  --cookie "${auth_test_directory}/cookies" \
  --output "${auth_test_directory}/workspace.html" \
  --write-out '%{http_code}' \
  "${base_url}/")"
test "${authenticated_status}" = "200"
rg -q 'Owner verified' "${auth_test_directory}/workspace.html"

curl --fail --silent --show-error \
  --cookie "${auth_test_directory}/cookies" \
  --cookie-jar "${auth_test_directory}/cookies" \
  --header 'Content-Type: application/json' \
  --header "Origin: ${base_url}" \
  --data '{}' \
  "${base_url}/api/auth/sign-out" >/dev/null

signed_out_status="$(curl --silent --output /dev/null --write-out '%{http_code}' \
  --cookie "${auth_test_directory}/cookies" "${base_url}/")"
test "${signed_out_status}" = "307"

printf 'Disconnected owner OAuth, cookie, workspace, and sign-out checks passed.\n'
