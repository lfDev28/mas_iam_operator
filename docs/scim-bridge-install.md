# SCIM Bridge Repo Guide

This guide covers repo-backed SCIM bridge development and maintainer operations.

The supported end-user path for full installs is now the bootstrapped local `mas-iam` CLI documented in [INSTALL-ALL-IN-ONE.md](INSTALL-ALL-IN-ONE.md). Use this document when:

- the MAS IAM operator and sample IAM stack already exist
- you want to iterate on bridge configuration or image selection from a local clone
- you need the script-backed deployment flow that this repo tests internally

## Support Boundary

Current repo maintainer paths are:

- full stack backend: `scripts/install-all-in-one.sh`
- bridge-focused repo flow: `scripts/scim-bridge-02-deploy.sh`

Published templates such as [`manifests/scim-bridge-install-template.yaml`](../manifests/scim-bridge-install-template.yaml) are release artifacts. They are useful when maintainers publish a tagged release, but they are not the authoritative repo-validation path.

## Prerequisites

- `oc` installed and logged into the target cluster
- an existing Keycloak/OpenLDAP/PostgreSQL stack in the target namespace
- a published bridge image available to the cluster
- a MAS API token name and value with SCIM access when `SCIM_BRIDGE_MAS_AUTH_TYPE=jwt`
- a suitable StorageClass for the bridge state PVC

## Required Values

`scripts/scim-bridge-02-deploy.sh` requires these values directly:

- `SCIM_BRIDGE_IMAGE`
- `SCIM_BRIDGE_KEYCLOAK_CLIENT_ID`
- `SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET`
- `SCIM_BRIDGE_MAS_BASE_URL`
- `SCIM_BRIDGE_MAS_PROFILE_ID`

In normal JWT mode you also need:

- `SCIM_BRIDGE_MAS_API_TOKEN_NAME`
- `SCIM_BRIDGE_MAS_API_TOKEN_VALUE`

Conditional requirements:

- if `SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ENABLED=true`, set `SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID` unless you provide `SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_JSON`
- if you disable Keycloak route automation or cannot derive the host automatically, set `SCIM_BRIDGE_KEYCLOAK_BASE_URL`

`SCIM_BRIDGE_MAS_BASE_URL` must be the MAS SCIM endpoint and must include `/scim/v2`.

## Recommended Repo Flow

Start from the example env file:

```bash
cp env/scim-bridge.env.example env/scim-bridge.env.local
```

Edit at least these fields in `env/scim-bridge.env.local`:

- `SCIM_BRIDGE_IMAGE`
- `SCIM_BRIDGE_NAMESPACE`
- `SCIM_BRIDGE_KEYCLOAK_BASE_URL` if you are not relying on route automation
- `SCIM_BRIDGE_KEYCLOAK_CLIENT_ID`
- `SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET`
- `SCIM_BRIDGE_MAS_BASE_URL`
- `SCIM_BRIDGE_MAS_PROFILE_ID`
- `SCIM_BRIDGE_MAS_API_TOKEN_NAME`
- `SCIM_BRIDGE_MAS_API_TOKEN_VALUE`

Optional but recommended when the cluster default is not what you want:

- `SCIM_BRIDGE_STORAGE_CLASS`

If you want the demo MAS profile bootstrap job, also set:

- `SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ENABLED=true`
- `SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ID=demo`
- `SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID=<workspace-id>`

Deploy from the repo:

```bash
./scripts/scim-bridge-02-deploy.sh
```

Verify:

```bash
oc get deployment,job,pvc -n iam | grep scim-bridge
oc logs deploy/scim-bridge -n iam --tail=200
MAS_SCIM_BASE="${SCIM_BRIDGE_MAS_BASE_URL}" \
MAS_PROFILE_ID="${SCIM_BRIDGE_MAS_PROFILE_ID}" \
API_TOKEN_NAME="${SCIM_BRIDGE_MAS_API_TOKEN_NAME}" \
API_TOKEN_VALUE="${SCIM_BRIDGE_MAS_API_TOKEN_VALUE}" \
./scripts/scim-bridge-03-verify.sh
```

The deploy script emits the same structured prefixes used elsewhere in the hardened script flow:

- `[config]`
- `[preflight]`
- `[install]`
- `[warn]`
- `[error]`
- `[result]`

## Key Behavior To Know

### MAS Route CA Auto-Detect

By default, `scripts/scim-bridge-02-deploy.sh` tries to:

- parse the host from `SCIM_BRIDGE_MAS_BASE_URL`
- find an OpenShift Route with that host
- read `spec.tls.caCertificate`
- populate the bridge CA bundle automatically

If multiple routes match the same host, set:

- `SCIM_BRIDGE_MAS_ROUTE_NAMESPACE`
- `SCIM_BRIDGE_MAS_ROUTE_NAME`

If no CA is present on the Route, set:

