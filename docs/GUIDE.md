# mas-est Guide

[Prerequisites](#prerequisites) · [Install](#install) · [Upgrade](#upgrade) · [What gets installed](#what-gets-installed) · [Log in with the demo users](#log-in-with-the-demo-users) · [Connection details](#connection-details) · [Connect S3 to Manage](#connect-s3-to-manage) · [Connect SMTP](#connect-smtp) · [Day-2 operations](#day-2-operations) · [Uninstall](#uninstall)

When something fails, see [Troubleshooting](TROUBLESHOOTING.md). What the toolkit doesn't do is in [Known limitations](KNOWN-LIMITATIONS.md).

Placeholders: `<instance>` is your MAS instance ID, `<cluster-domain>` the cluster's apps domain, `<workspace>` the MAS workspace ID.

## Prerequisites

| Need | Detail |
|---|---|
| Local tools | `podman`, `oc`, bash 3.2 or later |
| Cluster access | `oc` logged in as cluster-admin. The install creates namespaces, ClusterRoles and OLM resources. |
| MAS API key | Name and value, with the **userAdmin** and **systemAdmin** permissions. Preflight rejects a key without them. |
| Storage | A block (RBD) storage class. Preflight recommends one. |
| MAS version | 9.1.4, 9.1.18, 9.1.19 or 9.1.20. On MAS 9.0, leave out OIDC. |

## Install

### 1. Bootstrap the CLI

```bash
export MAS_EST_IMAGE='quay.io/lee_forster/mas-external-services-tool:v0.1.9'
mkdir -p "$HOME/mas-est"
podman run -ti --rm -v "$HOME/mas-est:/tmp" --pull always "$MAS_EST_IMAGE" bootstrap --force
export PATH="$HOME/mas-est:$PATH"
mas-est version
```

`version` must print `0.1.9`. `--force` overwrites an earlier runtime and is safe on a first install.

### 2. Preflight

```bash
mas-est preflight
```

Enter the MAS SCIM URL, `https://api.<instance>.<cluster-domain>/scim/v2`, and the API key. Success looks like this:

```
[ok] oc: oc CLI found on PATH
[ok] cluster-login: logged in as kube:admin against https://api.<cluster-domain>:6443
[ok] storage-classes: discovered 5 storage classes; recommended=localblock
[warn] storage-selection: default storage class ocs-storagecluster-cephfs looks filesystem-backed; preferring localblock for PostgreSQL
[ok] mas-base-url: parsed MAS host api.<instance>.<cluster-domain>
[ok] mas-api-key: MAS API key <token-name> authenticated and is authorized for the SCIM API
[ok] mas-route-lookup: matched route mas-<instance>-core/<instance>-api
```

`install` runs two more checks before it starts: whether MAS exposes the OIDC API, and whether login providers named `default` already exist.

### 3. Install

```bash
mas-est install
```

| Prompt | What to answer |
|---|---|
| Products to install | LDAP directory, Keycloak identity provider, SCIM bridge, S3 object storage, SMTP Mailpit capture server. SCIM adds Keycloak and LDAP; Keycloak adds LDAP. |
| MAS SCIM base URL, MAS API token name, MAS API token value | Same as preflight |
| Workspace ID, MAS profile ID | The workspace is filled in when MAS has one. Profile ID defaults to `demo`. |
| MAS instance ID, MAS core namespace | Filled in from the SCIM URL |
| Configure MAS authentication providers? | Yes. Then choose `ldap`, `oidc`, `saml`. On MAS 9.0 leave out `oidc`. |
| Primary storage class, SCIM bridge storage class | The block class preflight recommended |

Answers marked `[derived]` were worked out for you. Confirm at `Proceed with install?`.

The install runs as a Job named `mas-est-install` in the `mas-est` namespace and streams its log to your terminal. Ctrl-C detaches from the log. The Job keeps running.

```bash
mas-est logs --component install-job --follow   # reattach
oc delete job mas-est-install -n mas-est        # cancel, or clear a finished Job before re-running
mas-est install --local                         # run on this machine instead of in the cluster
```

Success ends with the summary:

```
Install summary (total 17m27s)
  ✓ Install LDAP, Keycloak, SCIM bridge      (5m44s)
  ✓ Install MinIO S3                         (1m47s)
  ✓ Install Mailpit SMTP                     (3s)
  ✓ Configure MAS LDAP / OIDC / SAML         (9m53s)
```

The Job's ServiceAccount gets a ClusterRole close to cluster-admin. The install reads secrets in the MAS namespaces and runs `pods/exec` on MongoDB to link the SCIM users. The permissions are applied by the CLI you run, not by the Job image, so re-bootstrap before installing after an upgrade. If your cluster's policy forbids this, use `--local`.

Scripted install, with every value as a flag:

```bash
mas-est install --non-interactive \
  --components ldap,keycloak,scim,s3,smtp \
  --mas-base-url 'https://api.<instance>.<cluster-domain>/scim/v2' \
  --mas-api-token-name '<name>' --mas-api-token-value '<value>' \
  --workspace-id '<workspace>' --mas-instance-id '<instance>' \
  --configure-mas-auth --mas-auth-providers ldap,oidc,saml \
  --storage-class '<block-class>' --scim-bridge-storage-class '<block-class>'
```

### 4. Verify

```bash
mas-est status
```

Every deployment shows `ready=1/1`, the CSV shows `phase=Succeeded`, and every PVC shows `phase=Bound`. Then log in as one of the [demo users](#log-in-with-the-demo-users).

## Upgrade

Re-run bootstrap with the new tag. Exporting a new `MAS_EST_IMAGE` doesn't replace the extracted binary, and `install` refuses to run while the two differ.

```bash
export MAS_EST_IMAGE='quay.io/lee_forster/mas-external-services-tool:v<new-version>'
podman run -ti --rm -v "$HOME/mas-est:/tmp" --pull always "$MAS_EST_IMAGE" bootstrap --force
mas-est version
```

Nothing in the cluster changes until you run `mas-est install` again.

## What gets installed

| Component | In namespace `mas-est` | In namespace `mas-<instance>-core` |
|---|---|---|
| MAS IAM operator | CSV `mas-iam-operator.v0.0.15`, from CatalogSource `mas-iam-operator` in `openshift-marketplace` | |
| OpenLDAP | `deployment/mas-est-iam-openldap` | `idpcfg/<instance>-ldap-default-system` |
| Keycloak, realm `maximo`, with PostgreSQL | `deployment/mas-est-iam`, route `mas-est-iam` | `idpcfg/<instance>-oidc-default-system`, `idpcfg/<instance>-saml-default-system` |
| SCIM bridge | `deployment/scim-bridge` | SCIM profile `demo` |
| MinIO | `deployment/mas-minio`, routes `mas-minio-api` and `mas-minio-console` | `objectstoragecfg/ibm-mas-<instance>-objectstoragecfg-system` |
| Mailpit | `deployment/mas-mailpit`, route `mas-mailpit` | |
| Self-registration | | `configmap/<instance>-selfreg`. Users are created on first login with PREMIUM entitlement and Manage in `<workspace>`. |

MAS calls a login provider an IDPCfg. The provider ID is always `default`, which is what makes MAS administration show each provider as configured. If providers with that ID already exist, the install overwrites them. Back them up first:

```bash
oc get idpcfg -n mas-<instance>-core -o yaml > idpcfg-backup.yaml
```

The install also raises the memory limit of `<instance>-entitymgr-idpcfg` to 2 Gi through the Suite CR, because the MAS default of 512 Mi runs out while the providers are applied. `--idpcfg-memory-limit off` skips this.

![MAS administration showing the LDAP, OIDC and SAML providers as configured](images/mas-admin-providers-configured.png)

## Log in with the demo users

| User | Directory | Log in with | Password |
|---|---|---|---|
| `ldap.user1`, `ldap.user2` | OpenLDAP | LDAP | `mas-est ldap-info --show-user-passwords` |
| `oidc.user1`, `oidc.user2` | Keycloak | OIDC | `maxadmin` |
| `saml.user1`, `saml.user2` | Keycloak | SAML | `maxadmin` |
| `scim.user1`, `scim.user2` | Keycloak | OIDC | `maxadmin` |

LDAP, OIDC and SAML users are created in MAS on their first login. SCIM users are created by the SCIM bridge before anyone logs in.

Use a private browser window for every login. Keycloak keeps you signed in after you log out of MAS, so the next login in the same window returns the previous user.

![MAS login page with the LDAP, OIDC and SAML options](images/mas-login-providers.png)

### Add a SCIM user

1. Open the Keycloak admin console at the `mas-est-iam` route, realm `maximo`. The admin password is in secret `mas-est-iam-bootstrap-admin`.
2. Create a user whose username starts with `scim.`, give it an email address, and add it to the group `mas-scim-users`.
3. Wait up to 5 minutes. `mas-est logs` shows `action=create username=scim.<name>` when the bridge has created it in MAS.
4. Link the user to its OIDC identity. Without this step it can't log in or reach Manage.

   ```bash
   mas-est mas-auth apply --mas-instance-id <instance> --self-reg-workspace <workspace>
   ```

   On MAS 9.0 add `--providers ldap,saml`.

![Keycloak realm maximo user list, and the members of group mas-scim-users](images/keycloak-scim-users-group.png)

![scim.user1 in MAS Users and in Manage](images/scim-user-in-mas-and-manage.png)

## Connection details

```bash
mas-est details        # --component ldap|oidc|saml|s3|smtp; --show-secrets
mas-est ldap-info      # --show-password; --show-user-passwords
```

Both print what MAS is configured with. The raw values are in these resources in `mas-est`:

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

### LDAP settings

| Setting | Value |
|---|---|
| URL, inside the cluster | `ldaps://mas-est-iam-openldap.mas-est.svc.cluster.local:636` |
| Bind DN | `cn=admin,dc=demo,dc=local`. Password: secret `mas-est-iam-openldap-admin`, key `password` |
| Base DN | `dc=demo,dc=local` |
| Users, groups | `ou=users,dc=demo,dc=local`, `ou=groups,dc=demo,dc=local` |
| User attribute | `uid` |
| Group object class, member attribute | `groupOfUniqueNames`, `uniqueMember` |

From your machine: `oc -n mas-est port-forward svc/mas-est-iam-openldap 1636:636`.

## Connect S3 to Manage

The install connects MinIO to MAS Suite through the `ObjectStorageCfg`. Manage attachments, called doclinks, need these steps.

1. In Manage, open **System Properties** and set:

   | Property | Value |
   |---|---|
   | `mxe.cosendpointuri` | `http://mas-est.svc.cluster.local:9000` |
   | `mxe.cosbucketname` | `mas-s3-demo` |
   | `mxe.cosregion` | `us-east-1`. Add it with **New Row** if it doesn't exist. |
   | `mxe.cosaccesskey` | `minioadmin` |
   | `mxe.cossecretkey` | Secret `mas-minio-root`, key `MINIO_ROOT_PASSWORD` |
   | `mxe.attachmentstorage` | `com.ibm.tivoli.maximo.oslc.provider.COSAttachmentStorage` |
   | `mxe.doclink.doctypes.defpath` | `cos:doclinks` |
   | `mxe.doclink.doctypes.topLevelPaths` | `cos:doclinks` |
   | `mxe.doclink.path01` | `cos:doclinks=<manage-ui-base-url>` |
   | `mxe.doclink.securedAttachment` | `True` |

2. Click **Live Refresh**.
3. In **Document Types**, set each document type's default path to `cos:doclinks/<name>`, for example `cos:doclinks/attachment`.
4. Restart Manage:

   ```bash
   oc rollout restart deployment/<instance>-<workspace>-all -n mas-<instance>-manage
   ```

Use the endpoint above, not the MinIO route. Manage addresses buckets as `<bucket>.<host>`. The install creates a Service for each bucket name that resolves only inside the cluster, and the route returns HTTP 503 or certificate errors. Without `mxe.cosregion`, every request fails with `SignatureDoesNotMatch`.

Browse the buckets at the `mas-minio-console` route as user `minioadmin`, with the `MINIO_ROOT_PASSWORD` key of secret `mas-minio-root`.

![MinIO console showing the mas-s3-demo, mas-s3-demobackup and mas-s3-demorecovery buckets](images/minio-buckets.png)

## Connect SMTP

Mailpit captures every message and shows it at the `mas-mailpit` route. It delivers nothing unless you add a relay. The install doesn't point MAS at it. Set these in MAS administration:

| Setting | Value |
|---|---|
| Host | `mas-mailpit.mas-est.svc.cluster.local` |
| Port | `1025` |
| TLS | Off |
| Authentication | None |

![Mailpit inbox showing a captured MAS email](images/mailpit-inbox.png)

To forward captured mail to real inboxes, install with relay flags. They also work with `mas-est smtp install-mailpit`.

```bash
--smtp-relay-host smtp.gmail.com --smtp-relay-port 587 --smtp-relay-starttls \
--smtp-relay-username 'lab@example.com' --smtp-relay-password "$APP_PASSWORD" \
--smtp-relay-from 'lab@example.com'
```

The MAS settings stay the same. Gmail needs an App Password. For SendGrid the username is the literal `apikey`. Microsoft 365 needs OAuth, which Mailpit doesn't support.

## Day-2 operations

### Health and logs

```bash
mas-est status
mas-est logs --component bridge --tail 300    # --follow to stream
```

Log components: `operator`, `keycloak`, `openldap`, `bridge`, `profile-bootstrap`, `minio`, `minio-init`, `smtp`, `install-job`.

### SCIM bridge settings

The bridge reads `configmap/scim-bridge-config` and `secret/scim-bridge-secret` at startup. These commands restart it for you:

```bash
mas-est config view
mas-est config set mas-api-token --token-name '<name>' --token-value '<value>'   # rotate the MAS API key
mas-est config set bridge --log-level debug                                     # or --payload-logging true
mas-est restart bridge                                                          # after editing the ConfigMap by hand
```

The bridge syncs a user only if it is in the Keycloak group named by `SCIM_BRIDGE_INCLUDE_GROUPS` (`mas-scim-users`) and its username starts with `SCIM_BRIDGE_INCLUDE_USERNAME_PREFIX` (`scim.`). The full setting list is in [services/scim-bridge/README.md](../services/scim-bridge/README.md).

Payload logging writes usernames, names and email addresses to the log. Turn it off when you're done.

### Re-apply or remove the MAS login providers

```bash
mas-est mas-auth apply --mas-instance-id <instance> --self-reg-workspace <workspace>   # re-create providers and re-link SCIM users
mas-est mas-auth delete --mas-instance-id <instance>                                   # remove all three; --providers to pick
```

`apply` reads the API key from `secret/scim-bridge-secret`. Always pass `--self-reg-workspace`, or self-registration falls back to a workspace named `workspace`.

### Report a problem

```bash
mas-est support-bundle
```

Writes a `mas-est-support-mas-est-<timestamp>` directory with status, events, pod and route listings, ConfigMaps and component logs. No Secret values are written. Check it for customer hostnames before sharing it outside the team.

## Uninstall

```bash
mas-est uninstall                          # also deletes SCIM profile demo
mas-est uninstall --skip-profile-delete    # keeps the profile
```

Deletes the `mas-est` namespace. It leaves behind:

- The login providers. Remove them with `mas-est mas-auth delete --mas-instance-id <instance>`.
- `configmap/<instance>-selfreg` and the Suite CR memory bump.
- MAS user records in MongoDB and Manage.
- `clusterrole/mas-est-installer`, `clusterrolebinding/mas-est-installer-mas-est`, the SCC `mas-est-iam-openldap-tls-generator`, and the CRD `masiamstacks.iam.mas.ibm.com`.
- CatalogSource `mas-iam-operator` in `openshift-marketplace`.

Reinstalling onto the same MAS with leftover user records can break self-registration and Manage sync. The fixes are in [Troubleshooting](TROUBLESHOOTING.md#login).
