# Keycloak Stack Operator (Helm-based)

This directory contains a Helm-based operator scaffold generated with
`operator-sdk` to manage the Keycloak stack chart.

## Prerequisites

- Docker (or another OCI image build tool)
- `kubectl`
- `kustomize` v5.0+
- `operator-sdk` v1.33.0 (the repo already includes `bin/operator-sdk`; add `bin/`
  to your `PATH` or invoke the binary explicitly)
- Access to a container registry where you can push operator and bundle images

Ensure the Keycloak/OpenLDAP TLS secret exists in the target namespace before
deploying the operator-managed CR (`<release>-keycloak-openldap-tls` by default).
The helper script at the repository root can generate dev material:

```bash
./scripts/dev-generate-openldap-tls.sh -n iam -r mas-iam
```

## Layout

- `helm-charts/keycloak-stack`: snapshot of the Helm chart consumed by the
  operator. Re-run `operator-sdk create api` if you need to refresh it from the
  root chart.
- `config/`: kustomize overlays for CRDs, RBAC, manager deployment, samples, and
  bundle manifests.
- `config/samples/iam_v1alpha1_keycloakstack.yaml`: base specification for a
  `KeycloakStack` custom resource. Update the fields (e.g. LDAP `caSecret`,
  `bindCredentialSecret`) to match your environment before applying it.

## Common tasks

Build and push the operator manager image (update `IMG` to match your registry;
set `CONTAINER_ENGINE=podman` if you use Podman):

```bash
cd operators/keycloak-stack-operator
CONTAINER_ENGINE=podman IMG=quay.io/<org>/keycloak-stack-operator:0.0.1 make docker-build docker-push
```

Deploy CRDs and the operator:

```bash
make install     # installs the KeycloakStack CRD
IMG=quay.io/<org>/keycloak-stack-operator:0.0.1 make deploy
```

Apply a sample CR (customise secrets and routing first):

```bash
kubectl apply -f config/samples/iam_v1alpha1_keycloakstack.yaml
```

Remove the operator and CRDs:

```bash
make undeploy
make uninstall
```

Generate and build an OLM bundle:

```bash
VERSION=0.0.1 make bundle
CONTAINER_ENGINE=podman VERSION=0.0.1 BUNDLE_IMG=quay.io/<org>/keycloak-stack-operator-bundle:0.0.1 make bundle-build bundle-push
```

Use `make catalog-build catalog-push` when you are ready to publish the operator
to a catalog source.
