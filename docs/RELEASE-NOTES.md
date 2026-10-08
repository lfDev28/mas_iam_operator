# Release Notes

Three artifacts are versioned separately:

- **CLI:** `quay.io/lee_forster/mas-external-services-tool`
- **SCIM bridge:** `quay.io/lee_forster/mas-iam-scim-bridge`
- **Operator:** `quay.io/lee_forster/mas-iam-operator`, plus its `catalog-*` image

Each entry lists which versions it ships with.

## v0.1.15

CLI `v0.1.15` · bridge `scim-bridge-v0.1.2` · operator `0.0.15`

- **Fixed:** every fresh install with S3 failed at `Install MinIO S3`, with pod `mas-minio` in `ImagePullBackOff` (`quay.io/minio/minio: unauthorized`). MinIO stopped serving its images from Docker Hub and Quay in September 2026. The install now uses Pigsty's drop-in fork, `docker.io/pgsty/minio` and `docker.io/pgsty/mc`, pinned to fixed releases. All earlier versions are affected. On an existing install, run `mas-est install` again to switch the image.

## v0.1.14

CLI `v0.1.14` · bridge `scim-bridge-v0.1.2` · operator `0.0.15`

- **New:** `mas-est update` upgrades the local mas-est to the newest release on Quay, so you no longer need to look up the tag. `--check` only reports whether a newer release exists, and `--version vX.Y.Z` installs a specific one. Afterwards, export the new `MAS_EST_IMAGE` it prints. Upgrading from v0.1.13 or earlier still needs one manual bootstrap.

## v0.1.13

CLI `v0.1.13` · bridge `scim-bridge-v0.1.2` · operator `0.0.15`

- **Fixed:** a fresh install could fail at `Seeding 6 grouped demo users` with `command terminated with exit code 1`. On first start OpenLDAP generates its TLS parameters before it accepts connections, which can take several minutes, and the install didn't wait for it. The install now waits until OpenLDAP accepts LDAPS connections. All earlier versions are affected, intermittently.

## v0.1.12

CLI `v0.1.12` · bridge `scim-bridge-v0.1.2` · operator `0.0.15`

- **Fixed:** every fresh install with SCIM failed with `timed out waiting for the condition on jobs/scim-bridge-keycloak-route-cert`. The Job used `registry.redhat.io/openshift4/ose-cli` without a tag, and Red Hat no longer serves it that way. The Job now runs on the cluster's own OpenShift CLI image. All earlier versions are affected.
- **Fixed:** the in-cluster install's log stream ended with `log stream ended early` when the connection dropped during a quiet step. `install` now reconnects and resumes where it left off.

## v0.1.11

CLI `v0.1.11` · bridge `scim-bridge-v0.1.2` · operator `0.0.15`

- **Fixed:** on OpenShift Data Foundation clusters, preflight recommended the `localblock` storage class, which can't provision new volumes. Accepting the default left every PVC `Pending`, and the install failed with `deployment "mas-est-iam-openldap" exceeded its progress deadline`. Preflight now never recommends a class with provisioner `kubernetes.io/no-provisioner`, and prefers the Ceph RBD class.

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
