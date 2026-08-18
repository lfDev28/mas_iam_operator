# MAS External Services Toolkit

`mas-est` is a support and troubleshooting tool for standing up a working MAS External Services Toolkit and SCIM bridge lab on OpenShift.

It exists because IAM support work often starts with the same slow setup: an identity provider, LDAP users and groups, certificate trust, SCIM provisioning into MAS, demo data, and a way to reset the environment when testing needs to start cleanly again. This repo turns that into one repeatable install path.

`mas-est` is aimed at support engineers and developers who need a usable IAM + SCIM environment running quickly so they can reproduce issues, validate generic user lifecycle behavior, and separate MAS/IAM behavior from customer-specific identity-provider configuration.

## Current Status

`v0.1.x` covers the validated install path:

- local `mas-est` bootstrap from the published image
- `mas-est preflight`
- interactive `mas-est install` with phased progress output
- MAS EST IAM operator install through OLM
- Keycloak, OpenLDAP, PostgreSQL, and SCIM bridge readiness
- optional MinIO S3 object storage + Mailpit SMTP capture
- optional MAS auth auto-configuration (LDAP / OIDC / SAML IDPCfgs)
- per-provider connection details Secrets/ConfigMaps for downstream wiring
- MAS profile bootstrap and SCIM bridge user sync into MAS
- `mas-est status`, `logs`, `details`, `ldap-info`, `support-bundle`

This is `v0.1.x` rather than `v1.0.x` because we expect minor breaking changes before stabilising — for example, SCIM bridge group-based profile routing and existing-user repair are still on the roadmap. Where the install path itself runs into cluster-specific issues (storage, registry, DNS, route, or certificate differences), capture evidence and treat those as bug reports against the validated default path.

## What This Project Is For

Use `mas-est` when you need to:

- stand up a repeatable IAM lab on OpenShift
- reproduce SCIM user lifecycle issues
- test generic IAM login and provisioning flows
- create demo users, LDAP data, and SCIM resources quickly
- validate certificate and trust behavior between the components
- uninstall and reinstall the lab without rebuilding it by hand

The default beta flow installs:

- MAS EST IAM operator
- Keycloak
- OpenLDAP
- PostgreSQL
- SCIM bridge
- demo LDAP users and groups
- one default MAS SCIM profile

Experimental post-beta work is exploring whether the project should broaden into a MAS external services toolkit. The first exploration is an OpenShift-hosted S3-compatible object storage path for reproducing MAS object storage integrations; see [MAS Object Storage POC](docs/OBJECT-STORAGE-POC.md).

## What It Is Not

`mas-est` does not try to emulate every enterprise identity provider.

In particular, `v0.1.x` does not claim:

- Microsoft Entra feature parity
- Entra-style expression mapping support
- group-based SCIM profile routing (planned for `v0.2.0`)
- full coverage for customer-specific tenant policy
- compatibility with every possible OpenShift storage and registry setup

The point is to give us a fast, lab-grade IAM and SCIM environment that covers the common support workflow. Customer-specific IdP behavior may still need customer-side validation.

## Install

Start here:

- [Beta quickstart](docs/BETA-QUICKSTART.md)
- [Detailed install and operations guide](docs/INSTALL-ALL-IN-ONE.md)
- [Connection details reference](docs/CONNECTION-DETAILS.md)
- [Beta install tutorial / capture checklist](docs/BETA-INSTALL-TUTORIAL.md)

Short version:

```bash
export MAS_EST_IMAGE='quay.io/lee_forster/mas-external-services-tool:v0.1.2'

mkdir -p "$HOME/mas-est"
podman run -ti --rm -v "$HOME/mas-est:/tmp" --pull always "$MAS_EST_IMAGE"
export PATH="$HOME/mas-est:$PATH"

mas-est preflight
mas-est install
mas-est status --namespace mas-est
```

The installer is interactive. It opens with a product catalog so users can choose LDAP only, Keycloak + LDAP, the full SCIM stack, S3 object storage, SMTP capture, or all services together. Users should not need to hand-edit manifests for the normal install path.

## Operations

Common post-install tasks are documented in the install guide:

- [Checking health](docs/INSTALL-ALL-IN-ONE.md#check-health)
- [Connection details](docs/INSTALL-ALL-IN-ONE.md#connection-details)
- [Getting LDAP connection details](docs/INSTALL-ALL-IN-ONE.md#ldap-connection-details)
- [MAS auth auto-configuration](docs/INSTALL-ALL-IN-ONE.md#mas-auth-auto-configuration)
- [Updating the MAS API key](docs/INSTALL-ALL-IN-ONE.md#updating-the-mas-api-key)
- [Editable runtime values](docs/INSTALL-ALL-IN-ONE.md#editable-runtime-values)
- [Uninstalling and reinstalling](docs/INSTALL-ALL-IN-ONE.md#uninstall-and-reinstall)
- [Troubleshooting](docs/INSTALL-ALL-IN-ONE.md#troubleshooting)

The two main cluster-side configuration objects are:

- `configmap/scim-bridge-config`
- `secret/scim-bridge-secret`

The bridge reads those values as environment variables when the pod starts, so config or secret changes normally require:

```bash
oc rollout restart deployment/scim-bridge -n mas-est
```

## Repository Layout

- `tools/mas-iam-installer/` - Go CLI that provides the local `mas-est` command
- `scripts/` - shell install engine used by the CLI
- `manifests/` - OLM, sample stack, and SCIM bridge manifests
- `env/` - example/release environment defaults
- `docs/` - user and beta rollout documentation
- `specs/` - design notes, release planning, and agent operating docs
- `operators/mas-iam-operator/` - MAS EST IAM operator and chart assets used by the IAM module

## Current Plan

The near-term plan is:

1. keep `v0.1.x` focused on the validated install + operate surface (`preflight`, `install`, `status`, `logs`, `details`, `ldap-info`, `uninstall`, plus the experimental `mas-auth apply`, `object-storage install-*`, and `smtp install-mailpit`)
2. collect real cluster failures and fix them in patch releases
3. tighten docs from real user feedback
4. publish immutable release image tags

Post-`v0.1.0` work — SCIM bridge group routing, existing-user repair, and broader product polish — is tracked in [docs/INITIAL-RELEASE-PLAN.md](docs/INITIAL-RELEASE-PLAN.md) and [specs/post-beta-roadmap.md](specs/post-beta-roadmap.md).

## Reporting Issues

If an install fails, capture evidence rather than only the final error:

```bash
oc whoami
oc whoami --show-server
mas-est preflight
mas-est status --namespace mas-est
mas-est logs --namespace mas-est --component operator
mas-est logs --namespace mas-est --component keycloak
mas-est logs --namespace mas-est --component bridge
```

Also include the storage classes shown during install and whether the cluster can pull from the referenced image registries.
