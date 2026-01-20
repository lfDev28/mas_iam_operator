#!/usr/bin/env bash

# Deploy the SCIM bridge into the iam namespace using manifests/scim-bridge.yaml
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
# shellcheck source=./_scim-bridge-env.sh
source "$ROOT_DIR/scripts/_scim-bridge-env.sh"

: "${SCIM_BRIDGE_IMAGE:?Set SCIM_BRIDGE_IMAGE to the pushed image (e.g., quay.io/<org>/scim-bridge:dev)}"
SCIM_BRIDGE_KEYCLOAK_BASE_URL=${SCIM_BRIDGE_KEYCLOAK_BASE_URL:-}
: "${SCIM_BRIDGE_KEYCLOAK_REALM:=maximo}"
: "${SCIM_BRIDGE_KEYCLOAK_CLIENT_ID:?Set SCIM_BRIDGE_KEYCLOAK_CLIENT_ID (e.g., scim-admin)}"
: "${SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET:?Set SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET}"
: "${SCIM_BRIDGE_MAS_BASE_URL:?Set SCIM_BRIDGE_MAS_BASE_URL (e.g., https://api....../scim/v2)}"
: "${SCIM_BRIDGE_MAS_PROFILE_ID:?Set SCIM_BRIDGE_MAS_PROFILE_ID (e.g., test1)}"
: "${SCIM_BRIDGE_NAMESPACE:=iam}"
: "${SCIM_BRIDGE_KEYCLOAK_RELEASE:=mas-iam-sample}"
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
SCIM_BRIDGE_INCLUDE_USERNAMES=${SCIM_BRIDGE_INCLUDE_USERNAMES:-}
SCIM_BRIDGE_INCLUDE_USERNAME_PREFIX=${SCIM_BRIDGE_INCLUDE_USERNAME_PREFIX:-}
SCIM_BRIDGE_STORAGE_CLASS=${SCIM_BRIDGE_STORAGE_CLASS:-}
SCIM_BRIDGE_KEYCLOAK_CA_FILE=${SCIM_BRIDGE_KEYCLOAK_CA_FILE:-}
SCIM_BRIDGE_MAS_CA_FILE=${SCIM_BRIDGE_MAS_CA_FILE:-}
SCIM_BRIDGE_IMAGE_PULL_SECRETS=${SCIM_BRIDGE_IMAGE_PULL_SECRETS:-[]}

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
  echo "error: SCIM_BRIDGE_KEYCLOAK_BASE_URL is required (set it or enable route cert automation)" >&2
  exit 1
fi

if [[ -z "$SCIM_BRIDGE_STORAGE_CLASS" ]]; then
  default_sc=$(oc get sc -o jsonpath='{range .items[?(@.metadata.annotations.storageclass\.kubernetes\.io/is-default-class=="true")]}{.metadata.name}{"\n"}{end}' 2>/dev/null | head -n1)
  if [[ -n "$default_sc" ]]; then
    SCIM_BRIDGE_STORAGE_CLASS="$default_sc"
  else
    rook_sc=$(oc get sc -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null | grep -E 'rook|ceph' | head -n1 || true)
    if [[ -n "$rook_sc" ]]; then
      SCIM_BRIDGE_STORAGE_CLASS="$rook_sc"
    else
      echo "no storage class set or detected; set SCIM_BRIDGE_STORAGE_CLASS explicitly" >&2
      exit 1
    fi
  fi
fi

command -v envsubst >/dev/null 2>&1 || { echo "envsubst is required" >&2; exit 1; }

