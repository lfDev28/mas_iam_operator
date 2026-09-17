# mas-est Guide

`mas-est` (MAS External Services Toolkit) installs a working lab of the external services MAS depends on — LDAP, an OIDC/SAML identity provider, SCIM provisioning, S3 object storage and SMTP — onto the same OpenShift cluster as a MAS instance, and wires them into MAS. It is built for support engineers reproducing customer issues without a customer's Entra, Okta or cloud subscription.

- [Install](#install)
- [What gets installed](#what-gets-installed)
- [Logging in](#logging-in)
- [How the install runs](#how-the-install-runs)
- [Connection details](#connection-details)
- [Using S3 with Manage](#using-s3-with-manage)
- [Using SMTP](#using-smtp)
- [Operations](#operations)
- [Troubleshooting](#troubleshooting)
- [Known limitations](#known-limitations)

Tested on MAS 9.1.4, 9.1.18, 9.1.19 and 9.1.20. MAS 9.0 works without OIDC (see [Known limitations](#known-limitations)).

---

## Install

### Prerequisites

| Need | Detail |
|---|---|
| Local tools | `podman`, `oc`, `bash` 3.2+ |
| Cluster access | `oc` logged in with rights to create namespaces, ClusterRoles and OLM resources (cluster-admin in practice) |
| MAS API key | Name and value, with **userAdmin** and **systemAdmin** permissions. Preflight rejects a key without them. |
| Storage | A block/RBD storage class for PostgreSQL, OpenLDAP and the SCIM bridge. Avoid CephFS defaults when an RBD class exists. |

### 1. Bootstrap the `mas-est` command

```bash
export MAS_EST_IMAGE='quay.io/lee_forster/mas-external-services-tool:v0.1.9'
mkdir -p "$HOME/mas-est"
podman run -ti --rm -v "$HOME/mas-est:/tmp" --pull always "$MAS_EST_IMAGE" bootstrap --force
export PATH="$HOME/mas-est:$PATH"

mas-est version      # must match the tag above
```

This extracts the CLI into `~/mas-est`. **To upgrade, re-run the same `podman run` with the new tag** — exporting a new `MAS_EST_IMAGE` alone doesn't replace the extracted binary. `install` refuses to run if `MAS_EST_IMAGE` and the binary disagree.

### 2. Preflight

```bash
mas-est preflight
```

Checks cluster access, storage classes, the MAS SCIM URL, whether the API key has the right permissions, whether MAS supports OIDC, and warns if MAS already has IDPCfgs that the install would overwrite.

### 3. Install

```bash
mas-est install
```

The installer is interactive. It asks for:

- **Components:** `ldap`, `keycloak`, `scim`, `s3`, `smtp`. SCIM needs Keycloak and LDAP; Keycloak needs LDAP. They're added automatically.
- **MAS details:** SCIM base URL (`https://api.<mas-host>/scim/v2`, discovered for you), API key, workspace, SCIM profile ID (default `demo`), MAS instance ID.
- **MAS login providers** to create: LDAP, OIDC, SAML.
- **Storage classes.**

Values it can work out (core namespace, auth host, the workspace when there is only one) are filled in and shown as `[derived]`. A full install takes 15–20 minutes.

For scripted installs, pass everything as flags:

```bash
mas-est install --non-interactive \
  --components ldap,keycloak,scim,s3,smtp \
  --mas-base-url 'https://api.<mas-host>/scim/v2' \
  --mas-api-token-name '<name>' --mas-api-token-value '<value>' \
  --workspace-id '<workspace>' \
  --mas-instance-id '<instance>' \
  --configure-mas-auth --mas-auth-providers ldap,oidc,saml \
  --storage-class '<rbd-class>' --scim-bridge-storage-class '<rbd-class>'
```

Run `mas-est install --help` for every flag.

---

## What gets installed

Everything lives in the `mas-est` namespace, except the MAS configuration, which goes into `mas-<instance>-core`.

| Component | What it is | Wired into MAS as |
|---|---|---|
| MAS IAM operator | OLM operator that deploys Keycloak, OpenLDAP and PostgreSQL | — |
| OpenLDAP | LDAP server, base DN `dc=demo,dc=local` | LDAP provider (`<instance>-ldap-default-system`) |
| Keycloak | Identity provider, realm `maximo`, backed by PostgreSQL | OIDC provider (`<instance>-oidc-default-system`) and SAML provider (`<instance>-saml-default-system`) |
| SCIM bridge | Polls Keycloak every 5 minutes and pushes users into MAS through the SCIM API | SCIM profile `demo` |
| MinIO | S3-compatible storage with bucket `mas-s3-demo` | `ObjectStorageCfg` (`ibm-mas-<instance>-objectstoragecfg-system`) |
| Mailpit | Capture-only SMTP server with a web UI | Not wired automatically; see [Using SMTP](#using-smtp) |

When MAS login providers are selected, the installer also:

- Writes the self-registration ConfigMap `<instance>-selfreg`. OIDC, SAML and LDAP users who don't exist in MAS yet are created on first login with a PREMIUM entitlement and Manage access in the chosen workspace.
- Raises the `<instance>-entitymgr-idpcfg` memory limit to 2 Gi through the Suite CR. At the default 512 Mi it runs out of memory while applying several providers. Use `--idpcfg-memory-limit` to change it, or `off` to skip.
- Links the SCIM users it finds to their OIDC identity (SAML if OIDC isn't configured), so they can log in and reach Manage.

MAS provider IDs are always `default`, which is what makes the MAS UI show each provider as **Configured**.

---

## Logging in

| User | Lives in | Log in with | How they get into MAS |
|---|---|---|---|
| `ldap.user1`, `ldap.user2` | OpenLDAP | LDAP | Self-registered on first login |
| `oidc.user1`, `oidc.user2` | Keycloak | OIDC | Self-registered on first login |
| `saml.user1`, `saml.user2` | Keycloak | SAML | Self-registered on first login |
| `scim.user1`, `scim.user2` | Keycloak | OIDC | Created by the SCIM bridge before anyone logs in |

The Keycloak users' password is `maxadmin`. Get the LDAP users' passwords with `mas-est ldap-info --show-user-passwords`.

**Use a private browser window for each login.** Keycloak keeps you signed in after you log out of MAS, so a second login in the same browser can sign in as the previous user or fail with `AIUOM0100E`.

### Adding more SCIM users

1. In Keycloak (route `mas-est-iam`, realm `maximo`), create a user whose username starts with `scim.`, give them an email address, and add them to the group `mas-scim-users`.
2. Within 5 minutes the bridge creates them in MAS.
3. Re-run `mas-est mas-auth apply` with the same settings as the install. This links the new user to their login identity; without it they can't log in or sync to Manage.

Removing a user from `mas-scim-users` deactivates them in MAS, and adding them back reactivates them. This is **experimental** and hasn't been validated on a cluster yet.

---

## How the install runs

`mas-est install` asks its questions and runs preflight on your machine, then hands the work to a Kubernetes Job (`mas-est-install`) in the cluster and streams its logs. Closing your laptop, losing VPN, or pressing Ctrl-C doesn't stop it.

```bash
mas-est logs --namespace mas-est --component install-job --follow   # reattach
oc delete job mas-est-install -n mas-est                            # cancel
mas-est install --local                                             # run on this machine instead
```

- The finished Job and its logs are kept for troubleshooting. To re-run, delete the Job first. Interactive mode offers to do this for you.
- `--uninstall-first` only works with `--local`, because the Job would delete its own namespace.
- The Job runs the image matching your CLI version. Override it with `--installer-image` for a mirrored registry, or `--job-name` when two people share a cluster.

**Permissions.** The Job's service account gets a broad ClusterRole, because the install touches the MAS core namespace, the MAS MongoDB namespace, OLM and cluster-scoped resources. That includes reading secrets across namespaces and `pods/exec` (used to link SCIM users in MongoDB), which is close to cluster-admin. Deleting workloads is only allowed inside `mas-est`. If your cluster's security policy doesn't allow this, use `--local`.

The permissions are applied by **the CLI you run**, not by the Job image. After an upgrade, re-bootstrap before installing, or the old permissions are applied again.

---

## Connection details

For a readable summary, with secrets hidden unless you add `--show-secrets`:

```bash
mas-est details --namespace mas-est --component all     # or ldap | oidc | saml | s3 | smtp
mas-est ldap-info --namespace mas-est                   # --show-password, --show-user-passwords
```

For raw values, each service has its own Secret, or a ConfigMap for SMTP, in `mas-est`:

| Resource | Keys |
|---|---|
| `secret/mas-est-ldap-connection` | `url`, `baseDN`, `bindDN`, `bindPassword`, `userIdMap`, `ca.crt` |
| `secret/mas-est-oidc-connection` | `issuerUrl`, `discoveryUrl`, `authorizationEndpoint`, `tokenEndpoint`, `jwksEndpoint`, `clientId`, `clientSecret`, `realm`, `redirectUri` |
| `secret/mas-est-saml-connection` | `entityId`, `acsUrl`, `sloUrl`, `idpMetadataUrl`, `idpMetadata`, `nameIdFormat` |
| `secret/mas-est-s3-connection` | `provider`, `endpoint`, `manageEndpoint`, `externalEndpoint`, `consoleUrl`, `accessKey`, `secretKey`, `region`, `bucket`, `siblingBuckets` |
| `configmap/mas-est-smtp-connection` | `host`, `port`, `from`, `webUI`, `tls`, `authentication`, `relayEnabled` |

```bash
oc get secret mas-est-oidc-connection -n mas-est -o jsonpath='{.data.clientSecret}' | base64 -d
```

### LDAP

| Setting | Value |
|---|---|
| URL (in-cluster) | `ldaps://mas-est-iam-openldap.mas-est.svc.cluster.local:636` |
| Bind DN | `cn=admin,dc=demo,dc=local` (password: secret `mas-est-iam-openldap-admin`, key `password`) |
| Users / groups DN | `ou=users,dc=demo,dc=local` / `ou=groups,dc=demo,dc=local` |
| User attribute | `uid` |
| Group object class / member attribute | `groupOfUniqueNames` / `uniqueMember` |

To reach it from your machine: `oc -n mas-est port-forward svc/mas-est-iam-openldap 1636:636`.

---

## Using S3 with Manage

The installer connects MinIO to MAS Suite through an `ObjectStorageCfg`. **Manage attachments (doclinks) are separate and need manual setup.** In Manage → **System Properties**, set:

| Property | Value |
|---|---|
| `mxe.cosendpointuri` | `http://mas-est.svc.cluster.local:9000` |
| `mxe.cosbucketname` | `mas-s3-demo` |
| `mxe.cosregion` | `us-east-1` (**required**; add it with **New Row** if it doesn't exist) |
| `mxe.cosaccesskey` | `minioadmin` |
| `mxe.cossecretkey` | secret `mas-minio-root`, key `MINIO_ROOT_PASSWORD` |
| `mxe.attachmentstorage` | `com.ibm.tivoli.maximo.oslc.provider.COSAttachmentStorage` |
| `mxe.doclink.doctypes.defpath` | `cos:doclinks` |
| `mxe.doclink.doctypes.topLevelPaths` | `cos:doclinks` |
| `mxe.doclink.path01` | `cos:doclinks=<manage-ui-base-url>` |
| `mxe.doclink.securedAttachment` | `True` |

Then Live Refresh. In **Document Types**, change each doctype's default path to `cos:doclinks/<name>` (for example `cos:doclinks/attachment`), and restart the Manage `all` deployment.

- **Use the in-cluster URL above, not the MinIO route.** Manage addresses buckets as `<bucket>.<host>`. The installer creates matching Services (`mas-s3-demo`, `mas-s3-demobackup`, `mas-s3-demorecovery`), but they only resolve inside the cluster. The route gives HTTP 503 or certificate errors.
- **Without `mxe.cosregion`, every request fails with `SignatureDoesNotMatch`.**
- The `LOADFLATOBJECT` cron task keeps logging `SignatureDoesNotMatch` even when doclinks work, because it ignores the region. It's harmless; disable the cron task to silence it.

Browse buckets in the MinIO console: the `mas-minio-console` route, logging in with the root credentials above.

---

## Using SMTP

Mailpit captures mail so you can inspect it in its web UI (the `mas-mailpit` route). It doesn't deliver mail anywhere unless you set up a relay. The installer doesn't point MAS at it; set this in MAS yourself:

| Setting | Value |
|---|---|
| Host | `mas-mailpit.mas-est.svc.cluster.local` |
| Port | `1025` |
| TLS | off |
| Authentication | none |

To also forward captured mail to real inboxes, add relay flags at install time (or to `mas-est smtp install-mailpit`):

```bash
--smtp-relay-host smtp.gmail.com --smtp-relay-port 587 --smtp-relay-starttls \
--smtp-relay-username 'lab@example.com' --smtp-relay-password "$APP_PASSWORD" \
--smtp-relay-from 'lab@example.com'
```

MAS settings stay the same; Mailpit forwards the mail. Gmail needs an App Password. For SendGrid, the username is the literal `apikey`. Microsoft 365 needs OAuth, which Mailpit doesn't support.

---

## Operations

### Health and logs

```bash
mas-est status --namespace mas-est
mas-est logs --namespace mas-est --component bridge --tail 300
```

Log components: `operator`, `keycloak`, `openldap`, `bridge`, `profile-bootstrap`, `minio`, `minio-init`, `smtp`, `install-job`. Add `--follow` to stream.

### SCIM bridge settings

Settings live in `configmap/scim-bridge-config` and `secret/scim-bridge-secret`. The bridge reads them only at startup.

```bash
mas-est config view --namespace mas-est                                      # current settings, secrets hidden
mas-est config set mas-api-token --namespace mas-est \
  --token-name '<name>' --token-value '<value>'                              # rotates the key and restarts the bridge
mas-est config set bridge --namespace mas-est --log-level debug              # or --payload-logging true
mas-est restart bridge --namespace mas-est                                   # after editing the ConfigMap by hand
```

Which users the bridge syncs is controlled by `SCIM_BRIDGE_INCLUDE_GROUPS` (default `mas-scim-users`) and `SCIM_BRIDGE_INCLUDE_USERNAME_PREFIX` (default `scim.`). A user has to pass **both**. The full list of settings is in [services/scim-bridge/README.md](../services/scim-bridge/README.md).

Payload logging can record names and email addresses, so turn it off when you're done.

### Uninstall and reinstall

```bash
mas-est uninstall --namespace mas-est                          # also deletes the MAS SCIM profile
mas-est uninstall --namespace mas-est --skip-profile-delete    # keep the profile
```

`uninstall` deletes the `mas-est` namespace. It **does not** remove:

- MAS login providers. Use `mas-est mas-auth delete`.
- The self-registration ConfigMap, `<instance>-selfreg`.
- MAS user records in MongoDB and Manage.
- The installer's `ClusterRole/mas-est-installer` and `ClusterRoleBinding/mas-est-installer-mas-est`.
- The operator `CatalogSource` in `openshift-marketplace`.

Reinstalling onto the same MAS with leftover user records can break self-registration and Manage sync; see [Troubleshooting](#troubleshooting). If MAS already has providers with the `default` ID, the install **overwrites them in place**. Back them up first:

```bash
oc get idpcfg -n mas-<instance>-core -o yaml > idpcfg-backup.yaml
```

### Reporting a problem

```bash
mas-est support-bundle --namespace mas-est
```

Collects status, events, logs, ConfigMaps and redacted secret metadata into a timestamped directory. Secret values are never included. Check it for customer hostnames before sharing it outside the team.

---

## Troubleshooting

### Install

| Symptom | Fix |
|---|---|
| `bootstrap`: `/tmp/mas-est already exists` | Add `bootstrap --force` after the image name. |
| `install`: `stale local runtime detected` | Your extracted CLI doesn't match `MAS_EST_IMAGE`. Re-run the bootstrap `podman run`. |
| `mas-est: command not found` | `export PATH="$HOME/mas-est:$PATH"` |
| Preflight `mas-api-key`: 403 `AIUCO1003E` | Recreate the API key with **userAdmin** and **systemAdmin**. |
| Preflight `mas-oidc-endpoint`: 404 `AIUCO1022E` | MAS 9.0 has no OIDC API. Use `--mas-auth-providers ldap,saml`. |
| `install`: a Job named `mas-est-install` already exists | `oc delete job mas-est-install -n mas-est`, then re-run. |
| `Forbidden` in the install-job log | You launched with an older CLI. Re-bootstrap, delete the Job, re-run. |
| A PVC stays `Pending` | Pick a block/RBD class: `oc get sc`, then reinstall with `--storage-class`. |
| Operator CSV never reaches `Succeeded` | `oc get csv -n mas-est` and `oc get catalogsource mas-iam-operator -n openshift-marketplace -o yaml`. Look for image pull or catalog errors. |
| Providers stuck applying or deleting; `entitymgr-idpcfg` restarting with OOMKilled | Its memory bump didn't apply. Check `oc get suite <instance> -n mas-<instance>-core -o jsonpath='{.spec.podTemplates}'`. |

### Login and sync

| Symptom | Cause | Fix |
|---|---|---|
| Wrong user signed in, or `AIUOM0100E`, after switching provider | Keycloak kept your session | Private browser window per login. |
| First SAML login fails: `AIUSC0019I Self Registration configuration either not setup or not enabled` | MAS 9.1.20 bug (see [Known limitations](#known-limitations)) | Log in again; it works on the second attempt. |
| `CWIML4537E` / "invalid username or password" for a user who authenticated fine | Leftover MongoDB user record from an earlier install | Delete that user's document from `mas_<instance>_core.User` and log in again. |
| SCIM user can't log in, or is `_local`-only in MAS | Created in Keycloak after install, so never linked | Re-run `mas-est mas-auth apply`. |
| User is in MAS but not in Manage; `entitlement.application` is `NONE` | User was created before the SCIM profile existed | Set entitlement and workspace, then set `sync.status` to `PENDING`. Fixed in v0.1.7 for new installs. |
| Manage sync fails with `AIUI1101E: 500` | Leftover `MAXIMO.PERSONANCESTOR` row | Delete the row for that `PERSONID`, then set the user's `sync.status` and `applications.manage.sync.state` to `PENDING`. |
| Manage sync fails with `BMXAA10249E ... DEFLTREG security group` | Leftover `MAXIMO.GROUPUSER` row (misleading message) | Delete that user's `GROUPUSER` rows, then set `sync.status` to `PENDING`. |
| SCIM users never appear in MAS | Bad API key, SCIM URL or workspace, or the user isn't in `mas-scim-users` | `mas-est logs --component bridge --tail 300`, then `mas-est config view`. |
| Doclinks: `SignatureDoesNotMatch`, HTTP 503, or `BMXAA4195E` | Wrong endpoint or missing region | See [Using S3 with Manage](#using-s3-with-manage). |

---

## Known limitations

- **Not a replacement for Entra or Okta.** No vendor-specific provisioning rules or expression mapping. Use mas-est to narrow a problem down, then confirm vendor-specific behaviour in the customer's own setup.
- **MAS 9.0 has no OIDC.** Use LDAP and SAML only.
- **Logging out of MAS doesn't log you out of Keycloak.** Use a private window per login, or visit `https://<keycloak-host>/realms/maximo/protocol/openid-connect/logout`.
- **MAS 9.1.20: the first SAML self-registration after the `<instance>-coreidp` pod starts fails once.** This is a MAS bug in `samlresolver` 4.0.43: it loads the self-registration config in the background on the first SAML login and checks it before loading finishes. Retrying works. Don't restart `coreidp` to fix it, because that resets the problem. MAS 9.1.19 and earlier, and OIDC, are unaffected.
- **SCIM users added after install need `mas-est mas-auth apply` to be linked** before they can log in or reach Manage.
- **Group-based deactivation and reactivation in the SCIM bridge is experimental** and hasn't been validated on a cluster.
- **One SCIM profile.** No group-based profile routing. Per-user routing works through the Keycloak `masProfile` attribute.
- **MAS isn't configured to use Mailpit, and Manage doclinks aren't set up automatically.** Both are manual. Mail from MAS into Mailpit hasn't been tested end to end.
- **`uninstall` leaves MAS-side state behind**; see [Uninstall and reinstall](#uninstall-and-reinstall).
- **The installer needs broad cluster permissions**; see [How the install runs](#how-the-install-runs).
