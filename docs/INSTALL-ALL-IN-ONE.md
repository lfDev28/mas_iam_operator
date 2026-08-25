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
export MAS_EST_IMAGE='quay.io/lee_forster/mas-external-services-tool:v0.1.0'
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

`mas-est install` runs the install **inside the cluster** as a Kubernetes Job (see [In-Cluster Install](#in-cluster-install-default)). Prompts and preflight still run on this machine; only the execution moves. Pass `--local` to run the whole thing on this machine instead — that path dies with the terminal, the VPN, or a sleeping laptop, and is intended for development and debugging.

The installer prompts for:

- namespace
- products to install: LDAP, Keycloak, SCIM bridge, S3 object storage, and/or SMTP capture
- MAS SCIM base URL, API token, workspace ID, and profile ID when SCIM is selected
- MAS instance ID when S3 object storage is selected (the MAS core namespace defaults to `mas-<instance-id>-core`)
- whether to configure MAS auth providers, and which providers to create: LDAP, OIDC (MAS 9.1+), and/or SAML
- primary storage class for Keycloak PostgreSQL and/or MinIO
- SCIM bridge storage class when SCIM is selected
- uninstall first

Selecting SCIM automatically includes Keycloak and LDAP. Selecting Keycloak automatically includes LDAP. LDAP-only installs are supported through the same operator profile without deploying Keycloak or PostgreSQL.

Values that can be derived from the MAS instance ID (`mas-<id>-core` for the core namespace, `auth.<mas-domain>` for the auth host, the workspace ID when only one matches) are auto-filled and shown as `[derived] …` lines rather than re-prompted. Pass `--mas-core-namespace`, `--mas-auth-instance-id`, `--mas-auth-core-namespace`, `--mas-auth-host`, or `--workspace-id` to override any of them.

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
export MAS_EST_COMPONENTS='ldap,keycloak,scim'
export POSTGRES_STORAGE_CLASS='<rbd-storage-class>'
export SCIM_BRIDGE_STORAGE_CLASS='<rbd-storage-class>'

mas-est install --non-interactive
```

Equivalent install flags:

```bash
mas-est install \
  --non-interactive \
  --components ldap,keycloak,scim \
  --mas-base-url 'https://api.<mas-host>/scim/v2' \
  --mas-api-token-name '<mas-api-token-name>' \
  --mas-api-token-value '<mas-api-token-value>' \
  --workspace-id '<workspace-id>' \
  --profile-id demo \
  --storage-class '<rbd-storage-class>' \
  --scim-bridge-storage-class '<rbd-storage-class>'
```

Other component examples:

```bash
mas-est install --components ldap --non-interactive
mas-est install --components ldap,keycloak --non-interactive
mas-est install --components s3 --mas-instance-id '<instance-id>' --non-interactive
mas-est install --components smtp --non-interactive
mas-est install --components ldap,keycloak,scim,s3,smtp --mas-instance-id '<instance-id>' --non-interactive
```

To also create MAS-side LDAP, OIDC, and SAML authentication providers backed by the installed OpenLDAP and Keycloak services:

```bash
mas-est install \
  --components ldap,keycloak,scim \
  --configure-mas-auth \
  --mas-auth-providers ldap,oidc,saml \
  --mas-auth-instance-id '<instance-id>' \
  --mas-auth-host 'auth.<mas-domain>' \
  --non-interactive
```

The generated MAS provider IDs are:

```text
mas-est-ldap
mas-est-oidc
mas-est-saml
```

You can also run the MAS auth configuration after the services are installed:

```bash
mas-est mas-auth apply \
  --namespace mas-est \
  --providers ldap,oidc,saml \
  --mas-instance-id '<instance-id>' \
  --mas-auth-host 'auth.<mas-domain>'
```

For S3/MinIO lab testing, set these as Manage System Properties (System Configuration → Platform Configuration → System Properties) after install:

```text
mxe.cosendpointuri  http://mas-est.svc.cluster.local:9000
mxe.cosbucketname   mas-s3-demo
mxe.cosregion       us-east-1                        ← REQUIRED
mxe.cosaccesskey    minioadmin (mas-minio-root MINIO_ROOT_USER)
mxe.cossecretkey    <value from mas-minio-root MINIO_ROOT_PASSWORD>
```

**`mxe.cosregion` is required, not optional.** Without it doclinks attach fails with `SignatureDoesNotMatch` even though the AWS SDK's internal default is `us-east-1`. On MAS 9.1.4 the property is not a built-in `MAXPROP` entry — add it via the Properties UI ("New Row"), then Live Refresh. See `OBJECT-STORAGE-POC.md` for the full doclinks setup including doctype `DEFAULTPATH` changes.

The installer creates both Manage bucket layouts: sibling buckets (`mas-s3-demo`, `mas-s3-demorecovery`, `mas-s3-demobackup`) and root prefixes (`recovery/`, `backup/`). The external HTTPS MinIO route is mainly for browser/manual testing and may require additional certificate trust before Manage can use it.

For SMTP lab testing, the installer deploys Mailpit as a capture-only SMTP server. Use these MAS SMTP values after install:

```text
Display name: MAS EST SMTP Capture
SMTP host: mas-mailpit.mas-est.svc.cluster.local
SMTP port: 1025
TLS/security: disabled
Authentication: none
```

Open the Mailpit route printed by the installer to inspect captured messages. By default Mailpit stores captured messages in the browser UI only and does NOT relay them externally — perfect for "did MAS attempt to send" testing.

To also have Mailpit deliver captured messages to real recipients (Gmail, SendGrid, SES, etc), pass the relay flags at install time:

```bash
mas-est install \
  --components ldap,keycloak,scim,smtp \
  --smtp-relay-host smtp.gmail.com \
  --smtp-relay-port 587 \
  --smtp-relay-username 'lab@example.com' \
  --smtp-relay-password "$GMAIL_APP_PASSWORD" \
  --smtp-relay-from 'lab@example.com' \
  --smtp-relay-starttls
```

MAS's own SMTP config doesn't change — it still talks plain SMTP to `mas-mailpit.mas-est.svc.cluster.local:1025`. Captured messages appear in the Mailpit UI as before AND get forwarded upstream. See [docs/CONNECTION-DETAILS.md](CONNECTION-DETAILS.md#smtp-relay-optional) for the full flag set + provider walkthroughs (Gmail, SendGrid, AWS SES, Outlook/365).

## In-Cluster Install (default)

Driving the cluster from a laptop for 15–30 minutes makes the laptop a single point of failure: sleeping the lid, dropping VPN, or closing the terminal kills the run mid-phase.

So the install runs **in the cluster by default**. The CLI still prompts you locally and still runs preflight locally, then hands the resolved settings to a Kubernetes Job that does the actual work:

```bash
mas-est install               # in-cluster (default)
mas-est install --in-cluster  # same thing, explicit
mas-est install --local       # opt out: run on this machine (development/debugging)
```

`--local` always wins over `--in-cluster`. The Job's own inner `est install` is issued with `--local`, which is what stops it launching a further Job.

It works with `--non-interactive` and every other install flag too:

```bash
mas-est install --non-interactive \
  --components ldap,keycloak,scim \
  --mas-base-url 'https://api.<mas-host>/scim/v2' \
  --mas-api-token-name '<mas-api-token-name>' \
  --mas-api-token-value '<mas-api-token-value>' \
  --workspace-id '<workspace-id>'
```

### What it creates

| Resource | Notes |
|---|---|
| `Namespace/mas-est` | Target namespace, same as the local path |
| `ServiceAccount/mas-est-installer` | Job identity |
| `ClusterRole/mas-est-installer` + `ClusterRoleBinding/mas-est-installer-<namespace>` | Cross-namespace permissions, see below |
| `Role/mas-est-installer` + `RoleBinding/mas-est-installer` | Full CRUD inside the target namespace |
| `Secret/mas-est-install-credentials` | MAS API token name/value and, if set, the SMTP relay password |
| `Job/mas-est-install` | Runs `est install --non-interactive …` |

Every non-secret setting is passed to the Job as explicit CLI flags, so `oc get job mas-est-install -n mas-est -o yaml` is a complete record of what was requested. The MAS API token and the SMTP relay password are the exceptions — they reach the container through `secretKeyRef` env vars so they never appear in the Job spec.

The Job runs `quay.io/lee_forster/mas-external-services-tool:v<cli-version>`, matching the CLI that launched it. Override with `--installer-image` when mirroring into a private registry. Override the Job name with `--job-name` when two engineers share a cluster.

The Job uses `restartPolicy: Never` and `backoffLimit: 0` — the install is idempotent, but a blind restart silently repeats 20+ minutes of work, so re-running is a human decision. `activeDeadlineSeconds` is 5400 (90 minutes).

### Ctrl-C is safe

After creating the Job, the CLI streams its logs. **Ctrl-C detaches from the log stream; it does not cancel the install.** So does closing the laptop, dropping VPN, or killing the terminal.

Reattach at any time — while it runs, or after it finishes:

```bash
mas-est logs --namespace mas-est --component install-job --follow
```

The Job has no `ttlSecondsAfterFinished`, so a finished Job and its logs stay around as the post-mortem artifact.

To actually cancel a running install:

```bash
oc delete job mas-est-install -n mas-est
```

### Cleaning up

`mas-est uninstall` does not remove the installer Job or its RBAC. Remove them by hand when you are done:

```bash
oc delete job mas-est-install -n mas-est --ignore-not-found
oc delete secret mas-est-install-credentials -n mas-est --ignore-not-found
oc delete rolebinding mas-est-installer -n mas-est --ignore-not-found
oc delete role mas-est-installer -n mas-est --ignore-not-found
oc delete serviceaccount mas-est-installer -n mas-est --ignore-not-found
oc delete clusterrolebinding mas-est-installer-mas-est --ignore-not-found
oc delete clusterrole mas-est-installer --ignore-not-found
```

Rerunning `mas-est install` when a Job of the same name already exists prompts to delete and recreate it in interactive mode, and fails with instructions in `--non-interactive` mode. It never silently replaces one — it may be somebody else's install.

### RBAC note: the ClusterRole is broad

The install genuinely touches a lot of the cluster, and the MAS core and MAS Mongo namespaces are not known until your settings are resolved, so they cannot be named in a namespaced Role. The shipped `ClusterRole/mas-est-installer` therefore grants, cluster-wide:

- `namespaces` get/list/create; `nodes` get/list; `customresourcedefinitions` get/create; `storageclasses` get/list
- `securitycontextconstraints` **use** on `anyuid` (needed by the OpenLDAP TLS generator job), plus `bind` on the `admin` and `system:openshift:scc:anyuid` ClusterRoles so `manifests/install-olm.yaml` can be applied
- `clusterroles`/`clusterrolebindings` get/list/create/update/patch
- OLM `catalogsources`, `operatorgroups`, `subscriptions`, `clusterserviceversions`, `installplans` full CRUD, and `packagemanifests` get/list
- `idpcfgs`/`objectstoragecfgs` get/list/create/update/patch, `suites` get/list/patch, `manageworkspaces` get/list
- `secrets` **get/list/create**, `configmaps` get/list/create/update/patch/delete, `routes` get/list
- `pods` get/list, `pods/log` get, and **`pods/exec` create**

`pods/exec` is required — `scripts/link-scim-users-oidc.sh` runs `oc exec … mongosh` inside the MAS Mongo pod to link SCIM users to their OIDC identities. `pods/exec` plus cross-namespace secret reads is close to cluster-admin in practice.

Destructive verbs are deliberately kept out of the ClusterRole: deleting and rewriting workloads (`secrets`, `configmaps`, `deployments`, `statefulsets`, `jobs`, `services`, `routes`, `persistentvolumeclaims`, `serviceaccounts`, `pods`, `masiamstacks`) is granted only by the namespaced `Role/mas-est-installer` inside the target namespace.

If your cluster's security posture rules this out, apply your own ServiceAccount and role bindings under the same names before installing, or use `--local`.

### Limitations

- `--uninstall-first` cannot run in the cluster: the Job lives in the namespace the uninstall would delete, so it would remove itself mid-run. Run `mas-est uninstall` first and then `mas-est install`, or pass `--local` to do both from this machine.
- The Job writes no run-log file — `/opt/mas-est` is not writable in the container. Stdout is the log, captured by `oc logs`.

## Connection Details

The installer writes common connection values to:

```text
secret/mas-est-connection-details
```

Use the CLI to print those values without exposing secret material:

```bash
mas-est details --namespace mas-est --component all
mas-est details --namespace mas-est --component s3
mas-est details --namespace mas-est --component smtp
mas-est details --namespace mas-est --component oidc
```

Secret values are redacted by default. Use `--show-secrets` only for local troubleshooting.

The details secret stores references to the real credential secrets rather than duplicating all passwords. For example, S3 points to the MAS credential secret and key names, SMTP lists the internal service host and port, and LDAP points to the OpenLDAP admin password secret.

For raw connection values (mount-as-secret use, scripted retrieval, third-party app wiring), the installer also writes one dedicated Secret (or ConfigMap for SMTP) per provider:

```bash
oc get secret    mas-est-ldap-connection  -n mas-est -o yaml
oc get secret    mas-est-oidc-connection  -n mas-est -o yaml
oc get secret    mas-est-saml-connection  -n mas-est -o yaml
oc get secret    mas-est-s3-connection    -n mas-est -o yaml
oc get configmap mas-est-smtp-connection  -n mas-est -o yaml
```

The full key list for each resource is in [CONNECTION-DETAILS.md](CONNECTION-DETAILS.md).

## MAS Auth Auto-Configuration

When `--configure-mas-auth` is selected, MAS-EST can configure:

- one MAS LDAP `IDPCfg` pointing to `ldaps://mas-est-iam-openldap.mas-est.svc.cluster.local:636`
- one MAS OIDC `IDPCfg` pointing to the Keycloak `maximo` realm
- one MAS SAML `IDPCfg` using Keycloak SAML IdP metadata

Use `--mas-auth-providers ldap,oidc,saml` on `install`, or `--providers ldap,oidc,saml` on `mas-auth apply`, to choose which providers to create. In the interactive installer this is shown as a checklist after you opt into MAS auth provisioning. OIDC requires MAS 9.1 or later with `spec.oidc` support in the `IDPCfg` CRD.

The command also creates or updates the required Keycloak OIDC and SAML clients when those providers are selected. The OIDC redirect URI uses:

```text
https://<mas-auth-host>/oidcclient/redirect/mas-est-oidc
```

The MAS resources are created in the MAS core namespace, usually:

```text
mas-<instance-id>-core
```

### Preflight warns about IDPCfgs it would overwrite

MAS-EST names its IDPCfgs `<instance-id>-<type>-<provider-id>-system`, and `install` always uses the provider id `default`. If MAS already has an IDPCfg under one of those names — for example a hand-made `<instance-id>-saml-default-system` — the install rewrites that object's spec **in place**: the object keeps its original `creationTimestamp` but its whole spec becomes MAS-EST's.

Preflight now checks for this and prints a `[warn] mas-idpcfg-overwrite: …` line naming each colliding IDPCfg and its current `spec.displayName`. It is only a warning; it never blocks the install. Back up anything you want to keep first:

```bash
oc get idpcfg -n mas-<instance-id>-core -o yaml > idpcfgs-backup.yaml
```

`install` has no flag to change the provider id. To keep an existing config, install without that provider (`--mas-auth-providers` limited to the others, or no `--configure-mas-auth` at all) and then run `mas-est mas-auth apply --saml-provider-id <other-id>` (likewise `--ldap-provider-id` / `--oidc-provider-id`).

This path uses the same `IDPCfg` resources created by the MAS Admin API underneath. If a cluster's MAS version rejects one provider shape, rerun with `mas-est support-bundle --namespace mas-est` and collect the MAS core `IDPCfg` status/events for review.

OIDC auto-configuration requires a MAS version whose `idpcfgs.config.mas.ibm.com` CRD exposes `spec.oidc`. Older MAS versions may support LDAP and SAML only; in that case `mas-est mas-auth apply` stops before changing Keycloak or MAS auth resources.

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
mas-est logs --namespace mas-est --component minio
mas-est logs --namespace mas-est --component minio-init
mas-est logs --namespace mas-est --component smtp
```

A healthy install should show:

- operator CSV `Succeeded`
- `deployment/mas-iam-operator-controller-manager` ready
- `deployment/mas-est-iam` ready
- `deployment/mas-est-iam-openldap` ready
- `statefulset/mas-est-iam-postgresql` ready
- `deployment/scim-bridge` ready
- `job/scim-bridge-mas-profile-bootstrap` complete
- optional `deployment/mas-minio` ready when S3 is selected
- optional `deployment/mas-mailpit` ready when SMTP is selected
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
| `SCIM_BRIDGE_INCLUDE_GROUPS` | optional Keycloak group names/paths to source users from (installer default `mas-scim-users`) | yes |

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

Likely post-`v0.1.0` work:

- group-based profile routing (planned for `v0.2.0`)
- better bridge sync summaries and diagnostics
- safer reconciliation for existing MAS users
- clearer upgrade and self-update paths

The beta should stay focused on proving the default install and SCIM demo flow before expanding scope.
