#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: preflight.sh [--namespace <ns>] [--storage-class <name>]

Required environment variables:
  SCIM_BRIDGE_MAS_BASE_URL

Optional environment variables:
  POSTGRES_STORAGE_CLASS
EOF
}

NAMESPACE="${TARGET_NAMESPACE:-iam}"
POSTGRES_STORAGE_CLASS="${POSTGRES_STORAGE_CLASS:-}"

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
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf '[error] unknown argument: %s\n' "$1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=./_all_in_one_common.sh
source "${ROOT_DIR}/scripts/_all_in_one_common.sh"

require_env_vars SCIM_BRIDGE_MAS_BASE_URL

log_config "namespace=${NAMESPACE}"
if [[ -n "${POSTGRES_STORAGE_CLASS}" ]]; then
  log_config "postgres_storage_class_override=${POSTGRES_STORAGE_CLASS}"
fi

log_preflight "checking oc CLI availability"
require_oc

log_preflight "checking cluster login"
ensure_oc_login
log_preflight "whoami=${OC_WHOAMI}"
log_preflight "server=${OC_SERVER}"

log_preflight "discovering StorageClasses"
if ! select_postgres_storage_class "${POSTGRES_STORAGE_CLASS}"; then
  die "no StorageClass found in cluster"
fi

log_preflight "storage_classes_detected=${#ALL_STORAGE_CLASSES[@]}"
if [[ -n "${DEFAULT_STORAGE_CLASS}" ]]; then
  log_preflight "default_storage_class=${DEFAULT_STORAGE_CLASS}"
else
  log_warn "no default StorageClass detected"
fi
if [[ -n "${PREFERRED_BLOCK_STORAGE_CLASS}" ]]; then
  log_preflight "preferred_block_storage_class=${PREFERRED_BLOCK_STORAGE_CLASS}"
elif [[ -n "${REGEX_BLOCK_STORAGE_CLASS}" ]]; then
  log_preflight "regex_block_storage_class=${REGEX_BLOCK_STORAGE_CLASS}"
fi
log_preflight "recommended_postgres_storage_class=${SELECTED_STORAGE_CLASS} reason=${SELECTED_STORAGE_REASON}"

if [[ -n "${POSTGRES_STORAGE_CLASS}" ]] && ! storage_class_exists "${POSTGRES_STORAGE_CLASS}"; then
  log_warn "explicit PostgreSQL storage class override ${POSTGRES_STORAGE_CLASS} is not present in the cluster"
fi
warn_if_cephfs_default_has_block_candidate

mas_host_parse_rc=0
MAS_HOST="$(parse_host_from_url "${SCIM_BRIDGE_MAS_BASE_URL}")" || mas_host_parse_rc=$?
if (( mas_host_parse_rc != 0 )); then
  if (( mas_host_parse_rc == 2 )); then
    die "SCIM_BRIDGE_MAS_BASE_URL is malformed (${SCIM_BRIDGE_MAS_BASE_URL})"
  fi
  die "unable to derive a host from SCIM_BRIDGE_MAS_BASE_URL=${SCIM_BRIDGE_MAS_BASE_URL}"
fi

log_preflight "mas_host=${MAS_HOST}"
mapfile -t MAS_ROUTE_MATCHES < <(lookup_routes_by_host "${MAS_HOST}")

if (( ${#MAS_ROUTE_MATCHES[@]} == 0 )); then
  log_warn "no OpenShift route matched MAS host ${MAS_HOST}; automatic MAS CA detection may not work"
elif (( ${#MAS_ROUTE_MATCHES[@]} > 1 )); then
  log_warn "multiple OpenShift routes matched MAS host ${MAS_HOST}; set SCIM_BRIDGE_MAS_ROUTE_NAMESPACE and SCIM_BRIDGE_MAS_ROUTE_NAME to disambiguate"
  printf '%s\n' "${MAS_ROUTE_MATCHES[@]}" | prefix_stream warn >&2
else
  MAS_ROUTE_REF="${MAS_ROUTE_MATCHES[0]}"
  log_preflight "mas_route=${MAS_ROUTE_REF}"
  MAS_ROUTE_CA="$(get_route_ca_certificate "${MAS_ROUTE_REF}")"
  if [[ -n "${MAS_ROUTE_CA}" ]]; then
    log_preflight "mas_route_ca=present"
  else
    log_warn "route ${MAS_ROUTE_REF} has no tls.caCertificate; set SCIM_BRIDGE_MAS_CA_BUNDLE manually if TLS verification fails"
  fi
fi

log_result "preflight checks passed for namespace ${NAMESPACE}"
