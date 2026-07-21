#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: link-scim-users-oidc.sh --instance <id> --oidc-idp <id> [--mongo-namespace <ns>] [--mongo-instance <name>]

For each user in mas_{instance}_core.User that has a scimLinkConfig but no
identities.{oidc-idp}, replace the auto-generated identities map with a single
IDP-shaped entry: { {oidc-idp}: { type: oidc, id: <userId>, ... } }. This
serves two purposes simultaneously:

  1) OIDC login linkage: the user can log in via the configured OIDC provider
     without needing self-registration (which would 409-conflict with the
     existing SCIM-created user record).
  2) Manage workspace sync: MAS Manage's MEA validates each identity-key
     against a registered IDPCfg in MASUSERIDP. The MAS SCIM endpoint
     auto-populates "_local" and "default" keys, which fail validation
     (system#idpnotfound) on MAS instances where the OIDC IDPCfg is
     registered under "default-oidc" (the post-beta.15 reserved-word shape).
     We replace the map rather than add alongside so the final state matches
     what a direct-OIDC-login user receives.

The update also flips applications.manage.sync.state (and sync.status) to
PENDING. The SCIM bridge creates users minutes before mas-auth apply runs
this linker, so the user-sync agent's first Manage pass always happens with
the pre-link identities and records a stale "local" MASUSERIDP row. The
PENDING flip forces the agent to re-sync with the corrected identity map on
its next poll — without it, Manage keeps the stale row and OIDC login into
Manage cannot map the user.

Flags:
  --instance         MAS instance id (e.g. mas91)
  --oidc-idp         OIDC IDP id MAS uses for identity-key lookups.
                     For idpId="default", MAS internally registers as
                     "default-oidc" — pass "default-oidc" here.
  --mongo-namespace  Namespace running the MAS Mongo replica set (default: mongoce)
  --mongo-instance   Mongo CR instance name (default: mas-mongo-ce)
EOF
}

instance=""
oidc_idp=""
mongo_namespace="mongoce"
mongo_instance="mas-mongo-ce"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --instance)         instance="${2-}"; shift 2 ;;
    --oidc-idp)         oidc_idp="${2-}"; shift 2 ;;
    --mongo-namespace)  mongo_namespace="${2-}"; shift 2 ;;
    --mongo-instance)   mongo_instance="${2-}"; shift 2 ;;
    -h|--help)          usage; exit 0 ;;
    *) echo "error: unknown argument: $1" >&2; usage >&2; exit 1 ;;
  esac
done

if [[ -z "${instance}" || -z "${oidc_idp}" ]]; then
  echo "error: --instance and --oidc-idp are required" >&2
  usage >&2
  exit 1
fi

if ! command -v oc >/dev/null 2>&1; then
  echo "error: oc CLI is required on PATH." >&2
  exit 1
fi

conn_secret="${mongo_instance}-admin-admin"
conn_string=$(oc get secret "${conn_secret}" -n "${mongo_namespace}" -o jsonpath='{.data.connectionString\.standard}' 2>/dev/null | base64 -d || true)
if [[ -z "${conn_string}" ]]; then
  echo "error: unable to read Mongo connectionString.standard from ${mongo_namespace}/${conn_secret}" >&2
  exit 1
fi

pod=$(oc get pod -n "${mongo_namespace}" -l app="${mongo_instance}-svc" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
if [[ -z "${pod}" ]]; then
  if oc get pod -n "${mongo_namespace}" "${mongo_instance}-0" >/dev/null 2>&1; then
    pod="${mongo_instance}-0"
  else
    echo "error: no Mongo pod found (label app=${mongo_instance}-svc) in ${mongo_namespace}" >&2
    exit 1
  fi
fi

db_name="mas_${instance}_core"

echo "[link-users] checking ${db_name}.User for SCIM-managed users missing identities.${oidc_idp}"
oc exec -n "${mongo_namespace}" "${pod}" -c mongod -- \
  mongosh "${conn_string}" --tls --tlsAllowInvalidCertificates --quiet --eval "
db = db.getSiblingDB('${db_name}');
const idp = '${oidc_idp}';
const users = db.User.find({
  scimLinkConfig: { \$exists: true },
  ['identities.' + idp]: { \$exists: false }
}, { _id: 1 }).toArray();
print('[link-users] scim users needing oidc link: ' + users.length);
const ts = new Date().toISOString();
users.forEach(u => {
  const identity = {
    type: 'oidc',
    id: u._id,
    uniqueId: 'OIDCRESOVER_' + idp + '_realm/' + u._id,
    subject: u._id,
    lastLogin: ts
  };
  db.User.updateOne(
    { _id: u._id },
    { \$set: {
        identities: { [idp]: identity },
        'applications.manage.sync.state': 'PENDING',
        'sync.status': 'PENDING'
    } }
  );
  print('[link-users]   linked: ' + u._id);
});
print('[link-users] done');
" 2>&1
