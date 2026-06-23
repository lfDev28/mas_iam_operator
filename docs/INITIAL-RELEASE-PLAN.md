# MAS External Services Toolkit Initial Release Plan

This document captures the first post-beta release direction for `mas-est`.

The internal beta has proven that the project can bootstrap a local CLI, install a working MAS External Services Toolkit lab on OpenShift, and sync demo users through the SCIM bridge. The next releases should harden that base, remove remaining manual operational steps, and add the known limitation features that make the lab more useful for real support scenarios.

## Project Direction

`mas-est` is a support and troubleshooting accelerator for MAS External Services Toolkit work.

The project should continue to focus on:

- quickly standing up Keycloak, OpenLDAP, PostgreSQL, MAS External Services Toolkit, and the SCIM bridge on OpenShift
- giving support engineers a repeatable lab for SCIM, LDAP, certificate, and generic IAM user-flow testing
- separating common MAS/IAM behavior from customer-specific IdP configuration
- making install, reset, diagnostics, and configuration changes easier through the `mas-est` CLI

The project should not try to become a full enterprise IdP emulator in the short term. Microsoft Entra-style behavior, customer tenant policy, and vendor-specific provisioning semantics should be treated as targeted compatibility features only when they directly help support work.

There is also a post-beta exploration to broaden the project beyond IAM into a MAS external services toolkit. The first candidate is S3-compatible object storage for reproducing MAS object storage and attachment scenarios on OpenShift. That work should remain experimental until the IAM beta is stable.

## Current Baseline

Current internal beta baseline:

- released image: `quay.io/lee_forster/mas-external-services-tool:v0.1.0-beta.14`
- local bootstrap supports macOS and Linux host workflows
- published image supports `linux/amd64` and `linux/arm64`
- supported CLI surface:
  - `mas-est preflight`
  - `mas-est install`
  - `mas-est status`
  - `mas-est logs`
  - `mas-est ldap-info`
  - `mas-est support-bundle`
  - `mas-est config view`
  - `mas-est config set mas-api-token`
  - `mas-est uninstall`
- default install creates one demo MAS SCIM profile flow
- default install exposes bundled OpenLDAP connection details through `mas-est ldap-info`

Recent beta hardening already completed:

- removed host `envsubst` requirement by bundling template rendering in the CLI
- lowered shell requirement to macOS-compatible Bash 3.2
- added early `oc login` detection and interactive login handoff
- added explicit PostgreSQL and SCIM bridge storage-class selection
- added LDAP connection detail reporting
- added support bundle collection for beta evidence capture
- added runtime config viewing and MAS API token rotation for the SCIM bridge
- documented quickstart, install, operations, and known limitations

## Priority Features

### 1. Runtime Configuration Management

Goal: let users safely update bridge configuration without reinstalling.

Initial targets:

- `mas-est config view` (done for v0.1.1)
- `mas-est config set mas-api-token` (done for v0.1.1)
- `mas-est config set bridge`
- `mas-est restart bridge`

Required behavior:

- update `secret/scim-bridge-secret` for MAS API token name/value
- update `configmap/scim-bridge-config` for non-secret bridge settings
- clearly restart only `deployment/scim-bridge` when env-backed values change
- preserve existing secret values that are not being changed
- print the exact resources changed and the restart performed

Why this is next:

- token rotation is already documented manually
- config changes currently require users to know Kubernetes object names and restart behavior
- this will reduce avoidable reinstall attempts

### 2. Support Bundle Collection

Goal: make beta bug reports easier to collect and easier to act on.

Initial target:

- `mas-est support-bundle --namespace mas-est` (done for v0.1.1)

Bundle contents:

- `mas-est preflight`
- `mas-est status`
- selected component logs
- deployment/job/pod/PVC summaries
- recent namespace events
- relevant configmaps and redacted secrets
- storage class summary
- CLI version and current cluster identity

Required behavior:

