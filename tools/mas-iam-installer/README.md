# MAS External Services Toolkit Installer CLI

This module contains the `mas-est` CLI and the bootstrap path that installs a local runtime on the host.

The supported delivery model is:

1. bootstrap `mas-est` once from the published image
2. run `mas-est install`, `mas-est uninstall`, `mas-est preflight`, and related commands locally without going back through `podman`

Current published image:

- `quay.io/lee_forster/mas-external-services-tool:v0.1.0-beta.7`

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
- `logs`
- `ldap-info`
- `version`

## Official User Flow

Set the image once:

```bash
export MAS_EST_IMAGE='quay.io/lee_forster/mas-external-services-tool:v0.1.0-beta.7'
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
mas-est ldap-info --namespace mas-est
mas-est logs --namespace mas-est --component bridge
mas-est uninstall --namespace mas-est --profile-id demo
```

## Interactive Prompts

The `install` command prompts for:

- namespace
- MAS base URL
- MAS API token name
- MAS API token value
- workspace ID
- MAS profile ID
- PostgreSQL storage class
- SCIM bridge storage class
- uninstall first

The `uninstall` command prompts for:

- namespace
- MAS profile ID
- whether to skip MAS profile deletion
- final destructive confirmation

The `support-bundle` command creates a timestamped local evidence directory and redacts secret values by default.

The `config view` command shows `configmap/scim-bridge-config` values and redacted `secret/scim-bridge-secret` key names. The `config set mas-api-token` command rotates the MAS API token name/value, preserves other secret keys, restarts `deployment/scim-bridge`, and waits for rollout completion.

The `object-storage install-minio` command is experimental post-beta work. It creates a lightweight MinIO S3 endpoint, an OpenShift route for the MinIO Console UI, a demo bucket, a MAS-compatible credentials secret, and a system-scoped MAS `ObjectStorageCfg`.

The older `object-storage install-rook-ceph` command remains available for Rook Ceph RGW experiments.

## Non-Interactive Install

Flags:

```bash
mas-est install \
  --namespace mas-est \
  --mas-base-url 'https://api.<mas-instance>.<domain>/scim/v2' \
  --mas-api-token-name '<token-name>' \
  --mas-api-token-value '<token-value>' \
  --workspace-id '<workspace-id>' \
  --profile-id demo \
  --storage-class ocs-external-storagecluster-ceph-rbd \
  --scim-bridge-storage-class ocs-external-storagecluster-ceph-rbd \
  --non-interactive
```

Env vars:

- `MAS_EST_NAMESPACE`
- `SCIM_BRIDGE_MAS_BASE_URL`
- `SCIM_BRIDGE_MAS_API_TOKEN_NAME`
- `SCIM_BRIDGE_MAS_API_TOKEN_VALUE`
- `SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID`
- `SCIM_BRIDGE_MAS_PROFILE_ID`
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
