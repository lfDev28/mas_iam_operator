# MAS IAM Beta Validation Capture

This page is a capture checklist for producing screenshots or terminal snippets from a clean beta install.

The supported install instructions live in:

- [BETA-QUICKSTART.md](BETA-QUICKSTART.md)
- [INSTALL-ALL-IN-ONE.md](INSTALL-ALL-IN-ONE.md)

Use this checklist only when preparing release notes, screenshots, or a walkthrough for the team.

## Capture Rules

- redact MAS API token values
- redact customer-sensitive hostnames when needed
- keep enough output to show success without pasting large logs
- prefer `mas-iam status` summaries over raw object dumps unless debugging

## 1. Bootstrap

Commands:

```bash
export MAS_IAM_IMAGE='quay.io/lee_forster/mas-iam-tool:v0.1.0-beta.5'
mkdir -p "$HOME/mas-iam"
podman run -ti --rm -v "$HOME/mas-iam:/tmp" --pull always "$MAS_IAM_IMAGE"
export PATH="$HOME/mas-iam:$PATH"
mas-iam version
mas-iam --help
```

Capture:

- image pull/bootstrap success
- `mas-iam version`
- top-level command help

## 2. Preflight

Command:

```bash
mas-iam preflight
```

Capture:

- current OpenShift user
- cluster API server
- storage class discovery
- MAS URL validation

## 3. Install

Command:

```bash
mas-iam install
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
mas-iam status --namespace iam
mas-iam logs --namespace iam --component bridge --tail 100
```

Capture:

- CSV `Succeeded`
- deployments ready
- jobs complete
- PVCs bound
- bridge log lines showing successful planning/sync behavior

Optional raw checks:

```bash
oc get pods -n iam -o wide
oc get deploy,statefulset,job,pvc,route -n iam
```

## 5. MAS-Side Confirmation

Capture from MAS:

- `demo` profile exists, or the chosen profile ID exists
- expected demo users are visible
- expected workspace assignment is present

Do not include tokens or customer-sensitive details in screenshots.

## 6. Optional Wipe

Command:

```bash
mas-iam wipe --namespace iam --profile-id demo
```

Capture:

- wipe confirmation
- namespace cleanup
- whether MAS profile deletion was performed or skipped
