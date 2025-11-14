#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: configure-scim-client.sh --namespace <ns> --release <name> --client-id <id> --client-secret <secret>

Creates the SCIM helper roles and configures a confidential client with service
accounts enabled inside Keycloak. The script shell-execs into the Keycloak pod
and uses kcadm.sh, so it requires access to the cluster via oc.

Required flags:
  -n, --namespace      Target namespace (default: iam)
  -r, --release        Helm release / MasIamStack name (default: mas-iam-sample)
      --client-id      OAuth client ID to create/use (default: scim-admin)
      --client-secret  Client secret (plain text). Generate a strong value.
EOF
}

namespace="iam"
release="mas-iam-sample"
client_id="scim-admin"
client_secret=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    -n|--namespace)
      namespace="${2-}"
      shift 2
      ;;
    -r|--release)
      release="${2-}"
      shift 2
      ;;
    --client-id)
      client_id="${2-}"
      shift 2
      ;;
    --client-secret)
      client_secret="${2-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "error: unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if [[ -z "${client_secret}" ]]; then
  echo "error: --client-secret is required." >&2
  usage >&2
  exit 1
fi

if ! command -v oc >/dev/null 2>&1; then
  echo "error: oc CLI is required on PATH." >&2
  exit 1
fi

bootstrap_secret="${release}-bootstrap-admin"
if ! oc get secret "${bootstrap_secret}" -n "${namespace}" >/dev/null 2>&1; then
  echo "error: bootstrap admin secret ${bootstrap_secret} not found in ${namespace}" >&2
  exit 1
fi

admin_user=$(oc get secret "${bootstrap_secret}" -n "${namespace}" -o jsonpath='{.data.username}' | base64 -d)
admin_pass=$(oc get secret "${bootstrap_secret}" -n "${namespace}" -o jsonpath='{.data.password}' | base64 -d)

pod=$(oc get pod -n "${namespace}" \
  -l app.kubernetes.io/component=keycloak,app.kubernetes.io/instance="${release}" \
  -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)

if [[ -z "${pod}" ]]; then
  echo "error: unable to locate Keycloak pod for release ${release} in ${namespace}" >&2
  exit 1
fi

echo "Configuring SCIM roles and client inside pod ${pod}..."
oc exec -n "${namespace}" "${pod}" -c keycloak -- env \
  ADMIN_USER="${admin_user}" \
  ADMIN_PASS="${admin_pass}" \
  SCIM_CLIENT_ID="${client_id}" \
  SCIM_CLIENT_SECRET="${client_secret}" \
  bash <<'EOF'
set -euo pipefail
export HOME=/tmp/scim-config
mkdir -p "${HOME}/.keycloak"
/opt/keycloak/bin/kcadm.sh config credentials \
  --server http://127.0.0.1:8080 \
  --realm master \
  --user "${ADMIN_USER}" \
  --password "${ADMIN_PASS}" >/tmp/kcadm.log 2>&1
/opt/keycloak/bin/kcadm.sh create roles -r master -s name=scim-access >/tmp/scim-role.log 2>&1 || true
/opt/keycloak/bin/kcadm.sh create roles -r master -s name=scim-managed >/tmp/scim-role.log 2>&1 || true
client_uuid=$(/opt/keycloak/bin/kcadm.sh get clients -r master --fields clientId,id --format csv --noquotes | awk -F, -v target="${SCIM_CLIENT_ID}" '$1==target {print $2; exit}')
if [[ -z "${client_uuid}" ]]; then
  /opt/keycloak/bin/kcadm.sh create clients -r master \
    -s clientId="${SCIM_CLIENT_ID}" \
    -s enabled=true \
    -s protocol=openid-connect \
    -s publicClient=false \
    -s serviceAccountsEnabled=true \
    -s standardFlowEnabled=false \
    -s directAccessGrantsEnabled=false \
    -s secret="${SCIM_CLIENT_SECRET}" >/tmp/scim-client.log 2>&1
  client_uuid=$(/opt/keycloak/bin/kcadm.sh get clients -r master --fields clientId,id --format csv --noquotes | awk -F, -v target="${SCIM_CLIENT_ID}" '$1==target {print $2; exit}')
else
  /opt/keycloak/bin/kcadm.sh update clients/"${client_uuid}" -r master -s secret="${SCIM_CLIENT_SECRET}" >/tmp/scim-client.log 2>&1 || true
fi
/opt/keycloak/bin/kcadm.sh add-roles -r master \
  --uusername "service-account-${SCIM_CLIENT_ID}" \
  --rolename scim-access >/tmp/scim-role.log 2>&1 || true
EOF

echo "SCIM client '${client_id}' configured. Store the following credentials securely:"
echo "  Client ID:     ${client_id}"
echo "  Client Secret: ${client_secret}"
