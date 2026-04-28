# Installer Agent Operating Guide

## Purpose

This document defines how an installer-focused agent should work in this repository.

The installer agent is responsible for validating real OpenShift installs, monitoring progress, finding blockers, and reporting evidence-backed fixes.

## Mission

The installer agent should:

- ask for the correct install inputs
- verify cluster access before acting
- run the standard scripted flow
- monitor OpenShift resources during install
- identify whether failures are caused by:
  - cluster prerequisites
  - published artifacts
  - repo scripts/manifests
  - user-provided inputs

## Standard Install Path

The installer agent should prefer these entry points:

- `scripts/wipe-all-in-one.sh`
- `scripts/install-all-in-one.sh`

It should only fall back to manual per-component commands when diagnosing failures.

## Required Inputs

Before a full install, the agent should request:

```bash
export SCIM_BRIDGE_MAS_BASE_URL='https://api.<mas-host>/scim/v2'
export SCIM_BRIDGE_MAS_API_TOKEN_NAME='<mas-api-token-name>'
export SCIM_BRIDGE_MAS_API_TOKEN_VALUE='<mas-api-token-value>'
export SCIM_BRIDGE_MAS_PROFILE_BOOTSTRAP_WORKSPACE_ID='<workspace-id>'
```

The agent should also verify:

- `oc whoami`
- `oc whoami --show-server`

## Optional Inputs

The agent may request or set:

```bash
export SCIM_BRIDGE_MAS_PROFILE_ID='demo'
export POSTGRES_STORAGE_CLASS='<block-or-rbd-class>'
```

## When To Ask For Storage Class Explicitly

If the cluster default storage class is unsuitable for PostgreSQL, the agent should recommend an explicit override.

Examples:

- default is `cephfs`
- block or `rbd` class exists but is not default

In those cases, the agent should recommend:

```bash
export POSTGRES_STORAGE_CLASS='<rbd-class>'
```

## Preflight Workflow

Before install, the agent should check:

1. `oc` is available
2. cluster login is valid
3. target cluster/server matches user expectation
4. image registry/operator catalog prerequisites are satisfied if relevant
5. storage classes are discoverable
6. MAS route can be derived from `SCIM_BRIDGE_MAS_BASE_URL`

Suggested commands:

```bash
oc whoami
oc whoami --show-server
oc get sc
oc get co image-registry
```

## Install Workflow

Default flow:

1. if appropriate, run wipe
2. run `install-all-in-one.sh`
3. monitor operator CSV
4. monitor IAM pods/jobs
5. monitor SCIM bridge rollout
6. verify MAS profile bootstrap job

## Monitoring Responsibilities

During install, the agent should watch:

- operator CSV phase
- operator deployment pod readiness
- IAM core pods:
  - Keycloak
  - OpenLDAP
  - PostgreSQL
- bootstrap jobs:
  - OpenLDAP TLS generator
  - LDAP config
  - SCIM bridge MAS profile bootstrap
- SCIM bridge deployment logs

## Blocker Triage Rules

The agent should classify blockers into one of these categories:

### Cluster Prerequisite

Examples:

- internal image registry not configured
- missing storage class
- lack of pull secret or registry access

### Published Artifact

Examples:

- broken operator bundle image
- bad catalog image
- image reference in CSV no longer valid

### Repo Logic

Examples:

- storage auto-detection chooses an unsuitable class
- script ordering issue
- missing wait or race condition

### User Input

Examples:

- malformed MAS base URL
- missing `/scim/v2`
- invalid token

## Evidence Standard

When reporting a blocker, the agent should always provide:

1. failing phase
2. exact failing resource
3. exact error text
4. root-cause classification
5. whether a repo change is needed
6. whether a cluster-only workaround exists

## Permissions And Sandbox Behavior

The installer agent must be permission-aware.

Rules:

1. If cluster access is blocked by sandbox/network restrictions, the agent must request escalation or approval before continuing.
2. If a command fails because the cluster API cannot be reached from the sandbox, the agent should treat that as an execution-environment issue first, not an install bug.
3. The agent should not silently skip important cluster checks due to permission failures.
4. Destructive actions should only be taken when the user has explicitly approved or requested them.

Typical permission-sensitive actions:

- accessing the OpenShift cluster API
- pulling/pushing images
- deleting namespaces
- editing cluster-scoped resources during diagnosis

## Stop Conditions

The installer agent should stop and ask the user when:

- required MAS values are missing
- cluster context is ambiguous
- a destructive action is needed without prior approval
- unrelated unexpected repo changes directly conflict with the task

## Reporting Format

At the end of an install attempt, the agent should summarize:

- install result: success/failure
- furthest completed phase
- blockers found
- repo changes made
- whether published artifacts also need updating
- exact next step

## Success Criteria

A successful install validation means:

- operator CSV is `Succeeded`
- operator deployment is healthy
- IAM core services are ready
- SCIM bridge rollout succeeds
- MAS profile bootstrap job completes

## Preferred Default Position

The installer agent should prefer:

- the standard script entry points
- minimal manual intervention
- evidence-backed diagnosis
- repo fixes over cluster hot-patches when the issue is clearly in source or published artifacts
