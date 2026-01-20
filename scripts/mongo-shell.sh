#!/usr/bin/env bash

# Launch a mongosh session into the MAS MongoDB replica set (namespace: mongoce).
# Requires: oc, jq, and access to the cluster. No arguments needed.

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=./_scim-bridge-env.sh
source "$ROOT_DIR/scripts/_scim-bridge-env.sh"

NAMESPACE=${MONGO_NAMESPACE:-mongoce}
INSTANCE=${MONGO_INSTANCE:-mas-mongo-ce}

# Fetch connection string (includes admin user/pass, replica set hosts, TLS opts)
CONN=$(oc get secret ${INSTANCE}-admin-admin -n ${NAMESPACE} -o jsonpath='{.data.connectionString\.standard}' | base64 -d)

# Pick a mongod pod
POD=$(oc get pod -n ${NAMESPACE} -l app=${INSTANCE}-svc -o jsonpath='{.items[0].metadata.name}')
if [[ -z "$POD" ]]; then
  # Fallback to the first StatefulSet pod name
  if oc get pod -n ${NAMESPACE} ${INSTANCE}-0 >/dev/null 2>&1; then
    POD=${INSTANCE}-0
  else
    echo "error: no Mongo pods found with label app=${INSTANCE}-svc in namespace ${NAMESPACE}, and ${INSTANCE}-0 missing" >&2
    oc get pods -n ${NAMESPACE} >&2
    exit 1
  fi
fi

echo "Using pod: ${POD}" >&2
echo "Connection: ${CONN}" >&2

# Use exec with TTY so mongosh is interactive
oc exec -it -n ${NAMESPACE} "${POD}" -c mongod -- \
  mongosh "${CONN}" --tls --tlsAllowInvalidCertificates
