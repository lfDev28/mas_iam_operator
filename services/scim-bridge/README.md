# SCIM bridge

A Go service that polls the Keycloak Admin REST API and pushes users to the MAS SCIM API (`/scim/v2`). It creates users, PATCHes changed attributes, and in group mode deactivates users who leave the scope. `mas-est install` deploys it when the `scim` component is selected, through `scripts/scim-bridge-02-deploy.sh` and `manifests/scim-bridge.yaml`. For installation, see [docs/GUIDE.md](../../docs/GUIDE.md).

## Layout

| Path | Contents |
| --- | --- |
| `cmd/scim-bridge` | Main binary: flag parsing on top of env config |
| `cmd/scim-bridge-cleanup` | Lists state entries with `status=error`. With `--delete`, it also deletes those users from MAS and from state |
| `internal/config` | Settings, defaults, `SCIM_BRIDGE_*` env loading, validation |
| `internal/bridge` | Poller, user scope (groups and username filters), planner, executor, backfill |
| `internal/keycloak`, `internal/mas` | API clients; MAS token fetch and refresh, payload redaction |
| `internal/state` | Correlation store (Keycloak ID to MAS ID, status, last error) |

## Behaviour

- **Modes.** `poll` (default) and `hybrid` run a cycle every poll interval. `hybrid` is currently the same as `poll`. `run-once` runs a single cycle and exits. `backfill` searches MAS for existing users by `externalId`, then `userName`, and seeds the state store without writing to MAS.
- **Scope.** Without `INCLUDE_GROUPS`, candidates come from one page of the realm user list (50 users). With `INCLUDE_GROUPS`, candidates are the direct members of the named groups, fetched page by page, and a group that cannot be resolved fails the cycle. `INCLUDE_USERNAMES` and `INCLUDE_USERNAME_PREFIX` then narrow the set; all configured filters must match. LDAP-federated users are skipped unless `KEYCLOAK_INCLUDE_FEDERATED_USERS=true`.
- **Deactivation (group mode only).** A tracked user who drops out of the scoped set gets SCIM `active:false` once. The bridge never deletes users. Without groups there is no removal detection, because the one-page user list is incomplete.
- **Conflicts and auth.** When a create returns 409, the bridge adopts the existing MAS user if exactly one match is found. When a request returns 401 and `MAS_API_TOKEN_NAME`/`MAS_API_TOKEN_VALUE` are set, the bridge fetches a new token and retries the cycle once.
- **Profile routing.** A user's `masProfile` Keycloak attribute is mapped through `MAS_PROFILE_MAP` to a MAS profile id. Users without a mapped label fall back to `MAS_PROFILE_ID`, or are skipped when `MAS_PROFILE_REQUIRE_LABEL=true`.

`mas-est install` sets `SCIM_BRIDGE_INCLUDE_GROUPS=mas-scim-users` and `SCIM_BRIDGE_INCLUDE_USERNAME_PREFIX=scim.` (`scripts/install-all-in-one.sh`). Design notes: [docs/design/SCIM-GROUP-SCOPING.md](../../docs/design/SCIM-GROUP-SCOPING.md).

## Configuration

Every setting is read from a `SCIM_BRIDGE_`-prefixed env var. Most settings also have a flag, which takes precedence over the env var. In the cluster, non-secret keys live in `ConfigMap/scim-bridge-config` and credentials in `Secret/scim-bridge-secret`. The Deployment passes `--bridge-mode=poll --bridge-state-backend=filesystem --bridge-state-path=/var/lib/scim-bridge/state.json`, and the state file lives on PVC `scim-bridge-state`.

