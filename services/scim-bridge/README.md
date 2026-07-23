# SCIM Provisioning Bridge

SCIM provisioning bridge for Keycloak -> Maximo Application Suite (MAS), with install templates and in-cluster bootstrap jobs.

## Installing on OpenShift (recommended)

If you are installing the bridge into an OpenShift cluster, use the repo-level flow and manifests:

- Single-file runbook (recommended entrypoint): `../../docs/INSTALL-ALL-IN-ONE.md`
- Install guide: `../../docs/scim-bridge-install.md`
- Published manifest (recommended for operators): `manifests/scim-bridge-install.yaml` in the repo or release tag
- Deployment helper (dev/maintainers): `../../scripts/scim-bridge-02-deploy.sh`

The default install path provisions the Keycloak `scim-admin` client in-cluster via a Kubernetes Job (`manifests/scim-bridge-keycloak-bootstrap.yaml`), so normal users should not need to run `kcadm.sh` or `scripts/configure-scim-client.sh`.

## Structure

```
services/scim-bridge/
├── cmd/scim-bridge      # CLI entrypoint
├── internal/            # Non-exported packages (config, adapters)
└── go.mod               # Go module definition
```

## Quick start

```bash
# build (or go run) via repo-level helper
make scim-bridge-build

# supply required config via flags or env vars
cd services/scim-bridge
go run ./cmd/scim-bridge \
  --keycloak-base-url https://keycloak.local \
  --keycloak-client-id demo \
  --keycloak-client-secret secret \
  --mas-base-url https://mas.local/scim/v2 \
  --mas-profile-id provisioned-profile \
  --mas-token mas-api-key

# if you don't have a live Keycloak yet, run with --bridge-dry-run to skip MAS HTTP calls
go run ./cmd/scim-bridge ... --bridge-dry-run
```

## Configuration surface

Every knob is exposed as both a flag and an env var (prefix `SCIM_BRIDGE_`). Examples:

Quick install (OpenShift Template, recommended):

```bash
oc process -f https://raw.githubusercontent.com/<org>/<repo>/main/manifests/scim-bridge-install-template.yaml \
  -p SCIM_BRIDGE_MAS_BASE_URL=https://api.<mas-instance>.<domain>/scim/v2 \
  -p SCIM_BRIDGE_MAS_API_TOKEN_NAME=<your-mas-api-key-name> \
  -p SCIM_BRIDGE_MAS_API_TOKEN_VALUE=<your-mas-api-key-value> \
  -p SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID=<workspace-id> \
| oc apply -f -
```

| Flag | Env var | Description |
| ---- | ------- | ----------- |
| `--keycloak-base-url` | `SCIM_BRIDGE_KEYCLOAK_BASE_URL` | Keycloak Admin REST root used for user/group queries. |
| `--keycloak-client-id` | `SCIM_BRIDGE_KEYCLOAK_CLIENT_ID` | Service client with `view-users`/`view-groups`. |
| `--keycloak-client-secret` | `SCIM_BRIDGE_KEYCLOAK_CLIENT_SECRET` | Secret for the above client. |
| `--keycloak-ca-file` | `SCIM_BRIDGE_KEYCLOAK_CA_FILE` | Optional CA bundle path for Keycloak TLS (mount via volume). |
| `--mas-base-url` | `SCIM_BRIDGE_MAS_BASE_URL` | MAS SCIM endpoint root (`https://api.<mas>/scim/v2`). |
| `--mas-profile-id` | `SCIM_BRIDGE_MAS_PROFILE_ID` | MAS SCIM profile applied to all pushes. |
| `--mas-profile-map` | `SCIM_BRIDGE_MAS_PROFILE_MAP` | Optional comma-separated `masProfile=masProfileID` map (e.g., `users=test1,management=mgmt1`). |
| `--mas-profile-map-json` | `SCIM_BRIDGE_MAS_PROFILE_MAP_JSON` | Optional JSON object with the same mapping shape (e.g., `{"users":"test1","management":"mgmt1"}`). |
| `--mas-profile-require-label` | `SCIM_BRIDGE_MAS_PROFILE_REQUIRE_LABEL` | When `true`, users without a mapped `masProfile` label are skipped (no default fallback). |
| `--mas-token` | `SCIM_BRIDGE_MAS_TOKEN` | API key / JWT with permission to call MAS SCIM APIs. |
| `--mas-ca-file` | `SCIM_BRIDGE_MAS_CA_FILE` | Optional CA bundle path for MAS TLS (mount via volume). |
| `--bridge-mode` | `SCIM_BRIDGE_BRIDGE_MODE` | `poll`, `run-once`, `backfill`, or `hybrid`. |
| `--keycloak-include-federated-users` | `SCIM_BRIDGE_KEYCLOAK_INCLUDE_FEDERATED_USERS` | Include LDAP-federated Keycloak users in sync. Defaults to `false` so LDAP self-reg in MAS isn't shadowed by duplicate SCIM creates. Set to `true` only if you genuinely want both paths to populate MAS. |
| `--bridge-poll-interval` | `SCIM_BRIDGE_BRIDGE_POLL_INTERVAL` | Go duration string (e.g., `30s`, `5m`). Dev: 30–60s; Prod: 5–10m. |
| `--bridge-state-backend` | `SCIM_BRIDGE_BRIDGE_STATE_BACKEND` | `memory` (default) or `filesystem`. |
| `--bridge-state-path` | `SCIM_BRIDGE_BRIDGE_STATE_PATH` | File to persist correlations when backend=`filesystem`. |
| `--bridge-dry-run` | `SCIM_BRIDGE_BRIDGE_DRY_RUN` | Skip MAS HTTP calls, only log + store simulated IDs (default `true`). |
| `--bridge-allow-updates` | `SCIM_BRIDGE_BRIDGE_ALLOW_UPDATES` | When `false`, log update attempts as “updates disabled” and skip MAS PUTs (creates still run). |
| `--bridge-log-level` | `SCIM_BRIDGE_BRIDGE_LOG_LEVEL` | `debug`, `info`, `warn`, or `error` (default `info`). |
| `--bridge-payload-logging` | `SCIM_BRIDGE_BRIDGE_PAYLOAD_LOGGING` | Logs redacted outbound MAS SCIM payloads for support debugging. Keep disabled by default. |
| `--include-usernames` | `SCIM_BRIDGE_INCLUDE_USERNAMES` | Optional comma-separated allowlist of usernames to reconcile. |
| `--include-username-prefix` | `SCIM_BRIDGE_INCLUDE_USERNAME_PREFIX` | Optional prefix filter applied to Keycloak usernames; combined with `--include-usernames` when both are set. |

