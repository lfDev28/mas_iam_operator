#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: link-scim-users-oidc.sh --instance <id> --idp <id> [--idp-type <oidc|saml>] [--mongo-namespace <ns>] [--mongo-instance <name>]

For each user in mas_{instance}_core.User that has a scimLinkConfig but no
identities.{idp}, set identities.{idp} to an IDP-shaped entry. This serves
two purposes simultaneously:

  1) IDP login linkage: the user can log in via the configured OIDC/SAML
     provider without needing self-registration (which would 409-conflict
     with the existing SCIM-created user record).
  2) Manage workspace sync: MAS Manage's MEA validates each identity-key
     against a registered IDPCfg in MASUSERIDP. The MAS SCIM endpoint
     auto-populates "_local" and "default" keys, which fail validation
     (system#idpnotfound) on MAS instances where the IDPCfg is registered
     under a "-{type}"-suffixed key (the post-beta.15 reserved-word shape,
     e.g. "default-oidc" / "default-saml"). Those two auto-generated keys
     are removed.

Only identities.{idp} is written and only identities._local /
identities.default are removed; every other identity (e.g. a default-saml
link when this run links default-oidc) is kept. The script used to replace
the whole identities map, which dropped those.

The identity document depends on --idp-type:
  oidc  full entry (type/id/uniqueId/subject/lastLogin) matching what MAS
        writes on a real OIDC login.
  saml  minimal stub { type: 'saml', id: <primaryEmail> } — the id must be
        the SAML NameID, which the mas-est Keycloak SAML client forces to
        EMAIL format (MAS's SamlCredentialResolver looks users up by NameID).
        MAS overwrites the stub with its own structure on the user's first
        successful login.

The update also flips applications.manage.sync.state (and sync.status) to
PENDING. The SCIM bridge creates users minutes before mas-auth apply runs
this linker, so the user-sync agent's first Manage pass always happens with
the pre-link identities and records a stale "local" MASUSERIDP row. The
PENDING flip forces the agent to re-sync with the corrected identity map on
its next poll — without it, Manage keeps the stale row and IDP login into
Manage cannot map the user.

Flags:
  --instance         MAS instance id (e.g. mas91)
  --idp              IDP id MAS uses for identity-key lookups.
                     For idpId="default", MAS internally registers as
                     "default-{type}" — pass e.g. "default-oidc" here.
  --idp-type         Provider type: oidc (default) or saml
  --oidc-idp         Deprecated alias for --idp
  --mongo-namespace  Namespace running the MAS Mongo replica set
                     (default: $MAS_EST_MONGO_NAMESPACE, else mongoce; when
                     neither is set and mongoce has no Mongo, the namespace
                     holding the {instance}-admin-admin secret is used)
  --mongo-instance   Mongo CR instance name
                     (default: $MAS_EST_MONGO_INSTANCE, else mas-mongo-ce)
EOF
}

instance=""
idp=""
idp_type="oidc"
mongo_namespace="${MAS_EST_MONGO_NAMESPACE:-}"
mongo_instance="${MAS_EST_MONGO_INSTANCE:-mas-mongo-ce}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --instance)         instance="${2-}"; shift 2 ;;
    --idp)              idp="${2-}"; shift 2 ;;
    --oidc-idp)         idp="${2-}"; shift 2 ;;  # deprecated alias, kept for pre-generalization callers
    --idp-type)         idp_type="${2-}"; shift 2 ;;
    --mongo-namespace)  mongo_namespace="${2-}"; shift 2 ;;
    --mongo-instance)   mongo_instance="${2-}"; shift 2 ;;
    -h|--help)          usage; exit 0 ;;
    *) echo "error: unknown argument: $1" >&2; usage >&2; exit 1 ;;
  esac
done

if [[ -z "${instance}" || -z "${idp}" ]]; then
  echo "error: --instance and --idp are required" >&2
  usage >&2
  exit 1
fi

case "${idp_type}" in
  oidc|saml) ;;
  *) echo "error: --idp-type must be oidc or saml (got: ${idp_type})" >&2; exit 1 ;;
esac

if ! command -v oc >/dev/null 2>&1; then
  echo "error: oc CLI is required on PATH." >&2
  exit 1
fi

# fail explains what an unlinked SCIM user means and how to finish the job by
# hand; a bare "re-run mas-auth apply" does not help when Mongo simply lives
# somewhere this script did not look.
fail() {
  cat >&2 <<EOF
error: $*
  Effect: SCIM-synced users in MAS instance ${instance} are NOT linked to ${idp}; they
  cannot sign in through the ${idp_type} provider (self-registration 409s on the existing
  user) and Manage sync may reject them with system#idpnotfound. Everything else
  mas-auth configured is in place.
  Fix: point the linker at the MAS Mongo replica set and run it directly:
    MAS_EST_MONGO_NAMESPACE=<mongo-namespace> MAS_EST_MONGO_INSTANCE=<mongo-cr-name> \\
      scripts/link-scim-users-oidc.sh --instance ${instance} --idp ${idp} --idp-type ${idp_type}
  Find the namespace with: oc get secret -A | grep -- '-admin-admin'
  (MAS 9.2+ keeps users in Manage rather than Mongo; there this step has nothing to link.)
EOF
  exit 1
}

