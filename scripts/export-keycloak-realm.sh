#!/usr/bin/env bash
set -euo pipefail

show_usage() {
  cat <<'USAGE'
Usage: export-keycloak-realm.sh <namespace> <release> <realm> [output-path]

Exports a Keycloak realm JSON from the running mas-est IAM deployment. The export
is performed with the Keycloak CLI inside the pod, avoiding HTTP management
port conflicts, and the resulting realm file is written locally.

Arguments:
  namespace    Kubernetes namespace where Keycloak runs (default: mas-est).
  release      Helm release name (default: mas-est-iam).
  realm        Realm name to export.
  output-path  Optional local path for the exported JSON. Defaults to
               charts/mas-iam-stack/realm-config/<realm>-realm.json relative
               to the repository root.
USAGE
}

if [[ ${1:-} == "--help" || ${1:-} == "-h" ]]; then
  show_usage
  exit 0
fi

if [[ $# -lt 3 || $# -gt 4 ]]; then
  show_usage >&2
  exit 1
fi

NAMESPACE=$1
RELEASE=$2
REALM=$3
OUTPUT=${4:-"charts/mas-iam-stack/realm-config/${REALM}-realm.json"}

LABEL_SELECTOR="app.kubernetes.io/name=keycloak,app.kubernetes.io/instance=${RELEASE}"
POD=$(kubectl get pods -n "${NAMESPACE}" -l "${LABEL_SELECTOR}" \
  -o jsonpath='{.items[0].metadata.name}')

if [[ -z ${POD} ]]; then
  echo "Failed to locate a Keycloak pod for release '${RELEASE}' in namespace '${NAMESPACE}'." >&2
  exit 2
fi

REMOTE_DIR="/tmp/${REALM}-realm-export"
REMOTE_FILE="${REMOTE_DIR}/${REALM}-realm.json"

cleanup_remote() {
  kubectl exec -n "${NAMESPACE}" "${POD}" -- rm -rf "${REMOTE_DIR}" >/dev/null 2>&1 || true
}

cleanup_remote
trap cleanup_remote EXIT

kubectl exec -n "${NAMESPACE}" "${POD}" -- mkdir -p "${REMOTE_DIR}" >/dev/null 2>&1

kubectl exec -n "${NAMESPACE}" "${POD}" -- env \
  KC_HTTP_PORT=0 \
  KC_HTTP_MANAGEMENT_PORT=0 \
  /opt/keycloak/bin/kc.sh export \
    --realm "${REALM}" \
    --dir "${REMOTE_DIR}" \
    --users skip >/dev/null

# The chart ships this file in a public repo and a public image, so the export
# must carry no key material. Keycloak generates fresh realm keys on import
# when the KeyProvider components are absent. This export once leaked the
# realm's RSA/HMAC/AES keys into every install.
command -v jq >/dev/null 2>&1 || { echo "jq is required to sanitise the export" >&2; exit 3; }
RAW=$(mktemp)
trap 'cleanup_remote; rm -f "${RAW}"' EXIT
kubectl exec -n "${NAMESPACE}" "${POD}" -- cat "${REMOTE_FILE}" > "${RAW}"

jq '
  del(.users)
  | del(.components["org.keycloak.keys.KeyProvider"])
  | if .clients then .clients |= map(del(.secret)) else . end
' "${RAW}" > "${OUTPUT}"

if grep -Eq '"(privateKey|hashedSaltedValue|secretData)"' "${OUTPUT}" \
  || jq -e '[.. | objects | select(has("secret")) | .secret | select(type == "string" and . != "" and . != "**********")] | length > 0' "${OUTPUT}" >/dev/null; then
  echo "Export still contains key material or secrets; refusing to leave it at ${OUTPUT}." >&2
  rm -f "${OUTPUT}"
  exit 4
fi

echo "Realm '${REALM}' exported to ${OUTPUT} (users, realm keys and client secrets stripped)" >&2
