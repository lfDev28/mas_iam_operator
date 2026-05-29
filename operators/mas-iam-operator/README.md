# MAS EST IAM Operator (Helm-based)

This directory contains a Helm-based operator scaffold generated with
`operator-sdk` to manage the MAS EST IAM stack chart.

## Prerequisites

- For a one-file end-to-end install flow (operator + sample stack + SCIM bridge),
  see `../../docs/INSTALL-ALL-IN-ONE.md`.
- Docker (or another OCI image build tool)
- `kubectl`
- `kustomize` v5.0+
- `operator-sdk` v1.33.0 (the repo already includes `bin/operator-sdk`; add `bin/`
  to your `PATH` or invoke the binary explicitly)
- Access to a container registry where you can push operator and bundle images

For the single-manifest install path, the YAML now includes a bootstrap job that
creates the LDAP TLS secret automatically (self-signed, dev use only) and
generates a random truststore password on each run. The password never appears
in pod logs—retrieve it from the secret instead:

```bash
kubectl get secret mas-est-iam-keycloak-openldap-tls \
  -n mas-est -o jsonpath='{.data.truststorePassword}' | base64 -d && echo
```

The helper script remains available if you need to regenerate the secret
manually or outside the cluster:

```bash
./scripts/dev-generate-openldap-tls.sh -n mas-est -r mas-est-iam
```

### TLS bootstrap image

The consolidated install manifest references a lightweight helper image that
generates self-signed TLS assets and writes the resulting secret through the
Kubernetes API. The Dockerfile and helper script live in
`images/openldap-tls-generator/`; the image only needs Bash, `openssl`, and
`kubectl`, and it runs as UID 10001 with the `mas-iam-openldap-tls-generator`
SCC. Use `make tls-image` / `make tls-push` from the repo root to build and
publish the image:

```bash
# Override TLS_IMG if you use a different namespace
TLS_IMG=quay.io/<org>/openldap-tls-generator:0.1.0 make tls-push
```

> The Makefile defaults to Podman (`CONTAINER_ENGINE=podman`). Set
> `CONTAINER_ENGINE=docker` if you prefer Docker.

After pushing, update `manifests/install-olm-sample.yaml` so the Job pulls your image
path. Keeping the helper in its own repository (for example,
`quay.io/<org>/openldap-tls-generator`) avoids mixing operator and catalog
images, keeps SBOM/vulnerability scans scoped to a single artifact, and makes it
easy to delegate push access via a robot account with write privileges on just
that repository.

**Recommended Quay repo layout**

| Purpose                     | Suggested repository / tag example                                   |
|-----------------------------|-----------------------------------------------------------------------|
| Operator manager binary     | `quay.io/<org>/mas-iam-operator:<version>`                            |
| OLM bundle image            | `quay.io/<org>/mas-iam-operator-bundle:<version>`                     |
| Catalog (index) image       | `quay.io/<org>/mas-iam-operator-catalog:<version>`                    |
| TLS bootstrap helper image  | `quay.io/<org>/openldap-tls-generator:<version>`                      |

Create a robot account per organization, grant it _write_ on the repositories it
needs, and log in once via `podman login quay.io -u '<org>+robot' -p '<token>'`.
All of the Makefile targets (`docker-build`, `bundle-push`, `tls-push`, etc.)
then reuse those cached credentials.

## Layout

- `helm-charts/mas-iam-stack`: canonical Helm chart tree consumed by the
  operator (the repo root symlinks `charts/mas-iam-stack` to this directory so
  Helm workflows outside the operator continue to work). Edit the chart in one
  place and run `make lint` from the repo root before pushing to confirm the
  symlink stayed intact.
- `config/`: kustomize overlays for CRDs, RBAC, manager deployment, samples, and
  bundle manifests.
- `config/samples/iam_v1alpha1_masiamstack.yaml`: base specification for a
  `MasIamStack` custom resource. Update the fields (e.g. LDAP `caSecret`,
  `bindCredentialSecret`) to match your environment before applying it.

## Common tasks

Build and push the operator manager image (update `IMG` to match your registry;
set `CONTAINER_ENGINE=podman` if you use Podman):

```bash
cd operators/mas-iam-operator
CONTAINER_ENGINE=podman IMG=quay.io/<org>/mas-iam-operator:0.0.11 make docker-build docker-push
```

