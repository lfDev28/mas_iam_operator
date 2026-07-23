#!/usr/bin/env bash

# Deploy the SCIM bridge into the mas-est namespace using manifests/scim-bridge.yaml
# Example:
#   SCIM_BRIDGE_IMAGE=quay.io/<org>/scim-bridge:dev \
#   SCIM_BRIDGE_KEYCLOAK_BASE_URL=https://keycloak.example.com \
#   SCIM_BRIDGE_KEYCLOAK_REALM=maximo \
#   SCIM_BRIDGE_KEYCLOAK_CLIENT_ID=scim-admin \
#   SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET=secret \
#   SCIM_BRIDGE_MAS_BASE_URL=https://mas.example.com/scim/v2 \
#   SCIM_BRIDGE_MAS_PROFILE_ID=provisioned-profile \
#   SCIM_BRIDGE_MAS_TOKEN=my-token \
#   ./scripts/scim-bridge-02-deploy.sh

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=./_all_in_one_common.sh
source "$ROOT_DIR/scripts/_all_in_one_common.sh"
# shellcheck source=./_scim-bridge-env.sh
source "$ROOT_DIR/scripts/_scim-bridge-env.sh"

SCIM_BRIDGE_KEYCLOAK_BASE_URL=${SCIM_BRIDGE_KEYCLOAK_BASE_URL:-}
: "${SCIM_BRIDGE_KEYCLOAK_REALM:=maximo}"
: "${SCIM_BRIDGE_NAMESPACE:=mas-est}"
: "${SCIM_BRIDGE_KEYCLOAK_RELEASE:=mas-est-iam}"
: "${SCIM_BRIDGE_KEYCLOAK_NAMESPACE:=${SCIM_BRIDGE_NAMESPACE}}"
: "${SCIM_BRIDGE_KEYCLOAK_SERVICE:=${SCIM_BRIDGE_KEYCLOAK_RELEASE}}"
: "${SCIM_BRIDGE_KEYCLOAK_IMAGE:=}"
: "${SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_METHOD:=job}" # job|script|none
: "${SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_JOB_NAME:=scim-bridge-keycloak-bootstrap}"
: "${SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_WAIT:=true}"
: "${SCIM_BRIDGE_KEYCLOAK_ASSIGN_MANAGED_ROLE:=false}"
: "${SCIM_BRIDGE_PROVISION_KEYCLOAK:=true}"
: "${SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_ENABLE:=true}"
: "${SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_JOB_NAME:=scim-bridge-keycloak-route-cert}"
: "${SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_IMAGE:=registry.redhat.io/openshift4/ose-cli}"
: "${SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_WAIT:=true}"
: "${SCIM_BRIDGE_KEYCLOAK_ROUTE_NAMESPACE:=${SCIM_BRIDGE_KEYCLOAK_NAMESPACE}}"
: "${SCIM_BRIDGE_KEYCLOAK_ROUTE_NAME:=scim-bridge-keycloak}"
: "${SCIM_BRIDGE_KEYCLOAK_ROUTE_HOST:=}"
: "${SCIM_BRIDGE_KEYCLOAK_ROUTE_CA_SECRET:=scim-bridge-route-ca}"
: "${SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_DAYS:=825}"
: "${SCIM_BRIDGE_KEYCLOAK_ROUTE_SERVICE:=${SCIM_BRIDGE_KEYCLOAK_SERVICE}}"
: "${SCIM_BRIDGE_KEYCLOAK_ROUTE_DOMAIN_SOURCE:=${SCIM_BRIDGE_KEYCLOAK_RELEASE}}"
: "${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ENABLED:=false}"
: "${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ID:=demo}"
: "${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID:=}"
: "${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_JSON:=}"
: "${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_JOB_NAME:=scim-bridge-mas-profile-bootstrap}"
: "${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_IMAGE:=quay.io/curl/curl:8.5.0}"
: "${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WAIT:=true}"

