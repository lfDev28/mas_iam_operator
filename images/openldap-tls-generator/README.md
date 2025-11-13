# OpenLDAP TLS generator image

This container image is used by the `mas-iam-sample-generate-openldap-tls`
Kubernetes job in `manifests/install-olm.yaml`. It contains only the tooling
required by the bootstrap script (`bash`, `openssl`, `kubectl`) and runs as an
unprivileged user by default.

## Build and push

```bash
TLS_BOOTSTRAP_IMG=quay.io/<org>/openldap-tls-generator:0.1.0
CONTAINER_ENGINE=${CONTAINER_ENGINE:-podman}
${CONTAINER_ENGINE} build \
  -t "${TLS_BOOTSTRAP_IMG}" \
  -f images/openldap-tls-generator/Dockerfile \
  images/openldap-tls-generator
${CONTAINER_ENGINE} push "${TLS_BOOTSTRAP_IMG}"
```

Update the job in `manifests/install-olm.yaml` (or your own overlay) to point at
the published image. Whenever you bump the base OS or `kubectl` version, rebuild
and repush the image, then update the manifest accordingly.
