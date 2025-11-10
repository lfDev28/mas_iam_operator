# MAS IAM Operator (Helm-based)

This directory contains a Helm-based operator scaffold generated with
`operator-sdk` to manage the MAS IAM stack chart.

## Prerequisites

- Docker (or another OCI image build tool)
- `kubectl`
- `kustomize` v5.0+
- `operator-sdk` v1.33.0 (the repo already includes `bin/operator-sdk`; add `bin/`
  to your `PATH` or invoke the binary explicitly)
- Access to a container registry where you can push operator and bundle images

For the single-manifest install path, the YAML now includes a bootstrap job that
creates the LDAP TLS secret automatically (self-signed, dev use only) and
generates a random truststore password on each run. The helper script remains
available if you need to regenerate the secret manually or outside the cluster:

```bash
./scripts/dev-generate-openldap-tls.sh -n iam -r mas-iam-sample
```

## Layout

- `helm-charts/mas-iam-stack`: snapshot of the Helm chart consumed by the
  operator. Re-run `operator-sdk create api` if you need to refresh it from the
  root chart.
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
CONTAINER_ENGINE=podman IMG=quay.io/<org>/mas-iam-operator:0.0.10 make docker-build docker-push
```

Deploy CRDs and the operator:

```bash
make install     # installs the MasIamStack CRD
IMG=quay.io/<org>/mas-iam-operator:0.0.10 make deploy
```

Apply a sample CR (customise secrets and routing first):

```bash
kubectl apply -f config/samples/iam_v1alpha1_masiamstack.yaml
```

> **Important:** populate database credentials before applying the sample.
> Either reference an existing secret via `postgresql.auth.existingSecret`, or
> set the inline `password`/`postgresPassword`/`replicationPassword` fields to
> secure values (they default to empty strings in the sample).

### Bootstrap admin secret

By default the chart keeps `keycloak.bootstrapAdmin.createSecret=true` and
generates `<release>-bootstrap-admin` with a random 24-character password. The
secret is reused on upgrades so the password remains stable. Retrieve it any
time with:

```bash
kubectl get secret mas-iam-sample-bootstrap-admin \
  -n iam -o jsonpath='{.data.password}' | base64 -d && echo
```

An init container now runs `kc.sh bootstrap-admin user` before Keycloak starts,
so the password from this secret is immediately valid for both CLI access and
the admin console (no forced change on first login).

To rotate:

```bash
# capture current password
OLD_PASS=$(kubectl get secret mas-iam-sample-bootstrap-admin -n iam \
  -o jsonpath='{.data.password}' | base64 -d)

kubectl delete secret mas-iam-sample-bootstrap-admin -n iam
kubectl rollout restart deployment/mas-iam-sample -n iam

# fetch the regenerated password
NEW_PASS=$(kubectl get secret mas-iam-sample-bootstrap-admin -n iam \
  -o jsonpath='{.data.password}' | base64 -d)

# update the existing admin account
kubectl exec -n iam deploy/mas-iam-sample -- bash -lc '
  export HOME=/tmp
  /opt/keycloak/bin/kcadm.sh config credentials \
    --server http://127.0.0.1:8080 \
    --realm master \
    --user admin \
    --password '"${OLD_PASS}"' &&
  /opt/keycloak/bin/kcadm.sh set-password \
    -r master \
    --username admin \
    --new-password '"${NEW_PASS}"'
'
```

If you prefer to manage credentials yourself, set
`keycloak.bootstrapAdmin.createSecret=false`, populate `secretName`, and create
the secret manually before applying the CR:

```bash
kubectl create secret generic mas-iam-keycloak-admin \
  --from-literal=username=<admin-user> \
  --from-literal=password="$(openssl rand -base64 24)" \
  -n iam
```

### PostgreSQL password reuse

The bundled Bitnami PostgreSQL chart stores generated passwords in its own
secret and looks them up on upgrades. No action is required when you let the
chart create the credentials (the default behaviour).

If you override `postgresql.auth.existingSecret` or set explicit password
values, ensure the CR mirrors the secret so upgrades continue to reconcile.
The following snippet copies the live secret values back into the CR:

```bash
PG_PASS=$(kubectl get secret mas-iam-sample-postgresql -n iam \
  -o jsonpath='{.data.password}' | base64 --decode)
PG_SUPER=$(kubectl get secret mas-iam-sample-postgresql -n iam \
  -o jsonpath='{.data.postgres-password}' | base64 --decode)

kubectl patch masiamstacks.iam.mas.ibm.com mas-iam-sample \
  -n iam --type merge \
  -p "{\"spec\":{\"postgresql\":{\"auth\":{\"password\":\"${PG_PASS}\",\"postgresPassword\":\"${PG_SUPER}\"}},\"global\":{\"postgresql\":{\"auth\":{\"password\":\"${PG_PASS}\",\"postgresPassword\":\"${PG_SUPER}\"}}}}}"