Deploy CRDs and the operator:

```bash
make install     # installs the MasIamStack CRD
IMG=quay.io/<org>/mas-iam-operator:0.0.11 make deploy
```

Apply a sample CR (dev-only secrets included):

```bash
kubectl apply -f config/samples/iam_v1alpha1_masiamstack.yaml
```

> **Important:** `manifests/install-olm-sample.yaml` now includes demo secrets
> (password `maxadmin`) so the sample can be applied in one step. Do **not** use
> these defaults in production—create your own secrets and apply a custom
> `MasIamStack` instead.

### Bootstrap admin secret

The chart now requires a pre-created secret referenced by
`keycloak.bootstrapAdmin.secretName`. Create it before applying the CR:

```bash
kubectl create secret generic mas-est-iam-bootstrap-admin \
  --from-literal=username=<admin-user> \
  --from-literal=password="$(openssl rand -base64 24)" \
  -n mas-est
```

> For the dev sample manifest, the secret is created automatically with
> password `maxadmin`.

If you rotate the secret, restart the Keycloak deployment and update the admin
password in Keycloak (the bootstrap init container does not overwrite existing
users).

### PostgreSQL credentials

The operator requires an existing secret for PostgreSQL credentials. Create a
secret that includes the standard Bitnami keys (`password`, `postgres-password`,
and `replication-password` if you enable replication), then reference it via
`postgresql.auth.existingSecret`.

> For the dev sample manifest, the PostgreSQL secret is created automatically
> with password `maxadmin`.

Remove the operator and CRDs:

```bash
make undeploy
make uninstall
```

Generate and build an OLM bundle (the Makefile auto-detects Docker vs Podman, but you can override via `CONTAINER_ENGINE` if needed):

```bash
VERSION=0.0.11 make bundle
CONTAINER_ENGINE=podman VERSION=0.0.11 BUNDLE_IMG=quay.io/<org>/mas-iam-operator-bundle:0.0.11 make bundle-build bundle-push
```

Use `make catalog-build catalog-push` when you are ready to publish the operator
to a catalog source.

```bash
CONTAINER_ENGINE=podman VERSION=0.0.11 \
  IMG=quay.io/<org>/mas-iam-operator:0.0.11 \
  BUNDLE_IMG=quay.io/<org>/mas-iam-operator-bundle:0.0.11 \
  CATALOG_IMG=quay.io/<org>/mas-iam-operator:catalog-0.0.11 \
  make docker-build docker-push bundle bundle-build bundle-push catalog-build catalog-push
```

### Installing via an operator catalog

After publishing the manager image, bundle, and catalog, you can install the
operator with a manifest, then apply the optional sample stack once the CSV is ready.

1. Apply the operator manifest (replace `<org>/<repo>` with this repository
   path, and substitute `mas-est` in the manifest if you plan to use a different
   namespace):

   ```bash
   oc apply -f https://raw.githubusercontent.com/<org>/<repo>/main/manifests/install-olm.yaml
   ```

   The manifest installs the CRD, catalog source, operator group, subscription,
   and the required operator RBAC/SCC bindings.

   The Keycloak deployment ships with an init container that reruns
   `kc.sh bootstrap-admin user` before startup, so the password stored in the
   bootstrap secret you provide is immediately permanent and post-install
   automation (for example the LDAP configuration job) can log in without manual
   intervention.

2. Wait for the CSV in the target namespace to report `Succeeded`:

   ```bash
   oc get csv -n mas-est
   ```

3. Apply the optional sample manifest (dev TLS job + example `MasIamStack`) once
   the operator is ready:

   ```bash
   oc apply -f https://raw.githubusercontent.com/<org>/<repo>/main/manifests/install-olm-sample.yaml
   ```

   Monitor `job/<release>-ldap-config` until it reports success—the job
   retries until Keycloak’s admin API becomes reachable.

   When the chart detects it is running on OpenShift (the
   `security.openshift.io/v1/SecurityContextConstraints` API is present) it
   creates a RoleBinding that grants the Keycloak service account the `anyuid`
   SCC (`system:openshift:scc:anyuid`). For OpenLDAP, the RoleBinding is only
   created when `openldap.containerSecurityContext.runAsNonRoot=false` (opt-out).

 ### Custom TLS for the Keycloak route

