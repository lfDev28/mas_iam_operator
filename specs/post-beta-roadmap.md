# Post-Beta Roadmap

This document captures the next-phase features that should be considered after the internal beta is stable.

The goal is to avoid scope drift during the beta while still keeping the real product direction explicit.

## Intent

The beta should prove that `mas-iam` can reliably stand up a working IAM lab and default SCIM flow.

Post-beta work should focus on making the tool:

- easier to operate after initial install
- better at support diagnostics
- more flexible for real customer-style routing scenarios
- less dependent on manual cleanup when state drifts

## Priority 1: Runtime Configuration Management

### Secret-Based MAS API Token Updates

We should support rotating the MAS API token from an OpenShift `Secret` without requiring a full reinstall.

Desired capability:

- update `SCIM_BRIDGE_MAS_API_TOKEN_NAME`
- update `SCIM_BRIDGE_MAS_API_TOKEN_VALUE`
- keep the authoritative source in an OpenShift `Secret`
- document or automate the minimal restart needed

Current reality:

- the bridge deployment consumes these values through `secretKeyRef` environment variables
- environment variables are resolved when the pod starts
- updating the `Secret` does not update the running pod automatically

Operational implication today:

- after changing the secret, `deployment/scim-bridge` needs a restart
- if the MAS profile bootstrap job needs the new token too, that job must be recreated or rerun because completed jobs do not re-read updated env values
- Keycloak, OpenLDAP, and PostgreSQL do not need to restart for MAS token rotation

Post-beta feature target:

- a documented and preferably CLI-backed workflow such as `mas-iam config set mas-api-token ...`
- optional `mas-iam restart bridge` or `mas-iam reconcile bridge-config`
- clear operator guidance on which components must restart and which do not

## Priority 2: Debug Logging And Payload Visibility

### Toggleable Debug Logging

We should add a supported debug mode that can be enabled from configuration rather than by rebuilding images or editing manifests manually.

Desired capability:

- enable verbose bridge logging from an OpenShift `Secret` or config path
- control normal log level separately from payload logging
- allow fast enable/disable during support cases

### SCIM Payload Logging

We should support optional logging of outbound SCIM requests and key inbound responses.

Desired capability:

- log outbound create, update, and patch payloads
- log useful response details for failures
- keep sensitive values redacted where appropriate
- keep this disabled by default

Important constraint:

- payload logging is sensitive and should be treated as a support/debug mode only
- logs must avoid leaking secrets or tokens
- it should be explicit when payload logging is enabled

Post-beta feature target:

- secret-backed or config-backed debug switches
- CLI support to enable and disable them safely
- a clear redaction policy for secrets, tokens, and other sensitive fields

## Priority 3: Group-Based Profile And User Routing

This is the main bridge capability gap relative to more feature-rich provisioning systems.

Desired capability:

- use Keycloak group membership to determine the target MAS SCIM profile
- optionally scope which users are in or out based on group membership
- support clearer separation between demo groups or support scenarios

Current reality:

- profile routing is based on the user attribute `masProfile`
- group membership is not currently used by the bridge

Design questions that need explicit answers:

- what should take precedence: `masProfile` or group-derived routing?
- should one user be allowed to match more than one profile path?
- what should happen when a user has no matching group?
- should group-based routing also control whether the user is provisioned at all?

Post-beta feature target:

- group-to-profile mapping
- clear precedence rules
- optional group-based inclusion/exclusion
- documentation that explains the supported routing model simply

## Other Features Worth Prioritising For Non-Beta Release

### Existing User Repair / Reconciliation

This is a strong candidate for non-beta because it already showed up in real testing.

Problem:

- if MAS users already exist and are adopted after SCIM conflict, workspace assignment may not be repaired automatically

Useful feature:

- a safe reconciliation path that can detect and repair adopted users missing expected workspace access
- a support command such as `mas-iam reconcile users` or a bridge mode that can backfill expected state

### Support Bundle / Diagnostics Export

For support use, a single command to gather useful evidence would reduce friction.

Useful feature:

- `mas-iam support-bundle`
- capture `preflight`, `status`, selected logs, deployment yaml, PVC state, and recent events
- write it to a timestamped local directory for sharing

### Better Bridge Observability

Useful additions:

- last successful sync timestamp
- number of users scanned in the last cycle
- number of creates, updates, skips, and failures
- current bridge mode and active routing configuration

This would make support triage faster than reading raw logs every time.

### Config Editing And Apply Workflow

Useful feature:

- a CLI workflow for viewing and editing current runtime config without reinstalling
- examples:
  - `mas-iam config view`
  - `mas-iam config set`
  - `mas-iam config apply`

This would make token rotation, debug toggles, and future routing changes much easier to operate.

### Upgrade / Refresh Flow

Useful feature:

- a supported path to refresh the local runtime or cluster-side bridge config without doing a full wipe and reinstall
- examples:
  - `mas-iam self-update`
  - `mas-iam upgrade bridge`

## Suggested Sequencing After Beta

1. Runtime config and token rotation workflow
2. Debug logging and payload logging with redaction
3. Existing-user reconciliation for adopted MAS users
4. Group-based routing model
5. Better diagnostics and support-bundle commands

## What Should Not Block Beta

These items are important, but they should not block the internal beta release if the default install and default demo flow are stable:

- group-based routing
- payload debug mode
- post-install config editing
- advanced reconciliation logic

The beta should stay focused on proving the default install path and default support workflow first.
