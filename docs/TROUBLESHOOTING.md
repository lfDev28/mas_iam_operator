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

## Install

The install log is at `mas-est logs --component install-job`.

| Error | Cause | Fix |
|---|---|---|
| `job/mas-est-install already exists in namespace mas-est and has already finished` | A previous Job is still there | `oc delete job mas-est-install -n mas-est`, then re-run. Interactive installs offer to do this. |
| `--uninstall-first cannot run in the cluster` | The Job would delete its own namespace | Run `mas-est uninstall` first, or add `--local`. |
| `Forbidden` in the install log | The Job's permissions were applied by an older CLI | Re-run bootstrap, delete the Job, re-run `install`. |
| `[mas-est] FATAL: installer image / CLI skew - refusing to run` | The Job image doesn't match the CLI | Delete the Job. Re-run bootstrap so the CLI and its default image match. |
| `job/mas-est-install in namespace mas-est produced no logs within 15m0s` | The Job pod never started | `oc describe job mas-est-install -n mas-est`. Look for image pull errors or a Pending pod. |
| `CatalogSource mas-iam-operator did not report READY within 5m` | The operator catalog image can't be pulled | `oc get catalogsource mas-iam-operator -n openshift-marketplace -o yaml` and check the status. |
| `timed out waiting for the condition on jobs/scim-bridge-keycloak-route-cert`, and its pod shows `ImagePullBackOff` for `registry.redhat.io/openshift4/ose-cli` | CLI v0.1.11 or earlier. Red Hat no longer serves that image untagged. | Upgrade to v0.1.12 or later, then `mas-est uninstall` and install again. |
| `command terminated with exit code 1` right after `Seeding 6 grouped demo users`, and the Keycloak log shows `Connection refused` for LDAP | CLI v0.1.12 or earlier. OpenLDAP was still generating its TLS parameters on first start, which can take several minutes. | Upgrade to v0.1.13 or later, which waits for LDAP. Then `mas-est uninstall` and install again. |
| `timed out waiting for the MAS EST IAM operator CSV to reach Succeeded` | OLM didn't install the operator | `oc get csv,sub,installplan -n mas-est`. Look for image pull or resolution errors. |
| `deployment "mas-est-iam-openldap" exceeded its progress deadline`, or `timed out waiting for <kind>/<name> in namespace mas-est`, and a PVC is `Pending` | The chosen storage class can't provision a volume. A class with provisioner `kubernetes.io/no-provisioner`, such as ODF's `localblock`, never binds a new PVC. | `oc get sc`, pick a dynamic block class such as `ocs-storagecluster-ceph-rbd`, then `mas-est uninstall` and reinstall with `--storage-class` and `--scim-bridge-storage-class` set to it. |
| `timed out waiting 10m0s for <type>/default to become Ready` | MAS didn't reconcile the provider | `oc get pods -n mas-<instance>-core \| grep entitymgr-idpcfg`. If it shows `OOMKilled`, the memory bump didn't apply: check `oc get suite <instance> -n mas-<instance>-core -o jsonpath='{.spec.podTemplates}'`. |
| `setOIDCConfig default: ... MAS does not expose the OIDC external-IdP API` | MAS 9.0 with `oidc` selected | Re-run with `--mas-auth-providers ldap,saml`. |
| `[mas-auth] WARN: link-scim-users-oidc step failed (non-fatal)` | The install couldn't reach MongoDB to link the SCIM users | Re-run `mas-est mas-auth apply --mas-instance-id <instance> --self-reg-workspace <workspace>` once the install finishes. |

## Login

| Symptom | Cause | Fix |
|---|---|---|
| The wrong user is signed in, or `AIUOM0100E`, after switching provider | Keycloak kept the previous session | Use a private browser window for each login. |
| First SAML login fails with `AIUSC0019I Self Registration configuration either not setup or not enabled` | MAS 9.1.20 loads the self-registration config lazily | Log in again. Don't restart `<instance>-coreidp`; that brings the failure back. |
| `CWIML4537E` or "invalid username or password" for a user Keycloak or LDAP accepted | A MongoDB user record left over from an earlier install | Delete that user's document from `mas_<instance>_core.User` and log in again. |
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
| `skip update due to prior MAS error` | An earlier MAS error is stuck in the bridge's state file | Fix the cause shown in `last_error`, then `mas-est restart bridge`. |
| A SCIM user can't log in, or has only a `_local` identity in MAS | Created in Keycloak after the install, so never linked | `mas-est mas-auth apply --mas-instance-id <instance> --self-reg-workspace <workspace>` |
| The user is in MAS but not in Manage, and `entitlement.application` is `NONE` | The user was created before the SCIM profile existed. Fixed for new installs in v0.1.7. | Set the user's entitlement and workspace in MAS, then set `sync.status` to `PENDING`. |
| Manage sync fails with `AIUI1101E: 500` | A leftover `MAXIMO.PERSONANCESTOR` row | Delete the row for that `PERSONID`, then set the user's `sync.status` and `applications.manage.sync.state` to `PENDING`. |
| Manage sync fails with `BMXAA10249E ... DEFLTREG security group` | A leftover `MAXIMO.GROUPUSER` row. The message is misleading. | Delete that user's `GROUPUSER` rows, then set `sync.status` to `PENDING`. |

## S3 and doclinks

| Symptom | Cause | Fix |
|---|---|---|
| `Install MinIO S3` times out, and pod `mas-minio` shows `ImagePullBackOff` with `quay.io/minio/minio: unauthorized` | CLI v0.1.14 or earlier. MinIO stopped serving its images in September 2026. | Upgrade to v0.1.15 or later, then run `mas-est install` again. On an older CLI, run `mas-est object-storage install-minio --mas-instance-id <instance> --image docker.io/pgsty/minio:RELEASE.2026-08-04T00-00-00Z --mc-image docker.io/pgsty/mc:RELEASE.2026-09-16T00-00-00Z`. |
| `SignatureDoesNotMatch` on every attachment | `mxe.cosregion` isn't set | Add it as `us-east-1`. See [Connect S3 to Manage](GUIDE.md#connect-s3-to-manage). |
| HTTP 503 or a certificate error on upload | `mxe.cosendpointuri` points at the MinIO route | Set it to `http://mas-est.svc.cluster.local:9000`. |
| `BMXAA4195E` on upload | Wrong bucket, key or endpoint | Check every property in [Connect S3 to Manage](GUIDE.md#connect-s3-to-manage). |
| `SignatureDoesNotMatch` from the `LOADFLATOBJECT` cron task while attachments work | That cron task ignores the region | Harmless. Disable the cron task to silence it. |
| `ObjectStorageCfg` not `Ready` | MinIO didn't finish starting | `mas-est logs --component minio` and `--component minio-init`. |

## Collect evidence

```bash
mas-est support-bundle
```

Attach the directory it creates when reporting a problem.
