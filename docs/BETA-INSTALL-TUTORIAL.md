# MAS External Services Toolkit Beta Validation Capture

This page is a capture checklist for producing screenshots or terminal snippets from a clean beta install.

The supported install instructions live in:

- [BETA-QUICKSTART.md](BETA-QUICKSTART.md)
- [INSTALL-ALL-IN-ONE.md](INSTALL-ALL-IN-ONE.md)

Use this checklist only when preparing release notes, screenshots, or a walkthrough for the team.

## 1. Bootstrap

Commands:

```bash
export MAS_EST_IMAGE='quay.io/lee_forster/mas-external-services-tool:v0.1.0-beta.14'
mkdir -p "$HOME/mas-est"
podman run -ti --rm -v "$HOME/mas-est:/tmp" --pull always "$MAS_EST_IMAGE"
export PATH="$HOME/mas-est:$PATH"
mas-est version
mas-est --help
```

Capture:

- image pull/bootstrap success
- `mas-est version`
- top-level command help

## 2. Preflight

Command:

```bash
mas-est preflight
```

Capture:

- current OpenShift user
- cluster API server
- storage class discovery
- MAS URL validation

## 3. Install

Command:

```bash
mas-est install
```

Capture:

- cluster context banner
- MAS URL prompt, with sensitive hostnames redacted if needed
- token name prompt
- token value prompt, with value hidden/redacted
- workspace ID prompt
- profile ID prompt
- PostgreSQL storage class selection
- SCIM bridge storage class selection
- final install success summary

## 4. Health Check

Commands:

```bash
mas-est status --namespace mas-est
mas-est logs --namespace mas-est --component bridge --tail 100
```

Capture:

- CSV `Succeeded`
- deployments ready
- jobs complete
- PVCs bound
- bridge log lines showing successful planning/sync behavior

Optional raw checks:

```bash
oc get pods -n mas-est -o wide
oc get deploy,statefulset,job,pvc,route -n mas-est
```

## 5. MAS-Side Confirmation

Capture from MAS:

- `demo` profile exists, or the chosen profile ID exists
- expected demo users are visible
- expected workspace assignment is present

Do not include tokens or customer-sensitive details in screenshots.

## 6. Optional Uninstall

Command:

```bash
mas-est uninstall --namespace mas-est --profile-id demo
```

Capture:

- uninstall confirmation
- namespace cleanup
- whether MAS profile deletion was performed or skipped
