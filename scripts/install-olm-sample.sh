#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=./_all_in_one_common.sh
source "${ROOT_DIR}/scripts/_all_in_one_common.sh"

usage() {
  cat <<'EOF'
Usage: install-olm-sample.sh [--namespace <ns>] [--manifest <path>] [--storage-class <name>]

Applies manifests/install-olm-sample.yaml with an explicit PostgreSQL storageClass.
Selection order:
1) --storage-class / POSTGRES_STORAGE_CLASS
2) Preferred block/RBD names
3) First class matching (rbd|block)
4) Cluster default StorageClass
5) First available StorageClass
EOF
}

NAMESPACE="${TARGET_NAMESPACE:-mas-est}"
MANIFEST=""
POSTGRES_STORAGE_CLASS="${POSTGRES_STORAGE_CLASS:-}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      NAMESPACE="${2-}"
      shift 2
      ;;
    --manifest)
      MANIFEST="${2-}"
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
      log_error "unknown argument: $1"
      usage >&2
      exit 1
      ;;
  esac
done

require_oc
ensure_oc_login

if [[ -z "${MANIFEST}" ]]; then
  MANIFEST="${ROOT_DIR}/manifests/install-olm-sample.yaml"
fi
if [[ ! -f "${MANIFEST}" ]]; then
  die "manifest not found: ${MANIFEST}"
fi

log_config "namespace=${NAMESPACE}"
if [[ -n "${POSTGRES_STORAGE_CLASS}" ]]; then
  log_config "postgres_storage_class_override=${POSTGRES_STORAGE_CLASS}"
fi
ensure_namespace_exists "${NAMESPACE}"

if ! select_postgres_storage_class "${POSTGRES_STORAGE_CLASS}"; then
  die "no StorageClass found in cluster"
fi

log_config "selected_postgres_storage_class=${SELECTED_STORAGE_CLASS} reason=${SELECTED_STORAGE_REASON}"
warn_if_cephfs_default_has_block_candidate
if [[ -n "${POSTGRES_STORAGE_CLASS}" ]] && ! storage_class_exists "${POSTGRES_STORAGE_CLASS}"; then
  log_warn "explicit PostgreSQL storage class override ${POSTGRES_STORAGE_CLASS} is not present in the cluster"
fi
if [[ "${SELECTED_STORAGE_REASON}" == "first-available" ]]; then
  log_warn "falling back to the first available StorageClass ${SELECTED_STORAGE_CLASS}; set POSTGRES_STORAGE_CLASS to pin a better class if needed"
fi

rendered_manifest="$(mktemp)"
tmp_manifest="$(mktemp)"
cleanup() { rm -f "${rendered_manifest}" "${tmp_manifest}"; }
trap cleanup EXIT

render_namespace_manifest "${MANIFEST}" "${rendered_manifest}" "${NAMESPACE}"

awk -v sc="${SELECTED_STORAGE_CLASS}" '
  { print }
  !injected && /^[[:space:]]*persistence:[[:space:]]*$/ {
    print "        storageClass: " sc
    injected=1
  }
  END {
    if (!injected) {
      exit 2
    }
  }
' "${rendered_manifest}" > "${tmp_manifest}"

prime_last_applied_annotations "${tmp_manifest}"
oc apply -f "${tmp_manifest}"
log_result "applied ${MANIFEST} to namespace ${NAMESPACE} with storageClass ${SELECTED_STORAGE_CLASS}"
