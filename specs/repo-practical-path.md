# Repo Practical Path

## Purpose

This document is the current execution plan for the repository.

It exists to keep work aligned across agents and to prevent unnecessary rewrites while the official install surface shifts to the bootstrapped `mas-iam` command.

## Current State

The repository now has:

- a working end-to-end shell install engine built around:
  - `scripts/install-all-in-one.sh`
  - `scripts/wipe-all-in-one.sh`
  - `scripts/install-olm-sample.sh`
  - `scripts/scim-bridge-02-deploy.sh`
- a bootstrapable CLI under `tools/mas-iam-installer/`
- a published user-facing installer image:
  - `quay.io/lee_forster/mas-iam-tool:v0.1.0-beta.1`
- a bootstrap path that installs a local `mas-iam` runtime from that image

Recent validation:

- the published operator-sidecar image issue was fixed by replacing the dead `gcr.io` `kube-rbac-proxy` reference with `quay.io/brancz/kube-rbac-proxy:v0.14.1`
- the operator bundle and catalog were rebuilt and pushed
- a fresh-cluster validation succeeded end to end with no manual cluster patching
- the CLI now supports separate PostgreSQL and SCIM bridge storage-class selection
- the image can now bootstrap a local `mas-iam` runtime into a mounted host directory

## Strategic Direction

For the near term, the repository should optimize for:

1. the bootstrapped `mas-iam` command as the official user path
2. reliable shell-engine behavior underneath the CLI
3. clear user documentation around the bootstrap flow and `mas-iam <command>`
4. low-risk UX polish rather than installer rewrites

It should not optimize yet for:

- a full installer rewrite in Go
- splitting the installer into a separate repo
- parallel, competing installation paths

## Official Install Surface

Supported user-facing commands are:

- `mas-iam preflight`
- `mas-iam install`
- `mas-iam wipe`
- `mas-iam status`
- `mas-iam logs`

Delivered through this bootstrap step:

```bash
mkdir -p "$HOME/mas-iam"
podman run -ti --rm -v "$HOME/mas-iam:/tmp" --pull always quay.io/lee_forster/mas-iam-tool:v0.1.0-beta.1
export PATH="$HOME/mas-iam:$PATH"
```

The shell scripts remain the authoritative backend implementation and maintainer support path.

## Hard Rules

The following should hold unless explicitly revised:

1. Do not create a separate installer repo yet.
2. Do not rewrite the install engine before the wrapper CLI clearly hits a limit.
3. Do not maintain two different authoritative user install flows.
4. Do not publish user docs that diverge from the shipped bootstrap and CLI behavior.
5. Do not assume the cluster default storage class is correct for PostgreSQL or for the SCIM bridge PVC.

## Current Priorities

### 1. Bootstrap Validation

Objective:

- validate the published bootstrap flow and the installed `mas-iam` runtime on real user machines

Success criteria:

- bootstrap works with a single `podman run`
- the installed `mas-iam` command can run `preflight`, `install`, `wipe`, `status`, and `logs`
- kubeconfig mounting works for the normal single-file case

### 2. CLI Validation

Objective:

- validate the published image on real clusters through the installed local runtime

Success criteria:

- `preflight`, `install`, `wipe`, `status`, and `logs` all behave correctly from `mas-iam`
- the image works against at least one clean cluster with no manual mid-run patching
- storage selection works across the known storage-class families

### 3. UX Polish

Objective:

- make the CLI feel like the primary supported product surface

Priority items:

1. improve prompt wording and defaults
2. tighten command help and examples
3. keep docs aligned with the published image name and bootstrap flow
4. reduce rough edges in non-interactive env/flag usage

### 4. Release Packaging

Objective:

- move from ad hoc development image tags toward tagged release images and clean release notes

Priority items:

1. define tagging policy for `mas-iam-tool`
2. document the publish workflow
3. decide whether to also publish `latest`

## CLI Direction

The CLI design lives in:

- [specs/mas-iam-installer-cli-spec.md](mas-iam-installer-cli-spec.md)
- [specs/mas-iam-installer-cli-agent-brief.md](mas-iam-installer-cli-agent-brief.md)

Current recommendation:

- wrapper CLI first
- native Go internals later only when justified by real pain in the shell layer

## Agent Model

The repo should be operable with at least two agent roles:

### 1. Planner/Designer Agent

Responsibilities:

- maintain repo direction
- design CLI/install architecture
- sequence work
- refine docs and agent instructions

### 2. Installer Agent

Responsibilities:

- run preflight/install/wipe/status flows on real clusters
- monitor OpenShift resources during install
- identify blockers with evidence
- report exact fixes needed in repo or cluster setup

Installer-agent behavior is documented in:

- [specs/installer-agent-operating-guide.md](installer-agent-operating-guide.md)
- [specs/installer-agent-prompt.md](installer-agent-prompt.md)

## Decision Log

Current decisions:

- installer stays in this repo for now
- the official user path is the bootstrapped `mas-iam` command backed by `quay.io/lee_forster/mas-iam-tool:v0.1.0-beta.1`
- v1 CLI wraps scripts instead of replacing them
- published operator/catalog artifacts are part of the tested install surface
- shell scripts remain the backend engine and maintainer/debug path

## Best Next Steps

1. validate bootstrap and `mas-iam install` on a real machine
2. run one full install via the published CLI image on a clean cluster through `mas-iam`
3. collect UX feedback and polish prompts/help/docs
4. continue from `v0.1.0-beta.1` to tagged release images, and decide whether to publish `latest`
5. keep script-engine changes focused on backend correctness, not a second user install surface
