# SCIM bridge deployment (sample)

This sample Dockerfile and manifest run the bridge with a PVC-backed state file at `/var/lib/scim-bridge/state.json`.

## Quick workflow (iam namespace)

### Option A: published manifest (recommended for operators)

1) Apply the published manifest (from a release tag):
   ```bash
   oc apply -f https://raw.githubusercontent.com/<org>/<repo>/<tag>/manifests/scim-bridge-install.yaml
   ```
2) Edit `scim-bridge-secret` + `scim-bridge-config` in the OpenShift console with your MAS and Keycloak values.
3) Restart the bridge:
   ```bash
   oc rollout restart deployment/scim-bridge -n iam
   ```
4) Re-run the Keycloak bootstrap Job after changing the client secret:
   ```bash
   oc delete job/scim-bridge-keycloak-bootstrap -n iam --ignore-not-found
   oc apply -f https://raw.githubusercontent.com/<org>/<repo>/<tag>/manifests/scim-bridge-install.yaml
   ```

### Option B: repo render + scripts (dev / maintainer flow)

1) From the repo root, copy the env template and fill values (image, Keycloak, MAS, profile):
   ```bash
   cp env/scim-bridge.env.example env/scim-bridge.env.local
   $EDITOR env/scim-bridge.env.local
   ```
   You can override any variable per run by exporting it before calling the scripts. Optional: set `SCIM_BRIDGE_MAS_PROFILE_MAP` / `SCIM_BRIDGE_MAS_PROFILE_MAP_JSON` to route users by `masProfile` attribute (e.g., `users=test1,management=mgmt1`). In strict environments set `SCIM_BRIDGE_MAS_PROFILE_REQUIRE_LABEL=true` to skip users without a mapped label.
2) Build and push the image (uses `SCIM_BRIDGE_IMAGE` from the env file):
   ```bash
   ./scripts/scim-bridge-01-build-image.sh
   ```
3) Deploy (renders `manifests/scim-bridge.yaml` with envsubst and applies to `SCIM_BRIDGE_NAMESPACE`, default `iam`):
   ```bash
   ./scripts/scim-bridge-02-deploy.sh
   ```
   By default, this also applies a one-shot Keycloak bootstrap Job (`manifests/scim-bridge-keycloak-bootstrap.yaml`) that creates/updates the `scim-admin` client in-cluster using `kcadm.sh`. Most users should not need to run `scripts/configure-scim-client.sh`.
4) Verify Deployment + MAS SCIM connectivity:
   ```bash
   ./scripts/scim-bridge-03-verify.sh
   ```

### Publishing the manifest (maintainers)

To generate the release-ready manifest referenced above:

```bash
SCIM_BRIDGE_ENV_FILE=env/scim-bridge.env.release \
SCIM_BRIDGE_OUTPUT=manifests/scim-bridge-install.yaml \
./scripts/scim-bridge-04-render-install-manifest.sh
```

Commit `manifests/scim-bridge-install.yaml` (or attach it to the release) after setting the correct Quay image tag and defaults in the env file.

The Deployment runs the bridge with filesystem state enabled; the PVC `scim-bridge-state` mounts at `/var/lib/scim-bridge` so the correlation file persists across pod restarts. Overrides via `SCIM_BRIDGE_*` env vars align with the bridge CLI flags and can be set either in the env file or directly in the shell.

### Setting `masProfile` in Keycloak

- Add a user attribute `masProfile` on each Keycloak user you want to route to a non-default MAS SCIM profile (Users → user → Attributes).
- Labels are mapped via `SCIM_BRIDGE_MAS_PROFILE_MAP`/`JSON` to MAS profile IDs. Missing/unmapped labels fall back to `SCIM_BRIDGE_MAS_PROFILE_ID` unless `SCIM_BRIDGE_MAS_PROFILE_REQUIRE_LABEL=true`, in which case the user is skipped.
- If the stored state entry’s `profileID` differs from the derived mapping, the bridge marks the entry `status="error"` so operators can clean it up before continuing.

### Polling interval and run-once mode

- `SCIM_BRIDGE_BRIDGE_POLL_INTERVAL` / `--bridge-poll-interval` accepts standard Go durations (e.g., `30s`, `1m`, `5m`). Recommended: dev 30–60s; prod 5–10m.
- `SCIM_BRIDGE_BRIDGE_MODE=run-once` performs a single reconciliation pass and exits (non-zero on critical errors). Useful for cron-driven backfills; omit in long-running deployments to keep polling.

### State backfill mode

- Before enabling continuous polling against an existing MAS tenant, you can seed the state with current MAS IDs via:
  ```bash
  SCIM_BRIDGE_BRIDGE_MODE=backfill ./cmd/scim-bridge ...
  ```
- Backfill performs one pass: it resolves each user’s MAS profile, searches MAS SCIM (externalId then userName), and writes MAS IDs/profileIDs with status=ok into the state store. Missing or ambiguous matches are marked status=error so operators can clean them up manually.

### TLS and CA bundles

- For production, mount a CA bundle into the pod (for example, Secret/ConfigMap `scim-bridge-ca`) at `/etc/scim-bridge/certs` and set:
  - `SCIM_BRIDGE_MAS_CA_FILE=/etc/scim-bridge/certs/mas-ca.crt`
  - `SCIM_BRIDGE_KEYCLOAK_CA_FILE=/etc/scim-bridge/certs/keycloak-ca.crt`
- `SCIM_BRIDGE_MAS_INSECURE_SKIP_VERIFY` and `SCIM_BRIDGE_KEYCLOAK_INSECURE_SKIP_VERIFY` are **dev-only**. Prefer trusted CAs in prod.
- If your image is in a private registry, set `SCIM_BRIDGE_IMAGE_PULL_SECRETS` to a YAML list (default `[]`) to populate `imagePullSecrets` on the Deployment.

### Day‑2 changes (Secret/ConfigMap)

For inexperienced users, the recommended workflow is editing resources in the OpenShift console:

- `Secret/scim-bridge-secret`:
  - `SCIM_BRIDGE_MAS_API_TOKEN_NAME` / `SCIM_BRIDGE_MAS_API_TOKEN_VALUE`
  - `SCIM_BRIDGE_KEYCLOAK_CLIENT_ID` / `SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET`
- `ConfigMap/scim-bridge-config`:
  - `SCIM_BRIDGE_KEYCLOAK_BASE_URL` / `SCIM_BRIDGE_KEYCLOAK_REALM`
  - `SCIM_BRIDGE_MAS_BASE_URL`
  - `SCIM_BRIDGE_MAS_PROFILE_ID` / `SCIM_BRIDGE_MAS_PROFILE_MAP`

After updating values, restart the bridge:

```bash
oc rollout restart deployment/scim-bridge -n iam
```

If you rotated `SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET`, re-run the Keycloak bootstrap Job so Keycloak is updated to match:

```bash
oc delete job/scim-bridge-keycloak-bootstrap -n iam --ignore-not-found
./scripts/scim-bridge-02-deploy.sh
```
