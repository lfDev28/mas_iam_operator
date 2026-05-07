# MAS IAM Install And Operations Guide

This is the detailed guide for the internal beta install path.

For the short path, use [BETA-QUICKSTART.md](BETA-QUICKSTART.md).

## What The Installer Does

`mas-iam` installs a working MAS IAM plus SCIM bridge lab on OpenShift. The beta path bootstraps a local command from a published container image, then runs the install locally against your current kubeconfig.

The install creates:

- MAS IAM operator through OLM
- Keycloak
- OpenLDAP
- PostgreSQL
- SCIM bridge
- demo LDAP users and groups
- one MAS SCIM profile, usually `demo`

The supported user entry point is the local `mas-iam` command. The shell scripts in this repo remain the backend implementation and maintainer/debug path.

## Prerequisites

Local tools:

- `podman`
- `oc`
- `bash` 3.2+

Cluster and MAS access:

- working OpenShift kubeconfig
- cluster can pull the published operator/catalog and bridge images
- usable block/RBD storage class for PostgreSQL and the SCIM bridge PVC
- MAS API token name and value with SCIM access
- MAS workspace ID for the demo profile

If you are not logged in to OpenShift yet, the interactive CLI will offer to run `oc login` before preflight or install continues.

The beta has been tested on a small number of clusters, but it cannot cover every possible OpenShift configuration. If install fails because of cluster storage, registry, DNS, route, or certificate behavior, capture evidence and report it as a beta issue.

## Bootstrap

Set the image:

```bash
export MAS_IAM_IMAGE='quay.io/lee_forster/mas-iam-tool:v0.1.0-beta.5'
```

Bootstrap the local command:

```bash
mkdir -p "$HOME/mas-iam"
podman run -ti --rm -v "$HOME/mas-iam:/tmp" --pull always "$MAS_IAM_IMAGE"
export PATH="$HOME/mas-iam:$PATH"
```

Confirm it works:

```bash
mas-iam version
mas-iam --help
```

If the runtime already exists and you want to refresh it:

```bash
podman run -ti --rm -v "$HOME/mas-iam:/tmp" --pull always "$MAS_IAM_IMAGE" bootstrap --force
export PATH="$HOME/mas-iam:$PATH"
```

The `bootstrap --force` part must come after the image name.

## Install

Run preflight:

```bash
mas-iam preflight
```

Run the interactive install:

```bash
mas-iam install
```

The installer prompts for:

- MAS SCIM base URL
- MAS API token name
- MAS API token value
- MAS workspace ID
- MAS profile ID
- PostgreSQL storage class
- SCIM bridge storage class

The MAS SCIM base URL must include `/scim/v2`:

```text
https://api.<mas-host>/scim/v2
```

Use an RBD/block storage class for PostgreSQL and the SCIM bridge PVC when one is available. Avoid relying on a `cephfs` default for these PVCs if an RBD class exists.

## Non-Interactive Install

For automation, pass flags or export environment variables and use `--non-interactive`.

```bash
export SCIM_BRIDGE_MAS_BASE_URL='https://api.<mas-host>/scim/v2'
export SCIM_BRIDGE_MAS_API_TOKEN_NAME='<mas-api-token-name>'
export SCIM_BRIDGE_MAS_API_TOKEN_VALUE='<mas-api-token-value>'
export SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID='<workspace-id>'
export SCIM_BRIDGE_MAS_PROFILE_ID='demo'
export POSTGRES_STORAGE_CLASS='<rbd-storage-class>'
export SCIM_BRIDGE_STORAGE_CLASS='<rbd-storage-class>'

mas-iam install --non-interactive
```

Equivalent install flags:

```bash
mas-iam install \
  --non-interactive \
  --mas-base-url 'https://api.<mas-host>/scim/v2' \
  --mas-api-token-name '<mas-api-token-name>' \
  --mas-api-token-value '<mas-api-token-value>' \
  --workspace-id '<workspace-id>' \
  --profile-id demo \
  --storage-class '<rbd-storage-class>' \
  --scim-bridge-storage-class '<rbd-storage-class>'
```

