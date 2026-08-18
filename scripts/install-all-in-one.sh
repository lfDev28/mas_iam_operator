#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=./_all_in_one_common.sh
source "${ROOT_DIR}/scripts/_all_in_one_common.sh"

usage() {
  cat <<'EOF'
Usage: install-all-in-one.sh [--namespace <ns>] [--components <ldap,keycloak,scim>] [--storage-class <name>] [--keycloak-bootstrap <script|job|none>]

Required environment variables:
  Required only when the scim component is selected:
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

COMPONENTS="${COMPONENTS// /,}"

has_component() {
  case ",${COMPONENTS}," in
    *",$1,"*) return 0 ;;
    *) return 1 ;;
  esac
}

install_ldap() {
  has_component ldap || has_component keycloak || has_component scim
}

install_keycloak() {
  has_component keycloak || has_component scim
}

install_scim() {
  has_component scim
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
  die "install-all-in-one.sh only installs IAM components; use mas-est install --components s3 for S3-only installs"
fi

if install_scim; then
  require_env_vars \
    SCIM_BRIDGE_MAS_BASE_URL \
    SCIM_BRIDGE_MAS_API_TOKEN_NAME \
    SCIM_BRIDGE_MAS_API_TOKEN_VALUE \
    SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID
fi

require_oc

log_config "namespace=${NAMESPACE}"
log_config "components=${COMPONENTS}"
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
if install_scim; then
  "${ROOT_DIR}/scripts/preflight.sh" "${preflight_args[@]}"
else
  ensure_oc_login
fi

ensure_namespace_exists "${NAMESPACE}"

log_install "applying operator resources"
render_namespace_manifest "${ROOT_DIR}/manifests/install-olm.yaml" "${operator_manifest}" "${NAMESPACE}"
prime_last_applied_annotations "${operator_manifest}"

# OLM resolver race guard. The CatalogSource and Subscription must NOT be
# applied in the same kubectl call, because OLM creates an InstallPlan from
# the cluster's packageserver cache, which may be stale until ~30-60s after
# a new CatalogSource image is pulled. If we apply both together the
# Subscription can resolve to a prior CSV version that's still in the cache.
# We saw this on the mas91 cluster: catalog-0.0.14 (containing only v0.0.14)
# was pushed, applied, and OLM still installed v0.0.13.
#
# Workflow: apply EVERYTHING EXCEPT the Subscription first, then wait for
# (a) CatalogSource.status.connectionState.lastObservedState=READY,
# (b) packagemanifests/mas-iam-operator to exist and report a currentCSV
#     (proves packageserver has actually queried the new catalog), then
# finally apply the Subscription as a separate step.
log_install "applying operator CRDs, RBAC, CatalogSource (deferring Subscription)"
pre_sub_manifest="${TMPDIR:-/tmp}/mas-est-pre-sub.yaml"
sub_manifest="${TMPDIR:-/tmp}/mas-est-sub.yaml"
# Split the multi-doc yaml into "everything except Subscription" + "only the
# Subscription" so we can apply them separately. python3 is available on
# both macOS (system default) and Linux installer images; awk's print-redirect
# syntax is fragile across BSD/GNU implementations.
python3 - "${operator_manifest}" "${pre_sub_manifest}" "${sub_manifest}" <<'PY'
import sys
src, pre_path, sub_path = sys.argv[1], sys.argv[2], sys.argv[3]
with open(src) as f:
    text = f.read()
# Split on `---` on its own line; first chunk may be empty if file starts with ---.
docs = []
buf = []
for line in text.splitlines():
    if line.strip() == '---':
        docs.append('\n'.join(buf))
        buf = []
    else:
        buf.append(line)
if buf:
    docs.append('\n'.join(buf))
pre_docs = [d for d in docs if d.strip() and 'kind: Subscription' not in d]
sub_docs = [d for d in docs if d.strip() and 'kind: Subscription' in d]
with open(pre_path, 'w') as f:
    f.write('\n---\n'.join(pre_docs) + '\n')
with open(sub_path, 'w') as f:
    if sub_docs:
        f.write('\n---\n'.join(sub_docs) + '\n')
PY
# Sanity check: if the split produced an empty Subscription file, the source
# yaml has no Subscription block and the apply will fail. Fall back to a
# single apply with a loud warning.
if [[ ! -s "${sub_manifest}" ]]; then
  log_warn "Subscription block not found in ${operator_manifest}; applying merged manifest (OLM race guard disabled)"
  oc apply -f "${operator_manifest}"
else
  oc apply -f "${pre_sub_manifest}"
fi

log_wait "waiting for CatalogSource mas-iam-operator to report READY"
for i in $(seq 1 60); do
  state="$(oc -n openshift-marketplace get catalogsource mas-iam-operator -o jsonpath='{.status.connectionState.lastObservedState}' 2>/dev/null || true)"
  if [[ "${state}" == "READY" ]]; then
    log_wait "catalog_source=mas-iam-operator state=READY"
    break
  fi
  sleep 5
done
if [[ "${state}" != "READY" ]]; then
  log_warn "CatalogSource mas-iam-operator did not report READY within 5m; proceeding anyway"
fi

# Now poll packagemanifests until packageserver has indexed the new catalog.
# packagemanifests/mas-iam-operator is the cluster-wide view of what
# packageserver knows about the package — it's what OLM's resolver actually
# reads when creating an InstallPlan. Once .status.channels[].currentCSV is
# populated, packageserver has talked to the new catalog pod and is ready
# to give a correct resolution.
log_wait "waiting for packageserver to index mas-iam-operator package"
for i in $(seq 1 60); do
  current_csv="$(oc get packagemanifests mas-iam-operator -n openshift-marketplace -o jsonpath='{.status.channels[?(@.name=="alpha")].currentCSV}' 2>/dev/null || true)"
  if [[ -n "${current_csv}" ]]; then
    log_wait "packagemanifests/mas-iam-operator currentCSV=${current_csv}"
    break
  fi
  sleep 5
done
if [[ -z "${current_csv}" ]]; then
  log_warn "packagemanifests/mas-iam-operator did not populate currentCSV within 5m; Subscription may resolve to a stale version"
fi

# Extra settle time after currentCSV first appears — packageserver's cache
# can briefly show OLDER catalog content while it transitions. 30s is
# usually enough for the cache to converge.
sleep 30

if [[ -s "${sub_manifest}" ]]; then
  log_install "applying operator Subscription"
  oc apply -f "${sub_manifest}"
fi

log_wait "waiting for the MAS EST IAM operator CSV to reach Succeeded"
wait_for_operator_csv_succeeded "${NAMESPACE}" 900

log_install "applying sample MAS EST IAM stack"
install_sample_args=(--namespace "${NAMESPACE}" --components "${COMPONENTS}")
if [[ -n "${POSTGRES_STORAGE_CLASS}" ]]; then
  install_sample_args+=(--storage-class "${POSTGRES_STORAGE_CLASS}")
fi
"${ROOT_DIR}/scripts/install-olm-sample.sh" "${install_sample_args[@]}"

log_wait "waiting for MAS EST IAM core services"
wait_for_namespaced_resource job mas-est-iam-generate-openldap-tls "${NAMESPACE}" 1200
oc wait --for=condition=complete -n "${NAMESPACE}" job/mas-est-iam-generate-openldap-tls --timeout=20m
wait_for_namespaced_resource deployment mas-est-iam-openldap "${NAMESPACE}" 1200
oc rollout status deployment/mas-est-iam-openldap -n "${NAMESPACE}" --timeout=20m
if install_keycloak; then
  wait_for_namespaced_resource deployment mas-est-iam "${NAMESPACE}" 1200
  oc rollout status deployment/mas-est-iam -n "${NAMESPACE}" --timeout=20m
  wait_for_namespaced_resource pod mas-est-iam-postgresql-0 "${NAMESPACE}" 1200
  oc wait --for=condition=ready -n "${NAMESPACE}" pod/mas-est-iam-postgresql-0 --timeout=20m
  wait_for_namespaced_resource job mas-est-iam-ldap-config "${NAMESPACE}" 1200
  oc wait --for=condition=complete -n "${NAMESPACE}" job/mas-est-iam-ldap-config --timeout=20m

  log_install "seeding grouped demo users into Keycloak realm (oidc.*, saml.*, scim.*)"
  "${ROOT_DIR}/scripts/configure-demo-users.sh" \
    --namespace "${NAMESPACE}" \
    --release "mas-est-iam" \
    --realm "${SCIM_BRIDGE_KEYCLOAK_REALM:-maximo}" \
    --password "${MAS_EST_DEMO_USER_PASSWORD:-maxadmin}"
fi

if install_scim; then
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
  SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_ENABLE='true' \
  SCIM_BRIDGE_MAS_BASE_URL="${SCIM_BRIDGE_MAS_BASE_URL}" \
  SCIM_BRIDGE_MAS_PROFILE_ID="${SCIM_BRIDGE_MAS_PROFILE_ID}" \
  SCIM_BRIDGE_MAS_AUTH_TYPE='jwt' \
  SCIM_BRIDGE_MAS_API_TOKEN_NAME="${SCIM_BRIDGE_MAS_API_TOKEN_NAME}" \
  SCIM_BRIDGE_MAS_API_TOKEN_VALUE="${SCIM_BRIDGE_MAS_API_TOKEN_VALUE}" \
  SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ENABLED='true' \
  SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ID="${SCIM_BRIDGE_MAS_PROFILE_ID}" \
  SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID="${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID}" \
  SCIM_BRIDGE_INCLUDE_USERNAME_PREFIX="${SCIM_BRIDGE_INCLUDE_USERNAME_PREFIX:-scim.}" \
  SCIM_BRIDGE_INCLUDE_USERNAMES="${SCIM_BRIDGE_INCLUDE_USERNAMES:-}" \
  SCIM_BRIDGE_INCLUDE_GROUPS="${SCIM_BRIDGE_INCLUDE_GROUPS:-mas-scim-users}" \
  "${ROOT_DIR}/scripts/scim-bridge-02-deploy.sh"

  log_wait "verifying SCIM bridge rollout"
  wait_for_namespaced_resource deployment scim-bridge "${NAMESPACE}" 900
  oc rollout status deployment/scim-bridge -n "${NAMESPACE}" --timeout=15m
  wait_for_namespaced_resource job scim-bridge-mas-profile-bootstrap "${NAMESPACE}" 900
  oc wait --for=condition=complete -n "${NAMESPACE}" job/scim-bridge-mas-profile-bootstrap --timeout=15m
fi

log_result "install flow completed for namespace ${NAMESPACE}"
oc get pods -n "${NAMESPACE}" | rg 'mas-est-iam|scim-bridge' | prefix_stream result || true
