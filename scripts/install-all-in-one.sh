#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=./_all_in_one_common.sh
source "${ROOT_DIR}/scripts/_all_in_one_common.sh"

usage() {
  cat <<'EOF'
Usage: install-all-in-one.sh [--namespace <ns>] [--storage-class <name>] [--keycloak-bootstrap <script|job|none>]

Required environment variables:
  SCIM_BRIDGE_MAS_BASE_URL
  SCIM_BRIDGE_MAS_API_TOKEN_NAME
  SCIM_BRIDGE_MAS_API_TOKEN_VALUE
  SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID

Optional environment variables:
  SCIM_BRIDGE_IMAGE (default: quay.io/lee_forster/mas-iam-scim-bridge:scim-bridge-v0.1.1)
  SCIM_BRIDGE_MAS_PROFILE_ID (default: demo)
  SCIM_BRIDGE_KEYCLOAK_CLIENT_ID (default: scim-admin)
  SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET (default: maxadmin)
  SCIM_BRIDGE_KEYCLOAK_REALM (default: maximo)
  SCIM_BRIDGE_KEYCLOAK_BASE_URL (default: http://mas-est-iam:8080)
EOF
}

NAMESPACE="mas-est"
POSTGRES_STORAGE_CLASS="${POSTGRES_STORAGE_CLASS:-}"
KEYCLOAK_BOOTSTRAP_METHOD="${SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_METHOD:-script}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      NAMESPACE="${2-}"
      shift 2
      ;;
    --storage-class)
      POSTGRES_STORAGE_CLASS="${2-}"
      shift 2
      ;;
    --keycloak-bootstrap)
      KEYCLOAK_BOOTSTRAP_METHOD="${2-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      log_error "unknown argument: $1"
      usage >&2
      exit 1
      ;;
  esac
done

require_env_vars \
  SCIM_BRIDGE_MAS_BASE_URL \
  SCIM_BRIDGE_MAS_API_TOKEN_NAME \
  SCIM_BRIDGE_MAS_API_TOKEN_VALUE \
  SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID

require_oc

log_config "namespace=${NAMESPACE}"
log_config "keycloak_bootstrap_method=${KEYCLOAK_BOOTSTRAP_METHOD}"
if [[ -n "${POSTGRES_STORAGE_CLASS}" ]]; then
  log_config "postgres_storage_class_override=${POSTGRES_STORAGE_CLASS}"
fi

operator_manifest="$(mktemp)"
cleanup() { rm -f "${operator_manifest}"; }
trap cleanup EXIT

preflight_args=(--namespace "${NAMESPACE}")
if [[ -n "${POSTGRES_STORAGE_CLASS}" ]]; then
  preflight_args+=(--storage-class "${POSTGRES_STORAGE_CLASS}")
fi
"${ROOT_DIR}/scripts/preflight.sh" "${preflight_args[@]}"

ensure_namespace_exists "${NAMESPACE}"

log_install "applying operator resources"
render_namespace_manifest "${ROOT_DIR}/manifests/install-olm.yaml" "${operator_manifest}" "${NAMESPACE}"
prime_last_applied_annotations "${operator_manifest}"
oc apply -f "${operator_manifest}"

log_wait "waiting for the MAS EST IAM operator CSV to reach Succeeded"
wait_for_operator_csv_succeeded "${NAMESPACE}" 900

log_install "applying sample MAS EST IAM stack"
install_sample_args=(--namespace "${NAMESPACE}")
if [[ -n "${POSTGRES_STORAGE_CLASS}" ]]; then
  install_sample_args+=(--storage-class "${POSTGRES_STORAGE_CLASS}")
fi
"${ROOT_DIR}/scripts/install-olm-sample.sh" "${install_sample_args[@]}"

