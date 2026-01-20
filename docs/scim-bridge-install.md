# Keycloak → MAS SCIM Bridge – Install & Configuration Guide

**Audience:** MAS / OpenShift admins who want to sync Keycloak users into MAS via SCIM, using this repo’s bridge.  
**Prereqs:** You have a running MAS instance, a Keycloak realm for MAS (for example, `maximo`), and cluster access to OpenShift.

This guide focuses on the “happy path” for inexperienced OpenShift users:

- You **apply** a manifest (via the provided script).
- You **fill in values** in a single Secret (MAS + Keycloak credentials).
- You set a simple Keycloak user attribute (`masProfile`) to choose which MAS SCIM profile each user should land in.

---

## 1. Prerequisites and terminology

- **MAS instance**
  - You know your MAS API base domain, for example:
    - `https://api.<mas-instance-id>.<domain>`
  - You can log into MAS as an admin and create **API keys**.
  - You can call MAS APIs (for example, via Postman or curl).

- **Keycloak**
  - You know which realm MAS uses (in this repo, `maximo`).
  - You can edit **user attributes** (for the `masProfile` label).
  - (Optional/advanced) You can create a confidential client in Keycloak if you choose to manage it manually.

- **OpenShift**
  - You have `oc` access to the cluster where MAS is running.
  - You know (or can create) a project/namespace for the bridge:
    - This guide assumes `iam`.

- **SCIM profiles in MAS**
  - MAS uses a **SCIM profile** object to decide what happens when a user is created via SCIM:
    - Default entitlements.
    - Default workspaces and applications.
  - The bridge needs to know **which profile ID** to use per user.
  - We will mark users in Keycloak with an attribute `masProfile` (for example, `users`, `management`) and map those labels to MAS profile IDs (for example, `users → test1`, `management → mgmt1`).

Relevant IBM documentation (MAS SCIM 2.0):  
https://www.ibm.com/docs/en/masv-and-l/cd?topic=synchronization-user-scim-20

---

## 2. MAS side – create SCIM profile(s) and API credentials

### 2.1 Create a MAS API key

1. Log in to MAS as an admin.
2. Create an **API key** that the bridge can use to obtain a SCIM token:
   - Note the **API key name** (for example, `a-my-mas-scim-key`).
   - Note the **API key value** (long secret string).
3. This pair will later be used in OpenShift Secret fields:
   - `SCIM_BRIDGE_MAS_API_TOKEN_NAME`
   - `SCIM_BRIDGE_MAS_API_TOKEN_VALUE`

The bridge will use these to call MAS `/v1/authenticate`, receive a JWT, and then call the SCIM endpoints.

### 2.2 Create one or more MAS SCIM profiles

Follow the IBM docs for SCIM profiles:  
https://www.ibm.com/docs/en/masv-and-l/cd?topic=synchronization-user-scim-20

Key ideas:

- MAS exposes a SCIM Profile API at:
  - `POST https://api.<mas-instance-id>.<domain>/scim/v2/Profiles`
- A profile roughly corresponds to a **template** for new SCIM users/groups:
  - Identity configuration (local / SAML / LDAP).
  - Default entitlements (`NONE`, `SELF_SERVICE`, `LIMITED`, `BASE`, `PREMIUM`).
  - Workspaces and applications they get on first sync.

Example profile body (from IBM docs, simplified):

```json
{
  "id": "test1",
  "version": 1,
  "identities": [
    { "type": "local" }
  ],
  "entitlement": {
    "application": "BASE"
  },
  "workspaces": [
    {
      "id": "workspace1",
      "applications": ["manage"]
    }
  ]
}
```

- Call `POST /scim/v2/Profiles` with a body like the above to create a profile.
- The important piece for the bridge is the **profile `id`** (for example, `test1`).
  - You can create multiple profiles (for example, `test1` for normal users, `mgmt1` for managers).

Later, you will map Keycloak’s `masProfile` labels to these MAS profile IDs.

---

## 3. Keycloak side – realm, SCIM client, and `masProfile` attribute

### 3.1 SCIM client bootstrap (automatic)

Assuming MAS is already integrated with Keycloak (for example, realm `maximo`), the SCIM bridge install flow bootstraps the Keycloak service client automatically by applying a one-shot Kubernetes Job.

By default, `./scripts/scim-bridge-02-deploy.sh` will:

