#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=./_all_in_one_common.sh
source "${ROOT_DIR}/scripts/_all_in_one_common.sh"

usage() {
  cat <<'EOF'
Usage: wipe-all-in-one.sh [--namespace <ns>] [--profile-id <id>] [--skip-profile-delete]

Optional environment variables for MAS profile deletion:
  SCIM_BRIDGE_MAS_BASE_URL
  SCIM_BRIDGE_MAS_API_TOKEN_NAME
  SCIM_BRIDGE_MAS_API_TOKEN_VALUE
EOF
}

NAMESPACE="mas-est"
PROFILE_ID="${SCIM_BRIDGE_MAS_PROFILE_ID:-demo}"
SKIP_PROFILE_DELETE=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      NAMESPACE="${2-}"
      shift 2
      ;;
    --profile-id)
      PROFILE_ID="${2-}"
      shift 2
      ;;
    --skip-profile-delete)
      SKIP_PROFILE_DELETE=true
      shift
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

require_oc
ensure_oc_login

log_config "namespace=${NAMESPACE}"
log_config "profile_id=${PROFILE_ID}"
log_config "skip_profile_delete=${SKIP_PROFILE_DELETE}"

if [[ "${SKIP_PROFILE_DELETE}" != "true" ]] \
  && [[ -n "${SCIM_BRIDGE_MAS_BASE_URL:-}" ]] \
  && [[ -n "${SCIM_BRIDGE_MAS_API_TOKEN_NAME:-}" ]] \
  && [[ -n "${SCIM_BRIDGE_MAS_API_TOKEN_VALUE:-}" ]]; then
  log_install "deleting MAS SCIM profile ${PROFILE_ID} if it exists"
  MAS_SCIM_BASE="${SCIM_BRIDGE_MAS_BASE_URL}" \
  MAS_PROFILE_ID="${PROFILE_ID}" \
  MAS_AUTH_SCHEME='Bearer' \
  API_TOKEN_NAME="${SCIM_BRIDGE_MAS_API_TOKEN_NAME}" \
  API_TOKEN_VALUE="${SCIM_BRIDGE_MAS_API_TOKEN_VALUE}" \
  "${ROOT_DIR}/scripts/mas-scim-curl.sh" profile-delete "${PROFILE_ID}" >/dev/null 2>&1 || true
elif [[ "${SKIP_PROFILE_DELETE}" == "true" ]]; then
  log_config "skipping MAS profile delete by request"
else
  log_warn "skipping MAS profile delete because the MAS API environment variables are incomplete"
fi

if oc get project "${NAMESPACE}" >/dev/null 2>&1; then
  log_install "deleting namespace ${NAMESPACE}"
  oc delete project "${NAMESPACE}" --wait=false || true

  for _ in $(seq 1 120); do
    if oc get project "${NAMESPACE}" >/dev/null 2>&1; then
      phase="$(oc get project "${NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
      log_wait "namespace=${NAMESPACE} phase=${phase:-unknown}"

      if [[ "${phase}" == "Terminating" ]]; then
        stacks=()
        while IFS= read -r stack; do
          [[ -n "${stack}" ]] || continue
          stacks+=("${stack}")
        done < <(oc get masiamstacks.iam.mas.ibm.com -n "${NAMESPACE}" -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true)
        if (( ${#stacks[@]} > 0 )); then
          for stack in "${stacks[@]}"; do
            if [[ -n "${stack}" ]]; then
              log_install "clearing finalizers on masiamstack/${stack}"
              oc patch masiamstack "${stack}" -n "${NAMESPACE}" --type=merge -p '{"metadata":{"finalizers":[]}}' >/dev/null 2>&1 || true
            fi
          done
        fi
      fi

      sleep 5
    else
      log_result "namespace ${NAMESPACE} deleted"
      break
    fi
  done

  if oc get project "${NAMESPACE}" >/dev/null 2>&1; then
    die "namespace ${NAMESPACE} is still present after the timeout"
  fi
else
  log_result "namespace ${NAMESPACE} is already absent"
fi
