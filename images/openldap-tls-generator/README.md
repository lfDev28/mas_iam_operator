# openldap-tls-generator image

A minimal UBI 9 image with `bash`, `openssl`, and `kubectl` (`oc` is a symlink to it). It runs as uid 10001 with `/bin/bash` as the entrypoint. The `mas-est-iam-generate-openldap-tls` Job in `manifests/install-olm-sample.yaml` uses it to create the self-signed `mas-est-iam-keycloak-openldap-tls` secret. The Job script lives inline in that manifest, not in this image.

The Dockerfile downloads the linux/amd64 `kubectl` binary, so the image is amd64-only.

## Build and push

From the repo root:

```bash
TLS_IMG=<registry>/openldap-tls-generator:<tag> make tls-push
```

The build uses `--platform linux/amd64` (`TLS_PLATFORM`) and podman (`CONTAINER_ENGINE`). If you publish a new tag, update the image reference in `manifests/install-olm-sample.yaml`.