- Create/update `Secret/scim-bridge-secret` with `SCIM_BRIDGE_KEYCLOAK_CLIENT_ID` and `SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET`.
- Apply `manifests/scim-bridge-keycloak-bootstrap.yaml` in the Keycloak namespace, using the existing `<release>-bootstrap-admin` Secret.
- Create/update:
  - Keycloak roles `scim-access` and `scim-managed` in the MAS realm.
  - A confidential client (default `scim-admin`) with service accounts enabled and the provided secret.
  - The `scim-access` role assignment to `service-account-scim-admin`.

Normal users should not need to touch `kcadm.sh` or run `scripts/configure-scim-client.sh`.

### 3.2 Add `masProfile` user attribute

To let the bridge route users into different MAS SCIM profiles, set `masProfile` on the users you care about:

1. In Keycloak Admin Console:
   - Realm: `maximo`
   - Go to **Users**, select a user.
2. Open the **Attributes** tab.
3. Add a new attribute:
   - Key: `masProfile`
   - Value: for example, `users` or `management`
4. Save.
5. Repeat for other users, or configure mappers if you want to populate it from an external IdP.

Later, the bridge will map:

- `masProfile=users` → MAS profile ID `test1`
- `masProfile=management` → MAS profile ID `mgmt1`

using configuration, not hardcoded logic.

### 3.3 OpenLDAP reference (for MAS external registry setup)

If you are wiring MAS to an external LDAP registry for identity sync, the sample OpenLDAP deployed by the MAS IAM stack uses:

- **LDAP URL:** `ldaps://mas-iam-sample-openldap.iam.svc.cluster.local:636`
- **Base DN:** `dc=demo,dc=local`
- **Users DN:** `ou=users,dc=demo,dc=local`
- **Bind DN:** `cn=admin,dc=demo,dc=local`
- **Bind password:** stored in `Secret/mas-iam-sample-openldap-admin` (key `password`)

If you need the CA bundle for the OpenLDAP TLS cert, it is stored in the OpenLDAP TLS Secret used by the Keycloak/LDAP integration (for example, `Secret/mas-iam-sample-keycloak-openldap-tls` in the IAM namespace).

---

## 4. Install the SCIM bridge on OpenShift

This repo ships:

- A manifest template: `manifests/scim-bridge.yaml`
- A Keycloak bootstrap Job template: `manifests/scim-bridge-keycloak-bootstrap.yaml`
- Helper scripts in `scripts/`
- Env template: `env/scim-bridge.env.example`

There are two supported install flows:

- **Recommended for operators:** apply a published, fully‑rendered manifest (raw URL) and then edit Secrets/ConfigMaps in the OpenShift console.
- **Dev/maintainer flow:** render from `env/scim-bridge.env.local` and apply via the repo scripts.

### 4.0 Install via published manifest (recommended)

1) Apply the published manifest from a tag/release:

```bash
oc apply -f https://raw.githubusercontent.com/<org>/<repo>/<tag>/manifests/scim-bridge-install.yaml
```

If the image is in a private registry, create an image pull secret and add it to the Deployment (`spec.template.spec.imagePullSecrets`) before starting the pod.

2) In the OpenShift console, edit:

- `Secret/scim-bridge-secret`
  - `SCIM_BRIDGE_MAS_API_TOKEN_NAME`
  - `SCIM_BRIDGE_MAS_API_TOKEN_VALUE`
  - `SCIM_BRIDGE_KEYCLOAK_CLIENT_ID` (optional override; default `scim-admin`)
  - `SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET`
- `ConfigMap/scim-bridge-config`
  - `SCIM_BRIDGE_KEYCLOAK_BASE_URL`
  - `SCIM_BRIDGE_KEYCLOAK_REALM`
  - `SCIM_BRIDGE_MAS_BASE_URL`
  - `SCIM_BRIDGE_MAS_PROFILE_ID`
  - `SCIM_BRIDGE_MAS_PROFILE_MAP` (optional)
- `PersistentVolumeClaim/scim-bridge-state`
  - Ensure `storageClassName` matches your cluster’s default (the published manifest uses `CHANGEME` as a placeholder).

3) Restart the bridge:

```bash
oc rollout restart deployment/scim-bridge -n iam
```

4) Re‑run the Keycloak bootstrap Job (so the client secret is updated in Keycloak):

```bash
oc delete job/scim-bridge-keycloak-bootstrap -n iam --ignore-not-found
oc apply -f https://raw.githubusercontent.com/<org>/<repo>/<tag>/manifests/scim-bridge-install.yaml
```

