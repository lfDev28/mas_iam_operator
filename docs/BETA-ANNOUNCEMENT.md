# MAS IAM Beta Announcement Draft

Team,

I am starting an internal beta rollout of `mas-iam`.

`mas-iam` is an all-in-one helper for getting a working MAS IAM plus SCIM bridge environment up quickly on OpenShift. I built it because getting a usable IAM lab together for support cases was taking too long and involved too many manual steps.

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
export MAS_IAM_IMAGE='quay.io/lee_forster/mas-iam-tool:v0.1.0-beta.5'
mkdir -p "$HOME/mas-iam"
podman run -ti --rm -v "$HOME/mas-iam:/tmp" --pull always $MAS_IAM_IMAGE
export PATH="$HOME/mas-iam:$PATH"

mas-iam preflight
mas-iam install
```

After install:

```bash
mas-iam status --namespace iam
mas-iam logs --namespace iam --component bridge
```

If you want to connect MAS directly to the bundled OpenLDAP server, the install exposes the connection values through:

```bash
mas-iam ldap-info --namespace iam
```

The LDAP bind password is stored in `secret/mas-iam-sample-openldap-admin`, key `password`. The seeded demo user passwords are stored in `secret/mas-iam-sample-openldap-user-passwords`. The CLI hides those values by default; use `--show-password` or `--show-user-passwords` only when you need to retrieve them.

If you hit issues, please send evidence rather than a summary. The minimum useful capture is:

```bash
oc whoami
oc whoami --show-server
mas-iam preflight
mas-iam status --namespace iam
mas-iam logs --namespace iam --component operator
mas-iam logs --namespace iam --component keycloak
mas-iam logs --namespace iam --component bridge
```

Supporting docs:

- `docs/BETA-QUICKSTART.md`
- `docs/INSTALL-ALL-IN-ONE.md`
- `docs/BETA-KNOWN-LIMITATIONS.md`
- `docs/TEAM-INSTALL-INTERNAL.md`
