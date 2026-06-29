# MAS External Services Toolkit Beta Announcement Draft

Team,

I am starting an internal beta rollout of `mas-est`.

`mas-est` is an all-in-one helper for getting a working MAS External Services Toolkit plus SCIM bridge environment up quickly on OpenShift. I built it because getting a usable IAM lab together for support cases was taking too long and involved too many manual steps.

The tool gives us one repeatable path to stand up:

- Keycloak
- OpenLDAP
- PostgreSQL
- the SCIM bridge into MAS
- the cert and trust wiring needed between those components
- demo data to get moving quickly

The main use case is support and troubleshooting work. It is useful when we need to reproduce SCIM issues, validate generic IAM user flows, or quickly build a clean environment to isolate whether a problem is in MAS or in a customer-specific identity setup.

This is an internal beta, not a final polished product. The supported story right now is one default demo profile flow that gets us a working environment quickly. It is not positioned as an Entra-equivalent provisioning platform.

The beta install path has now completed a clean validation run: bootstrap, preflight, install, operator readiness, Keycloak/OpenLDAP/PostgreSQL readiness, SCIM bridge readiness, MAS profile bootstrap, and MAS-side user/profile verification. That is the release gate for starting the beta.

There will still be cluster-specific issues. Storage defaults, image registry health, DNS, routes, certificates, and network access can vary enough to expose install bugs. Please send those back with evidence so I can fix the beta path as they appear.

Recommended flow:

```bash
export MAS_EST_IMAGE='quay.io/lee_forster/mas-external-services-tool:v0.1.0-beta.20'
mkdir -p "$HOME/mas-est"
podman run -ti --rm -v "$HOME/mas-est:/tmp" --pull always $MAS_EST_IMAGE
export PATH="$HOME/mas-est:$PATH"

mas-est preflight
mas-est install
```

After install:

```bash
mas-est status --namespace mas-est
mas-est logs --namespace mas-est --component bridge
```

If you want to connect MAS directly to the bundled OpenLDAP server, the install exposes the connection values through:

```bash
mas-est ldap-info --namespace mas-est
```

The LDAP bind password is stored in `secret/mas-est-iam-openldap-admin`, key `password`. The seeded demo user passwords are stored in `secret/mas-est-iam-openldap-user-passwords`. The CLI hides those values by default; use `--show-password` or `--show-user-passwords` only when you need to retrieve them.

If you hit issues, please send evidence rather than a summary. The minimum useful capture is:

```bash
oc whoami
oc whoami --show-server
mas-est preflight
mas-est status --namespace mas-est
mas-est logs --namespace mas-est --component operator
mas-est logs --namespace mas-est --component keycloak
mas-est logs --namespace mas-est --component bridge
```

Supporting docs:

- `docs/BETA-QUICKSTART.md`
- `docs/INSTALL-ALL-IN-ONE.md`
- `docs/BETA-KNOWN-LIMITATIONS.md`
- `docs/TEAM-INSTALL-INTERNAL.md`
