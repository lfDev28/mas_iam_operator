#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: test-openldap-bind.sh [--namespace <ns>] [--release <name>] [--base-dn <dn>] [--bind-dn <dn>]

Verifies that the MAS IAM chart can authenticate to the bundled OpenLDAP instance
by running ldapwhoami from inside the OpenLDAP pod. The script automatically
retrieves the admin password from <release>-openldap-admin and prints the DN
returned by the directory if the bind succeeds.

Options:
  -n, --namespace   Namespace that hosts the MAS IAM stack (default: iam)
  -r, --release     Helm release / MasIamStack name (default: mas-iam-sample)
      --base-dn     Directory base DN (default: dc=demo,dc=local)
      --bind-dn     Bind DN (default: cn=admin,<base-dn>)
  -h, --help        Show this help
EOF
}

namespace="iam"
release="mas-iam-sample"
base_dn="dc=demo,dc=local"
bind_dn=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    -n|--namespace) namespace="$2"; shift 2 ;;
    -r|--release) release="$2"; shift 2 ;;
    --base-dn) base_dn="$2"; shift 2 ;;
    --bind-dn) bind_dn="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage; exit 1 ;;
  esac
done

if [[ -z "$bind_dn" ]]; then
  bind_dn="cn=admin,${base_dn}"
fi

secret="${release}-openldap-admin"
svc_host="${release}-openldap.${namespace}.svc.cluster.local"

if ! command -v oc >/dev/null 2>&1; then
  echo "error: oc CLI not found in PATH" >&2
  exit 1
fi

password=$(oc get secret "$secret" -n "$namespace" -o jsonpath='{.data.password}' 2>/dev/null | base64 --decode)
if [[ -z "$password" ]]; then
  echo "error: unable to read password from secret ${secret} in namespace ${namespace}" >&2
  exit 1
fi

pod=$(oc get pods -n "$namespace" -l "app.kubernetes.io/instance=${release},app.kubernetes.io/component=openldap" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
if [[ -z "$pod" ]]; then
  echo "error: could not locate OpenLDAP pod in namespace ${namespace}" >&2
  exit 1
fi

echo "Running ldapwhoami against ldaps://${svc_host}:636 ..."
oc exec -n "$namespace" "$pod" -- ldapwhoami -x -H "ldaps://${svc_host}:636" -D "$bind_dn" -w "$password"
