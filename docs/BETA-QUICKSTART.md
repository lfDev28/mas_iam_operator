# MAS External Services Toolkit Beta Quickstart

This is the shortest supported path for the internal beta.

Use it when you want a working MAS External Services Toolkit plus SCIM bridge environment quickly and you are happy to let the installer guide you through the prompts.

## What You Need

Local tools:

- `podman`
- `oc`
- `bash` 3.2+

Access and inputs:

- a MAS SCIM base URL ending in `/scim/v2`
- a MAS API token name and value with SCIM access
- the MAS workspace ID you want the demo profile to use

Example MAS SCIM URL:

```text
https://api.<mas-host>/scim/v2
```

## 1. Bootstrap The Local Command

Set the beta image:

```bash
export MAS_EST_IMAGE='quay.io/lee_forster/mas-external-services-tool:v0.1.0-beta.24'
```

Install the local `mas-est` command:

```bash
mkdir -p "$HOME/mas-est"
podman run -ti --rm -v "$HOME/mas-est:/tmp" --pull always "$MAS_EST_IMAGE"
export PATH="$HOME/mas-est:$PATH"
```

If you already bootstrapped an older runtime, overwrite it with:

```bash
podman run -ti --rm -v "$HOME/mas-est:/tmp" --pull always "$MAS_EST_IMAGE" bootstrap --force
export PATH="$HOME/mas-est:$PATH"
```

Confirm the command is available:

```bash
mas-est version
mas-est --help
```

## 2. Run Preflight

```bash
mas-est preflight
```

Preflight checks the active cluster context, basic tool availability, storage classes, and MAS URL shape. If you are not logged in to OpenShift yet, the CLI will offer to run `oc login` before continuing. If the cluster has more than one plausible storage class, note the block/RBD class names. You may need them during install.

## 3. Run Install

```bash
mas-est install
```

The installer prompts for:

- MAS SCIM base URL
- MAS API token name
- MAS API token value
- MAS workspace ID
- MAS profile ID, defaulting to `demo`
- PostgreSQL storage class
- SCIM bridge storage class

For the beta, choose an RBD/block-style storage class for PostgreSQL and the SCIM bridge PVC when one is available.

## 4. Check Health

```bash
mas-est status --namespace mas-est
mas-est logs --namespace mas-est --component bridge
```

To print the bundled OpenLDAP connection values:

```bash
mas-est ldap-info --namespace mas-est
```

To print generated connection details for installed services:

```bash
mas-est details --namespace mas-est --component all
```

If you selected MAS auth auto-configuration during install, the generated provider IDs are `mas-est-ldap`, `mas-est-oidc`, and `mas-est-saml`. OIDC is MAS 9.1+ only; the installer checks for `spec.oidc` support before creating an OIDC provider.

A healthy install should show:

- operator CSV `Succeeded`
- operator deployment ready
- IAM core / Keycloak ready
- OpenLDAP ready
- PostgreSQL running
- SCIM bridge ready
- MAS profile bootstrap job completed
- PostgreSQL and SCIM bridge PVCs bound

## 5. Reset If Needed

To remove the namespace and optionally delete the MAS profile:

```bash
mas-est uninstall --namespace mas-est --profile-id demo
```

Use `--skip-profile-delete` if you only want to remove the OpenShift lab resources.

## If Something Fails

Capture this output before changing the cluster:

```bash
oc whoami
oc whoami --show-server
mas-est preflight
mas-est status --namespace mas-est
mas-est logs --namespace mas-est --component operator
mas-est logs --namespace mas-est --component keycloak
mas-est logs --namespace mas-est --component bridge
```

Most beta install failures so far have come from cluster prerequisites: storage class defaults, image pull access, registry health, DNS, or certificate/route differences. Capture the evidence and treat those as beta bug reports.
