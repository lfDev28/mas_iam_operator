---
title: "MAS External Services Toolkit (mas-est)"
subtitle: "v0.1.15"
date: "2026-09-17"
---

# What it is

`mas-est` installs the external services MAS depends on onto the OpenShift cluster that runs MAS, and connects them to MAS. One command gives you LDAP, an OIDC and SAML identity provider, SCIM provisioning, S3 storage and SMTP, with demo users for every login path, in 20 to 30 minutes.

# Why it exists

Most identity and integration cases need the customer's Entra, Okta or cloud storage subscription to reproduce. Support engineers don't have those. `mas-est` gives you the same MAS configuration against services you control, so you can follow the customer's steps, compare against a working baseline, and narrow the problem before going back to them.

# What you get

| Component                                                                        | Connected to MAS as                                                                   |
| -------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| OpenLDAP with users `ldap.user1` and `ldap.user2`                                | LDAP login provider                                                                   |
| Keycloak (realm `maximo`) with `oidc.user1/2`, `saml.user1/2` and `scim.user1/2` | OIDC and SAML login providers, with self-registration on first login                  |
| SCIM bridge                                                                      | SCIM profile `demo`. Creates the `scim.*` users in MAS and syncs them every 5 minutes |
| MinIO (S3) with bucket `mas-s3-demo`                                             | `ObjectStorageCfg` for MAS Suite                                                      |
| Mailpit, an SMTP capture server with a web inbox                                 | Not connected automatically. Settings are in the Guide                                |

Everything runs in the `mas-est` namespace. The MAS configuration goes into `mas-<instance>-core`, where `<instance>` is your MAS instance ID.

# Quick start

You need `podman`, `oc` logged in as cluster-admin, and a MAS API key with the **userAdmin** and **systemAdmin** permissions.

```bash
export MAS_EST_IMAGE='quay.io/lee_forster/mas-external-services-tool:v0.1.15'
mkdir -p "$HOME/mas-est"
podman run -ti --rm -v "$HOME/mas-est:/tmp" --pull always "$MAS_EST_IMAGE" bootstrap --force
export PATH="$HOME/mas-est:$PATH"
mas-est preflight
mas-est install
```

`install` asks for the MAS SCIM URL, the API key and the workspace, then runs as a Job inside the cluster, so a closed laptop doesn't interrupt it. A full install takes 20 to 30 minutes, most of it waiting for MAS to accept the login providers. When it finishes, open MAS in a private browser window and log in as `oidc.user1` with password `maxadmin`.

# Supported MAS versions

MAS 9.0.x and 9.1.x. MAS 9.0 has no OIDC API, so use LDAP and SAML there. MAS 9.2 has not been tested yet.

# Limitations to know up front

- **MAS 9.0 has no OIDC.** Leave `oidc` out of the providers.
- **MAS 9.1.20: the first SAML login after the `<instance>-coreidp` pod starts fails once.** Log in again. This is a MAS bug, not a toolkit bug. Don't restart the pod, because that brings the failure back.
- **SCIM users you add after the install can't log in until you run `mas-est mas-auth apply` again.**

The full list, with workarounds, is in [Known limitations](KNOWN-LIMITATIONS.md).

# Getting help

Run `mas-est support-bundle` and attach the directory it creates. It contains status, events, logs and configuration with secret values removed. Ping me on slack or email lee.forster@ibm.com

# Documentation

- [README](../README.md): overview and command list
- [Guide](GUIDE.md): install, log in, connect S3 and SMTP, day-2 operations, uninstall
- [Troubleshooting](TROUBLESHOOTING.md): error text, cause and fix
- [Known limitations](KNOWN-LIMITATIONS.md)
- [Release notes](RELEASE-NOTES.md)