SCIM_BRIDGE_MAS_TOKEN=${SCIM_BRIDGE_MAS_TOKEN:-}
SCIM_BRIDGE_MAS_API_TOKEN_NAME=${SCIM_BRIDGE_MAS_API_TOKEN_NAME:-}
SCIM_BRIDGE_MAS_API_TOKEN_VALUE=${SCIM_BRIDGE_MAS_API_TOKEN_VALUE:-}
SCIM_BRIDGE_MAS_AUTH_TYPE=${SCIM_BRIDGE_MAS_AUTH_TYPE:-jwt}
SCIM_BRIDGE_MAS_INSECURE_SKIP_VERIFY=${SCIM_BRIDGE_MAS_INSECURE_SKIP_VERIFY:-false}
SCIM_BRIDGE_MAS_PROFILE_MAP=${SCIM_BRIDGE_MAS_PROFILE_MAP:-}
SCIM_BRIDGE_MAS_PROFILE_MAP_JSON=${SCIM_BRIDGE_MAS_PROFILE_MAP_JSON:-}
SCIM_BRIDGE_MAS_PROFILE_REQUIRE_LABEL=${SCIM_BRIDGE_MAS_PROFILE_REQUIRE_LABEL:-false}
SCIM_BRIDGE_KEYCLOAK_INSECURE_SKIP_VERIFY=${SCIM_BRIDGE_KEYCLOAK_INSECURE_SKIP_VERIFY:-false}
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
SCIM_BRIDGE_KEYCLOAK_CA_FILE=${SCIM_BRIDGE_KEYCLOAK_CA_FILE:-}
SCIM_BRIDGE_MAS_CA_FILE=${SCIM_BRIDGE_MAS_CA_FILE:-}
SCIM_BRIDGE_MAS_CA_BUNDLE=${SCIM_BRIDGE_MAS_CA_BUNDLE:-}
SCIM_BRIDGE_MAS_CA_AUTO_DETECT=${SCIM_BRIDGE_MAS_CA_AUTO_DETECT:-true}
SCIM_BRIDGE_MAS_ROUTE_NAMESPACE=${SCIM_BRIDGE_MAS_ROUTE_NAMESPACE:-}
SCIM_BRIDGE_MAS_ROUTE_NAME=${SCIM_BRIDGE_MAS_ROUTE_NAME:-}
SCIM_BRIDGE_IMAGE_PULL_SECRETS=${SCIM_BRIDGE_IMAGE_PULL_SECRETS:-[]}
SCIM_BRIDGE_STORAGE_CLASS=${SCIM_BRIDGE_STORAGE_CLASS:-}
SCIM_BRIDGE_FS_GROUP=${SCIM_BRIDGE_FS_GROUP:-}
SCIM_BRIDGE_POD_SECURITY_CONTEXT_EXTRA=${SCIM_BRIDGE_POD_SECURITY_CONTEXT_EXTRA:-}

require_env_vars \
  SCIM_BRIDGE_IMAGE \
  SCIM_BRIDGE_KEYCLOAK_CLIENT_ID \
  SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET \
  SCIM_BRIDGE_MAS_BASE_URL \
  SCIM_BRIDGE_MAS_PROFILE_ID
require_oc
ensure_oc_login

if [[ -z "${SCIM_BRIDGE_KEYCLOAK_CA_FILE}" && "${SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_ENABLE}" == "true" ]]; then
  SCIM_BRIDGE_KEYCLOAK_CA_FILE="/etc/scim-bridge/certs/keycloak-ca.crt"
fi

route_domain_source_host=""
if [[ "${SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_ENABLE}" == "true" ]]; then
  route_domain_source_host="$(oc get route -n "${SCIM_BRIDGE_KEYCLOAK_ROUTE_NAMESPACE}" "${SCIM_BRIDGE_KEYCLOAK_ROUTE_DOMAIN_SOURCE}" -o jsonpath='{.spec.host}' 2>/dev/null || true)"
  if [[ -z "${SCIM_BRIDGE_KEYCLOAK_ROUTE_HOST}" && -n "${route_domain_source_host}" ]]; then
    base_domain="${route_domain_source_host#*.}"
    SCIM_BRIDGE_KEYCLOAK_ROUTE_HOST="${SCIM_BRIDGE_KEYCLOAK_ROUTE_NAME}-${SCIM_BRIDGE_KEYCLOAK_ROUTE_NAMESPACE}.${base_domain}"
  fi
fi

if [[ -n "${SCIM_BRIDGE_KEYCLOAK_ROUTE_HOST}" ]]; then
  if [[ -z "${SCIM_BRIDGE_KEYCLOAK_BASE_URL}" ]]; then
    SCIM_BRIDGE_KEYCLOAK_BASE_URL="https://${SCIM_BRIDGE_KEYCLOAK_ROUTE_HOST}"
  else
    base_url_host="${SCIM_BRIDGE_KEYCLOAK_BASE_URL#*://}"
    base_url_host="${base_url_host%%/*}"
    if [[ -n "${route_domain_source_host}" && "${base_url_host}" == "${route_domain_source_host}" ]]; then
      SCIM_BRIDGE_KEYCLOAK_BASE_URL="https://${SCIM_BRIDGE_KEYCLOAK_ROUTE_HOST}"
    fi
  fi
fi

if [[ -z "${SCIM_BRIDGE_KEYCLOAK_BASE_URL}" ]]; then
  die "SCIM_BRIDGE_KEYCLOAK_BASE_URL is required (set it or enable route cert automation)"
fi

case "${SCIM_BRIDGE_MAS_BASE_URL}" in
  http://http://*|https://https://*|http://https://*|https://http://*)
    die "SCIM_BRIDGE_MAS_BASE_URL is malformed (${SCIM_BRIDGE_MAS_BASE_URL})"
    ;;
esac

log_config "namespace=${SCIM_BRIDGE_NAMESPACE}"
log_config "keycloak_namespace=${SCIM_BRIDGE_KEYCLOAK_NAMESPACE}"
log_config "keycloak_bootstrap_method=${SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_METHOD}"
log_config "mas_profile_id=${SCIM_BRIDGE_MAS_PROFILE_ID}"
if [[ -n "${SCIM_BRIDGE_STORAGE_CLASS}" ]]; then
  log_config "scim_bridge_storage_class_override=${SCIM_BRIDGE_STORAGE_CLASS}"
