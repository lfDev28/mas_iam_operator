# Release Notes

Three artifacts are versioned separately:

- **CLI:** `quay.io/lee_forster/mas-external-services-tool`
- **SCIM bridge:** `quay.io/lee_forster/mas-iam-scim-bridge`
- **Operator:** `quay.io/lee_forster/mas-iam-operator`, plus its `catalog-*` image

Each entry lists which versions it ships with.

## v0.1.10

CLI `v0.1.10` · bridge `scim-bridge-v0.1.2` · operator `0.0.15`

- **Fixed:** an in-cluster install with MAS auth but without the SCIM component ignored `--workspace-id`, so self-registered users landed in a workspace named `workspace` and couldn't open Manage. The Job now receives the workspace, interactive installs ask for it when auth is chosen without SCIM, and non-interactive installs require it.
- **Fixed:** `mas-est ldap-info` listed four LDAP users that don't exist (`alex.manager`, `jane.doe`, `joe.bloggs`, `sysadmin`). Only `ldap.user1` and `ldap.user2` are seeded. Existing installs keep the stale names until the next `mas-est install`.
- New docs: `docs/ANNOUNCEMENT.md`, `docs/TROUBLESHOOTING.md` and `docs/KNOWN-LIMITATIONS.md`; `docs/GUIDE.md` is now task-only.

## v0.1.9

CLI `v0.1.9` · bridge `scim-bridge-v0.1.2` · operator `0.0.15`

- **Fixed:** the upgrade command in v0.1.8's version-check error left out `bootstrap --force`, so it failed for anyone who already had a runtime. The error message and all documented bootstrap commands now include `--force`. It's also safe on a first install.

## v0.1.8

CLI `v0.1.8` · bridge `scim-bridge-v0.1.2` · operator `0.0.15`

- `install` prints `[version] mas-est vX.Y.Z` first.
- `install` fails if `MAS_EST_IMAGE` names a different version than the extracted CLI, because exporting a new image doesn't replace the binary and an old binary applies old installer permissions. `--skip-version-check` bypasses it. `mas-est version` now shows when the runtime was extracted.

## v0.1.7

CLI `v0.1.7` · bridge `scim-bridge-v0.1.2` · operator `0.0.15`

- **Fixed:** on a MAS instance with no SCIM profile yet, some SCIM users were created before the profile existed. They got no entitlement, never appeared in Manage, and MAS still reported sync `SUCCESS`. The install now creates the profile before it starts the SCIM bridge. Reinstalls were never affected.

## v0.1.6

CLI `v0.1.6` · bridge `scim-bridge-v0.1.2` · operator `0.0.15`

- **Fixed:** installs with S3 or SMTP failed creating Routes (`spec.host: Forbidden`). The installer Job now has the `routes/custom-host` permission.

## v0.1.5

CLI `v0.1.5` · bridge `scim-bridge-v0.1.2` · operator `0.0.15`

- **Fixed:** installs with S3 or SMTP failed reading the cluster's default IngressController. The installer Job can now read it.

## v0.1.4

CLI `v0.1.4` · bridge `scim-bridge-v0.1.2` · operator `0.0.15`

- **Change:** `install` now runs as a Kubernetes Job in the cluster by default, so a closed laptop or dropped VPN doesn't kill it. Use `--local` for the old behaviour.
- **Fixed:** the OpenLDAP PVC ignored `--storage-class` and hung on clusters with no default StorageClass.
- Preflight warns when the install would overwrite existing MAS providers with the `default` ID.

## Operator 0.0.15

- **Fixed:** the `MasIamStack` went `Irreconcilable` (`PASSWORDS ERROR`) straight after every install, so later changes were never applied. It was caused by the Bitnami PostgreSQL chart's password check under helm-operator. Don't set `spec.postgresql.auth.password: ""` in the CR, as that brings the failure back.

## v0.1.3

CLI `v0.1.3` · bridge `scim-bridge-v0.1.2`

- SCIM users are linked to SAML when OIDC isn't configured, for example on MAS 9.0.
- New preflight checks:
  - `mas-oidc-endpoint` fails early on MAS 9.0, which has no OIDC.
  - `mas-api-key` fails early when the key lacks userAdmin and systemAdmin.
- The SCIM bridge syncs users by Keycloak group (`SCIM_BRIDGE_INCLUDE_GROUPS`, default `mas-scim-users`) as well as the username prefix, removing the 50-user limit.
- Users removed from the group are deactivated in MAS, and reactivated if added back. This is experimental.

## v0.1.2

- Preflight finds MAS API routes the same way `install` does, and pre-fills the SCIM URL.

## v0.1.1

- **Fixed:** deselecting options in the interactive install prompts had no effect. For example, OIDC stayed selected, which broke installs on MAS 9.0.

## v0.1.0

First release. Validated end to end on MAS 9.1.19 and 9.1.4:

- Install of all components
- All four login paths (LDAP, OIDC, SAML, SCIM via OIDC)
- Manage sync for every demo user
- S3 `ObjectStorageCfg` reaching `Ready`