- `SCIM_BRIDGE_MAS_CA_BUNDLE`
- `SCIM_BRIDGE_MAS_CA_FILE=/etc/scim-bridge/certs/mas-ca.crt`

### Keycloak Bootstrap Modes

`SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_METHOD` supports:

- `job`
- `script`
- `none`

The all-in-one flow defaults to `script`. For standalone bridge iteration, use whichever mode matches your environment, but do not document `none` as the normal user path unless you are intentionally managing Keycloak yourself.

### Storage Behavior

The bridge state PVC now has a dedicated override:

- `SCIM_BRIDGE_STORAGE_CLASS`

That means:

- `POSTGRES_STORAGE_CLASS` does not affect the bridge
- `SCIM_BRIDGE_STORAGE_CLASS` controls only the `scim-bridge-state` PVC
- if `SCIM_BRIDGE_STORAGE_CLASS` is unset, the bridge PVC still inherits the cluster default
- if there is no suitable default and no explicit override, `scim-bridge-state` can remain `Pending`

For full-stack installs, PostgreSQL storage selection is handled separately by `scripts/install-olm-sample.sh` and the user-facing `iam install` flow.

### MAS Profile Mapping

Use:

- `SCIM_BRIDGE_MAS_PROFILE_ID` for the default/fallback profile
- `SCIM_BRIDGE_MAS_PROFILE_MAP` for `masProfile` label mapping such as `users=demo,management=mgmt1`

The bridge does not create arbitrary profile IDs on demand. Either pre-create the profiles in MAS or enable the bootstrap job for the specific profile you want to seed.

## Published Template Relationship

[`manifests/scim-bridge-install-template.yaml`](../manifests/scim-bridge-install-template.yaml) and [`manifests/scim-bridge-install.yaml`](../manifests/scim-bridge-install.yaml) exist so maintainers can publish release artifacts.

Use those only when all of these are true:

- you are consuming a tagged release rather than validating repo state
- the published bridge image matches that release
- any matching operator/catalog artifacts are already published

For local repo work and for the current tested flow, `scripts/scim-bridge-02-deploy.sh` remains the source of sequencing.

## Day-2 Operations

Rotate MAS API token values:

```bash
oc create secret generic scim-bridge-secret -n iam \
  --from-literal=SCIM_BRIDGE_MAS_API_TOKEN_NAME='<mas-api-token-name>' \
  --from-literal=SCIM_BRIDGE_MAS_API_TOKEN_VALUE='<mas-api-token-value>' \
  --dry-run=client -o yaml \
| oc apply -f -

oc rollout restart deployment/scim-bridge -n iam
```

If the Keycloak client secret changes, rerun the bootstrap path that matches your deployment mode:

```bash
oc delete job scim-bridge-keycloak-bootstrap -n iam --ignore-not-found
./scripts/scim-bridge-02-deploy.sh
```

## Troubleshooting

### MAS Base URL Malformed Or Missing `/scim/v2`

Expected:

```text
https://api.<mas-host>/scim/v2
```

What breaks:

- malformed URLs fail early in the deploy script
- missing `/scim/v2` sends the MAS auth/bootstrap logic to the wrong path

Fix the env value and rerun `./scripts/scim-bridge-02-deploy.sh`.

### MAS Route Auto-Detect Cannot Find A Unique Route

Symptoms:

- warning about multiple routes matching the MAS host
- warning that no Route matched the host
- TLS verification failures after deploy

Fix:

- set `SCIM_BRIDGE_MAS_ROUTE_NAMESPACE` and `SCIM_BRIDGE_MAS_ROUTE_NAME` explicitly, or
- provide `SCIM_BRIDGE_MAS_CA_BUNDLE` manually

### Bridge PVC Pending

Symptom:

- `pvc/scim-bridge-state` stays `Pending`

Cause:

- no suitable default StorageClass and no explicit bridge override

Fix:

- set `SCIM_BRIDGE_STORAGE_CLASS` to a suitable class and redeploy, or
- configure a suitable cluster default StorageClass

### Keycloak Bootstrap Job Fails

Checks:

```bash
oc get job -n iam scim-bridge-keycloak-bootstrap
oc logs job/scim-bridge-keycloak-bootstrap -n iam --tail=200
```

Common causes:

- wrong `SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET`
- wrong Keycloak namespace or release values
- bootstrap mode does not match the environment

### Image Pull Or Published Artifact Problems

Symptoms:

- the bridge Deployment cannot pull `SCIM_BRIDGE_IMAGE`
- the published template points at a tag that is not available yet

Fix:

- confirm the image tag exists and is reachable from the cluster
- for release-artifact installs, use a tag where the published template and published image set match

## Related Docs

- [All-in-one install guide](INSTALL-ALL-IN-ONE.md)
- [README](../README.md)
- [Repo practical path](../specs/repo-practical-path.md)