Filtering knobs are optional; omitting them keeps the current “all users” behavior.

### MAS profile bootstrap job (install manifests)

When deploying via the Kubernetes manifests, you can optionally enable a one-shot
Job that creates a demo MAS SCIM profile using the same MAS credentials stored in
`scim-bridge-secret`. Set these in the ConfigMap before applying:

- `SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ENABLED=true`
- `SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_ID=demo`
- `SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID=<workspace-id>`
- Optional: `SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_JSON=<full profile JSON>`

Default bootstrap payload is local identity plus `manage` workspace app only.
Add SAML identities or extra apps only through `SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_JSON`.

The Job uses `SCIM_BRIDGE_MAS_BASE_URL`, `SCIM_BRIDGE_MAS_AUTH_TYPE`,
`SCIM_BRIDGE_MAS_CA_FILE`, and `SCIM_BRIDGE_MAS_INSECURE_SKIP_VERIFY` for the
API calls. To re-run it, delete the Job and re-apply the manifest.

### MAS profile routing via `masProfile`

- Each Keycloak user may carry a `masProfile` attribute (string) in realm `maximo`. The bridge reads the first value of `attributes.masProfile` returned by the Admin API.
- `SCIM_BRIDGE_MAS_PROFILE_MAP` / `SCIM_BRIDGE_MAS_PROFILE_MAP_JSON` map that label to a concrete MAS SCIM profile ID. Example: `users -> test1`, `management -> mgmt1`.
- Default mode: if `masProfile` is missing or unmapped, the bridge falls back to `SCIM_BRIDGE_MAS_PROFILE_ID`.
- Strict mode: set `SCIM_BRIDGE_MAS_PROFILE_REQUIRE_LABEL=true` to skip users without a mapped label (they remain untouched in MAS).
- If a stored state entry’s `profileID` differs from the derived profile (label → map), the bridge marks the entry `status="error"`, records a mismatch message, and skips further actions until an operator cleans it up.

## Current behavior

- `poll` (default) obtains a client-credentials token, lists a page of users from Keycloak’s Admin REST API, feeds them into the reconciliation planner, and logs whether each user maps to a `create` (no stored MAS ID) or `update` (stored correlation exists). It then either (a) simulates the MAS SCIM call when `--bridge-dry-run` is true (default) or (b) issues real POST/PUT requests against MAS and updates the correlation store with the returned IDs.
- When using `SCIM_BRIDGE_MAS_API_TOKEN_NAME` / `SCIM_BRIDGE_MAS_API_TOKEN_VALUE`, a MAS `401` triggers automatic token refresh (`/v1/authenticate`) and one retry of the current cycle.
- On MAS create conflicts (409), the bridge searches SCIM (`externalId` preferred, `userName` fallback) within the user’s MAS profile and adopts the existing MAS user when exactly one match is found. Zero or multiple matches mark the entry `status="error"` to avoid repeated failures.
- Updates issue SCIM PATCH requests with only the changed attributes (userName, given/family name, primary email, active). When no snapshot is available or MAS returns 404, the bridge falls back to PUT with the full document. No-op diffs are skipped.
- `event` simply logs a warning placeholder; future work will hook up the Keycloak event-listener SPI.
- `hybrid` currently reuses the polling loop until the event path lands.
- `run-once` performs a single poll/plan/execute cycle and exits (non-zero if a critical error occurs). Useful for ad-hoc backfills or cron-style runs.
- `backfill` runs a single pass that searches MAS for existing users (per profile) and seeds the state file with MAS IDs/status=ok. Missing/ambiguous matches are logged and marked status=error to avoid repeated noise. Recommended before enabling long-running poll mode against an existing MAS tenant.
- TLS: Prefer mounting CA bundles and setting `SCIM_BRIDGE_MAS_CA_FILE` / `SCIM_BRIDGE_KEYCLOAK_CA_FILE`. `*_INSECURE_SKIP_VERIFY=true` is dev-only and should not be used in production.

