# Troubleshooting

Search this page for the error text you see. Each row gives the cause and the fix.

[Bootstrap and upgrade](#bootstrap-and-upgrade) · [Preflight](#preflight) · [Install](#install) · [Login](#login) · [SCIM and Manage sync](#scim-and-manage-sync) · [S3 and doclinks](#s3-and-doclinks)

`<instance>` is your MAS instance ID and `<workspace>` the MAS workspace ID.

## Bootstrap and upgrade

| Error | Cause | Fix |
|---|---|---|
| `/tmp/mas-est already exists; rerun with --force to overwrite` | A runtime is already extracted | Add `bootstrap --force` after the image name. |
| `mas-est: command not found` | The extracted directory isn't on your PATH | `export PATH="$HOME/mas-est:$PATH"` |
| `stale local runtime detected` | `MAS_EST_IMAGE` names a different version than the extracted binary | `mas-est update --version <tag>`, or re-run the bootstrap `podman run` line. Then `mas-est version`. If you just ran `mas-est update`, export the new `MAS_EST_IMAGE` it printed. |
| `warning: MAS_EST_IMAGE requests <tag> but this binary is v<version>` from `mas-est version` | Same as above | Same as above. |
| `unknown command "update"` | CLI v0.1.13 or earlier, which has no `update` | Re-run the bootstrap `podman run` line with the new tag once. `update` works from then on. |
| `look up the newest release` from `mas-est update` | Quay isn't reachable from your machine (offline, proxy) | `mas-est update --version vX.Y.Z` skips the lookup. |
| `mas-est update only works from a bootstrapped mas-est` | The binary wasn't started through the `~/mas-est/mas-est` launcher | Run the `mas-est` from your bootstrap directory, or re-run bootstrap. |
| `mas-est update needs podman or docker on PATH` | No container engine found | Install podman, or pass `--engine <path>`. On macOS, also `podman machine start`. |

## Preflight

Check names appear as `[error] <name>:` in the output.

| Error | Cause | Fix |
|---|---|---|
| `mas-api-key: MAS API key <name> authenticated but is not authorized for the SCIM API (HTTP 403 AIUCO1003E)` | The key lacks a permission | Recreate the key with **userAdmin** and **systemAdmin**. |
| `mas-api-key: MAS API key <name> failed to authenticate (HTTP 401)` | Wrong key name or value | Check both against the key in MAS administration. |
| `mas-oidc-endpoint: MAS at <host> does not expose the OIDC external-IdP API (HTTP 404 AIUCO1022E; requires MAS 9.1+)` | MAS 9.0 | Leave `oidc` out of the providers. |
| `mas-idpcfg-overwrite: <n> existing IDPCfg(s) in namespace mas-<instance>-core will be OVERWRITTEN in place` | MAS already has providers named `default` | Back them up with `oc get idpcfg -n mas-<instance>-core -o yaml`, or keep them by installing without those providers. |
| `mas-base-url: MAS base URL must include /scim/v2` | URL typo | Use `https://api.<instance>.<cluster-domain>/scim/v2`. |
| `cluster-login: cluster login check failed; run 'oc login' first` | Not logged in | `oc login`, then retry. |
| `cluster-permissions: the logged-in user is missing <n> permission(s) the install needs` | You're not cluster-admin. `--local` needs the same rights. | Log in as cluster-admin, or ask an admin to run the install. |
| `--workspace-id "<id>" does not match any ManageWorkspace` | Typo, or the workspace belongs to another instance | `oc get manageworkspace -A` and use the workspace ID shown in the error's list. |

## Install

The install log is at `mas-est logs --component install-job`.

| Error | Cause | Fix |
|---|---|---|
| `job/mas-est-install already exists in namespace mas-est and has already finished` | A previous Job is still there | `oc delete job mas-est-install -n mas-est`, then re-run. Interactive installs offer to do this. |
| `--uninstall-first cannot run in the cluster` | The Job would delete its own namespace | Run `mas-est uninstall` first, or add `--local`. |
| `Forbidden` in the install log | The Job's permissions were applied by an older CLI, or by an older CLI run against another namespace on the same cluster | Re-run bootstrap, delete the Job, re-run `install`. |
| `[mas-est] FATAL: installer image / CLI skew - refusing to run` | The Job image doesn't match the CLI | Delete the Job. Re-run bootstrap so the CLI and its default image match. |
| `job/mas-est-install in namespace mas-est produced no logs within 15m0s` | The Job pod never started | `oc describe job mas-est-install -n mas-est`. Look for image pull errors or a Pending pod. |
| `CatalogSource mas-iam-operator cannot pull its image`, `... did not report READY within 300s`, or `... has been in TRANSIENT_FAILURE` | The operator catalog image can't be pulled or won't start. The install prints the catalog pod's state above the error. | `oc -n openshift-marketplace get pods -l olm.catalogSource=mas-iam-operator` and `oc describe` the pod. A pull error usually means a blocked registry or a missing pull secret for `quay.io`. |
| `installed operator mas-iam-operator.v<old> is OLDER than the catalog's current ...` | An earlier install left an older operator | Nothing to do: the install waits 2 minutes, then removes the old Subscription and CSV and subscribes again. If it then stops with `the MAS EST IAM operator is stuck on ...`, run the two `oc delete` commands it prints and re-run. |
| `the MAS EST IAM operator CSV ... has been Failed` | OLM couldn't run the operator | The CSV message is in the error. `oc get csv,installplan -n mas-est`. |
| `job/<name> in namespace mas-est failed; see the condition, pod states and logs above` | A setup Job failed. The pod logs are printed just above. | Fix the cause shown in the logs, then re-run `install`. |
| `job/mas-est-iam-ldap-config ... is still the Failed Job from an earlier run` | The Keycloak LDAP setup hook failed on an earlier run. The operator only re-runs it on a Helm upgrade. | `oc delete job mas-est-iam-ldap-config -n mas-est` and re-run `install`. If the Job doesn't come back, `mas-est uninstall` and install again. |
| `timed out waiting for the condition on jobs/scim-bridge-keycloak-route-cert`, and its pod shows `ImagePullBackOff` for `registry.redhat.io/openshift4/ose-cli` | CLI v0.1.11 or earlier. Red Hat no longer serves that image untagged. | Upgrade to v0.1.12 or later, then `mas-est uninstall` and install again. |
| `command terminated with exit code 1` right after `Seeding 6 grouped demo users`, and the Keycloak log shows `Connection refused` for LDAP | CLI v0.1.12 or earlier. OpenLDAP was still generating its TLS parameters on first start, which can take several minutes. | Upgrade to v0.1.13 or later, which waits for LDAP. Then `mas-est uninstall` and install again. |
| `timed out waiting for the MAS EST IAM operator CSV to reach Succeeded` | OLM didn't install the operator | `oc get csv,sub,installplan -n mas-est`. Look for image pull or resolution errors. |
| `every StorageClass uses kubernetes.io/no-provisioner; new PVCs will not bind` | The cluster has only static storage classes | Create a dynamically provisioned class (ODF RBD, LVMS, or your cloud's CSI), then install with `--storage-class`. |
| A warning that a PVC already uses a different storage class than the one you passed | Storage classes on existing PVCs can't change | The install keeps the existing class. To change it, `mas-est uninstall` and install again. |
| `deployment "mas-est-iam-openldap" exceeded its progress deadline`, or `timed out waiting for <kind>/<name> in namespace mas-est`, and a PVC is `Pending` | The chosen storage class can't provision a volume. A class with provisioner `kubernetes.io/no-provisioner`, such as ODF's `localblock`, never binds a new PVC. | `oc get sc`, pick a dynamic block class such as `ocs-storagecluster-ceph-rbd`, then `mas-est uninstall` and reinstall with `--storage-class` and `--scim-bridge-storage-class` set to it. |
| `timed out waiting 10m0s for <type>/default to become Ready` | MAS didn't reconcile the provider | `oc get pods -n mas-<instance>-core \| grep entitymgr-idpcfg`. If it shows `OOMKilled`, the memory bump didn't apply: check `oc get suite <instance> -n mas-<instance>-core -o jsonpath='{.spec.podTemplates}'`. |
| `setOIDCConfig default: ... MAS does not expose the OIDC external-IdP API` | MAS 9.0 with `oidc` selected | Re-run with `--mas-auth-providers ldap,saml`. |
| `[mas-auth] WARN: linking SCIM users to <idp> failed (non-fatal ...)` | The install couldn't find or reach MongoDB. It looks for MongoDB in `mongoce` and then searches the cluster. | If MongoDB runs elsewhere, set `--mongo-namespace` (and `--mongo-instance`) or `MAS_EST_MONGO_NAMESPACE`, then re-run `mas-est mas-auth apply --mas-instance-id <instance> --self-reg-workspace <workspace>`. External MongoDB (for example on IBM Cloud) can't be linked this way. |

## Login

| Symptom | Cause | Fix |
|---|---|---|
| The wrong user is signed in, or `AIUOM0100E`, after switching provider | Keycloak kept the previous session | Use a private browser window for each login. |
| First SAML login fails with `AIUSC0019I Self Registration configuration either not setup or not enabled` | MAS 9.1.20 loads the self-registration config lazily | Log in again. Don't restart `<instance>-coreidp`; that brings the failure back. |
| `CWIML4537E` or "invalid username or password" for a user Keycloak or LDAP accepted | A MongoDB user record left over from an earlier install | Delete that user's document from `mas_<instance>_core.User` and log in again. |
| Every OIDC login fails after a second `install` or `mas-auth apply`. The `<instance>-coreidp` log shows `CWWKS1708E ... unauthorized_client` | CLI v0.1.15 or earlier generated a new OIDC client secret on every run, and coreidp kept the old one | Upgrade to v0.1.16 or later. To recover now, restart coreidp: `oc rollout restart deploy/<instance>-coreidp -n mas-<instance>-core`. On MAS 9.1.20 the first SAML self-registration after the restart then fails once; log in again. |
| SAML logins fail after rotating the Keycloak realm keys | MAS holds a copy of Keycloak's SAML metadata with the old signing certificate | `mas-est mas-auth apply --mas-instance-id <instance> --self-reg-workspace <workspace> --providers saml` |
| New OIDC users fail with `AIUOM0013E`, existing OIDC users log in, SAML self-registration works. Every OIDC login logs `PROPAGATION_TOKEN_INTERNAL_ERR` in `<instance>-coreidp` | More than one OIDC provider is configured in MAS. With two or more, the MAS OIDC resolver can't match the token to any provider (seen on MAS 9.1.14), so OIDC self-registration never runs | `oc get idpcfg -n mas-<instance>-core`. mas-est configures one OIDC provider. Remove any other OIDC IDPCfg you don't need, or create those users in MAS another way. |
| `AIUOM0024E: User ... has been deactivated` | The user is inactive in MAS, for example after Manage deactivated it | Reactivate the user in MAS **Users**. |
| A user logs in but can't open Manage | Self-registration used the wrong workspace | `oc get cm <instance>-selfreg -n mas-<instance>-core -o yaml`. If `workspaces` shows `workspace`, re-run `mas-est mas-auth apply --mas-instance-id <instance> --self-reg-workspace <workspace>`. |

## SCIM and Manage sync

Start with `mas-est logs --component bridge --tail 300` and `mas-est config view`.

| Symptom or log line | Cause | Fix |
|---|---|---|
| `no users matched filters` or `no users returned from Keycloak` | No user is both in group `mas-scim-users` and named `scim.*` | Fix the user in Keycloak. Both conditions apply. |
| `group "mas-scim-users" not found in realm maximo` | The group was deleted | Recreate it in Keycloak. |
| `MAS returned unauthorized; refreshing token and retrying once`, repeated | The MAS API key was rotated or revoked | `mas-est config set mas-api-token --token-name '<name>' --token-value '<value>'` |
| `MAS create failed` with `status_code=404` | Wrong SCIM URL or profile ID | Compare `SCIM_BRIDGE_MAS_BASE_URL` and `SCIM_BRIDGE_MAS_PROFILE_ID` in `mas-est config view` with MAS. |
| `dry-run create` | The bridge is in dry-run mode | Set `SCIM_BRIDGE_BRIDGE_DRY_RUN` to `false` in `configmap/scim-bridge-config`, then `mas-est restart bridge`. |
| `MAS operation failed; will retry` | A temporary MAS error (5xx, 429, 403, network) | Nothing to do. The bridge retries with backoff, starting at 30 seconds and capped at 30 minutes. Bridge v0.1.2 and earlier logged `skip update due to prior MAS error` and never retried; upgrade the bridge. |
| `parking entry: not retrying until the Keycloak user, MAS profile or bridge config changes` | MAS rejected the user three times with a 400, 404 or 422 | Fix the cause in the log's `last_error`, then change anything on the Keycloak user (for example its first name) to trigger an immediate retry. |
| `MAS user conflict: existing user is not bridge-owned; parking entry` | A MAS user with the same username already exists and wasn't created by the bridge | Delete or rename the existing MAS user, or rename the Keycloak user. Then change anything on the Keycloak user to retry. |
| `poll cycle failed; will retry` | Keycloak or the state file was unavailable | Nothing to do if `poll cycle recovered` follows. If not, `mas-est logs --component keycloak`. The bridge's `/healthz` fails after 5 missed cycles, and OpenShift restarts it. |
| A SCIM user can't log in, or has only a `_local` identity in MAS | Created in Keycloak after the install, so never linked | `mas-est mas-auth apply --mas-instance-id <instance> --self-reg-workspace <workspace>` |
| The user is in MAS but not in Manage, and `entitlement.application` is `NONE` | The user was created before the SCIM profile existed. Fixed for new installs in v0.1.7. | Set the user's entitlement and workspace in MAS, then set `sync.status` to `PENDING`. |
| Manage sync fails with `AIUI1101E: 500` | A leftover `MAXIMO.PERSONANCESTOR` row | Delete the row for that `PERSONID`, then set the user's `sync.status` and `applications.manage.sync.state` to `PENDING`. |
| Manage sync fails with `BMXAA10249E ... DEFLTREG security group` | A leftover `MAXIMO.GROUPUSER` row. The message is misleading. | Delete that user's `GROUPUSER` rows, then set `sync.status` to `PENDING`. |

## S3 and doclinks

| Symptom | Cause | Fix |
|---|---|---|
| `Install MinIO S3` times out, and pod `mas-minio` shows `ImagePullBackOff` with `quay.io/minio/minio: unauthorized` | CLI v0.1.14 or earlier. MinIO stopped serving its images in September 2026. | Upgrade to v0.1.15 or later, then run `mas-est install` again. On an older CLI, run `mas-est object-storage install-minio --mas-instance-id <instance> --image docker.io/pgsty/minio:RELEASE.2026-08-04T00-00-00Z --mc-image docker.io/pgsty/mc:RELEASE.2026-09-16T00-00-00Z`. |
| `SignatureDoesNotMatch` on every attachment | `mxe.cosregion` isn't set | Add it as `us-east-1`. See [Connect S3 to Manage](GUIDE.md#connect-s3-to-manage). |
| HTTP 503 or a certificate error on upload | `mxe.cosendpointuri` points at the MinIO route | Set it to `http://<namespace>.svc.cluster.local:9000`, which is `http://mas-est.svc.cluster.local:9000` in the default namespace. `mas-est` here is the namespace, not the service: Manage reaches the bucket at `<bucket>.<namespace>.svc`. |
| `BMXAA4195E` on upload | Wrong bucket, key or endpoint | Check every property in [Connect S3 to Manage](GUIDE.md#connect-s3-to-manage). |
| `SignatureDoesNotMatch` from the `LOADFLATOBJECT` cron task while attachments work | That cron task ignores the region | Harmless. Disable the cron task to silence it. |
| `ObjectStorageCfg` not `Ready` | MinIO didn't finish starting | `mas-est logs --component minio` and `--component minio-init`. |

## Collect evidence

```bash
mas-est support-bundle
```

Attach the directory it creates when reporting a problem.