| Env var (after `SCIM_BRIDGE_`) | Flag | Default | Notes |
| --- | --- | --- | --- |
| `KEYCLOAK_BASE_URL` | `--keycloak-base-url` | required | |
| `KEYCLOAK_REALM` | `--keycloak-realm` | `master` | installer uses `maximo` |
| `KEYCLOAK_CLIENT_ID` / `_CLIENT_SECRET` | `--keycloak-client-id` / `--keycloak-client-secret` | required | installer uses client `scim-admin` |
| `KEYCLOAK_CA_FILE` | none | | PEM bundle |
| `KEYCLOAK_INSECURE_SKIP_VERIFY` | `--keycloak-insecure-skip-verify` | `false` | |
| `KEYCLOAK_INCLUDE_FEDERATED_USERS` | `--keycloak-include-federated-users` | `false` | |
| `MAS_BASE_URL` | `--mas-base-url` | required | `https://api.<mas-domain>/scim/v2` |
| `MAS_PROFILE_ID` | `--mas-profile-id` | required | installer uses `demo` |
| `MAS_PROFILE_MAP` / `MAS_PROFILE_MAP_JSON` | `--mas-profile-map` / `--mas-profile-map-json` | | `users=p1,mgmt=p2` or a JSON object |
| `MAS_PROFILE_REQUIRE_LABEL` | `--mas-profile-require-label` | `false` | |
| `MAS_AUTH_TYPE` | `--mas-auth-type` | `api-key` | `api-key` or `jwt`; installer uses `jwt` |
| `MAS_TOKEN` | `--mas-token` | | a static token, or use the name/value pair below |
| `MAS_API_TOKEN_NAME` / `_VALUE` | `--mas-api-token-name` / `--mas-api-token-value` | | used to call `/v1/authenticate` for a token |
| `MAS_CA_FILE` | none | | PEM bundle |
| `MAS_INSECURE_SKIP_VERIFY` | `--mas-insecure-skip-verify` | `false` | |
| `BRIDGE_MODE` | `--bridge-mode` | `poll` | `poll`, `hybrid`, `run-once`, `backfill` |
| `BRIDGE_POLL_INTERVAL` | `--bridge-poll-interval` | `5m` | Go duration |
| `BRIDGE_STATE_BACKEND` | `--bridge-state-backend` | `memory` | `memory` or `filesystem` |
| `BRIDGE_STATE_PATH` | `--bridge-state-path` | | required for `filesystem` |
| `BRIDGE_DRY_RUN` | `--bridge-dry-run` | `true` | the deploy script sets `false` |
| `BRIDGE_ALLOW_UPDATES` | `--bridge-allow-updates` | `true` | `false` skips updates and deactivations; creates still run |
| `BRIDGE_LOG_LEVEL` | `--bridge-log-level` | `info` | `debug`, `info`, `warn`, `error` |
| `BRIDGE_PAYLOAD_LOGGING` | `--bridge-payload-logging` | `false` | redacted outbound payloads |
| `INCLUDE_USERNAMES` | `--include-usernames` | | comma-separated |
| `INCLUDE_USERNAME_PREFIX` | `--include-username-prefix` | | |
| `INCLUDE_GROUPS` | `--include-groups` | | group names or paths, comma-separated |

The ConfigMap also holds `SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_*` keys. The bridge binary ignores them; they are read by the profile bootstrap Job in `manifests/scim-bridge-mas-profile-bootstrap.yaml`. The Deployment mounts CA bundles from the optional `Secret/scim-bridge-ca`, `ConfigMap/scim-bridge-ca`, and `ConfigMap/scim-bridge-mas-ca` at `/etc/scim-bridge/certs`.

On a running install, change the log level or payload logging with `mas-est config set bridge --log-level <level> --payload-logging <true|false>`. Rotate the MAS token with `mas-est config set mas-api-token`.

## Build and test

```bash
cd services/scim-bridge
go build ./... && go test ./...
go run ./cmd/scim-bridge --help
```

Image: `SCIM_BRIDGE_IMAGE=<image> SCIM_BRIDGE_TARGET_ARCH=amd64 bash scripts/scim-bridge-01-build-image.sh`, run from the repo root. It builds `Dockerfile` and pushes the image. Set the arch explicitly; when unset, the script reads it from the logged-in cluster's nodes.

## Release

Use the `/mas-est-release` skill ([.claude/skills/mas-est-release/SKILL.md](../../.claude/skills/mas-est-release/SKILL.md)). A code change has no effect until a new image is published and pinned.
