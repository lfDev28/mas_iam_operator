#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: dev-generate-openldap-tls.sh [-n namespace] [-r release] [-p truststore-password]

Generates a development CA, LDAP server certificate/key, PKCS#12 truststore,
and (re)creates the <release>-keycloak-openldap-tls secret in the target
namespace. Intended for local/dev clusters only.

Options:
  -n namespace            Kubernetes namespace (default: iam)
  -r release              Helm release name (default: mas-iam)
  -p truststore-password  Password for the PKCS#12 truststore (default: changeit; pass "random" for a generated value)
  -h                      Show this help
EOF
}

namespace="iam"
release="mas-iam"
truststore_password="changeit"

while getopts ":n:r:p:h" opt; do
  case "${opt}" in
    n) namespace="${OPTARG}" ;;
    r) release="${OPTARG}" ;;
    p) truststore_password="${OPTARG}" ;;
    h)
      usage
      exit 0
      ;;
    *)
      usage
      exit 1
      ;;
  esac
done

command -v kubectl >/dev/null 2>&1 || { echo "kubectl is required" >&2; exit 1; }
command -v openssl >/dev/null 2>&1 || { echo "openssl is required" >&2; exit 1; }

secret_name="${release}-keycloak-openldap-tls"
service_host="${release}-keycloak-openldap"
svc_fqdn="${service_host}.${namespace}.svc"
svc_cluster_fqdn="${svc_fqdn}.cluster.local"

# Allow callers to request a random password by passing "-p random".
if [[ "${truststore_password}" == "random" ]]; then
  truststore_password="$(head -c 24 /dev/urandom | LC_ALL=C tr -dc 'A-Za-z0-9')"
fi

workdir="$(mktemp -d)"
cleanup() {
  rm -rf "${workdir}"
}
trap cleanup EXIT

pushd "${workdir}" >/dev/null

openssl genrsa -out ca.key 4096
openssl req -x509 -new -key ca.key -sha256 -days 3650 \
  -out ca.crt \
  -subj "/CN=${release} OpenLDAP Dev CA"

openssl genrsa -out server.key 4096
openssl req -new -key server.key -out server.csr \
  -subj "/CN=${svc_fqdn}"

cat > server.ext <<EOF
subjectAltName=DNS:${service_host},DNS:${svc_fqdn},DNS:${svc_cluster_fqdn}
EOF

openssl x509 -req -in server.csr \
  -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out server.crt -days 730 -sha256 \
  -extfile server.ext

openssl pkcs12 -export \
  -in ca.crt \
  -nokeys \
  -out ldap-truststore.p12 \
  -passout pass:"${truststore_password}"

kubectl -n "${namespace}" get secret "${secret_name}" >/dev/null 2>&1 && \
  kubectl -n "${namespace}" delete secret "${secret_name}" >/dev/null

kubectl -n "${namespace}" create secret generic "${secret_name}" \
  --from-file=tls.crt=server.crt \
  --from-file=tls.key=server.key \
  --from-file=ca.crt=ca.crt \
  --from-file=ldap-truststore.p12=ldap-truststore.p12 \
  --from-literal=truststorePassword="${truststore_password}"

popd >/dev/null

cat <<EOF
Secret ${secret_name} recreated in namespace ${namespace}.
Truststore password: ${truststore_password}
EOF
