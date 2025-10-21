#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: reset-namespace.sh --namespace <ns> [--release <name>] [--force]

Deletes Keycloak stack resources created by the MAS IAM operator so that the
namespace can be re-used for fresh installs. The TLS secret required by LDAP is
left intact so you do not need to regenerate it for each reset.

Options:
  -n, --namespace   Namespace that hosts the Keycloak stack (required)
      --release     Helm release / KeycloakStack name (default: keycloakstack-sample)
  -f, --force       Do not prompt for confirmation
  -h, --help        Show this message and exit
EOF
}

namespace=""
release="keycloakstack-sample"
force=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    -n|--namespace)
      namespace="${2-}"
      shift 2
      ;;
    --release)
      release="${2-}"
      shift 2
      ;;
    -f|--force)
      force=true
      shift
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

if [[ -z "${namespace}" ]]; then
  echo "error: --namespace is required." >&2
  usage >&2
  exit 1
fi

if ! command -v oc >/dev/null 2>&1; then
  echo "error: oc CLI is required on PATH." >&2
  exit 1
fi

if [[ "${force}" == false ]]; then
  read -r -p "Delete Keycloak stack '${release}' resources in namespace '${namespace}'? [y/N] " reply
  case "${reply}" in
    y|Y|yes|YES)
      ;;
    *)
      echo "Aborted."
      exit 0
      ;;
  esac
fi

delete_resource() {
  local kind="$1"
  local name="$2"

  if oc get "${kind}" "${name}" -n "${namespace}" >/dev/null 2>&1; then
    echo "Deleting ${kind}/${name} in ${namespace}"
    oc delete "${kind}" "${name}" -n "${namespace}" --ignore-not-found >/dev/null 2>&1 || true
  else
    echo "Skipping ${kind}/${name}: not found in ${namespace}"
  fi
}

echo "Cleaning Keycloak stack '${release}' in namespace '${namespace}'"

# Remove the KeycloakStack custom resource first.
delete_resource keycloakstacks.iam.iam.mas.ibm.com "${release}"

# Delete operator-managed secrets created for the release.
mapfile -t release_secrets < <(oc get secret -n "${namespace}" \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null | grep "^${release}-" || true)

for secret in "${release_secrets[@]}"; do
  delete_resource secret "${secret}"
done

# Remove any dev TLS secret that may not share the release prefix.
delete_resource secret "keycloakstack-sample-keycloak-openldap-tls"

# Remove Jobs that may linger after the CR is deleted.
delete_resource job "${release}-keycloak-ldap-config"

# Delete the PostgreSQL PVC (name matches StatefulSet volume claim).
pvc_name="data-${release}-postgresql-0"
delete_resource pvc "${pvc_name}"

# Remove associated ConfigMaps to ensure clean re-apply of generated resources.
delete_resource configmap "${release}-postgresql-configuration"
delete_resource configmap "${release}-postgresql-scripts"

# Clean up subscription and CSV if the operator was scoped to this namespace.
subscription_name="keycloak-stack-operator"
delete_resource subscription "${subscription_name}"

csv_selector="operators.coreos.com/${subscription_name}.${namespace}"
mapfile -t csvs < <(oc get csv -n "${namespace}" -l "${csv_selector}" \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true)
for csv in "${csvs[@]}"; do
  [[ -n "${csv}" ]] || continue
  delete_resource csv "${csv}"
done

# Delete catalog source if it lives alongside the operator (not the global marketplace).
delete_resource catalogsource mas-iam-operator

echo "Namespace '${namespace}' cleanup complete."
echo "Reapply manifests once prerequisites (e.g., LDAP TLS secret) are in place."
