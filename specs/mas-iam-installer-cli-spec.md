# MAS IAM Installer CLI Spec

## Summary

The installer CLI is the intended official user-facing install surface for this repository.

Current image:

- `quay.io/lee_forster/mas-iam-tool:v0.1.0-beta.1`

Current intended user flow:

1. bootstrap a local `mas-iam` command from the image
2. run `mas-iam install`, `mas-iam wipe`, `mas-iam preflight`, and related commands locally

Bootstrap example:

```bash
mkdir -p "$HOME/mas-iam"
podman run -ti --rm -v "$HOME/mas-iam:/tmp" --pull always quay.io/lee_forster/mas-iam-tool:v0.1.0-beta.1
export PATH="$HOME/mas-iam:$PATH"
```

The image defaults to `bootstrap`, which writes a local `mas-iam` launcher plus bundled runtime assets into the mounted directory.

The CLI remains a thin wrapper around the existing, working shell runbooks.

## Why Same Repo

The installer is tightly coupled to:

- the operator catalog image
- the sample `MasIamStack`
- the SCIM bridge deployment flow
- the CA auto-detect logic
- the storage-class selection logic

Splitting into a new repo now would create version skew risk. The cleaner move is:

1. keep CLI development here
2. keep the shell engine here
3. split later only if packaging and release cadence diverge materially

## Product Goal

Provide a MAS-style installation experience where users bootstrap a local command from a container image and then run that local command directly.

The CLI should:

- guide the user through required inputs
- run preflight checks before applying anything
- detect or suggest compatible storage classes
- stream progress in a readable way
- surface actionable blockers clearly
- support non-interactive mode for automation

## Non-Goals For V1

- replacing the existing shell scripts with a full native Go installer
- implementing upgrades or migrations
- supporting every possible deployment topology
- building a pane-based TUI
- shipping a standalone installer outside the container bootstrap flow

## Scope

### V1

Implement and ship a containerized Go CLI that:

- exposes `bootstrap`, `install`, `wipe`, `preflight`, `status`, `logs`, and `version`
- defaults to `bootstrap` when the image is run with no explicit command
- installs a local `mas-iam` command named `mas-iam`
- wraps the current shell scripts underneath

Modes:

- interactive by default when attached to a TTY
- non-interactive with flags or env vars

### V2

Consider moving selected install logic from shell into Go packages once:

- the CLI UX is stable
- the config contract is stable
- packaging and release cadence are stable

## Repository Layout

```text
tools/mas-iam-installer/
  go.mod
  cmd/mas-iam-installer/main.go
  internal/app/
  internal/config/
  internal/installer/
  internal/preflight/
  internal/ui/
  internal/exec/
  internal/oc/
  internal/logging/
  Containerfile
```

Notes:

- keep it as a separate Go module under `tools/mas-iam-installer/`
- do not make the repo root a Go module just for this
- keep reusing the existing scripts in V1 rather than duplicating logic

## Core UX

### `bootstrap`

Behavior:

- writes a local launcher script named `mas-iam` into a mounted output directory
- defaults to output dir `/tmp` inside the container, matching a MAS-style `-v <hostdir>:/tmp` bootstrap flow
- installs bundled runtime assets and native binaries for supported host OS/arch combinations
- supports `--force` for reinstallation

The installed local runtime should:

- run the bootstrapped native `iam` binary directly on the host
- select the correct host binary for `darwin/linux` and `amd64/arm64`
- point `MAS_IAM_REPO_ROOT` at bundled repo assets
- require host `oc`
- require host `bash` 4+ for `install` and `wipe`
- require host `envsubst` for `install`

### `install`

Interactive prompts should collect:

- target namespace
- MAS SCIM base URL
- MAS API token name
- MAS API token value
- workspace ID
- MAS profile ID
- PostgreSQL storage class
- SCIM bridge storage class
- whether to wipe the namespace first

