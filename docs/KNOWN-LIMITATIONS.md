# Known limitations

One entry per limitation, with the workaround. `<instance>` is your MAS instance ID and `<workspace>` the MAS workspace ID.

**Not a replacement for Entra or Okta.** There are no vendor-specific provisioning rules or expression mappings. Use mas-est to narrow the problem, then confirm vendor behaviour in the customer's own tenant.

**MAS 9.0 has no OIDC API.** `install` stops at preflight with `AIUCO1022E`. Install with `--mas-auth-providers ldap,saml`, or leave `oidc` unselected.

**Logging out of MAS doesn't log you out of Keycloak.** The next login in the same browser returns the previous user. Use a private window per login, or open `https://<keycloak-host>/realms/maximo/protocol/openid-connect/logout`.

**MAS 9.1.20: the first SAML self-registration after the `<instance>-coreidp` pod starts fails once** with `AIUSC0019I`. This is a MAS bug in `samlresolver` 4.0.43, which loads the self-registration config lazily. Log in again. Don't restart the pod, because that resets the problem. MAS 9.1.19 and earlier, and OIDC, are unaffected.

**SCIM users added after the install aren't linked to a login identity.** They can't log in or reach Manage until you run `mas-est mas-auth apply --mas-instance-id <instance> --self-reg-workspace <workspace>`.

**Deactivating users removed from `mas-scim-users` is experimental.** The bridge sets them inactive in MAS, and reactivates them when they're added back. This has never been validated on a cluster. Check the user in MAS after relying on it.

**One SCIM profile.** Every synced user lands in profile `demo`. To route a user elsewhere, set the Keycloak user attribute `masProfile` and map it with `SCIM_BRIDGE_MAS_PROFILE_MAP`.

**Mailpit and Manage doclinks aren't connected automatically.** Both are manual steps in the [Guide](GUIDE.md#connect-smtp). Mail from MAS into Mailpit hasn't been tested end to end.

**`uninstall` leaves MAS-side state behind:** the login providers, the self-registration ConfigMap, user records in MongoDB and Manage, the installer's cluster-scoped RBAC, the operator CRD and the CatalogSource. The list and the cleanup commands are in the [Guide](GUIDE.md#uninstall).

**The install Job needs permissions close to cluster-admin,** including reading secrets in the MAS namespaces and `pods/exec` on MongoDB. If your cluster's policy forbids this, install with `--local`.

**In-cluster install with MAS auth but without SCIM ignores the workspace.** The Job doesn't receive `--workspace-id` unless the `scim` component is selected, so self-registered users get a workspace named `workspace` and can't open Manage. Install with `--local`, which passes the value through, or re-run `mas-est mas-auth apply --mas-instance-id <instance> --self-reg-workspace <workspace>` afterwards.

**`mas-est ldap-info` lists four users that don't exist.** `alex.manager`, `jane.doe`, `joe.bloggs` and `sysadmin` are stale keys in the passwords Secret. Only `ldap.user1` and `ldap.user2` are in LDAP.
