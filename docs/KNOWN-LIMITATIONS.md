# Known limitations

One entry per limitation, with the workaround. `<instance>` is your MAS instance ID and `<workspace>` the MAS workspace ID.

**Not a replacement for Entra or Okta.** There are no vendor-specific provisioning rules or expression mappings. Use mas-est to narrow the problem, then confirm vendor behaviour in the customer's own tenant.

**MAS 9.0 has no OIDC API.** `install` stops at preflight with `AIUCO1022E`. Install with `--mas-auth-providers ldap,saml`, or leave `oidc` unselected.

**Logging out of MAS doesn't log you out of Keycloak.** The next login in the same browser returns the previous user. Use a private window per login, or open `https://<keycloak-host>/realms/maximo/protocol/openid-connect/logout`.

**MAS 9.1.20: the first SAML self-registration after the `<instance>-coreidp` pod starts fails once** with `AIUSC0019I`. This is a MAS bug in `samlresolver` 4.0.43, which loads the self-registration config lazily. Log in again. Don't restart the pod, because that resets the problem. MAS 9.1.19 and earlier, and OIDC, are unaffected.

**SCIM users added after the install aren't linked to a login identity.** They can't log in or reach Manage until you run `mas-est mas-auth apply --mas-instance-id <instance> --self-reg-workspace <workspace>`. From v0.1.16 that re-run keeps the existing OIDC client secret, so it no longer breaks OIDC logins.

**MAS 9.2: the SCIM identity linker does nothing.** MAS 9.2 keeps users in Manage, not in MongoDB, so the linker finds no users to link. Whether SCIM users can log in through OIDC on 9.2 is untested. LDAP, OIDC and SAML self-registration are unaffected.

**Deactivating users removed from `mas-scim-users` is experimental.** The bridge sets them inactive in MAS, and reactivates them when they're added back. This has never been validated on a cluster. Check the user in MAS after relying on it.

**One SCIM profile.** Every synced user lands in profile `demo`. To route a user elsewhere, set the Keycloak user attribute `masProfile` and map it with `SCIM_BRIDGE_MAS_PROFILE_MAP`.

**MinIO is a community fork.** MinIO stopped publishing its images and is no longer maintained, so the install uses Pigsty's drop-in fork (`docker.io/pgsty/minio`), pinned to a fixed release. It's fine for demos and testing. Don't treat it as production storage.

**Mailpit and Manage doclinks aren't connected automatically.** Both are manual steps in the [Guide](GUIDE.md#connect-smtp). Mail from MAS into Mailpit hasn't been tested end to end.

**`uninstall` leaves MAS-side state behind:** the login providers, the self-registration ConfigMap, user records in MongoDB and Manage, the installer's cluster-scoped RBAC, the operator CRD and the CatalogSource. The list and the cleanup commands are in the [Guide](GUIDE.md#uninstall).

**The install needs cluster-admin or an equivalent role,** for both the in-cluster Job and `--local`. It creates a ClusterRole, a CatalogSource in `openshift-marketplace`, an SCC and `anyuid` bindings, reads secrets in the MAS namespaces, and runs `pods/exec` on MongoDB. From v0.1.16, preflight checks this and names any missing permission.

**Re-running `mas-auth apply` with nothing to change can take up to 2 minutes longer per provider.** The install only trusts a provider's `Ready` status once it is newer than its own update. When MAS has nothing to reconcile, that status never changes, so the install waits 2 minutes before accepting it.

**Proxies:** the MAS Admin API calls honour `HTTPS_PROXY` and `NO_PROXY`, but the Keycloak route-certificate probe connects directly. When the probe can't connect, it falls back to the route's own certificate secret.

**The operator and the SCIM bridge are built for amd64 only.** From operator 0.0.16 the operator pod runs only on amd64 nodes. Keycloak, OpenLDAP, PostgreSQL, MinIO and the CLI are multi-arch.