### Talking to a real MAS environment

1. Ensure the MAS SCIM profile referenced by `--mas-profile-id` exists and is configured with the desired entitlements/workspaces.
2. Create an API key or JWT with permissions to call `https://api.<mas>/scim/v2/{profileId}` endpoints.
3. Disable dry-run either via `--bridge-dry-run=false` or `SCIM_BRIDGE_BRIDGE_DRY_RUN=false`.
4. Run the bridge; successful creates will store the MAS-assigned resource ID in the in-memory state store so subsequent iterations emit updates instead of creates.

If MAS is unreachable or returns an error on create, the executor stops and the poll loop exits with the HTTP error message. Update failures from MAS are logged (username, MAS ID, HTTP status, body) and the poll continues with remaining users. For development without MAS access, leave dry-run enabled.

### Persistent correlation store (filesystem backend)

- Keep `memory` while prototyping; every restart will force the bridge to re-create users.
- To retain state across runs, set `--bridge-state-backend=filesystem --bridge-state-path=/path/to/state.json`. The file stores a JSON map of Keycloak IDs to objects containing `masID`, `profileID`, `status`, and an optional `lastError` (legacy string-only maps are read and upgraded in memory).
- The bridge loads the file on startup and writes atomically on every change. Back up / copy the state file if you need to migrate to another environment.
- If the file is missing it will be created automatically; if it contains malformed JSON the bridge exits with an error so you can inspect the contents before proceeding.
- When MAS rejects an update, the filesystem state entry is marked with `status=\"error\"` and the last error message; those users are skipped on subsequent runs until the entry is cleaned up or removed.
- If the derived MAS profile for a user (from `masProfile` mapping) differs from the stored `profileID`, the entry is marked `status=\"error\"` to prevent cross-profile updates. Clean up the MAS user/state entry (or align the attribute) before re-running.

See the spec for the remaining backlog (persistent state backends, Keycloak event listener integration, full reconciliation logic).

## Examples

- Full sync (default behavior): reconcile every user returned by Keycloak with create+update semantics.

  ```bash
  go run ./cmd/scim-bridge \
    --keycloak-base-url https://keycloak.local \
    --keycloak-client-id demo \
    --keycloak-client-secret secret \
    --mas-base-url https://mas.local/scim/v2 \
    --mas-profile-id provisioned-profile \
    --mas-token mas-api-key
  ```

- Create-only seed run: backfill MAS with users lacking a MAS ID while skipping updates until you are ready.

  ```bash
  go run ./cmd/scim-bridge \
    --keycloak-base-url https://keycloak.local \
    --keycloak-client-id demo \
    --keycloak-client-secret secret \
    --mas-base-url https://mas.local/scim/v2 \
    --mas-profile-id provisioned-profile \
    --mas-token mas-api-key \
    --bridge-allow-updates=false
  ```

- Multi-profile routing (users with `masProfile=users` → `test1`, `masProfile=management` → `mgmt1`; others fall back to `test1`):

  ```bash
  go run ./cmd/scim-bridge \
    --keycloak-base-url https://keycloak.local \
    --keycloak-client-id demo \
    --keycloak-client-secret secret \
    --mas-base-url https://mas.local/scim/v2 \
    --mas-profile-id test1 \
    --mas-profile-map users=test1,management=mgmt1 \
    --mas-token mas-api-key
  ```

## Legacy user cleanup

- List MAS users previously marked `status="error"` in the filesystem state (default dry-run):

  ```bash
  go run ./cmd/scim-bridge-cleanup \
    --state-path /path/to/state.json
  ```

- Delete those MAS users in dev environments so the bridge can recreate them via SCIM (also removes the entries from state):

  ```bash
  go run ./cmd/scim-bridge-cleanup \
    --state-path /path/to/state.json \
    --mas-base-url https://mas.local/scim/v2 \
    --mas-profile-id provisioned-profile \
    --mas-token mas-api-key \
    --delete
  ```