> Maintainers publish the rendered manifest using `scripts/scim-bridge-04-render-install-manifest.sh`. See `agents.md` for the release/publishing checklist.

### 4.1 Install from repo (dev / maintainer flow)

On your workstation:

```bash
git clone <this-repo-url>
cd mas-iam
cp env/scim-bridge.env.example env/scim-bridge.env.local
$EDITOR env/scim-bridge.env.local
```

Edit at least:

- Image / namespace:
  - `SCIM_BRIDGE_IMAGE` – default can be a public image you publish (e.g. `quay.io/your-org/scim-bridge:<tag>`).
  - `SCIM_BRIDGE_NAMESPACE=iam` – or your chosen namespace.
- Keycloak:
  - `SCIM_BRIDGE_KEYCLOAK_BASE_URL`
  - `SCIM_BRIDGE_KEYCLOAK_REALM`
  - `SCIM_BRIDGE_KEYCLOAK_CLIENT_ID`
  - `SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET`
- MAS SCIM:
  - `SCIM_BRIDGE_MAS_BASE_URL` – e.g. `https://api.<mas-instance>.<domain>/scim/v2`
  - `SCIM_BRIDGE_MAS_PROFILE_ID` – default MAS profile ID (for users with no `masProfile` or when you only use one profile).
  - Optional: `SCIM_BRIDGE_MAS_PROFILE_MAP` – e.g. `users=test1,management=mgmt1`
  - `SCIM_BRIDGE_MAS_AUTH_TYPE=jwt` (if using `/v1/authenticate`).
  - MAS API key credentials:
    - `SCIM_BRIDGE_MAS_API_TOKEN_NAME`
    - `SCIM_BRIDGE_MAS_API_TOKEN_VALUE`
- Bridge behavior:
  - `SCIM_BRIDGE_BRIDGE_MODE=poll`
  - `SCIM_BRIDGE_BRIDGE_POLL_INTERVAL=5m` (e.g. `30s` in dev).
  - `SCIM_BRIDGE_BRIDGE_STATE_BACKEND=filesystem`
  - `SCIM_BRIDGE_BRIDGE_STATE_PATH=/var/lib/scim-bridge/state.json`

You can leave the more advanced options at their defaults unless you have specific needs. After deployment, you can treat the env file as “seed values” and do day‑2 edits in the OpenShift console via the `scim-bridge-secret` and `scim-bridge-config` resources.

### 4.2 Apply to OpenShift (one command)

Log into the OpenShift cluster with `oc`, then run:

```bash
./scripts/scim-bridge-02-deploy.sh
```

This script will:

- Read `env/scim-bridge.env.local`.
- Render `manifests/scim-bridge.yaml` with those values.
- Render/apply `manifests/scim-bridge-keycloak-bootstrap.yaml` (Keycloak `scim-admin` bootstrap Job) when `SCIM_BRIDGE_PROVISION_KEYCLOAK=true` (default).
- Create/update:
  - `ConfigMap scim-bridge-config`
  - `Secret scim-bridge-secret`
  - `PVC scim-bridge-state`
  - `Deployment scim-bridge`
  - `Job scim-bridge-keycloak-bootstrap` (in the Keycloak namespace)
  - In the namespace `SCIM_BRIDGE_NAMESPACE` (default `iam`).

After it completes:

```bash
oc get pods -n iam
oc logs deploy/scim-bridge -n iam --tail=50
```

You should see the bridge start and log that it is polling Keycloak and MAS.

### 4.3 What to edit in OpenShift (day‑2)

For most users, the primary ongoing workflow is editing these resources in the OpenShift console (and restarting the Deployment afterward):

- `Secret/scim-bridge-secret`:
  - `SCIM_BRIDGE_MAS_API_TOKEN_NAME`
  - `SCIM_BRIDGE_MAS_API_TOKEN_VALUE`
  - `SCIM_BRIDGE_KEYCLOAK_CLIENT_ID` (optional override; default `scim-admin`)
  - `SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET`
- `ConfigMap/scim-bridge-config`:
  - `SCIM_BRIDGE_KEYCLOAK_BASE_URL`
  - `SCIM_BRIDGE_KEYCLOAK_REALM`
  - `SCIM_BRIDGE_MAS_BASE_URL`
  - `SCIM_BRIDGE_MAS_PROFILE_ID`
  - `SCIM_BRIDGE_MAS_PROFILE_MAP` (optional multi-profile routing; e.g. `users=test1,management=mgmt1`)

