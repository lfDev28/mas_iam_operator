#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=./_all_in_one_common.sh
source "${ROOT_DIR}/scripts/_all_in_one_common.sh"

usage() {
  cat <<'EOF'
Usage: logs-all-in-one.sh [--namespace <ns>] [--lines <n>] [--component <name>]

Components:
  all
  operator
  keycloak
  scim-bridge
  mas-profile-bootstrap
  keycloak-bootstrap
  route-cert
  ldap-config
EOF
}

NAMESPACE="${TARGET_NAMESPACE:-mas-est}"
LINES="${LINES:-200}"
COMPONENT="all"

stream_logs() {
  local label="$1"
  local resource_kind="$2"
  local resource_name="$3"
  local container_name="${4:-}"
  local missing_note="${5:-}"
  local -a log_args=(
    logs
    -n "${NAMESPACE}"
    "${resource_kind}/${resource_name}"
    --tail="${LINES}"
  )

  if ! oc get "${resource_kind}/${resource_name}" -n "${NAMESPACE}" >/dev/null 2>&1; then
    if [[ -n "${missing_note}" ]]; then
      log_result "skip ${label} logs (${resource_kind}/${resource_name}): ${missing_note}"
    else
      log_warn "${resource_kind}/${resource_name} not found in namespace ${NAMESPACE}"
    fi
    return
  fi

  if [[ -n "${container_name}" ]]; then
    log_args+=(-c "${container_name}")
  fi

  log_result "begin ${label} logs (${resource_kind}/${resource_name})"
  if ! oc "${log_args[@]}" 2>&1 | prefix_stream result; then
    log_warn "failed to fetch logs for ${resource_kind}/${resource_name}"
  fi
  log_result "end ${label} logs"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      NAMESPACE="${2-}"
      shift 2
      ;;
    --lines)
      LINES="${2-}"
      shift 2
      ;;
    --component)
      COMPONENT="${2-}"
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

case "${COMPONENT}" in
  all|operator|keycloak|scim-bridge|mas-profile-bootstrap|keycloak-bootstrap|route-cert|ldap-config)
    ;;
  *)
    log_error "unknown component: ${COMPONENT}"
    usage >&2
    exit 1
    ;;
esac

if ! [[ "${LINES}" =~ ^[0-9]+$ ]]; then
  die "--lines must be a non-negative integer"
fi

require_oc
ensure_oc_login

log_config "namespace=${NAMESPACE}"
log_config "lines=${LINES}"
log_config "component=${COMPONENT}"

if [[ "${COMPONENT}" == "all" || "${COMPONENT}" == "operator" ]]; then
  stream_logs "operator" deployment "mas-iam-operator-controller-manager" "manager"
fi
if [[ "${COMPONENT}" == "all" || "${COMPONENT}" == "keycloak" ]]; then
  stream_logs "keycloak" deployment "mas-est-iam"
fi
if [[ "${COMPONENT}" == "all" || "${COMPONENT}" == "scim-bridge" ]]; then
  stream_logs "scim-bridge" deployment "scim-bridge"
fi
if [[ "${COMPONENT}" == "all" || "${COMPONENT}" == "mas-profile-bootstrap" ]]; then
  stream_logs "mas-profile-bootstrap" job "scim-bridge-mas-profile-bootstrap"
fi
if [[ "${COMPONENT}" == "all" || "${COMPONENT}" == "keycloak-bootstrap" ]]; then
  stream_logs "keycloak-bootstrap" job "scim-bridge-keycloak-bootstrap" "" "not created when keycloak bootstrap mode=script"
fi
if [[ "${COMPONENT}" == "all" || "${COMPONENT}" == "route-cert" ]]; then
  stream_logs "route-cert" job "scim-bridge-keycloak-route-cert" "" "not created when route cert automation is disabled"
fi
if [[ "${COMPONENT}" == "all" || "${COMPONENT}" == "ldap-config" ]]; then
  stream_logs "ldap-config" job "mas-est-iam-ldap-config"
fi