- write to a timestamped local directory
- avoid printing MAS API token values
- clearly warn if any raw secret material is included
- make the output safe enough to share internally after normal hostname/customer review

### 3. Debug And Payload Logging

Goal: support deeper bridge troubleshooting without rebuilding images or hand-editing manifests.

Initial targets:

- config-backed bridge log level
- explicit payload logging toggle
- redaction of tokens, secrets, authorization headers, and sensitive config values
- CLI helpers to enable and disable debug mode

Required behavior:

- debug and payload logging must be disabled by default
- enabling payload logging must be explicit and visible in output
- disabling debug mode should restart the bridge and confirm the setting is off

### 4. Group-Based Profile Routing

Goal: close the largest known feature gap in the SCIM bridge.

Initial targets:

- map Keycloak group membership to MAS SCIM profile IDs
- optionally restrict provisioning to users in configured groups
- document precedence between direct `masProfile` user attributes and group-derived routing

Recommended default rules:

- explicit `masProfile` user attribute wins over group-derived routing
- if no `masProfile` exists, evaluate group-to-profile mapping
- if one group maps, route to that profile
- if multiple groups map to different profiles, skip the user and report a clear error
- if no group maps, fall back to the default profile unless strict mode is enabled

Required behavior:

- mapping can be configured in bridge config
- routing decisions are visible in logs
- conflict/error state is explicit when routing is ambiguous
- tests cover attribute routing, group routing, fallback, strict mode, and multiple matching groups

### 5. Existing MAS User Repair

Goal: improve behavior when the target MAS profile already contains users.

Known problem:

- when a MAS user already exists and the bridge adopts it after a conflict, expected workspace/profile assignment may not always be repaired automatically

Initial targets:

- documented `backfill` or reconcile workflow for existing users
- support command or bridge mode to detect adopted users missing expected state
- clear reporting for users that cannot be safely repaired

Required behavior:

- do not delete or overwrite existing MAS users without an explicit operator action
- prefer dry-run/report mode first
- make repairs idempotent where possible

### 6. Upgrade And Refresh Flow

Goal: avoid full uninstall/reinstall for routine updates.

Initial targets:

- refresh local `mas-est` runtime from a new image
- update bridge image/config without wiping the namespace
- rerun only the required bootstrap jobs when needed

Required behavior:

- print current and target versions
- show which OpenShift resources will change
- avoid deleting user state unless explicitly requested

### 7. S3-Compatible Object Storage Lab

Goal: make MAS object storage and S3 integration issues reproducible without external cloud credentials.

Initial targets:

- MinIO proof of concept with browser console access
- automatic demo bucket provisioning
- MAS-compatible `ObjectStorageCfg`
- clear output for endpoint, console URL, bucket, region, and secret names
- keep Rook Ceph RGW available as an alternate provider experiment

Required behavior:

- keep this outside the beta IAM launch path until validated
- verify the S3 endpoint before configuring Manage properties
- avoid hard-coding one cluster's route domain or MAS instance ID
- support a future provider split: MinIO, existing S3, Rook Ceph, and ODF/NooBaa

## Known Bugs And Limitations

### Install And Cluster Compatibility

- Some clusters may still require explicit block/RBD storage classes.
- Registry, DNS, route, proxy, and certificate differences can still break installs.
- Image pull access must be validated for all published runtime artifacts.
- Failed install evidence collection is improved by the v0.1.1 `support-bundle` command.

### Runtime Operations

- MAS API token rotation is supported through `mas-est config set mas-api-token`.
- General config changes still require users to know which configmap/secret to edit.
- Bridge pod restart is required after env-backed config changes; token rotation restarts the bridge automatically.
- Completed bootstrap jobs do not reread updated secrets unless recreated.

### SCIM Bridge Behavior

- Group membership does not currently drive profile routing.
- The bridge does not implement Entra-style expression mapping such as `Switch(...)`.
- Ambiguous existing MAS users are marked as errors and require cleanup.
- Existing MAS users adopted after conflict may need manual repair for workspace assignment.
- Multi-profile isolation is possible through `masProfile` mapping, but is not yet the primary supported user workflow.

