# MAS External Services Toolkit Install And Operations Guide

This is the detailed guide for the internal beta install path.

For the short path, use [BETA-QUICKSTART.md](BETA-QUICKSTART.md).

## What The Installer Does

`mas-est` installs a working MAS External Services Toolkit plus SCIM bridge lab on OpenShift. The beta path bootstraps a local command from a published container image, then runs the install locally against your current kubeconfig.

The install creates:

- MAS EST IAM operator through OLM
- Keycloak
- OpenLDAP
- PostgreSQL
- SCIM bridge
- demo LDAP users and groups
- one MAS SCIM profile, usually `demo`

The supported user entry point is the local `mas-est` command. The shell scripts in this repo remain the backend implementation and maintainer/debug path.

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
export MAS_EST_IMAGE='quay.io/lee_forster/mas-external-services-tool:v0.1.0-beta.7'
```

Bootstrap the local command:

```bash
mkdir -p "$HOME/mas-est"
podman run -ti --rm -v "$HOME/mas-est:/tmp" --pull always "$MAS_EST_IMAGE"
export PATH="$HOME/mas-est:$PATH"
```

Confirm it works:

```bash
mas-est version
mas-est --help
```

If the runtime already exists and you want to refresh it:

```bash
podman run -ti --rm -v "$HOME/mas-est:/tmp" --pull always "$MAS_EST_IMAGE" bootstrap --force
export PATH="$HOME/mas-est:$PATH"
```

The `bootstrap --force` part must come after the image name.

## Install

Run preflight:

```bash
mas-est preflight
```

Run the interactive install:

```bash
mas-est install
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

mas-est install --non-interactive
```

Equivalent install flags:

```bash
mas-est install \
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
mas-est status --namespace mas-est
mas-est logs --namespace mas-est --component bridge
```

Useful log shortcuts:

```bash
mas-est logs --namespace mas-est --component operator
mas-est logs --namespace mas-est --component keycloak
mas-est logs --namespace mas-est --component bridge
mas-est logs --namespace mas-est --component profile-bootstrap
```

A healthy install should show:

- operator CSV `Succeeded`
- `deployment/mas-iam-operator-controller-manager` ready
- `deployment/mas-est-iam` ready
- `deployment/mas-est-iam-openldap` ready
- `statefulset/mas-est-iam-postgresql` ready
- `deployment/scim-bridge` ready
- `job/scim-bridge-mas-profile-bootstrap` complete
- PostgreSQL PVC bound
- `pvc/scim-bridge-state` bound

Raw checks:

```bash
oc get pods -n mas-est -o wide
oc get deploy,statefulset,job,pvc,route -n mas-est
oc get csv -n mas-est
```

## Support Bundle

For beta bug reports or install/runtime triage, collect a local support bundle:

```bash
mas-est support-bundle --namespace mas-est
```

The command verifies `oc` access, offers the same interactive `oc login` handoff as the other CLI commands, then writes a timestamped directory such as:

```text
mas-est-support-mas-est-20260507-153000
```

The bundle includes the installed `mas-est` version, current OpenShift user/server, `mas-est status` output, namespace resource summaries, recent events, selected component logs, configmaps, storage classes, and redacted secret summaries.

Secret values are not written to the bundle. Secret summaries include names, types, and key names only, and collected text is scrubbed for known secret-derived token, password, and client secret values. Review hostnames, customer identifiers, and environment-specific resource names before sharing outside the team.

## LDAP Connection Details

The beta install includes a bundled OpenLDAP server. If you want to connect a MAS instance directly to that LDAP server, use:

```bash
mas-est ldap-info --namespace mas-est
```

By default the command hides passwords. To print the admin bind password:

```bash
mas-est ldap-info --namespace mas-est --show-password
```

To print the seeded demo user passwords:

```bash
mas-est ldap-info --namespace mas-est --show-user-passwords
```

The default cluster-internal values are:

| Setting | Value |
|---|---|
| URL | `ldaps://mas-est-iam-openldap.mas-est.svc.cluster.local:636` |
| Bind DN | `cn=admin,dc=demo,dc=local` |
| Base DN | `dc=demo,dc=local` |
| Users DN | `ou=users,dc=demo,dc=local` |
| Groups DN | `ou=groups,dc=demo,dc=local` |
| User attribute | `uid` |
| Group object class | `groupOfUniqueNames` |
| Group member attribute | `uniqueMember` |
| Admin password secret | `secret/mas-est-iam-openldap-admin`, key `password` |
| Demo user password secret | `secret/mas-est-iam-openldap-user-passwords` |
| TLS secret | `secret/mas-est-iam-keycloak-openldap-tls` |

The required MAS LDAP connection values are therefore available either from `mas-est ldap-info` or directly from the OpenShift secrets above. The admin bind credential is in `secret/mas-est-iam-openldap-admin`, key `password`.

This URL is meant for workloads inside the same OpenShift cluster. For a local command-line test, you can temporarily port-forward the service:

```bash
oc -n mas-est port-forward svc/mas-est-iam-openldap 1636:636
```

## Uninstall And Reinstall

To uninstall the OpenShift namespace and delete the MAS profile:

```bash
mas-est uninstall --namespace mas-est --profile-id demo
```

To keep the MAS profile and only remove the cluster-side lab resources:

```bash
mas-est uninstall --namespace mas-est --profile-id demo --skip-profile-delete
```

