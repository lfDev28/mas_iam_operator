#!/usr/bin/env bash
# Render a single, fully-materialized install manifest for release/download.
#
# Usage:
#   SCIM_BRIDGE_ENV_FILE=env/scim-bridge.env.local \
#   SCIM_BRIDGE_OUTPUT=manifests/scim-bridge-install.yaml \
#   ./scripts/scim-bridge-04-render-install-manifest.sh
#
# This script renders:
#   - manifests/scim-bridge.yaml
#   - manifests/scim-bridge-keycloak-bootstrap.yaml (optional)
#   - manifests/scim-bridge-keycloak-route-cert.yaml (optional)
#   - manifests/scim-bridge-mas-profile-bootstrap.yaml (optional)
#
# into one file suitable for `oc apply -f <raw-url>`.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=./_all_in_one_common.sh
source "$ROOT_DIR/scripts/_all_in_one_common.sh"

# Rendered release artifacts should come from env/scim-bridge.env.release by default.
# Local dev/test can override with SCIM_BRIDGE_ENV_FILE=env/scim-bridge.env.local.
if [[ -z "${SCIM_BRIDGE_ENV_FILE:-}" ]]; then
  SCIM_BRIDGE_ENV_FILE="$ROOT_DIR/env/scim-bridge.env.release"
fi

# shellcheck source=./_scim-bridge-env.sh
source "$ROOT_DIR/scripts/_scim-bridge-env.sh"

OUTPUT="${SCIM_BRIDGE_OUTPUT:-$ROOT_DIR/manifests/scim-bridge-install.yaml}"

# Ensure defaults are exported for the template renderer.
set -a

: "${SCIM_BRIDGE_IMAGE:?Set SCIM_BRIDGE_IMAGE (published image tag for release)}"
: "${SCIM_BRIDGE_NAMESPACE:=mas-est}"
: "${SCIM_BRIDGE_KEYCLOAK_RELEASE:=mas-est-iam}"
: "${SCIM_BRIDGE_KEYCLOAK_NAMESPACE:=${SCIM_BRIDGE_NAMESPACE}}"
: "${SCIM_BRIDGE_KEYCLOAK_SERVICE:=${SCIM_BRIDGE_KEYCLOAK_RELEASE}}"
: "${SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_JOB_NAME:=scim-bridge-keycloak-bootstrap}"
: "${SCIM_BRIDGE_KEYCLOAK_ASSIGN_MANAGED_ROLE:=false}"
: "${SCIM_BRIDGE_PROVISION_KEYCLOAK:=true}"
: "${SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_ENABLE:=true}"
: "${SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_JOB_NAME:=scim-bridge-keycloak-route-cert}"
: "${SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_IMAGE:=image-registry.openshift-image-registry.svc:5000/openshift/cli:latest}"
: "${SCIM_BRIDGE_KEYCLOAK_ROUTE_NAMESPACE:=${SCIM_BRIDGE_KEYCLOAK_NAMESPACE}}"
: "${SCIM_BRIDGE_KEYCLOAK_ROUTE_NAME:=scim-bridge-keycloak}"
: "${SCIM_BRIDGE_KEYCLOAK_ROUTE_HOST:=}"
: "${SCIM_BRIDGE_KEYCLOAK_ROUTE_CA_SECRET:=scim-bridge-route-ca}"
: "${SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_DAYS:=825}"
: "${SCIM_BRIDGE_KEYCLOAK_ROUTE_SERVICE:=${SCIM_BRIDGE_KEYCLOAK_SERVICE}}"
: "${SCIM_BRIDGE_KEYCLOAK_ROUTE_DOMAIN_SOURCE:=${SCIM_BRIDGE_KEYCLOAK_RELEASE}}"
: "${SCIM_BRIDGE_KEYCLOAK_REALM:=maximo}"
: "${SCIM_BRIDGE_KEYCLOAK_CLIENT_ID:=scim-admin}"
: "${SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET:=CHANGEME}"
: "${SCIM_BRIDGE_KEYCLOAK_BASE_URL:=https://keycloak.example.com}"