```

Without these values, reconciles will fail when the chart cannot locate the
existing credentials.

Remove the operator and CRDs:

```bash
make undeploy
make uninstall
```

Generate and build an OLM bundle (the Makefile auto-detects Docker vs Podman, but you can override via `CONTAINER_ENGINE` if needed):

```bash
VERSION=0.0.10 make bundle
CONTAINER_ENGINE=podman VERSION=0.0.10 BUNDLE_IMG=quay.io/<org>/mas-iam-operator-bundle:0.0.10 make bundle-build bundle-push
```

Use `make catalog-build catalog-push` when you are ready to publish the operator
to a catalog source.

```bash
CONTAINER_ENGINE=podman VERSION=0.0.10 \
  IMG=quay.io/<org>/mas-iam-operator:0.0.10 \
  BUNDLE_IMG=quay.io/<org>/mas-iam-operator-bundle:0.0.10 \
  CATALOG_IMG=quay.io/<org>/mas-iam-operator:catalog-0.0.10 \
  make docker-build docker-push bundle bundle-build bundle-push catalog-build catalog-push
```

### Installing via an operator catalog

After publishing the manager image, bundle, and catalog, you can install the
operator (and optionally a starter MAS IAM stack) with a single manifest.

1. Apply the consolidated manifest (replace `<org>/<repo>` with this repository
   path, and substitute `iam` in the manifest if you plan to use a different
   namespace):

   ```bash
   oc apply -f https://raw.githubusercontent.com/<org>/<repo>/main/manifests/install-olm.yaml
   ```

   The manifest installs the catalog source, operator group, subscription, an
   in-cluster TLS generation job (which prints the generated truststore password
   in its logs), and includes an example `MasIamStack` custom
   resource at the end. Download and edit the file locally first if you need to
   customise the release name, replace the cert material, or remove the sample CR.

   The Keycloak deployment now ships with an init container that reruns
   `kc.sh bootstrap-admin user` before startup, so the password stored in the
   bootstrap secret is immediately permanent and post-install automation (for
   example the LDAP configuration job) can log in without manual intervention.

2. Wait for the CSV in the target namespace to report `Succeeded`:

   ```bash
   oc get csv -n iam
   ```

3. If you trimmed the sample from the manifest, apply your own configuration (or
   start from the default sample) once the operator is ready:

   ```bash
   oc apply -f operators/mas-iam-operator/config/samples/iam_v1alpha1_masiamstack.yaml
   ```

Monitor `job/<release>-ldap-config` until it reports success—the job
retries until Keycloak’s admin API becomes reachable.

### Resetting a development namespace

Use `scripts/reset-namespace.sh` to tear down an environment and start from a
clean slate:

```bash
./scripts/reset-namespace.sh --namespace iam --release mas-iam-sample
```

Add `--force` to skip the confirmation prompt. The script deletes the
`MasIamStack` custom resource, related secrets (including the dev TLS
material), the LDAP configuration job, the PostgreSQL PVC, and the
namespace-scoped OLM objects (subscription/CSV). Reapply
`manifests/install-olm.yaml` to bring the stack back—the TLS bootstrap job will
recreate the secret automatically (and print the new truststore password) and
the bootstrap-admin init container will make the stored password permanent
again. Run `scripts/dev-generate-openldap-tls.sh` only if you want to rotate the
secret outside of that flow.

If you cannot use the helper script, run the equivalent `oc` commands manually
(adjust `RELEASE`/`NAMESPACE` if you customised them):

```bash
NAMESPACE=iam
RELEASE=mas-iam-sample

oc delete masiamstack "${RELEASE}" -n "${NAMESPACE}" --ignore-not-found
oc delete keycloakstack "${RELEASE}" -n "${NAMESPACE}" --ignore-not-found || true
oc get secret -n "${NAMESPACE}" --no-headers \
  | awk -v r="${RELEASE}-" '$1 ~ r { print $1 }' \
  | xargs -r -I {} oc delete secret {} -n "${NAMESPACE}"
oc delete secret "${RELEASE}-keycloak-openldap-tls" -n "${NAMESPACE}" --ignore-not-found
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
you need a completely fresh project, then reapply `manifests/install-olm.yaml`.

## Troubleshooting

### "Test authentication" fails in the Keycloak console

Keycloak never displays the stored bind credential for LDAP user storage
providers, so the password field on the admin page is blank. The **Test
authentication** button reuses the values currently shown in the form. If you
click it without pasting the password, Keycloak attempts to bind with an empty
credential and OpenLDAP responds with `LDAP: error code 49 - Invalid
Credentials`.

1. Retrieve the generated password from the secret created by the chart:
   ```bash
   oc get secret mas-iam-sample-openldap-admin \
     -n iam -o jsonpath='{.data.password}' | base64 --decode
   ```
2. Paste the password into the **Bind credential** field before clicking **Test
   authentication** or saving updates in the console.

To validate the connection non-interactively, run the helper script (it executes
`ldapwhoami` inside the OpenLDAP pod and prints the DN returned by the server):

```bash
scripts/test-openldap-bind.sh --namespace iam --release mas-iam-sample
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
redeploy (`oc rollout restart statefulset/mas-iam-sample-postgresql`, etc.) so
the pods pull the rebuilt images.

### LDAP auto-config job reruns every minute

When the Helm-based operator reconciles a release it performs `helm upgrade`
even if no values changed. Helm hooks run on every upgrade, so the
`mas-iam-sample-ldap-config` job will restart each reconcile unless you disable
post-upgrade hooks. Set `keycloak.ldap.autoConfigureOnUpgrade: false` in your
`MasIamStack` spec (or `values.yaml` when using plain Helm) to keep the job as a
*post-install* hook only. The job still runs once on the initial install, and
you can re-run it manually later by deleting the job or temporarily toggling the
flag when you need to push new LDAP settings.
