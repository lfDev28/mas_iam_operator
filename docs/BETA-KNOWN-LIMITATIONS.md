# MAS External Services Toolkit Beta Known Limitations

This document defines the support boundary for the internal beta.

## Beta Scope

The current beta is designed to support one thing well:

- getting a working MAS External Services Toolkit plus SCIM bridge lab up quickly for support and troubleshooting work

The supported default flow is:

- one default MAS SCIM profile
- one primary MAS workspace flow
- demo users syncing successfully into MAS
- explicit storage-class selection when the cluster default is not suitable
- bug reports with enough install/status/log evidence to identify cluster-specific gaps

## Current Limitations

### Identity Provider Parity

`mas-est` is not a feature-for-feature replacement for Microsoft Entra or other enterprise IdPs.

Current limitations:

- no Entra-style expression mapping support such as `Switch(...)`
- no vendor-specific provisioning semantics
- no claim that customer-specific tenant policy can be reproduced outside the customer environment

### SCIM Profile Routing

Current profile routing is simple.

The bridge currently:

- uses the Keycloak user attribute `masProfile`
- falls back to the default profile when `masProfile` is not set or not required

The bridge does not currently:

- route users by group membership
- scope provisioning by Keycloak or LDAP group assignment
- provide Entra-style group-driven profile assignment

### Existing MAS Users

The cleanest path is a fresh install into a clean MAS-side profile context.

Known limitation:

- if MAS users already exist and the bridge adopts them after a conflict, workspace assignment may not be repaired automatically

Operationally, this means:

- fresh user creation is the supported default path
- stale MAS-side state may require cleanup and recreation in some cases

### Storage And Cluster Assumptions

The beta is more portable than before, but cluster prerequisites still matter.

Known realities:

- some clusters still need explicit block/RBD storage selection instead of the default
- image registry health still matters
- published operator and bridge artifacts must be current and reachable
- DNS, route, proxy, and certificate behavior varies by cluster
- beta testing cannot cover every OpenShift configuration before release

Operationally, failed installs should be treated as beta feedback unless the same failure reproduces on the validated default path. Capture `preflight`, `status`, component logs, PVC state, and events before changing the cluster.

The current CLI includes `mas-est support-bundle --namespace mas-est` to collect the common status, resource, event, log, configmap, and redacted secret evidence into a timestamped local directory.

### Product Scope

This beta is not yet positioned as:

- a final polished product
- a general-purpose identity simulation platform
- a complete multi-profile isolation solution

## What Is Supported

Reasonable things to rely on in the beta:

- local bootstrap of `mas-est` from the published container image
- `mas-est preflight`
- `mas-est install`
- `mas-est status`
- `mas-est support-bundle`
- `mas-est logs`
- `mas-est uninstall`
- one working default demo flow for SCIM user provisioning
- manual MAS API key rotation by updating `secret/scim-bridge-secret` and restarting `deployment/scim-bridge`

## What Is Planned Later

These are good next-phase enhancements, but they are not part of the current beta promise:

- group-based profile routing
- precedence rules between `masProfile` and groups
- richer remediation for adopted existing users
- more advanced identity-source emulation
- broader UX polish and richer install summaries
- CLI-backed config editing and token rotation