### Product UX

- Install summaries are useful but still basic.
- Troubleshooting guidance is split across docs and command output.
- v0.1.1 includes the single-command support evidence package with `mas-est support-bundle`.
- There is no guided config editing flow yet.

## Suggested Release Phases

### v0.1.1: Beta Hardening

Focus:

- reduce support friction for the current beta users
- improve evidence collection
- remove manual config-editing pain points

Feature targets:

- `mas-est support-bundle` (done)
- `mas-est config view` (done)
- `mas-est config set mas-api-token` (done)
- `mas-est restart bridge`
- clearer install failure messages from real beta feedback
- updated docs based on team install results

Exit criteria:

- a user can rotate the MAS API token without reinstalling
- a user can generate a useful support bundle in one command
- common beta failures have documented triage steps

### v0.2.0: Routing Release

Focus:

- support group-based routing scenarios
- make multi-profile testing easier and safer

Feature targets:

- group-to-profile mapping
- group-based inclusion/exclusion
- precedence rules for `masProfile` versus group-derived routing
- routing summary logs
- documentation for supported routing patterns

Exit criteria:

- users can route demo groups to different MAS SCIM profiles
- ambiguous routing is skipped safely with clear errors
- routing tests cover the supported precedence model

### v0.3.0: Reconciliation And Diagnostics

Focus:

- improve behavior against non-clean MAS profile state
- make bridge state easier to inspect and repair

Feature targets:

- existing-user backfill/reconcile workflow
- dry-run repair report
- safer cleanup guidance
- bridge sync summary output
- last sync timestamp and create/update/skip/failure counts

Exit criteria:

- support can identify why a user was skipped or failed without reading raw logs first
- adopted user state can be reported and repaired through a documented workflow

### v0.4.0: Product Polish

Focus:

- turn the beta tool into a more complete internal product experience

Feature targets:

- local runtime update workflow
- bridge upgrade workflow
- richer install completion summary
- custom demo users and groups
- custom SCIM profile bootstrap inputs
- clearer examples for Fyre, TechZone, and bastion-shell use

Exit criteria:

- routine upgrades do not require uninstall/reinstall
- users can customize common demo inputs without editing manifests
- documentation is organized around install, operate, troubleshoot, and extend

## Bug Backlog

| Area | Item | Priority | Target |
|---|---|---:|---|
| Install | Capture cluster-specific failures from beta users and convert common ones into preflight checks | High | v0.1.1 |
| Config | MAS API token rotation is manual and easy to get wrong | High | v0.1.1 |
| Diagnostics | No one-command evidence bundle for bug reports | High | v0.1.1 |
| Bridge | Existing MAS users may not receive repaired workspace assignment after adoption | High | v0.3.0 |
| Routing | Group membership cannot drive MAS profile routing | High | v0.2.0 |
| Logging | Payload visibility requires better debug controls and redaction | Medium | v0.1.1/v0.2.0 |
| UX | Install summaries do not yet explain next operational steps enough | Medium | v0.1.1 |
| Upgrade | No supported refresh path for bridge/runtime updates | Medium | v0.4.0 |
| Docs | Tutorial screenshots and support playbooks need real beta feedback | Medium | rolling |

## Acceptance Criteria For New Features

Every next-version feature should include:

- CLI help text
- user-facing docs
- focused tests for success and failure cases
- clear output showing what changed
- no unredacted secrets in default output
- a rollback or recovery note when a change affects cluster resources

Sensitive features must additionally include:

- redaction tests
- explicit enable/disable behavior
- documentation warning when output may contain customer-sensitive data

## Related Documents

- [Beta known limitations](BETA-KNOWN-LIMITATIONS.md)
- [Install and operations guide](INSTALL-ALL-IN-ONE.md)
- [Post-beta roadmap](../specs/post-beta-roadmap.md)
- [Beta release plan](../specs/beta-release-plan.md)