Suggested flow:

1. verify `oc` access and current cluster identity
2. run preflight checks
3. detect storage classes and present a ranked list
4. confirm the resolved config
5. optionally run wipe
6. run the full install flow
7. print final verification summary

### `wipe`

Interactive prompts should collect:

- namespace
- MAS profile ID
- whether to skip MAS profile deletion

### `preflight`

Checks should include:

- `oc` present
- logged into cluster
- storage class discovery
- route discovery for MAS base URL host
- warning if default storage is `cephfs` and block/RBD is available

### `status`

Summarize:

- operator CSV phase
- controller manager readiness
- IAM core pod readiness
- SCIM bridge readiness
- relevant jobs and PVCs

### `logs`

Shortcuts for:

- operator controller logs
- Keycloak logs
- SCIM bridge logs
- MAS profile bootstrap job logs

## Configuration Model

V1 supports three config sources:

1. flags
2. environment variables
3. interactive prompts

Priority:

1. flags
2. env vars
3. prompt defaults

Do not persist secret values unless explicitly requested in a future iteration.

## Storage Class Behavior

The CLI must not silently trust the cluster default when a better explicit class is known.

PostgreSQL selection order:

1. explicit user override
2. preferred block class names
3. first class matching `rbd|block`
4. cluster default
5. first available class with warning

SCIM bridge behavior:

- support explicit override with `--scim-bridge-storage-class` or `SCIM_BRIDGE_STORAGE_CLASS`
- if unset, the bridge PVC may still inherit the cluster default
- warn clearly when the default looks unsuitable

## Integration Strategy

### V1 Wrapper Approach

The CLI calls the current scripts rather than reimplementing them:

- `scripts/install-all-in-one.sh`
- `scripts/wipe-all-in-one.sh`
- `scripts/scim-bridge-02-deploy.sh`

Implementation pattern:

- build an env map
- execute the relevant script with `os/exec`
- stream stdout/stderr line-by-line
- surface the same key milestones that maintainers already use

This keeps risk low because the proven logic stays where it is.

## Logging

The CLI should produce two outputs:

- human-readable live console output
- optional structured log file for support/debug

The bootstrapped runtime should keep logs in its local runtime tree so they persist across local command runs.

## Packaging

Installer image contents:

- `oc`
- bash
- the CLI binary exposed as `iam`
- native `iam` binaries for supported host platforms
- repo manifests/scripts/env required for install

Runtime behavior:

- user bootstraps a local runtime from the image
- bootstrap writes the launcher plus bundled runtime tree to the mounted host directory
- the local launcher executes the native host binary directly
- CLI uses the bootstrapped local manifests/scripts
- no raw GitHub dependency during install

## Acceptance Criteria For V1

- user can bootstrap `mas-iam` with a single `podman run`
- user can then run `mas-iam install` locally
- user is not required to `oc apply` raw GitHub URLs directly
- CLI can run `preflight` without changing cluster state
- CLI can `wipe` and `install` against the current supported flow
- CLI can detect storage classes and present recommended options
- CLI can control PostgreSQL and SCIM bridge storage separately
- CLI prints the same final validation points currently used manually
- CLI exits non-zero on failed steps and prints the relevant failing component

## Stretch Goals

- resumable install sessions
- automatic support bundle collection on failure
- richer status table output
- native Go implementation of selected install steps
- optional JSON output mode
- native host binary installers for macOS/Linux

## Open Decisions

- tagging and release policy for `quay.io/lee_forster/mas-iam-tool`
- whether to persist non-secret config across runs by default
- whether to support YAML answers files in V1.x

## Recommended Next Build Order

1. validate the bootstrap flow and installed wrapper on a real machine
2. validate the published CLI image on a real cluster through `mas-iam`
3. polish prompts, help text, and docs based on that run
4. continue from `v0.1.0-beta.1` to tagged release images
5. only then consider deeper Go-native refactors