For non-interactive uninstall:

```bash
mas-est uninstall --namespace mas-est --profile-id demo --yes
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

To view the current bridge runtime configuration without exposing secret values:

```bash
mas-est config view --namespace mas-est
```

The command prints values from `configmap/scim-bridge-config` and prints only key names for `secret/scim-bridge-secret`.

To rotate the MAS API key:

```bash
mas-est config set mas-api-token \
  --namespace mas-est \
  --token-name '<new-mas-api-token-name>' \
  --token-value '<new-mas-api-token-value>'
```

The command updates only these keys in `secret/scim-bridge-secret`, preserves the other secret keys, restarts `deployment/scim-bridge`, and waits for rollout completion.

Why the restart is required:

- the bridge reads the secret through environment variables
- Kubernetes does not update environment variables inside an already running pod
- restarting `deployment/scim-bridge` makes the pod read the new secret values

If you also need to rerun the MAS profile bootstrap job with the new key:

```bash
oc delete job scim-bridge-mas-profile-bootstrap -n mas-est --ignore-not-found

# rerun the install path, or reapply the rendered bootstrap manifest from the same runtime/config
mas-est install
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
mas-est restart bridge --namespace mas-est
```

To enable bridge debug logging without reinstalling:

```bash
mas-est config set bridge --namespace mas-est --log-level debug
```

Payload logging is disabled by default. Only enable it for support debugging:

```bash
mas-est config set bridge --namespace mas-est --payload-logging true
```

Payload logs are redacted on a best-effort basis, but they can still contain customer-sensitive identity data such as usernames, names, and email addresses. Disable it after collecting evidence:

```bash
mas-est config set bridge --namespace mas-est --log-level info --payload-logging false
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
| `SCIM_BRIDGE_BRIDGE_LOG_LEVEL` | bridge logging level: `debug`, `info`, `warn`, or `error` | yes |
| `SCIM_BRIDGE_BRIDGE_PAYLOAD_LOGGING` | redacted outbound MAS SCIM payload logging for support debugging | yes |
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
oc patch configmap scim-bridge-config -n mas-est \
  --type merge \
  -p '{"data":{"SCIM_BRIDGE_BRIDGE_POLL_INTERVAL":"2m"}}'

oc rollout restart deployment/scim-bridge -n mas-est
```

To inspect non-secret values:

```bash
oc get configmap scim-bridge-config -n mas-est -o yaml
```

## Storage Values

The install prompts separately for:

- PostgreSQL storage class
- SCIM bridge storage class

They control different PVCs:

- `POSTGRES_STORAGE_CLASS` controls `data-mas-est-iam-postgresql-0`
- `SCIM_BRIDGE_STORAGE_CLASS` controls `scim-bridge-state`

If either PVC is `Pending`, check:

```bash
oc get sc
oc get pvc -n mas-est
oc describe pvc -n mas-est <pvc-name>
```

Then uninstall and reinstall with explicit storage-class choices if needed.

## Published Artifact Dependencies

The bootstrap image contains the CLI, scripts, and manifests, but install still depends on published runtime artifacts:

- operator catalog image referenced by `manifests/install-olm.yaml`
- operator image referenced by the published bundle/catalog
- SCIM bridge image referenced by the release env/manifests
- supporting images such as Keycloak, OpenLDAP, PostgreSQL, and curl

If repo code changes but the corresponding images were not rebuilt and pushed, the install will still use the older published behavior.

## Troubleshooting

### Bootstrap Says `/tmp/mas-est` Already Exists

Refresh the runtime with:

```bash
podman run -ti --rm -v "$HOME/mas-est:/tmp" --pull always "$MAS_EST_IMAGE" bootstrap --force
```

The command is `bootstrap --force` after the image name.

### `mas-est` Is Not Found

```bash
export PATH="$HOME/mas-est:$PATH"
which mas-est
```

### Cluster API DNS Fails

Example:

```text
Unable to connect to the server: dial tcp: lookup api.<cluster>: no such host
```

Fix cluster DNS/VPN/network access first. The installer cannot proceed until `oc get ns` works with the active kubeconfig.

### Operator CSV Stuck

```bash
mas-est status --namespace mas-est
oc get csv -n mas-est
oc describe csv -n mas-est <csv-name>
oc get catalogsource mas-iam-operator -n openshift-marketplace -o yaml
```

Look for catalog image pull failures, bundle errors, or an unhealthy operator deployment.

### PVCs Stay Pending

```bash
oc get pvc -n mas-est
oc describe pvc -n mas-est <pvc-name>
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
mas-est logs --namespace mas-est --component bridge --tail 300
```

Also check:

```bash
oc get configmap scim-bridge-config -n mas-est -o yaml
oc get secret scim-bridge-secret -n mas-est -o yaml
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
mas-est support-bundle --namespace mas-est
```

Review customer-sensitive hostnames and identifiers before sharing outside the team. The support bundle avoids raw secret values by default.

## Future Plans

Likely post-beta work:

- continued tagged beta/release images after `v0.1.0-beta.7`
- CLI-backed config editing and token rotation
- better bridge sync summaries and diagnostics
- safer reconciliation for existing MAS users
- group-based profile routing
- clearer upgrade and self-update paths

The beta should stay focused on proving the default install and SCIM demo flow before expanding scope.
