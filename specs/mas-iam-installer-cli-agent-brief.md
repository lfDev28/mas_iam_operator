# MAS IAM Installer CLI Agent Brief

## Mission

Build a first-pass interactive installer CLI for MAS IAM in this repository.

The CLI must improve user experience without rewriting the proven shell install logic yet.

## Repository Decision

Keep the work in this repo.

Use a feature branch:

```text
feature/installer-cli
```

Implement under:

```text
tools/mas-iam-installer/
```

## Primary Constraint

Do not replace the current install scripts in the first iteration.

Instead, wrap:

- `scripts/install-all-in-one.sh`
- `scripts/wipe-all-in-one.sh`

The CLI should be a safer and more interactive entry point, not a parallel installer with different behavior.

## Product Outcome

Users should eventually be able to run something like:

```bash
podman run -it --rm \
  -v $HOME/.kube/config:/kubeconfig:ro \
  -e KUBECONFIG=/kubeconfig \
  quay.io/lee_forster/mas-iam-installer:0.1.0 install
```

## Must-Have Commands

- `install`
- `wipe`
- `preflight`
- `status`
- `logs`
- `version`

## UX Requirements

- Interactive mode when attached to a TTY
- Non-interactive mode through flags/env vars
- Explicit confirmation before destructive wipe
- Clean progress logging while shell scripts run
- Helpful failure summaries with next-step hints

## Prompt Requirements

The `install` flow must collect or confirm:

- namespace
- MAS base URL
- MAS API token name
- MAS API token value
- workspace ID
- MAS profile ID
- storage class
- wipe first: yes/no

The CLI should detect storage classes and rank them sensibly.

Important:

- prefer block/RBD classes for PostgreSQL
- warn if cluster default is `cephfs` but `rbd` exists

## Technical Constraints

- Language: Go
- Suggested libraries:
  - `cobra` for CLI structure
  - `promptui` or `survey` for prompts
  - standard library `os/exec` for script execution
- Keep the module isolated under `tools/mas-iam-installer/`
- Do not introduce a repo-root Go module

## Implementation Guidance

### Phase 1

Scaffold:

- `tools/mas-iam-installer/go.mod`
- `tools/mas-iam-installer/cmd/mas-iam-installer/main.go`
- package skeletons for config, ui, preflight, exec, installer

### Phase 2

Implement `preflight`:

- verify `oc` exists
- verify cluster login
- discover namespace state
- discover storage classes
- parse MAS host from URL
- attempt route lookup for MAS host

### Phase 3

Implement `install` wrapper:

- collect config interactively
- build env vars expected by `scripts/install-all-in-one.sh`
- pass optional `--storage-class`
- stream logs live
- capture a final success/failure summary

### Phase 4

Implement `wipe` wrapper:

- collect confirmation
- build env vars expected by `scripts/wipe-all-in-one.sh`
- stream logs live

### Phase 5

Implement `status` and `logs` helpers:

- summarize operator CSV
- summarize main pods/jobs/PVCs
- show shortcut logs for operator and bridge

### Phase 6

Package it into a container image that includes:

- CLI binary
- `oc`
- the required manifests/scripts from this repo

## Output Requirements

Produce:

1. Working Go CLI scaffold
2. Container packaging
3. Minimal developer README for the CLI
4. Example invocations for interactive and non-interactive usage

## Non-Goals

- Native Go reimplementation of the install logic
- Upgrade workflows
- File-based catalog migration
- Fancy TUI

## Acceptance Criteria

- `preflight` runs without mutating the cluster
- `install` can drive the existing install script end to end
- `wipe` can drive the existing wipe script end to end
- storage class choice is surfaced clearly
- errors from underlying scripts are visible and not swallowed
- the CLI is usable from a container image

## Risks To Handle Explicitly

- secret handling in prompts and logs
- cluster default storage not being appropriate for PostgreSQL
- `oc` auth failures inside container
- divergence between CLI assumptions and script env var names

## First PR Target

Aim for a first PR that includes:

- CLI scaffold
- `preflight`
- interactive config collection
- `install` wrapper only

Leave `wipe`, `status`, and container packaging for the next PR if needed.