## Check Health

Use the CLI first:

```bash
mas-iam status --namespace iam
mas-iam logs --namespace iam --component bridge
```

Useful log shortcuts:

```bash
mas-iam logs --namespace iam --component operator
mas-iam logs --namespace iam --component keycloak
mas-iam logs --namespace iam --component bridge
mas-iam logs --namespace iam --component profile-bootstrap
```

A healthy install should show:

- operator CSV `Succeeded`
- `deployment/mas-iam-operator-controller-manager` ready
- `deployment/mas-iam-sample` ready
- `deployment/mas-iam-sample-openldap` ready
- `statefulset/mas-iam-sample-postgresql` ready
- `deployment/scim-bridge` ready
- `job/scim-bridge-mas-profile-bootstrap` complete
- PostgreSQL PVC bound
- `pvc/scim-bridge-state` bound

Raw checks:

```bash
oc get pods -n iam -o wide
oc get deploy,statefulset,job,pvc,route -n iam
oc get csv -n iam
```

## Support Bundle

For beta bug reports or install/runtime triage, collect a local support bundle:

```bash
mas-iam support-bundle --namespace iam
```

The command verifies `oc` access, offers the same interactive `oc login` handoff as the other CLI commands, then writes a timestamped directory such as:

```text
mas-iam-support-iam-20260507-153000
```

The bundle includes the installed `mas-iam` version, current OpenShift user/server, `mas-iam status` output, namespace resource summaries, recent events, selected component logs, configmaps, storage classes, and redacted secret summaries.

Secret values are not written to the bundle. Secret summaries include names, types, and key names only, and collected text is scrubbed for known secret-derived token, password, and client secret values. Review hostnames, customer identifiers, and environment-specific resource names before sharing outside the team.

## LDAP Connection Details

The beta install includes a bundled OpenLDAP server. If you want to connect a MAS instance directly to that LDAP server, use:

```bash
mas-iam ldap-info --namespace iam
```

By default the command hides passwords. To print the admin bind password:

```bash
mas-iam ldap-info --namespace iam --show-password
```

To print the seeded demo user passwords:

```bash
mas-iam ldap-info --namespace iam --show-user-passwords
```

The default cluster-internal values are:

| Setting | Value |
|---|---|
| URL | `ldaps://mas-iam-sample-openldap.iam.svc.cluster.local:636` |
| Bind DN | `cn=admin,dc=demo,dc=local` |
| Base DN | `dc=demo,dc=local` |
| Users DN | `ou=users,dc=demo,dc=local` |
| Groups DN | `ou=groups,dc=demo,dc=local` |
| User attribute | `uid` |
| Group object class | `groupOfUniqueNames` |
| Group member attribute | `uniqueMember` |
| Admin password secret | `secret/mas-iam-sample-openldap-admin`, key `password` |
| Demo user password secret | `secret/mas-iam-sample-openldap-user-passwords` |
| TLS secret | `secret/mas-iam-sample-keycloak-openldap-tls` |

The required MAS LDAP connection values are therefore available either from `mas-iam ldap-info` or directly from the OpenShift secrets above. The admin bind credential is in `secret/mas-iam-sample-openldap-admin`, key `password`.

This URL is meant for workloads inside the same OpenShift cluster. For a local command-line test, you can temporarily port-forward the service:

```bash
oc -n iam port-forward svc/mas-iam-sample-openldap 1636:636
```

## Wipe And Reinstall

To wipe the OpenShift namespace and delete the MAS profile:

```bash
mas-iam wipe --namespace iam --profile-id demo
```

To keep the MAS profile and only remove the cluster-side lab resources:

```bash
mas-iam wipe --namespace iam --profile-id demo --skip-profile-delete
```

For non-interactive wipe:

```bash
mas-iam wipe --namespace iam --profile-id demo --yes
```

If profile deletion needs MAS credentials, provide the MAS URL and token values with flags or environment variables.

## Updating The MAS API Key

The SCIM bridge stores MAS API token values in:

```text
secret/scim-bridge-secret
```

The relevant keys are:

