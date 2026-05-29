#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=./_all_in_one_common.sh
source "${ROOT_DIR}/scripts/_all_in_one_common.sh"

usage() {
  cat <<'EOF'
Usage: status-all-in-one.sh [--namespace <ns>]
EOF
}

NAMESPACE="${TARGET_NAMESPACE:-mas-est}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      NAMESPACE="${2-}"
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

report_deployment_status() {
  local name="$1"
  local ready_replicas=""
  local replicas=""
  local available_replicas=""
  local updated_replicas=""

  if ! oc get "deployment/${name}" -n "${NAMESPACE}" >/dev/null 2>&1; then
    log_warn "deployment/${name} not found in namespace ${NAMESPACE}"
    return
  fi

  IFS=$'\t' read -r ready_replicas replicas available_replicas updated_replicas < <(
    oc get "deployment/${name}" -n "${NAMESPACE}" \
      -o jsonpath='{.status.readyReplicas}{"\t"}{.spec.replicas}{"\t"}{.status.availableReplicas}{"\t"}{.status.updatedReplicas}'
  ) || true
  log_result "deployment/${name} ready=${ready_replicas:-0}/${replicas:-0} available=${available_replicas:-0} updated=${updated_replicas:-0}"
}

report_job_status() {
  local name="$1"
  local missing_note="${2:-}"
  local succeeded=""
  local failed=""
  local active=""
  local completions=""

  if ! oc get "job/${name}" -n "${NAMESPACE}" >/dev/null 2>&1; then
    if [[ -n "${missing_note}" ]]; then
      log_result "job/${name} absent note=${missing_note}"
    else
      log_warn "job/${name} not found in namespace ${NAMESPACE}"
    fi
    return
  fi

  succeeded="$(oc get "job/${name}" -n "${NAMESPACE}" -o jsonpath='{.status.succeeded}' 2>/dev/null || true)"
  failed="$(oc get "job/${name}" -n "${NAMESPACE}" -o jsonpath='{.status.failed}' 2>/dev/null || true)"
  active="$(oc get "job/${name}" -n "${NAMESPACE}" -o jsonpath='{.status.active}' 2>/dev/null || true)"
  completions="$(oc get "job/${name}" -n "${NAMESPACE}" -o jsonpath='{.spec.completions}' 2>/dev/null || true)"
  log_result "job/${name} succeeded=${succeeded:-0} failed=${failed:-0} active=${active:-0} completions=${completions:-1}"
}

report_pod_status() {
  local name="$1"
  local phase=""
  local ready_condition=""

  if ! oc get "pod/${name}" -n "${NAMESPACE}" >/dev/null 2>&1; then
    log_warn "pod/${name} not found in namespace ${NAMESPACE}"
    return
  fi

  phase="$(oc get "pod/${name}" -n "${NAMESPACE}" -o jsonpath='{.status.phase}')"
  ready_condition="$(oc get "pod/${name}" -n "${NAMESPACE}" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}')"
  log_result "pod/${name} phase=${phase:-Unknown} ready=${ready_condition:-Unknown}"
}

report_pvcs() {
  local pvc_output=""

  pvc_output="$(oc get pvc -n "${NAMESPACE}" -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.status.phase}{"\t"}{.spec.storageClassName}{"\n"}{end}' 2>/dev/null || true)"
  if [[ -z "${pvc_output}" ]]; then
    log_warn "no PVCs found in namespace ${NAMESPACE}"
    return
  fi

  while IFS=$'\t' read -r name phase storage_class; do
    if [[ -n "${name}" ]]; then
      log_result "pvc/${name} phase=${phase:-Unknown} storageClass=${storage_class:-unset}"
    fi
  done <<< "${pvc_output}"
}

require_oc
ensure_oc_login

log_config "namespace=${NAMESPACE}"
log_config "whoami=${OC_WHOAMI}"
log_config "server=${OC_SERVER}"

csv_name="$(find_operator_csv_name "${NAMESPACE}" || true)"
if [[ -n "${csv_name}" ]]; then
  csv_phase="$(oc get csv -n "${NAMESPACE}" "${csv_name}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  log_result "operator_csv=${csv_name} phase=${csv_phase:-Unknown}"
else
  log_warn "MAS EST IAM operator CSV not found in namespace ${NAMESPACE}"
fi

report_deployment_status "mas-iam-operator-controller-manager"
report_deployment_status "mas-est-iam"
report_deployment_status "mas-est-iam-openldap"
report_deployment_status "scim-bridge"

report_pod_status "mas-est-iam-postgresql-0"

report_job_status "mas-est-iam-generate-openldap-tls" "ttl-cleaned after completion"
report_job_status "mas-est-iam-ldap-config"
report_job_status "scim-bridge-keycloak-bootstrap" "not created when keycloak bootstrap mode=script"
report_job_status "scim-bridge-keycloak-route-cert" "not created when route cert automation is disabled"
report_job_status "scim-bridge-mas-profile-bootstrap"

report_pvcs
