#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: configure-demo-users.sh --namespace <ns> --release <name> --realm <realm> [--password <pw>]

Creates 6 demo users directly inside the Keycloak realm (not via LDAP
federation), grouped by intended MAS auth path:

  oidc.user1, oidc.user2  - log in via MAS OIDC self-registration
  saml.user1, saml.user2  - log in via MAS SAML JIT
  scim.user1, scim.user2  - synced into MAS by the SCIM bridge (the bridge's
                             include-username-prefix is "scim."), log in via
                             MAS OIDC with pre-linked identity

The two ldap.* demo users live in OpenLDAP via the LDIF seed (not here).

The script execs into the Keycloak pod and uses kcadm.sh; it requires oc and
the bootstrap admin secret to be present.

Flags:
  -n, --namespace      Namespace running Keycloak (default: mas-est)
  -r, --release        MAS EST IAM release / Helm name (default: mas-est-iam)
      --realm          Realm to seed (default: maximo)
      --password       Password for all demo users (default: maxadmin)
EOF
}

namespace="mas-est"
release="mas-est-iam"
realm="maximo"
password="maxadmin"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -n|--namespace)   namespace="${2-}"; shift 2 ;;
    -r|--release)     release="${2-}"; shift 2 ;;
    --realm)          realm="${2-}"; shift 2 ;;
    --password)       password="${2-}"; shift 2 ;;
    -h|--help)        usage; exit 0 ;;
    *) echo "error: unknown argument: $1" >&2; usage >&2; exit 1 ;;
  esac
done

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

echo "Seeding 6 grouped demo users (oidc.*, saml.*, scim.*) into realm ${realm} via pod ${pod}..."
oc exec -i -n "${namespace}" "${pod}" -c keycloak -- env \
  ADMIN_USER="${admin_user}" \
  ADMIN_PASS="${admin_pass}" \
  REALM="${realm}" \
  DEMO_PASS="${password}" \
  bash <<'EOF'
set -euo pipefail
export HOME=/tmp/demo-users
mkdir -p "${HOME}/.keycloak"
/opt/keycloak/bin/kcadm.sh config credentials \
  --server http://127.0.0.1:8080 \
  --realm master \
  --user "${ADMIN_USER}" \
  --password "${ADMIN_PASS}" >/tmp/kcadm-demo.log 2>&1

# username : firstName : lastName
demo_users="\
oidc.user1:Oidc:One
oidc.user2:Oidc:Two
saml.user1:Saml:One
saml.user2:Saml:Two
scim.user1:Scim:One
scim.user2:Scim:Two"

# Optional: groups for the routing
for g in mas-oidc-users mas-saml-users mas-scim-users; do
  if ! /opt/keycloak/bin/kcadm.sh get groups -r "${REALM}" --fields name --format csv --noquotes 2>/dev/null \
       | tr -d '\r' | grep -Fxq "${g}"; then
    /opt/keycloak/bin/kcadm.sh create groups -r "${REALM}" -s "name=${g}" >/dev/null 2>&1 || true
  fi
done

while IFS=: read -r username first last; do
  [[ -z "${username}" ]] && continue
  existing_uuid=$(
    /opt/keycloak/bin/kcadm.sh get users -r "${REALM}" -q "username=${username}" --fields id,username --format csv --noquotes 2>/dev/null \
      | tr -d '\r' \
      | { grep -F ",${username}" || true; } \
      | head -n1 \
      | cut -d, -f1
  )
  if [[ -n "${existing_uuid}" ]]; then
    echo "  ${username} already exists (id=${existing_uuid}), skipping"
    user_uuid="${existing_uuid}"
  else
    /opt/keycloak/bin/kcadm.sh create users -r "${REALM}" \
      -s "username=${username}" \
      -s "enabled=true" \
      -s "emailVerified=true" \
      -s "firstName=${first}" \
      -s "lastName=${last}" \
      -s "email=${username}@demo.local" >/dev/null
    user_uuid=$(
      /opt/keycloak/bin/kcadm.sh get users -r "${REALM}" -q "username=${username}" --fields id --format csv --noquotes 2>/dev/null \
        | tr -d '\r' \
        | head -n1
    )
    /opt/keycloak/bin/kcadm.sh set-password -r "${REALM}" --userid "${user_uuid}" --new-password "${DEMO_PASS}" >/dev/null
    echo "  created ${username} (id=${user_uuid})"
  fi

  # Add to matching mas-<prefix>-users group
  prefix="${username%%.*}"
  group_name="mas-${prefix}-users"
  group_id=$(
    /opt/keycloak/bin/kcadm.sh get groups -r "${REALM}" -q "search=${group_name}" --fields id,name --format csv --noquotes 2>/dev/null \
      | tr -d '\r' \
      | { grep -F ",${group_name}" || true; } \
      | head -n1 \
      | cut -d, -f1
  )
  if [[ -n "${group_id}" ]]; then
    /opt/keycloak/bin/kcadm.sh update "users/${user_uuid}/groups/${group_id}" -r "${REALM}" \
      -s "realm=${REALM}" -s "userId=${user_uuid}" -s "groupId=${group_id}" -n >/dev/null 2>&1 || true
  fi
done <<<"${demo_users}"

echo "demo-users seed complete"
EOF