REPO_ROOT="$ROOT_DIR"
MANIFEST_TEMPLATE="$REPO_ROOT/manifests/scim-bridge.yaml"
KEYCLOAK_BOOTSTRAP_TEMPLATE="$REPO_ROOT/manifests/scim-bridge-keycloak-bootstrap.yaml"
KEYCLOAK_ROUTE_CERT_TEMPLATE="$REPO_ROOT/manifests/scim-bridge-keycloak-route-cert.yaml"
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
  local subst_vars=""
  if (( ${#vars[@]} > 0 )); then
    subst_vars="$(printf '${%s} ' "${vars[@]}")"
  fi
  envsubst "$subst_vars" <"$template_path" >"$out_path.tmp"
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
        echo "error: SCIM_BRIDGE_KEYCLOAK_IMAGE is required (unable to auto-detect from Keycloak pods)" >&2
        exit 1
      fi
      if [[ ! -f "$KEYCLOAK_BOOTSTRAP_TEMPLATE" ]]; then
        echo "bootstrap manifest not found: $KEYCLOAK_BOOTSTRAP_TEMPLATE" >&2
        exit 1
      fi
      echo "[scim-bridge] provisioning Keycloak client ${SCIM_BRIDGE_KEYCLOAK_CLIENT_ID} in realm ${SCIM_BRIDGE_KEYCLOAK_REALM} via Kubernetes Job"
      oc delete job -n "$SCIM_BRIDGE_KEYCLOAK_NAMESPACE" "$SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_JOB_NAME" --ignore-not-found >/dev/null 2>&1 || true
      render_manifest "$KEYCLOAK_BOOTSTRAP_TEMPLATE" "$WORK_DIR/scim-bridge-keycloak-bootstrap-rendered.yaml" bootstrap_subst_vars[@]
      oc apply -f "$WORK_DIR/scim-bridge-keycloak-bootstrap-rendered.yaml"
      if [[ "${SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_WAIT}" == "true" ]]; then
        oc wait -n "$SCIM_BRIDGE_KEYCLOAK_NAMESPACE" --for=condition=complete "job/${SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_JOB_NAME}" --timeout=10m
      fi
      ;;
    script)
      if [[ ! -x "$CONFIGURE_SCRIPT" ]]; then
        echo "configure-scim-client.sh not found or not executable at $CONFIGURE_SCRIPT" >&2
        exit 1
      fi
      echo "[scim-bridge] provisioning Keycloak client ${SCIM_BRIDGE_KEYCLOAK_CLIENT_ID} in realm ${SCIM_BRIDGE_KEYCLOAK_REALM} via oc exec (advanced)"
      "$CONFIGURE_SCRIPT" \
        --namespace "$SCIM_BRIDGE_KEYCLOAK_NAMESPACE" \
        --release "$SCIM_BRIDGE_KEYCLOAK_RELEASE" \
        --realm "$SCIM_BRIDGE_KEYCLOAK_REALM" \
        --client-id "$SCIM_BRIDGE_KEYCLOAK_CLIENT_ID" \
        --client-secret "$client_secret"
      ;;
    none|disabled|false)
      echo "[scim-bridge] skipping Keycloak provisioning (SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_METHOD=${SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_METHOD})"
      ;;
    *)
      echo "error: unknown SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_METHOD=${SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_METHOD} (expected job|script|none)" >&2
      exit 1
      ;;
  esac
}

provision_keycloak_route_cert() {
  if [[ "${SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_ENABLE}" != "true" ]]; then
    return
  fi

  if [[ "${SCIM_BRIDGE_KEYCLOAK_ROUTE_NAMESPACE}" != "${SCIM_BRIDGE_NAMESPACE}" ]]; then
    echo "error: route cert job requires SCIM_BRIDGE_KEYCLOAK_ROUTE_NAMESPACE to match SCIM_BRIDGE_NAMESPACE" >&2
    exit 1
  fi

  if [[ ! -f "$KEYCLOAK_ROUTE_CERT_TEMPLATE" ]]; then
    echo "route cert manifest not found: $KEYCLOAK_ROUTE_CERT_TEMPLATE" >&2
    exit 1
  fi

  echo "[scim-bridge] provisioning Keycloak route TLS certificate via Kubernetes Job"
  oc delete job -n "$SCIM_BRIDGE_KEYCLOAK_ROUTE_NAMESPACE" "$SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_JOB_NAME" --ignore-not-found >/dev/null 2>&1 || true
  render_manifest "$KEYCLOAK_ROUTE_CERT_TEMPLATE" "$WORK_DIR/scim-bridge-keycloak-route-cert-rendered.yaml" route_cert_subst_vars[@]
  oc apply -f "$WORK_DIR/scim-bridge-keycloak-route-cert-rendered.yaml"
  if [[ "${SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_WAIT}" == "true" ]]; then
    oc wait -n "$SCIM_BRIDGE_KEYCLOAK_ROUTE_NAMESPACE" --for=condition=complete "job/${SCIM_BRIDGE_KEYCLOAK_ROUTE_CERT_JOB_NAME}" --timeout=10m
  fi
}