SCIM_BRIDGE_MAS_BASE_URL=${SCIM_BRIDGE_MAS_BASE_URL:-https://api.<mas-instance>.<domain>/scim/v2}
SCIM_BRIDGE_MAS_PROFILE_ID=${SCIM_BRIDGE_MAS_PROFILE_ID:-test1}
SCIM_BRIDGE_MAS_AUTH_TYPE=${SCIM_BRIDGE_MAS_AUTH_TYPE:-jwt}
SCIM_BRIDGE_MAS_API_TOKEN_NAME=${SCIM_BRIDGE_MAS_API_TOKEN_NAME:-CHANGEME}
SCIM_BRIDGE_MAS_API_TOKEN_VALUE=${SCIM_BRIDGE_MAS_API_TOKEN_VALUE:-CHANGEME}
SCIM_BRIDGE_MAS_TOKEN=${SCIM_BRIDGE_MAS_TOKEN:-}

SCIM_BRIDGE_MAS_PROFILE_MAP=${SCIM_BRIDGE_MAS_PROFILE_MAP:-}
SCIM_BRIDGE_MAS_PROFILE_MAP_JSON=${SCIM_BRIDGE_MAS_PROFILE_MAP_JSON:-}
SCIM_BRIDGE_MAS_PROFILE_REQUIRE_LABEL=${SCIM_BRIDGE_MAS_PROFILE_REQUIRE_LABEL:-false}
SCIM_BRIDGE_MAS_INSECURE_SKIP_VERIFY=${SCIM_BRIDGE_MAS_INSECURE_SKIP_VERIFY:-false}
SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ENABLED=${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ENABLED:-false}
SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ID=${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ID:-demo}
SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID=${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID:-workspace}
SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_JSON=${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_JSON:-}
SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_JOB_NAME=${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_JOB_NAME:-scim-bridge-mas-profile-bootstrap}
SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_IMAGE=${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_IMAGE:-quay.io/curl/curl:8.5.0}

SCIM_BRIDGE_KEYCLOAK_CA_FILE=${SCIM_BRIDGE_KEYCLOAK_CA_FILE:-}
SCIM_BRIDGE_KEYCLOAK_INSECURE_SKIP_VERIFY=${SCIM_BRIDGE_KEYCLOAK_INSECURE_SKIP_VERIFY:-false}
SCIM_BRIDGE_MAS_CA_FILE=${SCIM_BRIDGE_MAS_CA_FILE:-}

SCIM_BRIDGE_BRIDGE_MODE=${SCIM_BRIDGE_BRIDGE_MODE:-poll}
SCIM_BRIDGE_BRIDGE_POLL_INTERVAL=${SCIM_BRIDGE_BRIDGE_POLL_INTERVAL:-5m}
SCIM_BRIDGE_BRIDGE_STATE_BACKEND=${SCIM_BRIDGE_BRIDGE_STATE_BACKEND:-filesystem}
SCIM_BRIDGE_BRIDGE_STATE_PATH=${SCIM_BRIDGE_BRIDGE_STATE_PATH:-/var/lib/scim-bridge/state.json}
SCIM_BRIDGE_BRIDGE_ALLOW_UPDATES=${SCIM_BRIDGE_BRIDGE_ALLOW_UPDATES:-true}
SCIM_BRIDGE_BRIDGE_DRY_RUN=${SCIM_BRIDGE_BRIDGE_DRY_RUN:-false}
SCIM_BRIDGE_BRIDGE_LOG_LEVEL=${SCIM_BRIDGE_BRIDGE_LOG_LEVEL:-info}
SCIM_BRIDGE_BRIDGE_PAYLOAD_LOGGING=${SCIM_BRIDGE_BRIDGE_PAYLOAD_LOGGING:-false}
SCIM_BRIDGE_INCLUDE_USERNAMES=${SCIM_BRIDGE_INCLUDE_USERNAMES:-}
SCIM_BRIDGE_INCLUDE_USERNAME_PREFIX=${SCIM_BRIDGE_INCLUDE_USERNAME_PREFIX:-}
SCIM_BRIDGE_INCLUDE_GROUPS=${SCIM_BRIDGE_INCLUDE_GROUPS:-}
SCIM_BRIDGE_IMAGE_PULL_SECRETS=${SCIM_BRIDGE_IMAGE_PULL_SECRETS:-[]}
SCIM_BRIDGE_KEYCLOAK_IMAGE=${SCIM_BRIDGE_KEYCLOAK_IMAGE:-}
SCIM_BRIDGE_POD_SECURITY_CONTEXT_EXTRA=${SCIM_BRIDGE_POD_SECURITY_CONTEXT_EXTRA:-}
# Empty = the PVC uses the cluster's default StorageClass.
SCIM_BRIDGE_STORAGE_CLASS=${SCIM_BRIDGE_STORAGE_CLASS:-}

if [[ -z "${SCIM_BRIDGE_KEYCLOAK_CA_FILE}" && "${SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_ENABLE}" == "true" ]]; then
  SCIM_BRIDGE_KEYCLOAK_CA_FILE="/etc/scim-bridge/certs/keycloak-ca.crt"
fi

if [[ "${SCIM_BRIDGE_PROVISION_KEYCLOAK}" == "true" && -z "${SCIM_BRIDGE_KEYCLOAK_IMAGE}" ]]; then
  echo "error: SCIM_BRIDGE_KEYCLOAK_IMAGE is required when provisioning Keycloak via Job" >&2
  exit 1
fi

set +a

MANIFEST_TEMPLATE="$ROOT_DIR/manifests/scim-bridge.yaml"
BOOTSTRAP_TEMPLATE="$ROOT_DIR/manifests/scim-bridge-keycloak-bootstrap.yaml"
ROUTE_CERT_TEMPLATE="$ROOT_DIR/manifests/scim-bridge-keycloak-route-cert.yaml"
MAS_PROFILE_TEMPLATE="$ROOT_DIR/manifests/scim-bridge-mas-profile-bootstrap.yaml"

tmp="$(mktemp)"
trap 'rm -f "$tmp" "$tmp.rendered"' EXIT

# Only substitute the SCIM_BRIDGE_* placeholders a template actually uses.
# A bare `envsubst` also expanded the Jobs' runtime shell variables
# ($HOME, $host, $client_uuid, ...) to the renderer's environment, so the
# published bootstrap Jobs were broken and the output depended on who ran
# the render (the CI manifest check could never match a laptop render).
template_vars() {
  grep -oE '\$\{SCIM_BRIDGE_[A-Z0-9_]+\}' "$1" | sed -E 's/^\$\{//; s/\}$//' | sort -u | paste -sd, -
}

render_into_tmp() {
  render_template_file "$1" "$tmp.rendered" "$(template_vars "$1")"
  cat "$tmp.rendered" >>"$tmp"
}

{
  echo "# Generated by scripts/scim-bridge-04-render-install-manifest.sh"
  echo "# Regenerate before publishing a release."
  echo ""
} >"$tmp"

render_into_tmp "$MANIFEST_TEMPLATE"

if [[ "${SCIM_BRIDGE_PROVISION_KEYCLOAK}" == "true" && -f "$BOOTSTRAP_TEMPLATE" ]]; then
  printf '\n---\n' >>"$tmp"
  render_into_tmp "$BOOTSTRAP_TEMPLATE"
fi

if [[ "${SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_ENABLE}" == "true" && -f "$ROUTE_CERT_TEMPLATE" ]]; then
  printf '\n---\n' >>"$tmp"
  render_into_tmp "$ROUTE_CERT_TEMPLATE"
fi

if [[ -f "$MAS_PROFILE_TEMPLATE" ]]; then
  printf '\n---\n' >>"$tmp"
  render_into_tmp "$MAS_PROFILE_TEMPLATE"
fi

# Empty placeholders (e.g. storageClassName with no SCIM_BRIDGE_STORAGE_CLASS)
# leave trailing whitespace that editors and linters strip from the
# committed copy; strip it here so the CI diff is stable.
sed -E 's/[[:space:]]+$//' "$tmp" >"$OUTPUT"
echo "Rendered install manifest: $OUTPUT"
