# MAS IAM

`mas-iam` is an internal beta tool for standing up a working MAS IAM and SCIM bridge lab on OpenShift.

I built it because IAM support work often starts with the same slow setup: an identity provider, LDAP users and groups, certificate trust, SCIM provisioning into MAS, demo data, and a way to reset the environment when testing needs to start cleanly again. This repo turns that into one repeatable install path.

The current beta is focused on one thing: making it easy for support engineers and developers to get a usable IAM plus SCIM environment running quickly so they can reproduce issues, validate generic user lifecycle behavior, and separate MAS/IAM behavior from customer-specific identity-provider configuration.

## Current Beta Status

The beta install path has now been validated through a clean OpenShift install:

- local `mas-iam` bootstrap from the published image
- `mas-iam preflight`
- interactive `mas-iam install`
- MAS IAM operator install through OLM
- Keycloak, OpenLDAP, PostgreSQL, and SCIM bridge readiness
- MAS profile bootstrap
- SCIM bridge user sync into MAS
- `mas-iam status` and `mas-iam logs`
- explicit PostgreSQL and SCIM bridge storage-class selection

That is enough to begin an internal beta. It is not a guarantee that every OpenShift cluster configuration will work first time. Cluster storage, registry, DNS, pull policy, route, and certificate differences can still expose issues. The plan for beta is to collect those failures with evidence, fix the install path as they appear, and keep the supported flow tight.

## What This Project Is For

Use `mas-iam` when you need to:

- stand up a repeatable IAM lab on OpenShift
- reproduce SCIM user lifecycle issues
- test generic IAM login and provisioning flows
- create demo users, LDAP data, and SCIM resources quickly
- validate certificate and trust behavior between the components
- wipe and reinstall the lab without rebuilding it by hand

The default beta flow installs:

- MAS IAM operator
- Keycloak
- OpenLDAP
- PostgreSQL
- SCIM bridge
- demo LDAP users and groups
- one default MAS SCIM profile

## What It Is Not

This is not a final product installer yet, and it is not trying to emulate every enterprise identity provider.

In particular, the beta does not claim:

- Microsoft Entra feature parity
- Entra-style expression mapping support
- group-based SCIM profile routing
- full coverage for customer-specific tenant policy
- compatibility with every possible OpenShift storage and registry setup

The point is to give us a fast, open-source-style IAM and SCIM lab that covers the common support workflow. Customer-specific IdP behavior may still need customer-side validation.

## Install

Start here:

- [Beta quickstart](docs/BETA-QUICKSTART.md)
- [Detailed install and operations guide](docs/INSTALL-ALL-IN-ONE.md)

Short version:

```bash
export MAS_IAM_IMAGE='quay.io/lee_forster/mas-iam-tool:v0.1.0-beta.3'

mkdir -p "$HOME/mas-iam"
podman run -ti --rm -v "$HOME/mas-iam:/tmp" --pull always "$MAS_IAM_IMAGE"
export PATH="$HOME/mas-iam:$PATH"

mas-iam preflight
mas-iam install
mas-iam status --namespace iam
```

The installer is interactive. It prompts for the MAS SCIM URL, MAS API token, workspace/profile details, and storage classes. Users should not need to hand-edit manifests for the normal beta path.

## Operations

Common post-install tasks are documented in the install guide:

- [Checking health](docs/INSTALL-ALL-IN-ONE.md#check-health)
- [Updating the MAS API key](docs/INSTALL-ALL-IN-ONE.md#updating-the-mas-api-key)
- [Editable runtime values](docs/INSTALL-ALL-IN-ONE.md#editable-runtime-values)
- [Wiping and reinstalling](docs/INSTALL-ALL-IN-ONE.md#wipe-and-reinstall)
- [Troubleshooting](docs/INSTALL-ALL-IN-ONE.md#troubleshooting)

The two main cluster-side configuration objects are:

- `configmap/scim-bridge-config`
- `secret/scim-bridge-secret`

The bridge reads those values as environment variables when the pod starts, so config or secret changes normally require:

```bash
oc rollout restart deployment/scim-bridge -n iam
```

## Repository Layout

- `tools/mas-iam-installer/` - Go CLI that provides the local `mas-iam` command
- `scripts/` - shell install engine used by the CLI
- `manifests/` - OLM, sample stack, and SCIM bridge manifests
- `env/` - example/release environment defaults
- `docs/` - user and beta rollout documentation
- `specs/` - design notes, release planning, and agent operating docs
- `operators/mas-iam-operator/` - MAS IAM operator and chart assets

## Current Plan

The near-term plan is:

1. release this as an internal beta
2. keep the supported install surface focused on `mas-iam preflight`, `install`, `status`, `logs`, and `wipe`
3. collect real cluster failures and fix them as beta bug reports
4. tighten docs from real user feedback
5. publish immutable beta/release image tags, starting with `v0.1.0-beta.3`

Post-beta work is tracked in [specs/post-beta-roadmap.md](specs/post-beta-roadmap.md). The strongest next candidates are API key/config update workflows, support bundle collection, better diagnostics, and group-based profile routing.

## Reporting Beta Issues

If an install fails, capture evidence rather than only the final error:

```bash
oc whoami
oc whoami --show-server
mas-iam preflight
mas-iam status --namespace iam
mas-iam logs --namespace iam --component operator
mas-iam logs --namespace iam --component keycloak
mas-iam logs --namespace iam --component bridge
```

Also include the storage classes shown during install and whether the cluster can pull from the referenced image registries.