fi

detect_mas_ca_bundle() {
  local mas_host route_ref
  local -a route_matches=()

  if [[ "${SCIM_BRIDGE_MAS_CA_AUTO_DETECT}" != "true" ]]; then
    return
  fi
  if [[ -n "${SCIM_BRIDGE_MAS_CA_BUNDLE}" ]]; then
    return
  fi

  if ! mas_host="$(parse_host_from_url "${SCIM_BRIDGE_MAS_BASE_URL}")"; then
    log_warn "unable to derive MAS API host from SCIM_BRIDGE_MAS_BASE_URL=${SCIM_BRIDGE_MAS_BASE_URL}"
    return
  fi

  if [[ -n "${SCIM_BRIDGE_MAS_ROUTE_NAMESPACE}" && -n "${SCIM_BRIDGE_MAS_ROUTE_NAME}" ]]; then
    route_matches=("${SCIM_BRIDGE_MAS_ROUTE_NAMESPACE}/${SCIM_BRIDGE_MAS_ROUTE_NAME}")
  else
    while IFS= read -r route_ref; do
      [[ -n "${route_ref}" ]] || continue
      route_matches+=("${route_ref}")
    done < <(lookup_routes_by_host "${mas_host}")
  fi

  if (( ${#route_matches[@]} == 0 )); then
    log_warn "unable to auto-detect MAS route for host ${mas_host}; set SCIM_BRIDGE_MAS_CA_BUNDLE manually if TLS verification fails"
    return
  fi
  if (( ${#route_matches[@]} > 1 )); then
    log_warn "multiple routes matched host ${mas_host}; set SCIM_BRIDGE_MAS_ROUTE_NAMESPACE/SCIM_BRIDGE_MAS_ROUTE_NAME to disambiguate"
    printf '%s\n' "${route_matches[@]}" | prefix_stream warn >&2
    return
  fi

  route_ref="${route_matches[0]}"
  SCIM_BRIDGE_MAS_CA_BUNDLE="$(get_route_ca_certificate "${route_ref}")"
  if [[ -z "${SCIM_BRIDGE_MAS_CA_BUNDLE}" ]]; then
    log_warn "route ${route_ref} has no tls.caCertificate; set SCIM_BRIDGE_MAS_CA_BUNDLE manually if TLS verification fails"
    return
  fi

  if [[ -z "${SCIM_BRIDGE_MAS_CA_FILE}" ]]; then
    SCIM_BRIDGE_MAS_CA_FILE="/etc/scim-bridge/certs/mas-ca.crt"
  fi
  log_preflight "auto-detected MAS route CA from ${route_ref}"
}

validate_bridge_storage_class() {
  local block_candidate=""
  local recommendation=""

  if ! discover_storage_classes; then
    if [[ -n "${SCIM_BRIDGE_STORAGE_CLASS}" ]]; then
      die "SCIM_BRIDGE_STORAGE_CLASS=${SCIM_BRIDGE_STORAGE_CLASS} was provided but no StorageClasses were discovered"
    fi
    log_warn "no StorageClass resources detected; scim-bridge-state PVC may remain Pending"
    return
  fi

  if [[ -n "${SCIM_BRIDGE_STORAGE_CLASS}" ]]; then
    if ! storage_class_exists "${SCIM_BRIDGE_STORAGE_CLASS}"; then
      die "SCIM_BRIDGE_STORAGE_CLASS ${SCIM_BRIDGE_STORAGE_CLASS} is not present in the cluster"
    fi
    log_preflight "using explicit SCIM bridge PVC storage class ${SCIM_BRIDGE_STORAGE_CLASS}"
    return
  fi

  if [[ -z "${DEFAULT_STORAGE_CLASS}" ]]; then
    recommendation="$(preferred_block_storage_class || true)"
    if [[ -n "${recommendation}" ]]; then
      log_warn "no default StorageClass detected; set SCIM_BRIDGE_STORAGE_CLASS=${recommendation} to avoid Pending PVCs"
    else
      log_warn "no default StorageClass detected; scim-bridge-state PVC may remain Pending"
    fi
    return
  fi

  log_preflight "scim-bridge-state PVC will use cluster default StorageClass ${DEFAULT_STORAGE_CLASS}"
  block_candidate="$(preferred_block_storage_class || true)"
  if [[ -n "${block_candidate}" ]] \
    && is_cephfs_storage_class "${DEFAULT_STORAGE_CLASS}" \
    && [[ "${DEFAULT_STORAGE_CLASS}" != "${block_candidate}" ]]; then
    log_warn "scim-bridge-state PVC will inherit cephfs default ${DEFAULT_STORAGE_CLASS}; consider SCIM_BRIDGE_STORAGE_CLASS=${block_candidate}"
  fi
}

detect_scim_bridge_fs_group() {
  local supplemental_groups=""
  local uid_range=""
  local first_range=""
  local candidate=""

  if [[ -n "${SCIM_BRIDGE_POD_SECURITY_CONTEXT_EXTRA}" ]]; then
    return
  fi

  if [[ -n "${SCIM_BRIDGE_FS_GROUP}" ]]; then
    candidate="${SCIM_BRIDGE_FS_GROUP}"
  else
    supplemental_groups="$(oc get namespace "${SCIM_BRIDGE_NAMESPACE}" -o jsonpath='{.metadata.annotations.openshift\.io/sa\.scc\.supplemental-groups}' 2>/dev/null || true)"
    uid_range="$(oc get namespace "${SCIM_BRIDGE_NAMESPACE}" -o jsonpath='{.metadata.annotations.openshift\.io/sa\.scc\.uid-range}' 2>/dev/null || true)"

    first_range="${supplemental_groups}"
    if [[ -z "${first_range}" ]]; then
      first_range="${uid_range}"
    fi
    first_range="${first_range%%,*}"
    candidate="${first_range%%/*}"
  fi

  if [[ "${candidate}" =~ ^[0-9]+$ ]]; then
    SCIM_BRIDGE_POD_SECURITY_CONTEXT_EXTRA=$'        fsGroup: '"${candidate}"$'\n        fsGroupChangePolicy: Always'
    log_preflight "using namespace-supported fsGroup ${candidate} for scim-bridge"
    return
  fi

  log_warn "unable to determine an allowed fsGroup for namespace ${SCIM_BRIDGE_NAMESPACE}; leaving fsGroup unset"
}

detect_mas_ca_bundle
validate_bridge_storage_class

ensure_namespace_exists "$SCIM_BRIDGE_NAMESPACE"
if [[ "$SCIM_BRIDGE_KEYCLOAK_NAMESPACE" != "$SCIM_BRIDGE_NAMESPACE" ]]; then
  ensure_namespace_exists "$SCIM_BRIDGE_KEYCLOAK_NAMESPACE"
fi
detect_scim_bridge_fs_group

REPO_ROOT="$ROOT_DIR"
MANIFEST_TEMPLATE="$REPO_ROOT/manifests/scim-bridge.yaml"
KEYCLOAK_BOOTSTRAP_TEMPLATE="$REPO_ROOT/manifests/scim-bridge-keycloak-bootstrap.yaml"
KEYCLOAK_ROUTE_CERT_TEMPLATE="$REPO_ROOT/manifests/scim-bridge-keycloak-route-cert.yaml"
MAS_PROFILE_BOOTSTRAP_TEMPLATE="$REPO_ROOT/manifests/scim-bridge-mas-profile-bootstrap.yaml"
CONFIGURE_SCRIPT="$REPO_ROOT/scripts/configure-scim-client.sh"

detect_keycloak_image() {
  local pod image
  pod="$(oc get pod -n "${SCIM_BRIDGE_KEYCLOAK_NAMESPACE}" \
    -l "app.kubernetes.io/component=keycloak,app.kubernetes.io/instance=${SCIM_BRIDGE_KEYCLOAK_RELEASE}" \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  if [[ -z "${pod}" ]]; then
    return 0
  fi
  image="$(oc get pod -n "${SCIM_BRIDGE_KEYCLOAK_NAMESPACE}" "${pod}" -o jsonpath='{.spec.containers[?(@.name=="keycloak")].image}' 2>/dev/null || true)"
  if [[ -z "${image}" ]]; then
    image="$(oc get pod -n "${SCIM_BRIDGE_KEYCLOAK_NAMESPACE}" "${pod}" -o jsonpath='{.spec.containers[0].image}' 2>/dev/null || true)"
  fi
  printf '%s' "${image}"
}

render_manifest() {
  local template_path="$1"
  local out_path="$2"
  local -a vars=("${!3}")
  local vars_csv=""
  if (( ${#vars[@]} > 0 )); then
    vars_csv="$(IFS=,; printf '%s' "${vars[*]}")"
  fi
  render_template_file "$template_path" "$out_path.tmp" "$vars_csv"
  mv "$out_path.tmp" "$out_path"
}

provision_keycloak() {
  if [[ "${SCIM_BRIDGE_PROVISION_KEYCLOAK}" != "true" ]]; then
    return
  fi

  local client_secret="${SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET}"
  if [[ -z "$client_secret" ]]; then
    if command -v openssl >/dev/null 2>&1; then
      client_secret=$(openssl rand -hex 32)
    else
      client_secret=$(head -c 32 /dev/urandom | base64 | tr -dc 'a-f0-9' | head -c 64)
    fi
    SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET="$client_secret"
  fi

  # Ensure the bridge secret exists in the bridge namespace.
  oc create secret generic scim-bridge-secret \
    -n "$SCIM_BRIDGE_NAMESPACE" \
    --from-literal=SCIM_BRIDGE_KEYCLOAK_CLIENT_ID="$SCIM_BRIDGE_KEYCLOAK_CLIENT_ID" \
    --from-literal=SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET="$SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET" \
    --from-literal=SCIM_BRIDGE_MAS_TOKEN="$SCIM_BRIDGE_MAS_TOKEN" \
    --from-literal=SCIM_BRIDGE_MAS_API_TOKEN_NAME="$SCIM_BRIDGE_MAS_API_TOKEN_NAME" \
    --from-literal=SCIM_BRIDGE_MAS_API_TOKEN_VALUE="$SCIM_BRIDGE_MAS_API_TOKEN_VALUE" \
    --dry-run=client -o yaml | oc apply -f -

  # If Keycloak lives in a different namespace, the in-cluster bootstrap Job needs the
  # client secret in that namespace as well (Kubernetes Secret refs are namespace-scoped).
  if [[ "$SCIM_BRIDGE_KEYCLOAK_NAMESPACE" != "$SCIM_BRIDGE_NAMESPACE" ]]; then
    oc create secret generic scim-bridge-secret \
      -n "$SCIM_BRIDGE_KEYCLOAK_NAMESPACE" \
      --from-literal=SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET="$SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET" \
      --dry-run=client -o yaml | oc apply -f -
  fi

  case "${SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_METHOD}" in
    job)
      if [[ -z "${SCIM_BRIDGE_KEYCLOAK_IMAGE}" ]]; then
        SCIM_BRIDGE_KEYCLOAK_IMAGE="$(detect_keycloak_image)"
      fi
      if [[ -z "${SCIM_BRIDGE_KEYCLOAK_IMAGE}" ]]; then
        die "SCIM_BRIDGE_KEYCLOAK_IMAGE is required (unable to auto-detect from Keycloak pods)"
      fi
      if [[ ! -f "$KEYCLOAK_BOOTSTRAP_TEMPLATE" ]]; then
        die "bootstrap manifest not found: $KEYCLOAK_BOOTSTRAP_TEMPLATE"
      fi
      log_install "provisioning Keycloak client ${SCIM_BRIDGE_KEYCLOAK_CLIENT_ID} in realm ${SCIM_BRIDGE_KEYCLOAK_REALM} via Kubernetes Job"
      oc delete job -n "$SCIM_BRIDGE_KEYCLOAK_NAMESPACE" "$SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_JOB_NAME" --ignore-not-found >/dev/null 2>&1 || true
      render_manifest "$KEYCLOAK_BOOTSTRAP_TEMPLATE" "$WORK_DIR/scim-bridge-keycloak-bootstrap-rendered.yaml" bootstrap_subst_vars[@]
      prime_last_applied_annotations "$WORK_DIR/scim-bridge-keycloak-bootstrap-rendered.yaml"
      oc apply -f "$WORK_DIR/scim-bridge-keycloak-bootstrap-rendered.yaml"
      if [[ "${SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_WAIT}" == "true" ]]; then
        oc wait -n "$SCIM_BRIDGE_KEYCLOAK_NAMESPACE" --for=condition=complete "job/${SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_JOB_NAME}" --timeout=10m
      fi
      ;;
    script)
      if [[ ! -x "$CONFIGURE_SCRIPT" ]]; then
        die "configure-scim-client.sh not found or not executable at $CONFIGURE_SCRIPT"
      fi
      log_install "provisioning Keycloak client ${SCIM_BRIDGE_KEYCLOAK_CLIENT_ID} in realm ${SCIM_BRIDGE_KEYCLOAK_REALM} via oc exec"
      "$CONFIGURE_SCRIPT" \
        --namespace "$SCIM_BRIDGE_KEYCLOAK_NAMESPACE" \
        --release "$SCIM_BRIDGE_KEYCLOAK_RELEASE" \
        --realm "$SCIM_BRIDGE_KEYCLOAK_REALM" \
        --client-id "$SCIM_BRIDGE_KEYCLOAK_CLIENT_ID" \
        --client-secret "$client_secret"
      ;;
    none|disabled|false)
      log_config "skipping Keycloak provisioning (SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_METHOD=${SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_METHOD})"
      ;;
    *)
      die "unknown SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_METHOD=${SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_METHOD} (expected job|script|none)"
      ;;
  esac
}

provision_keycloak_route_cert() {
  if [[ "${SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_ENABLE}" != "true" ]]; then
    return
  fi

  if [[ "${SCIM_BRIDGE_KEYCLOAK_ROUTE_NAMESPACE}" != "${SCIM_BRIDGE_NAMESPACE}" ]]; then
    die "route cert job requires SCIM_BRIDGE_KEYCLOAK_ROUTE_NAMESPACE to match SCIM_BRIDGE_NAMESPACE"
  fi

  if [[ ! -f "$KEYCLOAK_ROUTE_CERT_TEMPLATE" ]]; then
    die "route cert manifest not found: $KEYCLOAK_ROUTE_CERT_TEMPLATE"
  fi

  log_install "provisioning Keycloak route TLS certificate via Kubernetes Job"
  oc delete job -n "$SCIM_BRIDGE_KEYCLOAK_ROUTE_NAMESPACE" "$SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_JOB_NAME" --ignore-not-found >/dev/null 2>&1 || true
  render_manifest "$KEYCLOAK_ROUTE_CERT_TEMPLATE" "$WORK_DIR/scim-bridge-keycloak-route-cert-rendered.yaml" route_cert_subst_vars[@]
  prime_last_applied_annotations "$WORK_DIR/scim-bridge-keycloak-route-cert-rendered.yaml"
  oc apply -f "$WORK_DIR/scim-bridge-keycloak-route-cert-rendered.yaml"
  if [[ "${SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_WAIT}" == "true" ]]; then
    oc wait -n "$SCIM_BRIDGE_KEYCLOAK_ROUTE_NAMESPACE" --for=condition=complete "job/${SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_JOB_NAME}" --timeout=10m
  fi
}

provision_mas_profile() {
  if [[ "${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ENABLED}" != "true" ]]; then
    return
  fi

  if [[ ! -f "$MAS_PROFILE_BOOTSTRAP_TEMPLATE" ]]; then
    die "MAS profile bootstrap manifest not found: $MAS_PROFILE_BOOTSTRAP_TEMPLATE"
  fi

  log_install "provisioning MAS SCIM profile via Kubernetes Job"
  oc delete job -n "$SCIM_BRIDGE_NAMESPACE" "$SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_JOB_NAME" --ignore-not-found >/dev/null 2>&1 || true
  render_manifest "$MAS_PROFILE_BOOTSTRAP_TEMPLATE" "$WORK_DIR/scim-bridge-mas-profile-bootstrap-rendered.yaml" mas_profile_subst_vars[@]
  prime_last_applied_annotations "$WORK_DIR/scim-bridge-mas-profile-bootstrap-rendered.yaml"
  oc apply -f "$WORK_DIR/scim-bridge-mas-profile-bootstrap-rendered.yaml"
  if [[ "${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WAIT}" == "true" ]]; then
    oc wait -n "$SCIM_BRIDGE_NAMESPACE" --for=condition=complete "job/${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_JOB_NAME}" --timeout=10m
  fi
}

if [[ ! -f "$MANIFEST_TEMPLATE" ]]; then
  die "manifest not found: $MANIFEST_TEMPLATE"
fi

WORK_DIR="$(mktemp -d)"
cleanup() { rm -rf "$WORK_DIR"; }
trap cleanup EXIT

MANIFEST_RENDERED="$WORK_DIR/scim-bridge-rendered.yaml"

cp "$MANIFEST_TEMPLATE" "$MANIFEST_RENDERED"

placeholders=(
  "\${SCIM_BRIDGE_IMAGE}"
  "\${SCIM_BRIDGE_KEYCLOAK_BASE_URL}"
  "\${SCIM_BRIDGE_KEYCLOAK_REALM}"
  "\${SCIM_BRIDGE_KEYCLOAK_CA_FILE}"
  "\${SCIM_BRIDGE_MAS_BASE_URL}"
  "\${SCIM_BRIDGE_MAS_PROFILE_ID}"
  "\${SCIM_BRIDGE_MAS_PROFILE_MAP}"
  "\${SCIM_BRIDGE_MAS_PROFILE_MAP_JSON}"
  "\${SCIM_BRIDGE_MAS_PROFILE_REQUIRE_LABEL}"
  "\${SCIM_BRIDGE_MAS_TOKEN}"
  "\${SCIM_BRIDGE_MAS_API_TOKEN_NAME}"
  "\${SCIM_BRIDGE_MAS_API_TOKEN_VALUE}"
  "\${SCIM_BRIDGE_KEYCLOAK_CLIENT_ID}"
  "\${SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET}"
  "\${SCIM_BRIDGE_NAMESPACE}"
  "\${SCIM_BRIDGE_MAS_AUTH_TYPE}"
  "\${SCIM_BRIDGE_MAS_CA_FILE}"
  "\${SCIM_BRIDGE_MAS_INSECURE_SKIP_VERIFY}"
  "\${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ENABLED}"
  "\${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ID}"
  "\${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID}"
  "\${SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_JSON}"
  "\${SCIM_BRIDGE_KEYCLOAK_INSECURE_SKIP_VERIFY}"
  "\${SCIM_BRIDGE_BRIDGE_MODE}"
  "\${SCIM_BRIDGE_BRIDGE_POLL_INTERVAL}"
  "\${SCIM_BRIDGE_BRIDGE_STATE_BACKEND}"
  "\${SCIM_BRIDGE_BRIDGE_STATE_PATH}"
  "\${SCIM_BRIDGE_BRIDGE_ALLOW_UPDATES}"
  "\${SCIM_BRIDGE_BRIDGE_DRY_RUN}"
  "\${SCIM_BRIDGE_BRIDGE_LOG_LEVEL}"
  "\${SCIM_BRIDGE_BRIDGE_PAYLOAD_LOGGING}"
  "\${SCIM_BRIDGE_INCLUDE_USERNAMES}"
  "\${SCIM_BRIDGE_INCLUDE_USERNAME_PREFIX}"
  "\${SCIM_BRIDGE_IMAGE_PULL_SECRETS}"
  "\${SCIM_BRIDGE_STORAGE_CLASS}"
  "\${SCIM_BRIDGE_POD_SECURITY_CONTEXT_EXTRA}"
)

scim_bridge_subst_vars=(
  SCIM_BRIDGE_IMAGE
  SCIM_BRIDGE_KEYCLOAK_BASE_URL
  SCIM_BRIDGE_KEYCLOAK_REALM
  SCIM_BRIDGE_KEYCLOAK_CA_FILE
  SCIM_BRIDGE_MAS_BASE_URL
  SCIM_BRIDGE_MAS_PROFILE_ID
  SCIM_BRIDGE_MAS_PROFILE_MAP
  SCIM_BRIDGE_MAS_PROFILE_MAP_JSON
  SCIM_BRIDGE_MAS_PROFILE_REQUIRE_LABEL
  SCIM_BRIDGE_MAS_TOKEN
  SCIM_BRIDGE_MAS_API_TOKEN_NAME
  SCIM_BRIDGE_MAS_API_TOKEN_VALUE
  SCIM_BRIDGE_KEYCLOAK_CLIENT_ID
  SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET
  SCIM_BRIDGE_NAMESPACE
  SCIM_BRIDGE_MAS_AUTH_TYPE
  SCIM_BRIDGE_MAS_CA_FILE
  SCIM_BRIDGE_MAS_INSECURE_SKIP_VERIFY
  SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ENABLED
  SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ID
  SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID
  SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_JSON
  SCIM_BRIDGE_KEYCLOAK_INSECURE_SKIP_VERIFY
  SCIM_BRIDGE_BRIDGE_MODE
  SCIM_BRIDGE_BRIDGE_POLL_INTERVAL
  SCIM_BRIDGE_BRIDGE_STATE_BACKEND
  SCIM_BRIDGE_BRIDGE_STATE_PATH
  SCIM_BRIDGE_BRIDGE_ALLOW_UPDATES
  SCIM_BRIDGE_BRIDGE_DRY_RUN
  SCIM_BRIDGE_BRIDGE_LOG_LEVEL
  SCIM_BRIDGE_BRIDGE_PAYLOAD_LOGGING
  SCIM_BRIDGE_INCLUDE_USERNAMES
  SCIM_BRIDGE_INCLUDE_USERNAME_PREFIX
  SCIM_BRIDGE_IMAGE_PULL_SECRETS
  SCIM_BRIDGE_STORAGE_CLASS
  SCIM_BRIDGE_POD_SECURITY_CONTEXT_EXTRA
)

bootstrap_subst_vars=(
  SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_JOB_NAME
  SCIM_BRIDGE_KEYCLOAK_NAMESPACE
  SCIM_BRIDGE_KEYCLOAK_IMAGE
  SCIM_BRIDGE_KEYCLOAK_SERVICE
  SCIM_BRIDGE_KEYCLOAK_REALM
  SCIM_BRIDGE_KEYCLOAK_CLIENT_ID
  SCIM_BRIDGE_KEYCLOAK_ASSIGN_MANAGED_ROLE
  SCIM_BRIDGE_KEYCLOAK_RELEASE
)

route_cert_subst_vars=(
  SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_JOB_NAME
  SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_IMAGE
  SCIM_BRIDGE_KEYCLOAK_ROUTE_NAMESPACE
  SCIM_BRIDGE_KEYCLOAK_ROUTE_NAME
  SCIM_BRIDGE_KEYCLOAK_ROUTE_HOST
  SCIM_BRIDGE_KEYCLOAK_ROUTE_CA_SECRET
  SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_DAYS
  SCIM_BRIDGE_KEYCLOAK_ROUTE_SERVICE
  SCIM_BRIDGE_KEYCLOAK_ROUTE_DOMAIN_SOURCE
  SCIM_BRIDGE_NAMESPACE
)

mas_profile_subst_vars=(
  SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_JOB_NAME
  SCIM_BRIDGE_NAMESPACE
  SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_IMAGE
  SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ENABLED
  SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ID
  SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID
  SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_JSON
  SCIM_BRIDGE_MAS_BASE_URL
  SCIM_BRIDGE_MAS_AUTH_TYPE
  SCIM_BRIDGE_MAS_CA_FILE
  SCIM_BRIDGE_MAS_INSECURE_SKIP_VERIFY
  SCIM_BRIDGE_MAS_TOKEN
  SCIM_BRIDGE_MAS_API_TOKEN_NAME
  SCIM_BRIDGE_MAS_API_TOKEN_VALUE
)

for ph in "${placeholders[@]}"; do
  if ! grep -q "$ph" "$MANIFEST_RENDERED"; then
    log_warn "placeholder $ph not found in manifest; ensure manifests/scim-bridge.yaml includes it"
  fi
done

export SCIM_BRIDGE_IMAGE \
  SCIM_BRIDGE_KEYCLOAK_BASE_URL \
  SCIM_BRIDGE_KEYCLOAK_REALM \
  SCIM_BRIDGE_KEYCLOAK_CA_FILE \
  SCIM_BRIDGE_MAS_BASE_URL \
  SCIM_BRIDGE_MAS_PROFILE_ID \
  SCIM_BRIDGE_MAS_PROFILE_MAP \
  SCIM_BRIDGE_MAS_PROFILE_MAP_JSON \
  SCIM_BRIDGE_MAS_PROFILE_REQUIRE_LABEL \
  SCIM_BRIDGE_MAS_TOKEN \
  SCIM_BRIDGE_MAS_API_TOKEN_NAME \
  SCIM_BRIDGE_MAS_API_TOKEN_VALUE \
  SCIM_BRIDGE_KEYCLOAK_CLIENT_ID \
  SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET \
  SCIM_BRIDGE_NAMESPACE \
  SCIM_BRIDGE_MAS_AUTH_TYPE \
  SCIM_BRIDGE_MAS_CA_FILE \
  SCIM_BRIDGE_MAS_INSECURE_SKIP_VERIFY \
  SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ENABLED \
  SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ID \
  SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID \
  SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_JSON \
  SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_JOB_NAME \
  SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_IMAGE \
  SCIM_BRIDGE_KEYCLOAK_INSECURE_SKIP_VERIFY \
  SCIM_BRIDGE_BRIDGE_MODE \
  SCIM_BRIDGE_BRIDGE_POLL_INTERVAL \
  SCIM_BRIDGE_BRIDGE_STATE_BACKEND \
  SCIM_BRIDGE_BRIDGE_STATE_PATH \
  SCIM_BRIDGE_BRIDGE_ALLOW_UPDATES \
  SCIM_BRIDGE_BRIDGE_DRY_RUN \
  SCIM_BRIDGE_BRIDGE_LOG_LEVEL \
  SCIM_BRIDGE_BRIDGE_PAYLOAD_LOGGING \
  SCIM_BRIDGE_INCLUDE_USERNAMES \
  SCIM_BRIDGE_INCLUDE_USERNAME_PREFIX \
  SCIM_BRIDGE_IMAGE_PULL_SECRETS \
  SCIM_BRIDGE_STORAGE_CLASS \
  SCIM_BRIDGE_POD_SECURITY_CONTEXT_EXTRA \
  SCIM_BRIDGE_KEYCLOAK_NAMESPACE \
  SCIM_BRIDGE_KEYCLOAK_SERVICE \
  SCIM_BRIDGE_KEYCLOAK_RELEASE \
  SCIM_BRIDGE_KEYCLOAK_IMAGE \
  SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_JOB_NAME \
  SCIM_BRIDGE_KEYCLOAK_ASSIGN_MANAGED_ROLE \
  SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_JOB_NAME \
  SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_IMAGE \
  SCIM_BRIDGE_KEYCLOAK_ROUTE_NAMESPACE \
  SCIM_BRIDGE_KEYCLOAK_ROUTE_NAME \
  SCIM_BRIDGE_KEYCLOAK_ROUTE_HOST \
  SCIM_BRIDGE_KEYCLOAK_ROUTE_CA_SECRET \
  SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_DAYS \
  SCIM_BRIDGE_KEYCLOAK_ROUTE_SERVICE \
  SCIM_BRIDGE_KEYCLOAK_ROUTE_DOMAIN_SOURCE \
  SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_JOB_NAME \
  SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_IMAGE

render_manifest "$MANIFEST_RENDERED" "$MANIFEST_RENDERED" scim_bridge_subst_vars[@]
if [[ -n "${SCIM_BRIDGE_MAS_CA_BUNDLE}" ]]; then
  oc create configmap scim-bridge-mas-ca \
    -n "$SCIM_BRIDGE_NAMESPACE" \
    --from-literal=mas-ca.crt="$SCIM_BRIDGE_MAS_CA_BUNDLE" \
    --dry-run=client -o yaml | oc apply -f -
fi
provision_keycloak_route_cert
provision_keycloak
log_install "applying manifest to ${SCIM_BRIDGE_NAMESPACE} namespace"
prime_last_applied_annotations "$MANIFEST_RENDERED"
oc apply -n "$SCIM_BRIDGE_NAMESPACE" -f "$MANIFEST_RENDERED"
provision_mas_profile

log_result "deployed_image=${SCIM_BRIDGE_IMAGE}"
log_result "mas_base_url=${SCIM_BRIDGE_MAS_BASE_URL} profile_id=${SCIM_BRIDGE_MAS_PROFILE_ID}"
log_result "keycloak_base_url=${SCIM_BRIDGE_KEYCLOAK_BASE_URL} realm=${SCIM_BRIDGE_KEYCLOAK_REALM}"
log_result "check status: oc get pods -n ${SCIM_BRIDGE_NAMESPACE}"
log_result "logs: oc logs deploy/scim-bridge -n ${SCIM_BRIDGE_NAMESPACE}"
