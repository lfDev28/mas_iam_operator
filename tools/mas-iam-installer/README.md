# MAS External Services Toolkit Installer CLI

This module contains the `mas-est` CLI and the bootstrap path that installs a local runtime on the host.

The supported delivery model is:

1. bootstrap `mas-est` once from the published image
2. run `mas-est install`, `mas-est uninstall`, `mas-est preflight`, and related commands locally without going back through `podman`

Current published image:

- `quay.io/lee_forster/mas-external-services-tool:v0.1.0-beta.9`

The CLI wraps the repo's hardened shell install engine. It does not replace it.

## Commands

- `bootstrap`
- `install`
- `uninstall`
- `preflight`
- `status`
- `support-bundle`
- `config view`
- `config set mas-api-token`
- `config set bridge`
- `restart bridge`
- `object-storage install-minio` (experimental)
- `object-storage install-rook-ceph` (experimental)
- `smtp install-mailpit` (experimental)
- `logs`
- `ldap-info`
- `version`

## Official User Flow

Set the image once:

```bash
export MAS_EST_IMAGE='quay.io/lee_forster/mas-external-services-tool:v0.1.0-beta.9'
```

Bootstrap the host command:

```bash
mkdir -p "$HOME/mas-est"
podman run -ti --rm -v "$HOME/mas-est:/tmp" --pull always $MAS_EST_IMAGE
export PATH="$HOME/mas-est:$PATH"
```

The image defaults to `bootstrap`, so running it with no command writes the local `mas-est` runtime into the mounted directory.

After bootstrap:

```bash
mas-est preflight
mas-est install
mas-est status --namespace mas-est
mas-est support-bundle --namespace mas-est
mas-est config view --namespace mas-est
mas-est config set mas-api-token --namespace mas-est --token-name '<token-name>' --token-value '<token-value>'
mas-est config set bridge --namespace mas-est --log-level debug --payload-logging true
mas-est restart bridge --namespace mas-est
mas-est object-storage install-minio --mas-instance-id '<instance-id>'
mas-est smtp install-mailpit --namespace mas-est
mas-est ldap-info --namespace mas-est
mas-est logs --namespace mas-est --component bridge
mas-est uninstall --namespace mas-est --profile-id demo
```

## Interactive Prompts

The `install` command prompts for:

- namespace
- products to install: LDAP, Keycloak, SCIM bridge, S3 object storage, and/or SMTP capture
- MAS base URL, token, workspace ID, and profile ID when SCIM is selected
- MAS instance/core namespace when S3 is selected and MAS ObjectStorageCfg creation is enabled
- primary storage class for Keycloak PostgreSQL and/or MinIO
- SCIM bridge storage class when SCIM is selected
- uninstall first

Dependency handling is automatic: selecting SCIM also selects Keycloak and LDAP, and selecting Keycloak also selects LDAP.

The `uninstall` command prompts for:

- namespace
- MAS profile ID
- whether to skip MAS profile deletion
- final destructive confirmation

The `support-bundle` command creates a timestamped local evidence directory and redacts secret values by default.

The `config view` command shows `configmap/scim-bridge-config` values and redacted `secret/scim-bridge-secret` key names. The `config set mas-api-token` command rotates the MAS API token name/value, preserves other secret keys, restarts `deployment/scim-bridge`, and waits for rollout completion.

The `object-storage install-minio` command is experimental post-beta work. It creates a lightweight MinIO S3 endpoint, an OpenShift route for the MinIO Console UI, Manage-compatible demo buckets/prefixes, a MAS-compatible credentials secret, and a system-scoped MAS `ObjectStorageCfg`.

For Manage cron or doclinks tests, use the internal endpoint `http://mas-est.svc.cluster.local:9000`, bucket `mas-s3-demo`, region `us-east-1`, and the MAS credential secret keys `username` and `password`.

The older `object-storage install-rook-ceph` command remains available for Rook Ceph RGW experiments.

The `smtp install-mailpit` command is experimental post-beta work. It creates a Mailpit SMTP capture service, an internal SMTP endpoint for MAS tests, and an OpenShift route for the Mailpit browser UI. It captures messages for inspection and does not relay mail externally.

For MAS SMTP tests, use host `mas-mailpit.mas-est.svc.cluster.local`, port `1025`, no TLS, and no authentication.

## Non-Interactive Install

Flags:

```bash
mas-est install \
  --components ldap,keycloak,scim,s3,smtp \
  --namespace mas-est \
  --mas-base-url 'https://api.<mas-instance>.<domain>/scim/v2' \
  --mas-api-token-name '<token-name>' \
  --mas-api-token-value '<token-value>' \
  --workspace-id '<workspace-id>' \
  --profile-id demo \
  --mas-instance-id '<instance-id>' \
  --storage-class ocs-external-storagecluster-ceph-rbd \
  --scim-bridge-storage-class ocs-external-storagecluster-ceph-rbd \
  --non-interactive
```

Env vars:

- `MAS_EST_NAMESPACE`
- `MAS_EST_COMPONENTS`
- `SCIM_BRIDGE_MAS_BASE_URL`
- `SCIM_BRIDGE_MAS_API_TOKEN_NAME`
- `SCIM_BRIDGE_MAS_API_TOKEN_VALUE`
- `SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID`
- `SCIM_BRIDGE_MAS_PROFILE_ID`
- `MAS_INSTANCE_ID`
- `MAS_CORE_NAMESPACE`
- `MAS_EST_SKIP_S3_MAS_CONFIG`
- `POSTGRES_STORAGE_CLASS`
- `SCIM_BRIDGE_STORAGE_CLASS`
- `MAS_EST_UNINSTALL_FIRST`

## Bootstrap Details

Bootstrap requires `podman`.

The installed local runtime:

- writes `mas-est` plus a bundled runtime tree into the target directory
- includes native binaries for `darwin/amd64`, `darwin/arm64`, `linux/amd64`, and `linux/arm64`
- bundles the repo `scripts/`, `manifests/`, and `env/` needed by install and uninstall
- expects these host tools on `PATH`:
  - `oc`
  - `bash` 3.2+ for `install` and `uninstall`

You can re-bootstrap explicitly:

```bash
podman run -ti --rm -v "$HOME/mas-est:/tmp" --pull always $MAS_EST_IMAGE bootstrap --force
```

## Development Workflow

Build locally from the repo root:

```bash
podman build -f tools/mas-iam-installer/Containerfile -t mas-est-tool:dev .
```

Test bootstrap locally:

```bash
mkdir -p /tmp/mas-est-bootstrap
podman run -ti --rm -v /tmp/mas-est-bootstrap:/tmp localhost/mas-est-tool:dev
PATH="/tmp/mas-est-bootstrap:$PATH" mas-est --help
```

Build the binary directly during development:

```bash
cd tools/mas-iam-installer
go build -o mas-est ./cmd/mas-iam-installer
```

For local development on this machine, a clean temporary `GOMODCACHE` may be more reliable than the default module cache if dependency extraction has become stale.

## Runtime Layout

Bootstrap installs:

- `<install-dir>/mas-est`
- `<install-dir>/.mas-est-runtime/bin/<os>-<arch>/est`
- `<install-dir>/.mas-est-runtime/repo/scripts`
- `<install-dir>/.mas-est-runtime/repo/manifests`
- `<install-dir>/.mas-est-runtime/repo/env`

The launcher sets `MAS_EST_REPO_ROOT` to the bundled repo path automatically.