- `SCIM_BRIDGE_MAS_API_TOKEN_NAME`
- `SCIM_BRIDGE_MAS_API_TOKEN_VALUE`
- `SCIM_BRIDGE_MAS_TOKEN`, only if using a direct token instead of API token name/value

To rotate the MAS API key:

```bash
keycloak_client_id="$(oc get secret scim-bridge-secret -n iam -o jsonpath='{.data.SCIM_BRIDGE_KEYCLOAK_CLIENT_ID}' | base64 -d)"
keycloak_client_secret="$(oc get secret scim-bridge-secret -n iam -o jsonpath='{.data.SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET}' | base64 -d)"

oc create secret generic scim-bridge-secret -n iam \
  --from-literal=SCIM_BRIDGE_KEYCLOAK_CLIENT_ID="${keycloak_client_id}" \
  --from-literal=SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET="${keycloak_client_secret}" \
  --from-literal=SCIM_BRIDGE_MAS_TOKEN='' \
  --from-literal=SCIM_BRIDGE_MAS_API_TOKEN_NAME='<new-mas-api-token-name>' \
  --from-literal=SCIM_BRIDGE_MAS_API_TOKEN_VALUE='<new-mas-api-token-value>' \
  --dry-run=client -o yaml \
| oc apply -f -

oc rollout restart deployment/scim-bridge -n iam
oc rollout status deployment/scim-bridge -n iam --timeout=5m
```

Why the restart is required:

- the bridge reads the secret through environment variables
- Kubernetes does not update environment variables inside an already running pod
- restarting `deployment/scim-bridge` makes the pod read the new secret values

If you also need to rerun the MAS profile bootstrap job with the new key:

```bash
oc delete job scim-bridge-mas-profile-bootstrap -n iam --ignore-not-found

# rerun the install path, or reapply the rendered bootstrap manifest from the same runtime/config
mas-iam install
```

For most token rotations, restarting `deployment/scim-bridge` is enough. Keycloak, OpenLDAP, and PostgreSQL do not need to restart for MAS API token changes.

## Editable Runtime Values

Most bridge runtime settings live in:

```text
configmap/scim-bridge-config
secret/scim-bridge-secret
```

After editing either object, restart the bridge:

```bash
oc rollout restart deployment/scim-bridge -n iam
```

Common editable values in `scim-bridge-config`:

| Key | Purpose | Restart required |
|---|---|---|
| `SCIM_BRIDGE_MAS_BASE_URL` | MAS SCIM endpoint, must include `/scim/v2` | yes |
| `SCIM_BRIDGE_MAS_PROFILE_ID` | default MAS SCIM profile ID | yes |
| `SCIM_BRIDGE_MAS_PROFILE_MAP` | optional mapping from Keycloak `masProfile` labels to MAS profile IDs | yes |
| `SCIM_BRIDGE_MAS_PROFILE_REQUIRE_LABEL` | require a `masProfile` label before provisioning | yes |
| `SCIM_BRIDGE_BRIDGE_POLL_INTERVAL` | bridge poll interval, for example `5m` | yes |
| `SCIM_BRIDGE_BRIDGE_ALLOW_UPDATES` | allow updates to existing users | yes |
| `SCIM_BRIDGE_BRIDGE_DRY_RUN` | plan without writing changes | yes |
| `SCIM_BRIDGE_INCLUDE_USERNAMES` | optional comma-separated allow list | yes |
| `SCIM_BRIDGE_INCLUDE_USERNAME_PREFIX` | optional username prefix filter | yes |

Common editable values in `scim-bridge-secret`:

| Key | Purpose | Restart required |
|---|---|---|
| `SCIM_BRIDGE_MAS_API_TOKEN_NAME` | MAS API token name | yes |
| `SCIM_BRIDGE_MAS_API_TOKEN_VALUE` | MAS API token value | yes |
| `SCIM_BRIDGE_MAS_TOKEN` | direct MAS token, when used | yes |
| `SCIM_BRIDGE_KEYCLOAK_CLIENT_ID` | Keycloak client ID used by the bridge | yes |
| `SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET` | Keycloak client secret used by the bridge | yes |