conn_secret="${mongo_instance}-admin-admin"
if [[ -z "${mongo_namespace}" ]]; then
  mongo_namespace="mongoce"
  if ! oc get secret "${conn_secret}" -n "${mongo_namespace}" >/dev/null 2>&1; then
    # Not the default location: look for the Mongo admin secret cluster-wide.
    detected=$(oc get secret -A --field-selector "metadata.name=${conn_secret}" \
      -o jsonpath='{.items[0].metadata.namespace}' 2>/dev/null || true)
    if [[ -n "${detected}" ]]; then
      echo "[link-users] Mongo admin secret ${conn_secret} found in namespace ${detected}"
      mongo_namespace="${detected}"
    fi
  fi
fi

conn_string=$(oc get secret "${conn_secret}" -n "${mongo_namespace}" -o jsonpath='{.data.connectionString\.standard}' 2>/dev/null | base64 -d || true)
if [[ -z "${conn_string}" ]]; then
  fail "unable to read Mongo connectionString.standard from secret ${mongo_namespace}/${conn_secret}"
fi

pod=$(oc get pod -n "${mongo_namespace}" -l app="${mongo_instance}-svc" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
if [[ -z "${pod}" ]]; then
  if oc get pod -n "${mongo_namespace}" "${mongo_instance}-0" >/dev/null 2>&1; then
    pod="${mongo_instance}-0"
  else
    fail "no Mongo pod found in namespace ${mongo_namespace} (label app=${mongo_instance}-svc, or pod ${mongo_instance}-0)"
  fi
fi

db_name="mas_${instance}_core"

echo "[link-users] checking ${db_name}.User for SCIM-managed users missing identities.${idp}"
oc exec -n "${mongo_namespace}" "${pod}" -c mongod -- \
  mongosh "${conn_string}" --tls --tlsAllowInvalidCertificates --quiet --eval "
db = db.getSiblingDB('${db_name}');
// MAS 9.2 keeps users in Manage, not in a Mongo User collection: there is
// nothing to link here, and a silent 'needing link: 0' would read as success.
if (!db.getCollectionNames().includes('User')) {
  print('[link-users] ${db_name} has no User collection (MAS 9.2 and later keep users in Manage); skipping the identity link');
  quit(0);
}
const idp = '${idp}';
const idpType = '${idp_type}';
const users = db.User.find({
  scimLinkConfig: { \$exists: true },
  ['identities.' + idp]: { \$exists: false }
}, { _id: 1, emails: 1 }).toArray();
print('[link-users] scim users needing ' + idpType + ' link: ' + users.length);
const ts = new Date().toISOString();
users.forEach(u => {
  // oidc: full entry matching a real OIDC login; MAS's OIDC resolver keys
  // on preferred_username, so id = the MAS user id.
  // saml: minimal stub — MAS's SamlCredentialResolver looks the user up by
  // the assertion NameID, which our Keycloak SAML client forces to EMAIL
  // format, so id must be the user's primary email (validated live on MAS
  // 9.0: AIUEV2150A logs the lookup key as the email; a username id is
  // never matched). MAS overwrites the stub with its own structure on the
  // user's first successful login.
  const primaryEmail =
    ((u.emails || []).filter(e => e.primary)[0] || (u.emails || [])[0] || {}).value || u._id;
  const identity = idpType === 'oidc'
    ? {
        type: 'oidc',
        id: u._id,
        uniqueId: 'OIDCRESOVER_' + idp + '_realm/' + u._id,
        subject: u._id,
        lastLogin: ts
      }
    : {
        type: 'saml',
        id: primaryEmail
      };
  // Write only identities.<idp>; drop only the SCIM auto-generated _local /
  // default keys Manage rejects (system#idpnotfound). Replacing the whole
  // map used to wipe every other identity, e.g. a default-saml link.
  // (the set and unset touch different paths, so one update is fine; the
  // unset list is never empty because idp is never both _local and default.)
  const unset = {};
  ['_local', 'default'].filter(k => k !== idp).forEach(k => { unset['identities.' + k] = ''; });
  db.User.updateOne(
    { _id: u._id },
    { \$set: {
        ['identities.' + idp]: identity,
        'applications.manage.sync.state': 'PENDING',
        'sync.status': 'PENDING'
    }, \$unset: unset }
  );
  print('[link-users]   linked: ' + u._id);
});
print('[link-users] done');
" 2>&1