By default the Keycloak route (`https://<release>-<ns>.<apps-domain>`) uses the
cluster ingress certificate. If your ingress wildcard is self-signed, browsers
will show a warning. You can now inject a trusted certificate directly via the
`MasIamStack` spec:

```yaml
spec:
  keycloak:
    route:
      tls:
        certificate: |-
          -----BEGIN CERTIFICATE-----
          ...
          -----END CERTIFICATE-----
        key: |-
          -----BEGIN PRIVATE KEY-----
          ...
          -----END PRIVATE KEY-----
        # Optional chain presented to clients
        caCertificate: |-
          -----BEGIN CERTIFICATE-----
          ...
          -----END CERTIFICATE-----
```

The `destinationCACertificate` field is also available if you need to present a
custom CA toward the backend service. Update an existing stack with:

```bash
cat <<'EOF' > keycloak-route-tls-patch.yaml
spec:
  keycloak:
    route:
      tls:
        certificate: |-
          -----BEGIN CERTIFICATE-----
          YOUR-CERT
          -----END CERTIFICATE-----
        key: |-
          -----BEGIN PRIVATE KEY-----
          YOUR-KEY
          -----END PRIVATE KEY-----
        caCertificate: |-
          -----BEGIN CERTIFICATE-----
          OPTIONAL-CHAIN
          -----END CERTIFICATE-----
EOF

oc patch masiamstack mas-est-iam -n mas-est --type merge --patch "$(cat keycloak-route-tls-patch.yaml)"
```

Reapply the patch whenever you rotate the certificate. The route will begin
serving the supplied PEM immediately after the operator reconciles.

### Resetting a development namespace

Use `scripts/reset-namespace.sh` to tear down an environment and start from a
clean slate (add `--purge-tls` if you want to delete the OpenLDAP TLS secret):

```bash
./scripts/reset-namespace.sh --namespace mas-est --release mas-est-iam
```

Add `--force` to skip the confirmation prompt. The script deletes the
`MasIamStack` custom resource, related secrets (including the dev TLS
material), the LDAP configuration job, the PostgreSQL PVC, and the
namespace-scoped OLM objects (subscription/CSV). Reapply
`manifests/install-olm.yaml` to reinstall the operator, then apply
`manifests/install-olm-sample.yaml` to bring the sample stack back. The sample
manifest creates demo secrets (password `maxadmin`) and the TLS bootstrap job
will recreate the OpenLDAP TLS secret automatically; run
`scripts/dev-generate-openldap-tls.sh` only if you want to rotate it outside of
that flow. For production, recreate your own secrets instead of using the demo
defaults.

If you cannot use the helper script, run the equivalent `oc` commands manually
(adjust `RELEASE`/`NAMESPACE` if you customised them):

```bash
NAMESPACE=mas-est
RELEASE=mas-est-iam

oc delete masiamstack "${RELEASE}" -n "${NAMESPACE}" --ignore-not-found
oc delete keycloakstack "${RELEASE}" -n "${NAMESPACE}" --ignore-not-found || true
oc get secret -n "${NAMESPACE}" --no-headers \
  | awk -v r="${RELEASE}-" '$1 ~ r { print $1 }' \
  | xargs -r -I {} oc delete secret {} -n "${NAMESPACE}"
if [[ "${PURGE_TLS:-false}" == "true" ]]; then
  oc delete secret "${RELEASE}-keycloak-openldap-tls" -n "${NAMESPACE}" --ignore-not-found
fi
oc delete job "${RELEASE}-ldap-config" -n "${NAMESPACE}" --ignore-not-found
oc delete job "${RELEASE}-keycloak-ldap-config" -n "${NAMESPACE}" --ignore-not-found
oc delete pvc "data-${RELEASE}-postgresql-0" -n "${NAMESPACE}" --ignore-not-found
oc delete configmap "${RELEASE}-postgresql-configuration" -n "${NAMESPACE}" --ignore-not-found
oc delete configmap "${RELEASE}-postgresql-scripts" -n "${NAMESPACE}" --ignore-not-found
oc delete subscription mas-iam-operator -n "${NAMESPACE}" --ignore-not-found
oc delete csv -n "${NAMESPACE}" -l "operators.coreos.com/mas-iam-operator.${NAMESPACE}" --ignore-not-found
oc delete catalogsource mas-iam-operator -n "${NAMESPACE}" --ignore-not-found || true
```

