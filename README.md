# MAS External Services Toolkit (mas-est)

`mas-est` installs the external services MAS depends on (LDAP, an OIDC and SAML identity provider, SCIM provisioning, S3 storage and SMTP) onto the OpenShift cluster running MAS, and connects them to MAS. It's for support engineers reproducing identity and integration issues without a customer's Entra, Okta or cloud subscription.

One `mas-est install` gives you:

- **OpenLDAP**, set up in MAS as an LDAP login provider
- **Keycloak**, set up in MAS as an OIDC and a SAML login provider, with self-registration on first login
- **A SCIM bridge** that creates Keycloak users in MAS through the SCIM API
- **MinIO** (S3), connected to MAS through an `ObjectStorageCfg`
- **Mailpit**, a capture-only SMTP server with a web inbox
- **Demo users** for every login path: `ldap.user1`, `oidc.user1`, `saml.user1`, `scim.user1`, and `*.user2`

## Install

You need `podman`, `oc` logged in as cluster-admin, and a MAS API key with the **userAdmin** and **systemAdmin** permissions.

```bash
export MAS_EST_IMAGE='quay.io/lee_forster/mas-external-services-tool:v0.1.13'
mkdir -p "$HOME/mas-est"
podman run -ti --rm -v "$HOME/mas-est:/tmp" --pull always "$MAS_EST_IMAGE" bootstrap --force
export PATH="$HOME/mas-est:$PATH"

mas-est preflight
mas-est install
```

The install is interactive, takes 20 to 30 minutes, and runs as a Job inside the cluster. To upgrade to the newest release, run `mas-est update`.

## Commands

| Command | Purpose |
|---|---|
| `preflight` | Check the cluster, storage, MAS URL and API key before installing |
| `install` | Install the selected components and configure MAS |
| `status`, `logs` | Health and component logs (`--component bridge`, `install-job`, and others) |
| `details`, `ldap-info` | Connection details for each service |
| `config view`, `config set` | View or change SCIM bridge settings, rotate the MAS API key |
| `mas-auth apply`, `mas-auth delete` | Create or remove the MAS LDAP, OIDC and SAML login providers |
| `support-bundle` | Collect redacted diagnostics for a bug report |
| `update` | Update the local mas-est to the newest release (`--check` only reports) |
| `uninstall` | Remove the `mas-est` namespace |

## Documentation

- [Announcement](docs/ANNOUNCEMENT.md) ([PDF](docs/ANNOUNCEMENT.pdf)): what it is and who it's for
- [Guide](docs/GUIDE.md) ([PDF](docs/GUIDE.pdf)): install, log in, connect S3 and SMTP, day-2 operations, uninstall
- [Troubleshooting](docs/TROUBLESHOOTING.md) ([PDF](docs/TROUBLESHOOTING.pdf)): error text, cause and fix
- [Known limitations](docs/KNOWN-LIMITATIONS.md) ([PDF](docs/KNOWN-LIMITATIONS.pdf))
- [Release notes](docs/RELEASE-NOTES.md) ([PDF](docs/RELEASE-NOTES.pdf))
- Design records: [in-cluster installer](docs/design/IN-CLUSTER-INSTALLER.md), [SCIM group scoping](docs/design/SCIM-GROUP-SCOPING.md)

## Repository layout

| Path | Contents |
|---|---|
| `tools/mas-iam-installer/` | The `mas-est` CLI (Go) |
| `services/scim-bridge/` | The SCIM bridge (Go) |
| `operators/mas-iam-operator/` | Helm-based operator for Keycloak, OpenLDAP and PostgreSQL |
| `scripts/`, `manifests/`, `env/` | Install engine and manifests bundled into the CLI image |
| `.claude/skills/mas-est-release/` | The release procedure, run as the `/mas-est-release` Claude Code skill |