Then restart the bridge:

```bash
oc rollout restart deployment/scim-bridge -n iam
```

---

## 5. Managing MAS / Keycloak credentials via Secrets

Once deployed, most day‑to‑day changes are:

- Rotating the MAS API key.
- Rotating the Keycloak client secret.
- Adjusting profile mappings.

You can do this entirely by editing the Secret and ConfigMap.

### 5.1 Using the OpenShift web console

1. Navigate to the `iam` project.
2. Go to **Workloads → Secrets** (or the equivalent section) and open `scim-bridge-secret`.
3. Edit the following fields as needed:
   - `SCIM_BRIDGE_MAS_API_TOKEN_NAME`
   - `SCIM_BRIDGE_MAS_API_TOKEN_VALUE`
   - `SCIM_BRIDGE_KEYCLOAK_CLIENT_ID`
   - `SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET`
4. Save the Secret.
5. Restart the bridge so it picks up the changes:
   ```bash
   oc rollout restart deployment/scim-bridge -n iam
   ```

If you need to **retrieve** the current values from the CLI (for troubleshooting or handoff), use:

```bash
oc get secret scim-bridge-secret -n iam -o jsonpath='{.data.SCIM_BRIDGE_MAS_API_TOKEN_NAME}' | base64 -d; echo
oc get secret scim-bridge-secret -n iam -o jsonpath='{.data.SCIM_BRIDGE_MAS_API_TOKEN_VALUE}' | base64 -d; echo
oc get secret scim-bridge-secret -n iam -o jsonpath='{.data.SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET}' | base64 -d; echo
```

### 5.2 Adjusting profile mappings

- To change which MAS profile a label maps to, edit the `ConfigMap`:
  - `scim-bridge-config` (key `SCIM_BRIDGE_MAS_PROFILE_MAP`).
- For example, to map:
  - `users → test1`
  - `management → mgmt1`
- Set:
  - `SCIM_BRIDGE_MAS_PROFILE_MAP=users=test1,management=mgmt1`
- Then restart the Deployment:
  ```bash
  oc rollout restart deployment/scim-bridge -n iam
  ```

Be careful when changing mappings for users that already exist in MAS: see the main SCIM bridge spec for how profile changes interact with state (`status="error"` vs re-create).

### 5.3 Updating Keycloak / MAS URLs and realm

For non-secret values (URLs, realm, default profile), edit `ConfigMap/scim-bridge-config`:

- `SCIM_BRIDGE_KEYCLOAK_BASE_URL`
- `SCIM_BRIDGE_KEYCLOAK_REALM`
- `SCIM_BRIDGE_MAS_BASE_URL`
- `SCIM_BRIDGE_MAS_PROFILE_ID`

Then restart the Deployment:

```bash
oc rollout restart deployment/scim-bridge -n iam
```

If you changed `SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET`, also re-run the Keycloak bootstrap Job so Keycloak is updated to match:

```bash
oc delete job/scim-bridge-keycloak-bootstrap -n iam --ignore-not-found
./scripts/scim-bridge-02-deploy.sh
```

---

## 6. Verifying end‑to‑end behavior

### 6.1 Basic connectivity

Use the helper script:

```bash
./scripts/scim-bridge-03-verify.sh
```

This will:

- Print the image and environment variables used by the Deployment.
- Show recent logs.
- Call MAS SCIM with the current credentials to list users.

### 6.2 User sync test

1. In Keycloak:
   - Create or pick a test user.
   - Ensure they have an email and are enabled.
   - Set `masProfile` to one of your labels (e.g. `users` or `management`).
2. Wait for the bridge polling interval (or temporarily set a short interval).
3. Check the bridge logs:
   - You should see a plan and action for that user (create or update), with the profile ID resolved.
4. Use MAS SCIM (curl/Postman) or MAS UI:
   - Verify the user appears under the expected MAS profile and has the entitlements/workspaces you configured in that SCIM profile.

---

## 7. Where to learn more about MAS SCIM profiles

For deeper control over:

- Entitlements (NONE, SELF_SERVICE, LIMITED, BASE, PREMIUM).
- Workspace / application assignments.
- Identity mappings (local vs SAML vs LDAP).

see the IBM MAS SCIM 2.0 documentation:  
https://www.ibm.com/docs/en/masv-and-l/cd?topic=synchronization-user-scim-20

In particular:

- The section describing **SCIM profile API endpoints**:  
  - `POST /scim/v2/Profiles`  
  - `GET /scim/v2/Profiles`  
  - `GET /scim/v2/Profiles/{profileId}`  
  - `PUT /scim/v2/Profiles/{profileId}`  
  - `DELETE /scim/v2/Profiles/{profileId}`
- The profile structure:
  - `identities` (local/saml/ldap).
  - `entitlement` block.
  - `workspaces` block.

The SCIM bridge does not care about the internal entitlements/workspaces in the profile; it only needs the profile **ID** and will call:

- `POST /scim/v2/{profileId}/Users`
- `PATCH/PUT /scim/v2/{profileId}/Users/{id}`

You are free to design the profiles to match your MAS access model; just be sure your Keycloak `masProfile` labels map cleanly to the desired MAS profile IDs.

When you create new MAS SCIM profiles, plug those profile IDs into:

- `SCIM_BRIDGE_MAS_PROFILE_ID` (default/fallback)
- `SCIM_BRIDGE_MAS_PROFILE_MAP` (optional per-label mapping from Keycloak `masProfile`)

---

This guide is intentionally opinionated and tries to keep all operator actions to:

- Running a single deploy script (`scim-bridge-02-deploy.sh`).
- Editing the `scim-bridge-secret` and `scim-bridge-config` in OpenShift.
- Setting the `masProfile` attribute on Keycloak users.

Advanced users can still customize images, manifests, and mappings via the underlying YAML and scripts, but the above covers the common install and maintenance tasks.

---

## 8. Automatic Keycloak client bootstrap via manifests

By default, this repo provisions the `scim-admin` client and roles via an in-cluster Kubernetes Job:

- `manifests/scim-bridge-keycloak-bootstrap.yaml` runs once in the Keycloak namespace, logs in with the existing `<release>-bootstrap-admin` Secret, and creates/updates:
  - roles `scim-access` and `scim-managed` in the MAS realm
  - the confidential `scim-admin` client with service accounts enabled and the secret from `scim-bridge-secret`
  - the role assignment to `service-account-scim-admin`
- The Job is idempotent; delete and re-apply the Job to force a re-run after changing the client secret.

### 8.1 Advanced / manual Keycloak setup (optional)

Use this path only if you cannot or do not want to run the in-cluster bootstrap Job (for example: locked-down environments, custom realms/clients, or you prefer managing Keycloak manually).

Options:

- Use the repo script (advanced): `scripts/configure-scim-client.sh` (uses `oc exec` into the Keycloak pod and runs `kcadm.sh`).
  - When deploying with `scripts/scim-bridge-02-deploy.sh`, force this mode with:
    - `SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_METHOD=script`
- Disable provisioning entirely (advanced; you manage everything in Keycloak):
  - `SCIM_BRIDGE_KEYCLOAK_BOOTSTRAP_METHOD=none`
- Manage the client in the Keycloak Admin Console (advanced):
  - Create/update a confidential client (default `scim-admin`) with service accounts enabled in the MAS realm.
  - Ensure the service account has permissions consistent with `scim-access` (and optionally `scim-managed`).

If you choose a manual path, you must keep `Secret/scim-bridge-secret` (`SCIM_BRIDGE_KEYCLOAK_CLIENT_ID` / `SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET`) in sync with what is configured in Keycloak.

---

## 9. TLS and backfill quick notes

### 9.1 TLS / CA bundles (recommended for production)

- Mount a CA bundle into the pod at `/etc/scim-bridge/certs` using `Secret/scim-bridge-ca` or `ConfigMap/scim-bridge-ca` (the Deployment already mounts both if present).
- Set:
  - `SCIM_BRIDGE_MAS_CA_FILE=/etc/scim-bridge/certs/mas-ca.crt`
  - `SCIM_BRIDGE_KEYCLOAK_CA_FILE=/etc/scim-bridge/certs/keycloak-ca.crt`
- `SCIM_BRIDGE_MAS_INSECURE_SKIP_VERIFY=true` and `SCIM_BRIDGE_KEYCLOAK_INSECURE_SKIP_VERIFY=true` are dev-only.

### 9.2 Backfill / run-once (high-level)

- `SCIM_BRIDGE_BRIDGE_MODE=run-once` runs a single reconciliation pass and exits.
- `SCIM_BRIDGE_BRIDGE_MODE=backfill` seeds the state store by searching MAS for existing users before you enable continuous polling.

For details and examples, see `services/scim-bridge/README.md`.
