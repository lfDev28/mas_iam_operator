#!/usr/bin/env bash
set -euo pipefail

show_usage() {
  cat <<'USAGE'
Usage: export-keycloak-realm.sh <namespace> <release> <realm> [output-path]

Exports a Keycloak realm JSON from the running mas-iam deployment. The export
is performed with the Keycloak CLI inside the pod, avoiding HTTP management
port conflicts, and the resulting realm file is written locally.

Arguments:
  namespace    Kubernetes namespace where Keycloak runs (for MAS this is often 'iam').
  release      Helm release name (for MAS this is often 'mas-iam').
  realm        Realm name to export.
  output-path  Optional local path for the exported JSON. Defaults to
               charts/keycloak-stack/realm-config/<realm>-realm.json relative
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
OUTPUT=${4:-"charts/keycloak-stack/realm-config/${REALM}-realm.json"}

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
    --users realm_file >/dev/null

kubectl exec -n "${NAMESPACE}" "${POD}" -- cat "${REMOTE_FILE}" > "${OUTPUT}"

echo "Realm '${REALM}' exported to ${OUTPUT}" >&2