if [[ ! -f "$MANIFEST_TEMPLATE" ]]; then
  echo "manifest not found: $MANIFEST_TEMPLATE" >&2
  exit 1
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
  "\${SCIM_BRIDGE_KEYCLOAK_INSECURE_SKIP_VERIFY}"
  "\${SCIM_BRIDGE_BRIDGE_MODE}"
  "\${SCIM_BRIDGE_BRIDGE_POLL_INTERVAL}"
  "\${SCIM_BRIDGE_BRIDGE_STATE_BACKEND}"
  "\${SCIM_BRIDGE_BRIDGE_STATE_PATH}"
  "\${SCIM_BRIDGE_BRIDGE_ALLOW_UPDATES}"
  "\${SCIM_BRIDGE_BRIDGE_DRY_RUN}"
  "\${SCIM_BRIDGE_INCLUDE_USERNAMES}"
  "\${SCIM_BRIDGE_INCLUDE_USERNAME_PREFIX}"
  "\${SCIM_BRIDGE_STORAGE_CLASS}"
  "\${SCIM_BRIDGE_IMAGE_PULL_SECRETS}"
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
  SCIM_BRIDGE_KEYCLOAK_INSECURE_SKIP_VERIFY
  SCIM_BRIDGE_BRIDGE_MODE
  SCIM_BRIDGE_BRIDGE_POLL_INTERVAL
  SCIM_BRIDGE_BRIDGE_STATE_BACKEND
  SCIM_BRIDGE_BRIDGE_STATE_PATH
  SCIM_BRIDGE_BRIDGE_ALLOW_UPDATES
  SCIM_BRIDGE_BRIDGE_DRY_RUN
  SCIM_BRIDGE_INCLUDE_USERNAMES
  SCIM_BRIDGE_INCLUDE_USERNAME_PREFIX
  SCIM_BRIDGE_STORAGE_CLASS
  SCIM_BRIDGE_IMAGE_PULL_SECRETS
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

for ph in "${placeholders[@]}"; do
  if ! grep -q "$ph" "$MANIFEST_RENDERED"; then
    echo "warning: placeholder $ph not found in manifest; ensure manifests/scim-bridge.yaml includes it" >&2
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
  SCIM_BRIDGE_KEYCLOAK_INSECURE_SKIP_VERIFY \
  SCIM_BRIDGE_BRIDGE_MODE \
  SCIM_BRIDGE_BRIDGE_POLL_INTERVAL \
  SCIM_BRIDGE_BRIDGE_STATE_BACKEND \
  SCIM_BRIDGE_BRIDGE_STATE_PATH \
  SCIM_BRIDGE_BRIDGE_ALLOW_UPDATES \
  SCIM_BRIDGE_BRIDGE_DRY_RUN \
  SCIM_BRIDGE_INCLUDE_USERNAMES \
  SCIM_BRIDGE_INCLUDE_USERNAME_PREFIX \
  SCIM_BRIDGE_STORAGE_CLASS \
  SCIM_BRIDGE_IMAGE_PULL_SECRETS \
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
  SCIM_BRIDGE_KEYCLOAK_ROUTE_DOMAIN_SOURCE

render_manifest "$MANIFEST_RENDERED" "$MANIFEST_RENDERED" scim_bridge_subst_vars[@]

oc create namespace "$SCIM_BRIDGE_NAMESPACE" 2>/dev/null || true
if [[ "$SCIM_BRIDGE_KEYCLOAK_NAMESPACE" != "$SCIM_BRIDGE_NAMESPACE" ]]; then
  oc create namespace "$SCIM_BRIDGE_KEYCLOAK_NAMESPACE" 2>/dev/null || true
fi
provision_keycloak_route_cert
provision_keycloak
echo "[scim-bridge] applying manifest to $SCIM_BRIDGE_NAMESPACE namespace"
oc apply -n "$SCIM_BRIDGE_NAMESPACE" -f "$MANIFEST_RENDERED"

echo "[scim-bridge] deployed image: $SCIM_BRIDGE_IMAGE"
echo "[scim-bridge] MAS base/profile: $SCIM_BRIDGE_MAS_BASE_URL / $SCIM_BRIDGE_MAS_PROFILE_ID"
echo "[scim-bridge] Keycloak: $SCIM_BRIDGE_KEYCLOAK_BASE_URL realm=$SCIM_BRIDGE_KEYCLOAK_REALM"
echo "[scim-bridge] check status: oc get pods -n $SCIM_BRIDGE_NAMESPACE"
echo "[scim-bridge] logs: oc logs deploy/scim-bridge -n $SCIM_BRIDGE_NAMESPACE"
