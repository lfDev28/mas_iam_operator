#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=./_all_in_one_common.sh
source "${ROOT_DIR}/scripts/_all_in_one_common.sh"

usage() {
  cat <<'EOF'
Usage: install-olm-sample.sh [--namespace <ns>] [--components <ldap,keycloak,scim>] [--manifest <path>] [--storage-class <name>]

Applies manifests/install-olm-sample.yaml with an explicit storageClass for the
PostgreSQL and OpenLDAP volumes.
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
COMPONENTS="${MAS_EST_COMPONENTS:-ldap,keycloak,scim}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      NAMESPACE="${2-}"
      shift 2
      ;;
    --components)
      COMPONENTS="${2-}"
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

COMPONENTS="${COMPONENTS// /,}"

has_component() {
  case ",${COMPONENTS}," in
    *",$1,"*) return 0 ;;
    *) return 1 ;;
  esac
}

install_keycloak() {
  has_component keycloak || has_component scim
}

install_ldap() {
  has_component ldap || has_component keycloak || has_component scim
}

IFS=',' read -r -a component_parts <<< "${COMPONENTS}"
for component in "${component_parts[@]}"; do
  [[ -n "${component}" ]] || continue
  case "${component}" in
    ldap|keycloak|scim|s3) ;;
    *) die "unknown component '${component}'; supported components are ldap,keycloak,scim,s3" ;;
  esac
done

if ! install_ldap; then
  die "install-olm-sample.sh only applies the IAM sample; use mas-est install --components s3 for S3-only installs"
fi

if [[ -z "${MANIFEST}" ]]; then
  MANIFEST="${ROOT_DIR}/manifests/install-olm-sample.yaml"
fi
if [[ ! -f "${MANIFEST}" ]]; then
  die "manifest not found: ${MANIFEST}"
fi

log_config "namespace=${NAMESPACE}"
log_config "components=${COMPONENTS}"
if [[ -n "${POSTGRES_STORAGE_CLASS}" ]]; then
  log_config "postgres_storage_class_override=${POSTGRES_STORAGE_CLASS}"
fi
ensure_namespace_exists "${NAMESPACE}"

if install_keycloak; then
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
else
  SELECTED_STORAGE_CLASS=""
  log_config "keycloak_disabled=true"
fi

rendered_manifest="$(mktemp)"
tmp_manifest="$(mktemp)"
cleanup() { rm -f "${rendered_manifest}" "${tmp_manifest}"; }
trap cleanup EXIT

render_namespace_manifest "${MANIFEST}" "${rendered_manifest}" "${NAMESPACE}"

keycloak_enabled=false
postgresql_enabled=false
if install_keycloak; then
  keycloak_enabled=true
  postgresql_enabled=true
fi

awk -v sc="${SELECTED_STORAGE_CLASS}" -v keycloak_enabled="${keycloak_enabled}" -v postgresql_enabled="${postgresql_enabled}" '
  { print }
  !keycloak_injected && /^  keycloak:[[:space:]]*$/ {
    print "    enabled: " keycloak_enabled
    keycloak_injected=1
  }
  !postgresql_injected && /^  postgresql:[[:space:]]*$/ {
    print "    enabled: " postgresql_enabled
    postgresql_injected=1
  }
  postgresql_enabled == "true" && !storage_injected && /^[[:space:]]*persistence:[[:space:]]*$/ {
    print "        storageClass: " sc
    storage_injected=1
  }
  # OpenLDAP has no persistence block in the sample, so inject the whole thing.
  # Without an explicit class the PVC is created with no storageClassName, which
  # never binds on a cluster that has no default StorageClass (the chart
  # supports openldap.persistence.storageClass; it was simply never set).
  sc != "" && !ldap_storage_injected && /^  openldap:[[:space:]]*$/ {
    print "    persistence:"
    print "      storageClass: " sc
    ldap_storage_injected=1
  }
  END {
    if (!keycloak_injected || !postgresql_injected) {
      exit 2
    }
    if (postgresql_enabled == "true" && !storage_injected) {
      exit 2
    }
    if (sc != "" && !ldap_storage_injected) {
      exit 2
    }
  }
' "${rendered_manifest}" > "${tmp_manifest}"

prime_last_applied_annotations "${tmp_manifest}"
oc apply -f "${tmp_manifest}"
if [[ -n "${SELECTED_STORAGE_CLASS}" ]]; then
  log_result "applied ${MANIFEST} to namespace ${NAMESPACE} with storageClass ${SELECTED_STORAGE_CLASS}"
else
  log_result "applied ${MANIFEST} to namespace ${NAMESPACE}"
fi