Edit with care. Some values are generated by install and may be overwritten if you rerun install.

To patch a non-secret value:

```bash
oc patch configmap scim-bridge-config -n iam \
  --type merge \
  -p '{"data":{"SCIM_BRIDGE_BRIDGE_POLL_INTERVAL":"2m"}}'

oc rollout restart deployment/scim-bridge -n iam
```

To inspect non-secret values:

```bash
oc get configmap scim-bridge-config -n iam -o yaml
```

## Storage Values

The install prompts separately for:

- PostgreSQL storage class
- SCIM bridge storage class

They control different PVCs:

- `POSTGRES_STORAGE_CLASS` controls `data-mas-iam-sample-postgresql-0`
- `SCIM_BRIDGE_STORAGE_CLASS` controls `scim-bridge-state`

If either PVC is `Pending`, check:

```bash
oc get sc
oc get pvc -n iam
oc describe pvc -n iam <pvc-name>
```

Then wipe and reinstall with explicit storage-class choices if needed.

## Published Artifact Dependencies

The bootstrap image contains the CLI, scripts, and manifests, but install still depends on published runtime artifacts:

- operator catalog image referenced by `manifests/install-olm.yaml`
- operator image referenced by the published bundle/catalog
- SCIM bridge image referenced by the release env/manifests
- supporting images such as Keycloak, OpenLDAP, PostgreSQL, and curl

If repo code changes but the corresponding images were not rebuilt and pushed, the install will still use the older published behavior.

## Troubleshooting

### Bootstrap Says `/tmp/mas-iam` Already Exists

Refresh the runtime with:

```bash
podman run -ti --rm -v "$HOME/mas-iam:/tmp" --pull always "$MAS_IAM_IMAGE" bootstrap --force
```

The command is `bootstrap --force` after the image name.

### `mas-iam` Is Not Found

```bash
export PATH="$HOME/mas-iam:$PATH"
which mas-iam
```

### Cluster API DNS Fails

Example:

```text
Unable to connect to the server: dial tcp: lookup api.<cluster>: no such host
```

Fix cluster DNS/VPN/network access first. The installer cannot proceed until `oc get ns` works with the active kubeconfig.

### Operator CSV Stuck

```bash
mas-iam status --namespace iam
oc get csv -n iam
oc describe csv -n iam <csv-name>
oc get catalogsource mas-iam-operator -n openshift-marketplace -o yaml
```

Look for catalog image pull failures, bundle errors, or an unhealthy operator deployment.

### PVCs Stay Pending

```bash
oc get pvc -n iam
oc describe pvc -n iam <pvc-name>
oc get sc
```

Choose explicit block/RBD storage classes and reinstall if the default class is unsuitable.

### MAS URL Is Wrong

The expected form is:

```text
https://api.<mas-host>/scim/v2
```

Do not use only the MAS API root. Do not duplicate the scheme, such as `https://https://...`.

### Bridge Is Running But Users Do Not Appear In MAS

Check bridge logs:

```bash
mas-iam logs --namespace iam --component bridge --tail 300
```

Also check:

```bash
oc get configmap scim-bridge-config -n iam -o yaml
oc get secret scim-bridge-secret -n iam -o yaml
```

Do not paste unredacted secret output into bug reports.

Common causes:

- wrong MAS API token
- token lacks SCIM access
- wrong MAS SCIM URL
- wrong workspace/profile ID
- stale MAS-side users from an earlier run

## What To Include In Beta Bug Reports

Capture:

```bash
mas-iam support-bundle --namespace iam
```

Review customer-sensitive hostnames and identifiers before sharing outside the team. The support bundle avoids raw secret values by default.

## Future Plans

Likely post-beta work:

- continued tagged beta/release images after `v0.1.0-beta.5`
- CLI-backed config editing and token rotation
- better bridge sync summaries and diagnostics
- safer reconciliation for existing MAS users
- group-based profile routing
- clearer upgrade and self-update paths

The beta should stay focused on proving the default install and SCIM demo flow before expanding scope.
