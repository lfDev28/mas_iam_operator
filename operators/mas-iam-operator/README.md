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
CONTAINER_ENGINE=podman IMG=quay.io/<org>/mas-iam-operator:0.0.5 make docker-build docker-push
```

Deploy CRDs and the operator:

```bash
make install     # installs the MasIamStack CRD
IMG=quay.io/<org>/mas-iam-operator:0.0.5 make deploy
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
VERSION=0.0.5 make bundle
CONTAINER_ENGINE=podman VERSION=0.0.5 BUNDLE_IMG=quay.io/<org>/mas-iam-operator-bundle:0.0.5 make bundle-build bundle-push
```

Use `make catalog-build catalog-push` when you are ready to publish the operator
to a catalog source.

```bash
CONTAINER_ENGINE=podman VERSION=0.0.5 \
  IMG=quay.io/<org>/mas-iam-operator:0.0.5 \
  BUNDLE_IMG=quay.io/<org>/mas-iam-operator-bundle:0.0.5 \
  CATALOG_IMG=quay.io/<org>/mas-iam-operator:catalog-0.0.5 \
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
