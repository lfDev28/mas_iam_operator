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
  SCIM_BRIDGE_IMAGE (default: quay.io/lee_forster/mas-iam-scim-bridge:scim-bridge-v0.1.0)
  SCIM_BRIDGE_MAS_PROFILE_ID (default: demo)
  SCIM_BRIDGE_KEYCLOAK_CLIENT_ID (default: scim-admin)
  SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET (default: maxadmin)
  SCIM_BRIDGE_KEYCLOAK_REALM (default: maximo)
  SCIM_BRIDGE_KEYCLOAK_BASE_URL (default: http://mas-iam-sample:8080)
EOF
}

NAMESPACE="iam"
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

preflight_args=(--namespace "${NAMESPACE}")
if [[ -n "${POSTGRES_STORAGE_CLASS}" ]]; then
  preflight_args+=(--storage-class "${POSTGRES_STORAGE_CLASS}")
fi
"${ROOT_DIR}/scripts/preflight.sh" "${preflight_args[@]}"

ensure_namespace_exists "${NAMESPACE}"

log_install "applying operator resources"
prime_last_applied_annotations "${ROOT_DIR}/manifests/install-olm.yaml"
oc apply -f "${ROOT_DIR}/manifests/install-olm.yaml"

log_wait "waiting for the MAS IAM operator CSV to reach Succeeded"
wait_for_operator_csv_succeeded "${NAMESPACE}" 900

log_install "applying sample IAM stack"
install_sample_args=(--namespace "${NAMESPACE}")
if [[ -n "${POSTGRES_STORAGE_CLASS}" ]]; then
  install_sample_args+=(--storage-class "${POSTGRES_STORAGE_CLASS}")
fi
"${ROOT_DIR}/scripts/install-olm-sample.sh" "${install_sample_args[@]}"

log_wait "waiting for IAM core services"
wait_for_namespaced_resource job mas-iam-sample-generate-openldap-tls "${NAMESPACE}" 1200
oc wait --for=condition=complete -n "${NAMESPACE}" job/mas-iam-sample-generate-openldap-tls --timeout=20m
wait_for_namespaced_resource deployment mas-iam-sample-openldap "${NAMESPACE}" 1200
oc rollout status deployment/mas-iam-sample-openldap -n "${NAMESPACE}" --timeout=20m
wait_for_namespaced_resource deployment mas-iam-sample "${NAMESPACE}" 1200
oc rollout status deployment/mas-iam-sample -n "${NAMESPACE}" --timeout=20m
wait_for_namespaced_resource pod mas-iam-sample-postgresql-0 "${NAMESPACE}" 1200
oc wait --for=condition=ready -n "${NAMESPACE}" pod/mas-iam-sample-postgresql-0 --timeout=20m
wait_for_namespaced_resource job mas-iam-sample-ldap-config "${NAMESPACE}" 1200
oc wait --for=condition=complete -n "${NAMESPACE}" job/mas-iam-sample-ldap-config --timeout=20m

log_install "deploying SCIM bridge"
SCIM_BRIDGE_IMAGE="${SCIM_BRIDGE_IMAGE:-quay.io/lee_forster/mas-iam-scim-bridge:scim-bridge-v0.1.0}"
SCIM_BRIDGE_KEYCLOAK_CLIENT_ID="${SCIM_BRIDGE_KEYCLOAK_CLIENT_ID:-scim-admin}"
SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET="${SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET:-maxadmin}"
SCIM_BRIDGE_KEYCLOAK_REALM="${SCIM_BRIDGE_KEYCLOAK_REALM:-maximo}"
SCIM_BRIDGE_KEYCLOAK_BASE_URL="${SCIM_BRIDGE_KEYCLOAK_BASE_URL:-http://mas-iam-sample:8080}"
SCIM_BRIDGE_MAS_PROFILE_ID="${SCIM_BRIDGE_MAS_PROFILE_ID:-demo}"

SCIM_BRIDGE_ENV_FILE=/dev/null \
SCIM_BRIDGE_NAMESPACE="${NAMESPACE}" \
SCIM_BRIDGE_IMAGE="${SCIM_BRIDGE_IMAGE}" \
SCIM_BRIDGE_KEYCLOAK_BASE_URL="${SCIM_BRIDGE_KEYCLOAK_BASE_URL}" \
SCIM_BRIDGE_KEYCLOAK_REALM="${SCIM_BRIDGE_KEYCLOAK_REALM}" \
SCIM_BRIDGE_KEYCLOAK_CLIENT_ID="${SCIM_BRIDGE_KEYCLOAK_CLIENT_ID}" \
SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET="${SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET}" \
SCIM_BRIDGE_KEYCLOAK_RELEASE='mas-iam-sample' \
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
oc get pods -n "${NAMESPACE}" | rg 'mas-iam-sample|scim-bridge' | prefix_stream result || true
