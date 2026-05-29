# MAS IAM Installer CLI

This module contains the `iam` CLI and the bootstrap path that installs a local `mas-iam` runtime on the host.

The supported delivery model is:

1. bootstrap `mas-iam` once from the published image
2. run `mas-iam install`, `mas-iam wipe`, `mas-iam preflight`, and related commands locally without going back through `podman`

Current published image:

- `quay.io/lee_forster/mas-iam-tool:v0.1.0-beta.5`

The CLI wraps the repo's hardened shell install engine. It does not replace it.

## Commands

- `bootstrap`
- `install`
- `wipe`
- `preflight`
- `status`
- `support-bundle`
- `config view`
- `config set mas-api-token`
- `object-storage install-minio` (experimental)
- `object-storage install-rook-ceph` (experimental)
- `logs`
- `ldap-info`
- `version`

## Official User Flow

Set the image once:

```bash
export MAS_IAM_IMAGE='quay.io/lee_forster/mas-iam-tool:v0.1.0-beta.5'
```

Bootstrap the host command:

```bash
mkdir -p "$HOME/mas-iam"
podman run -ti --rm -v "$HOME/mas-iam:/tmp" --pull always $MAS_IAM_IMAGE
export PATH="$HOME/mas-iam:$PATH"
```

The image defaults to `bootstrap`, so running it with no command writes the local `mas-iam` runtime into the mounted directory.

After bootstrap:

```bash
mas-iam preflight
mas-iam install
mas-iam status --namespace iam
mas-iam support-bundle --namespace iam
mas-iam config view --namespace iam
mas-iam config set mas-api-token --namespace iam --token-name '<token-name>' --token-value '<token-value>'
mas-iam object-storage install-minio --mas-instance-id '<instance-id>'
mas-iam ldap-info --namespace iam
mas-iam logs --namespace iam --component bridge
mas-iam wipe --namespace iam --profile-id demo
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
- wipe first

The `wipe` command prompts for:

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
mas-iam install \
  --namespace iam \
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

- `MAS_IAM_NAMESPACE`
- `SCIM_BRIDGE_MAS_BASE_URL`
- `SCIM_BRIDGE_MAS_API_TOKEN_NAME`
- `SCIM_BRIDGE_MAS_API_TOKEN_VALUE`
- `SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID`
- `SCIM_BRIDGE_MAS_PROFILE_ID`
- `POSTGRES_STORAGE_CLASS`
- `SCIM_BRIDGE_STORAGE_CLASS`
- `MAS_IAM_WIPE_FIRST`

## Bootstrap Details

Bootstrap requires `podman`.

The installed local runtime:

- writes `mas-iam` plus a bundled runtime tree into the target directory
- includes native binaries for `darwin/amd64`, `darwin/arm64`, `linux/amd64`, and `linux/arm64`
- bundles the repo `scripts/`, `manifests/`, and `env/` needed by install and wipe
- expects these host tools on `PATH`:
  - `oc`
  - `bash` 3.2+ for `install` and `wipe`

You can re-bootstrap explicitly:

```bash
podman run -ti --rm -v "$HOME/mas-iam:/tmp" --pull always $MAS_IAM_IMAGE bootstrap --force
```

## Development Workflow

Build locally from the repo root:

```bash
podman build -f tools/mas-iam-installer/Containerfile -t mas-iam-tool:dev .
```

Test bootstrap locally:

```bash
mkdir -p /tmp/mas-iam-bootstrap
podman run -ti --rm -v /tmp/mas-iam-bootstrap:/tmp localhost/mas-iam-tool:dev
PATH="/tmp/mas-iam-bootstrap:$PATH" mas-iam --help
```

Build the binary directly during development:

```bash
cd tools/mas-iam-installer
go build ./cmd/mas-iam-installer
```

For local development on this machine, a clean temporary `GOMODCACHE` may be more reliable than the default module cache if dependency extraction has become stale.

## Runtime Layout

Bootstrap installs:

- `<install-dir>/mas-iam`
- `<install-dir>/.mas-iam-runtime/bin/<os>-<arch>/iam`
- `<install-dir>/.mas-iam-runtime/repo/scripts`
- `<install-dir>/.mas-iam-runtime/repo/manifests`
- `<install-dir>/.mas-iam-runtime/repo/env`

The launcher sets `MAS_IAM_REPO_ROOT` to the bundled repo path automatically.
