# SCIM Bridge Status Log

## 2026-01-06

### Actions
- Deleted namespace `iam-scim` (cleanup of old deployment).
- Re-ran `./scripts/scim-bridge-02-deploy.sh` with Job-based Keycloak bootstrap enabled.
- Fixed bootstrap Job template and `scripts/configure-scim-client.sh` to avoid `awk` (not present in the Keycloak image).
- Bootstrap Job completed; Keycloak client and roles verified.

### Verification
- Keycloak client `scim-admin` exists in realm `maximo`.
- Roles `scim-access` and `scim-managed` exist.
- `service-account-scim-admin` has `scim-access`.

### Current status
- `scim-bridge` Deployment is `1/1` Running in `iam`.
- Pod logs/exec on some nodes fail with kubelet TLS errors (`remote error: tls: internal error`).

### Next steps
1) Fix kubelet TLS access on worker nodes so `oc logs`/`oc exec` work consistently.
2) Run `./scripts/scim-bridge-03-verify.sh` to confirm MAS SCIM list calls succeed.
3) Replace `*_INSECURE_SKIP_VERIFY=true` with CA bundles using `scim-bridge-ca` and set:
   - `SCIM_BRIDGE_MAS_CA_FILE`
   - `SCIM_BRIDGE_KEYCLOAK_CA_FILE`
4) Confirm MAS SCIM profile IDs exist and update `SCIM_BRIDGE_MAS_PROFILE_ID` / `SCIM_BRIDGE_MAS_PROFILE_MAP` as needed.
5) Publish a stable bridge image tag and update `SCIM_BRIDGE_IMAGE` for coworkers.

## 2026-01-06 (later)

### Findings
- `scim-bridge` pods are failing to start due to PVC mount errors:
  - `MountVolume.MountDevice failed... driver name rook-ceph.rbd.csi.ceph.com not found in the list of registered CSI drivers`.
- `rook-ceph` namespace does not have CSI driver pods (no `rbdplugin`/`cephfsplugin` DaemonSets running).
- `CephCluster/rook-ceph` is `Progressing` with error:
  - missing `CephConnection` CRD (`no matches for kind "CephConnection" in version "csi.ceph.io/v1"`).

### Impact
- The `scim-bridge-state` PVC cannot mount, so `scim-bridge` pods stay in `ContainerCreating`/`CrashLoopBackOff`.

### Recommended fix
1) Restore the rook-ceph CSI CRDs/drivers (verify `CephConnection` CRD exists) so the RBD CSI plugins run.
2) Confirm `rook-ceph` CSI DaemonSets are running (e.g., `rook-ceph-csi-rbdplugin`, `rook-ceph-csi-cephfsplugin`).
3) Once CSI is healthy, delete the pending `scim-bridge` pods so they reattach the PVC and start.

### Recovery actions taken
- Applied Rook Ceph CSI operator manifest: `https://raw.githubusercontent.com/rook/rook/master/deploy/examples/csi-operator.yaml`.
- Restarted `rook-ceph-operator`.
- Granted `rook-ceph-csi` SCC to ceph-csi service accounts.
- Confirmed CSI Driver CRs and nodeplugin DaemonSets created.
- Deleted stuck `scim-bridge` pods so the PVC could reattach.

### Current status
- `scim-bridge` PVC attaches and the pod can start, but it exits with:
  - `obtain MAS token: auth request failed: 500 INTERNAL SERVER ERROR`.
- MAS `/v1/authenticate` is returning 500 (also seen in `./scripts/scim-bridge-03-verify.sh`).

## 2026-01-12

### Actions
- Created `scim-bridge-ca` Secret from `openshift-config-managed/default-ingress-cert` (`ca-bundle.crt`).
- Set `SCIM_BRIDGE_KEYCLOAK_CA_FILE=/etc/scim-bridge/certs/keycloak-ca.crt`.
- Verified the CA bundle succeeds from an in-cluster debug pod (`curl --cacert ...` to Keycloak route).
- Switched `SCIM_BRIDGE_KEYCLOAK_BASE_URL` to the internal service `http://mas-iam-sample:8080`.

