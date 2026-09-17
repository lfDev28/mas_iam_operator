# MAS External Services Toolkit (mas-est)

`mas-est` installs a lab of the external services MAS depends on (LDAP, an OIDC/SAML identity provider, SCIM provisioning, S3 storage and SMTP) onto the OpenShift cluster running MAS, and wires them into MAS. It's for support engineers who need to reproduce identity and integration issues without a customer's Entra, Okta or cloud subscription.

One `mas-est install` gives you:

- **OpenLDAP**, set up in MAS as an LDAP login provider
- **Keycloak**, set up in MAS as both an OIDC and a SAML login provider, with self-registration on first login
- **A SCIM bridge** that creates Keycloak users in MAS through the SCIM API
- **MinIO** (S3), connected to MAS through an `ObjectStorageCfg`
- **Mailpit**, a capture-only SMTP server with a web UI
- **Demo users** for every login path: `ldap.user1`, `oidc.user1`, `saml.user1`, `scim.user1` (and `*.user2`)

## Install

You need `podman`, `oc` (logged in with cluster-admin rights) and a MAS API key with **userAdmin** and **systemAdmin** permissions.

```bash
export MAS_EST_IMAGE='quay.io/lee_forster/mas-external-services-tool:v0.1.9'
mkdir -p "$HOME/mas-est"
podman run -ti --rm -v "$HOME/mas-est:/tmp" --pull always "$MAS_EST_IMAGE" bootstrap --force
export PATH="$HOME/mas-est:$PATH"

mas-est preflight
mas-est install
```

The install is interactive, takes 15–20 minutes, and runs as a Job inside the cluster, so it survives a closed laptop. To upgrade, re-run the `podman run` line with the new tag.

## Commands

| Command | Purpose |
|---|---|
| `preflight` | Check the cluster, storage, MAS URL and API key before installing |
| `install` | Install selected components and configure MAS |
| `status` / `logs` | Health and component logs (`--component bridge`, `install-job`, …) |
| `details` / `ldap-info` | Connection details for each service |
| `config view` / `config set` | View or change SCIM bridge settings, rotate the MAS API key |
| `mas-auth apply` / `delete` | Create or remove the MAS LDAP/OIDC/SAML providers |
| `support-bundle` | Collect redacted diagnostics for a bug report |
| `uninstall` | Remove the `mas-est` namespace |

## Documentation

- **[Guide](docs/GUIDE.md)**: install, logins, connection details, S3/SMTP setup, operations, troubleshooting, known limitations
- **[Release notes](docs/RELEASE-NOTES.md)**
- Design records: [in-cluster installer](docs/design/IN-CLUSTER-INSTALLER.md), [SCIM group scoping](docs/design/SCIM-GROUP-SCOPING.md)

## Repository layout

| Path | Contents |
|---|---|
| `tools/mas-iam-installer/` | The `mas-est` CLI (Go) |
| `services/scim-bridge/` | The SCIM bridge (Go) |
| `operators/mas-iam-operator/` | Helm-based operator for Keycloak, OpenLDAP and PostgreSQL |
| `scripts/`, `manifests/`, `env/` | Install engine and manifests bundled into the CLI image |
| `images/` | Helper container images |

Releases are published with the `/mas-est-release` Claude Code skill (`.claude/skills/mas-est-release/`).