log_wait "waiting for MAS EST IAM core services"
wait_for_namespaced_resource job mas-est-iam-generate-openldap-tls "${NAMESPACE}" 1200
oc wait --for=condition=complete -n "${NAMESPACE}" job/mas-est-iam-generate-openldap-tls --timeout=20m
wait_for_namespaced_resource deployment mas-est-iam-openldap "${NAMESPACE}" 1200
oc rollout status deployment/mas-est-iam-openldap -n "${NAMESPACE}" --timeout=20m
wait_for_namespaced_resource deployment mas-est-iam "${NAMESPACE}" 1200
oc rollout status deployment/mas-est-iam -n "${NAMESPACE}" --timeout=20m
wait_for_namespaced_resource pod mas-est-iam-postgresql-0 "${NAMESPACE}" 1200
oc wait --for=condition=ready -n "${NAMESPACE}" pod/mas-est-iam-postgresql-0 --timeout=20m
wait_for_namespaced_resource job mas-est-iam-ldap-config "${NAMESPACE}" 1200
oc wait --for=condition=complete -n "${NAMESPACE}" job/mas-est-iam-ldap-config --timeout=20m

log_install "deploying SCIM bridge"
SCIM_BRIDGE_IMAGE="${SCIM_BRIDGE_IMAGE:-quay.io/lee_forster/mas-iam-scim-bridge:scim-bridge-v0.1.1}"
SCIM_BRIDGE_KEYCLOAK_CLIENT_ID="${SCIM_BRIDGE_KEYCLOAK_CLIENT_ID:-scim-admin}"
SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET="${SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET:-maxadmin}"
SCIM_BRIDGE_KEYCLOAK_REALM="${SCIM_BRIDGE_KEYCLOAK_REALM:-maximo}"
SCIM_BRIDGE_KEYCLOAK_BASE_URL="${SCIM_BRIDGE_KEYCLOAK_BASE_URL:-http://mas-est-iam:8080}"
SCIM_BRIDGE_MAS_PROFILE_ID="${SCIM_BRIDGE_MAS_PROFILE_ID:-demo}"

SCIM_BRIDGE_ENV_FILE=/dev/null \
SCIM_BRIDGE_NAMESPACE="${NAMESPACE}" \
SCIM_BRIDGE_IMAGE="${SCIM_BRIDGE_IMAGE}" \
SCIM_BRIDGE_KEYCLOAK_BASE_URL="${SCIM_BRIDGE_KEYCLOAK_BASE_URL}" \
SCIM_BRIDGE_KEYCLOAK_REALM="${SCIM_BRIDGE_KEYCLOAK_REALM}" \
SCIM_BRIDGE_KEYCLOAK_CLIENT_ID="${SCIM_BRIDGE_KEYCLOAK_CLIENT_ID}" \
SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET="${SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET}" \
SCIM_BRIDGE_KEYCLOAK_RELEASE='mas-est-iam' \
SCIM_BRIDGE_PROVISION_KEYCLOAK='true' \
SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_METHOD="${KEYCLOAK_BOOTSTRAP_METHOD}" \
SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_ENABLE='false' \
SCIM_BRIDGE_MAS_BASE_URL="${SCIM_BRIDGE_MAS_BASE_URL}" \
SCIM_BRIDGE_MAS_PROFILE_ID="${SCIM_BRIDGE_MAS_PROFILE_ID}" \
SCIM_BRIDGE_MAS_AUTH_TYPE='jwt' \
SCIM_BRIDGE_MAS_API_TOKEN_NAME="${SCIM_BRIDGE_MAS_API_TOKEN_NAME}" \
SCIM_BRIDGE_MAS_API_TOKEN_VALUE="${SCIM_BRIDGE_MAS_API_TOKEN_VALUE}" \
SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ENABLED='true' \
SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ID="${SCIM_BRIDGE_MAS_PROFILE_ID}" \
SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID="${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID}" \
"${ROOT_DIR}/scripts/scim-bridge-02-deploy.sh"

log_wait "verifying SCIM bridge rollout"
wait_for_namespaced_resource deployment scim-bridge "${NAMESPACE}" 900
oc rollout status deployment/scim-bridge -n "${NAMESPACE}" --timeout=15m
wait_for_namespaced_resource job scim-bridge-mas-profile-bootstrap "${NAMESPACE}" 900
oc wait --for=condition=complete -n "${NAMESPACE}" job/scim-bridge-mas-profile-bootstrap --timeout=15m

log_result "install flow completed for namespace ${NAMESPACE}"
oc get pods -n "${NAMESPACE}" | rg 'mas-est-iam|scim-bridge' | prefix_stream result || true
