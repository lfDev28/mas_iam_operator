# MAS External Services Toolkit v0.1.0 Release Announcement

Team,

`mas-est` v0.1.0 is released and ready for use.

`mas-est` is an all-in-one helper for getting a working MAS External Services Toolkit plus SCIM bridge environment up quickly on OpenShift. I built it because getting a usable IAM lab together for support cases was taking too long and involved too many manual steps.

One command stands up:

- Keycloak (OIDC + SAML identity provider, seeded `maximo` realm)
- OpenLDAP (with seeded demo users)
- PostgreSQL
- the SCIM bridge into MAS (polls Keycloak, provisions `scim.*` users into MAS)
- MAS auth auto-configuration: LDAP, OIDC, and SAML IDPCfgs via the MAS Admin API, self-registration ConfigMap, and SCIM-user identity linking — login buttons show up green in the MAS Admin UI
- optional S3-compatible object storage (MinIO) wired into MAS as an ObjectStorageCfg
- optional SMTP capture (Mailpit)
- the cert and trust wiring needed between all of those
- grouped demo users per auth path: `ldap.user1/2`, `oidc.user1/2`, `saml.user1/2`, `scim.user1/2`

The main use case is support and troubleshooting work: reproducing SCIM issues, validating generic IAM user flows end-to-end (login through workspace entitlement and Manage user sync), or quickly building a clean environment to isolate whether a problem is in MAS or in a customer-specific identity setup.

## What v0.1.0 has been validated against

The release gate was two full clean-cluster validation runs (MAS 9.1.19 on OpenShift 4.18 and MAS 9.1.4 on 4.1x): bootstrap, preflight, install, operator readiness, all component readiness, MAS profile bootstrap, all four login paths (LDAP, OIDC, SAML, and SCIM-provisioned users via OIDC), Manage workspace user sync for every demo user, and S3 ObjectStorageCfg readiness.

This is a v0.1.0, not a finished platform. The supported story is one default demo profile flow that gets you a working environment quickly. It is not positioned as an Entra-equivalent provisioning platform. Read `docs/BETA-KNOWN-LIMITATIONS.md` for the support boundary — notably: Manage doclinks-to-S3 wiring is a documented manual follow-up (auto-wiring is planned for v0.1.1), and Mailpit is capture-only and not UI-validated.

There will still be cluster-specific issues. Storage defaults, image registry health, DNS, routes, certificates, and network access vary enough to expose install bugs. Please send those back with evidence so I can fix them.

## Install

```bash
export MAS_EST_IMAGE='quay.io/lee_forster/mas-external-services-tool:v0.1.0'
mkdir -p "$HOME/mas-est"
podman run -ti --rm -v "$HOME/mas-est:/tmp" --pull always $MAS_EST_IMAGE
export PATH="$HOME/mas-est:$PATH"

mas-est preflight
mas-est install
```

After install:

```bash
mas-est status --namespace mas-est
mas-est details --namespace mas-est --component all
mas-est logs --namespace mas-est --component bridge
```

If you want to connect MAS directly to the bundled OpenLDAP server, the install exposes the connection values through:

```bash
mas-est ldap-info --namespace mas-est
```

The LDAP bind password is stored in `secret/mas-est-iam-openldap-admin`, key `password`. The seeded demo user passwords are stored in `secret/mas-est-iam-openldap-user-passwords`. Per-provider connection details (LDAP, OIDC, SAML, S3, SMTP) are written to `mas-est-{provider}-connection` secrets. The CLI hides secret values by default; use `--show-secrets` variants only when you need to retrieve them.

## Reporting issues

Send evidence rather than a summary. The minimum useful capture is:

```bash
oc whoami
oc whoami --show-server
mas-est preflight
mas-est status --namespace mas-est
mas-est logs --namespace mas-est --component operator
mas-est logs --namespace mas-est --component keycloak
mas-est logs --namespace mas-est --component bridge
```

Or in one shot: `mas-est support-bundle --namespace mas-est`.

## Supporting docs

- `docs/BETA-QUICKSTART.md` — shortest supported path
- `docs/INSTALL-ALL-IN-ONE.md` — detailed install and operations guide
- `docs/BETA-KNOWN-LIMITATIONS.md` — support boundary and v0.1.1 roadmap
- `docs/MAS-EST-USER-GUIDE.md` — full user guide (PDF-buildable)
- `docs/DEMO-SCRIPT.md` — 10-minute demo walkthrough
- `docs/TEAM-INSTALL-INTERNAL.md`