### Findings
- TLS verification to the Keycloak route still failed in the bridge despite the CA bundle.
- The bridge now runs when using the internal service URL.

### Current status
- `scim-bridge` pod is `Running`.
- Logs show 409 conflicts for existing users (expected when users already exist in MAS).

### Next steps
1) Decide whether to keep the internal Keycloak service URL or re-test route TLS with a re-issued ingress cert.
2) If the route is required, confirm why the bridge is not honoring the CA bundle despite in-cluster verification.

## 2026-01-13

### Actions
- Generated a custom route certificate for `mas-iam-sample-iam.apps.masapmonitor.cp.fyre.ibm.com` signed by a new local CA (`mas-iam-route-ca`).
- Patched the `mas-iam-sample` Route to use the custom cert (edge termination).
- Updated `scim-bridge-ca` with the new CA.
- Set `SSL_CERT_FILE` and `SSL_CERT_DIR` on the `scim-bridge` Deployment to ensure Go trusts the mounted CA bundle.
- Restored `SCIM_BRIDGE_KEYCLOAK_BASE_URL` to the https route and restarted the bridge.
- Added a manifest-driven route certificate Job and wired it into `scim-bridge-02-deploy.sh` for automated TLS.

### Current status
- `scim-bridge` pod is `Running`.
- Keycloak TLS verification succeeds without disabling verification.

## 2026-01-13 (later)

### Actions
- Deleted `scim-bridge` resources (configmap/secret/PVC/deploy) to simulate a fresh install.
- Added a dedicated Keycloak Route TLS bootstrap Job that creates its own Route instead of patching the operator-managed one.
- Updated the deploy script to derive the custom Route host and set `SCIM_BRIDGE_KEYCLOAK_BASE_URL` accordingly.

### Findings
- The Keycloak Route TLS Job failed when attempting to patch the operator-managed Route; fields `spec.tls.certificate`/`key` are immutable on that Route.
- The new Job version creates a dedicated Route (`scim-bridge-keycloak`) and avoids touching the operator-managed Route.
- Cluster API DNS lookup started failing (`api.masapmonitor.cp.fyre.ibm.com: no such host`), so re-deploy verification paused.

### Next steps
1) Re-run `./scripts/scim-bridge-02-deploy.sh` once API connectivity returns.
2) Confirm Job success: `oc get job -n iam scim-bridge-keycloak-route-cert`.
3) Verify `scim-bridge` pod starts and can authenticate to Keycloak over the custom Route.

## 2026-01-15

### Actions
- Reset the `iam` namespace to simulate a fresh install (secrets cleared, OpenLDAP TLS re-created, Keycloak DB reset).
- Fixed the Keycloak bootstrap Job and `configure-scim-client.sh` to avoid `awk`/pipefail issues and use the service account ID for role assignment.
- Added `realm-management` role grants (`view-users`, `query-users`, `query-groups`) to the SCIM service account.
- Added a Keycloak list-users fallback when the admin API rejects `max > 1`.
- Rebuilt/pushed `quay.io/lee_forster/mas-iam-operator:scim-bridge-dev` and redeployed the bridge.

### Findings
- Keycloak admin API returns `400 Cannot parse the JSON` for `users?max>1`; `max=1` succeeds. The bridge now falls back to `max=1`.

### Current status
- Keycloak bootstrap Job completes and the `scim-admin` client can list users via the admin API.
- `scim-bridge` pod is running; logs show planned create actions but MAS returns conflicts for existing users.

### Next steps
1) Verify MAS SCIM profile IDs and token values in `scim-bridge-secret` / `scim-bridge-config`.
2) Run `./scripts/scim-bridge-03-verify.sh` and resolve any MAS-side conflicts.
3) Publish a stable image tag for coworkers and update `SCIM_BRIDGE_IMAGE`.