Recreate the namespace itself (`oc delete project <ns>; oc new-project <ns>`) if
you need a completely fresh project, then reapply `manifests/install-olm.yaml`
and (optionally) `manifests/install-olm-sample.yaml`.

## Troubleshooting

### "Test authentication" fails in the Keycloak console

Keycloak never displays the stored bind credential for LDAP user storage
providers, so the password field on the admin page is blank. The **Test
authentication** button reuses the values currently shown in the form. If you
click it without pasting the password, Keycloak attempts to bind with an empty
credential and OpenLDAP responds with `LDAP: error code 49 - Invalid
Credentials`.

1. Retrieve the password from your OpenLDAP admin secret:
   ```bash
   oc get secret mas-est-iam-openldap-admin \
     -n mas-est -o jsonpath='{.data.password}' | base64 --decode
   ```
2. Paste the password into the **Bind credential** field before clicking **Test
   authentication** or saving updates in the console.

To validate the connection non-interactively, run the helper script (it executes
`ldapwhoami` inside the OpenLDAP pod and prints the DN returned by the server):

```bash
scripts/test-openldap-bind.sh --namespace mas-est --release mas-est-iam
```

Override `--base-dn` or `--bind-dn` if you customised the LDAP hierarchy in
your `MasIamStack`.

### Docker Hub rate limits (PostgreSQL / OpenLDAP)

Bitnami's PostgreSQL base image and Osixia's OpenLDAP image are only
distributed via Docker Hub, so anonymous clusters quickly hit the 500 pulls/day
quota. We mirror both artifacts to Quay as
`quay.io/lee_forster/mas-iam-operator:postgresql-17.6.0-debian-12-r4` and
`quay.io/lee_forster/mas-iam-operator:openldap-1.5.0`, and the default values
now point to those mirrors. If you would rather host the bits yourself, override
the image blocks before applying your `MasIamStack`:

```yaml
spec:
  postgresql:
    image:
      registry: quay.io
      repository: <org>/my-postgresql-mirror
      tag: postgresql-17.6.0-debian-12-r4

  openldap:
    image:
      repository: quay.io/<org>/my-openldap-mirror
      tag: openldap-1.5.0
```

Mirror the upstream images once with `podman pull`/`podman push` (or your
preferred tooling) before applying the `MasIamStack`. Using a non-Docker Hub
registry is strongly
recommended for shared clusters.

### Refreshing the mirrored images with multi-arch manifests

If you pull the upstream images on a single architecture and push them to Quay
directly, only that architecture lands in the manifest. Clusters with a different
CPU architecture will then hit an `exec format error` at container start-up.

Use `scripts/mirror-dev-images.sh` to re-sync the mirrors while keeping the
multi-architecture manifest list intact:

```bash
podman login docker.io
podman login quay.io
./scripts/mirror-dev-images.sh --dest quay.io/<org>/mas-iam-operator
```

The script calls `oc image mirror --keep-manifest-list` so both `linux/amd64`
and `linux/arm64` artifacts reach the destination repository. The PostgreSQL
entry is pinned to a manifest-list digest because Bitnami frequently prunes the
versioned tags from Docker Hub; mirroring by digest keeps the image retrievable
even after the tag disappears. Verify the result with:

```bash
oc image info --filter-by-os=linux/amd64 quay.io/<org>/mas-iam-operator:postgresql-17.6.0-debian-12-r4 >/dev/null
oc image info --filter-by-os=linux/arm64 quay.io/<org>/mas-iam-operator:postgresql-17.6.0-debian-12-r4 >/dev/null
```

Repeat for the OpenLDAP tag. Once the manifest publishes both architectures,
redeploy (`oc rollout restart statefulset/mas-est-iam-postgresql`, etc.) so
the pods pull the rebuilt images.

### LDAP auto-config job reruns every minute

When the Helm-based operator reconciles a release it performs `helm upgrade`
even if no values changed. Helm hooks run on every upgrade, so the
`mas-est-iam-ldap-config` job will restart each reconcile unless you disable
post-upgrade hooks. Set `keycloak.ldap.autoConfigureOnUpgrade: false` in your
`MasIamStack` spec (or `values.yaml` when using plain Helm) to keep the job as a
*post-install* hook only. The job still runs once on the initial install, and
you can re-run it manually later by deleting the job or temporarily toggling the
flag when you need to push new LDAP settings.
