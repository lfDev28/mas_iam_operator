# MAS External Services Toolkit Known Limitations

This document defines the support boundary for `v0.1.x`.

## Scope

`v0.1.x` is designed to support one thing well:

- getting a working MAS External Services Toolkit plus SCIM bridge lab up quickly for support and troubleshooting work

The supported default flow is:

- one default MAS SCIM profile
- one primary MAS workspace flow
- demo users syncing successfully into MAS
- explicit storage-class selection when the cluster default is not suitable
- bug reports with enough install/status/log evidence to identify cluster-specific gaps

## Current Limitations

### Identity Provider Parity

`mas-est` is not a feature-for-feature replacement for Microsoft Entra or other enterprise IdPs.

Current limitations:

- no Entra-style expression mapping support such as `Switch(...)`
- no vendor-specific provisioning semantics
- no claim that customer-specific tenant policy can be reproduced outside the customer environment

### SCIM Profile Routing

Current profile routing is simple.

The bridge currently:

- uses the Keycloak user attribute `masProfile`
- falls back to the default profile when `masProfile` is not set or not required

The bridge does not currently:

- route users by group membership
- scope provisioning by Keycloak or LDAP group assignment
- provide Entra-style group-driven profile assignment

### Existing MAS Users

The cleanest path is a fresh install into a clean MAS-side profile context.

Known limitation:

- if MAS users already exist and the bridge adopts them after a conflict, workspace assignment may not be repaired automatically

Operationally, this means:

- fresh user creation is the supported default path
- stale MAS-side state may require cleanup and recreation in some cases

### Storage And Cluster Assumptions

The beta is more portable than before, but cluster prerequisites still matter.

Known realities:

- some clusters still need explicit block/RBD storage selection instead of the default
- image registry health still matters
- published operator and bridge artifacts must be current and reachable
- DNS, route, proxy, and certificate behavior varies by cluster
- beta testing cannot cover every OpenShift configuration before release

Operationally, failed installs should be treated as bug reports unless the same failure reproduces on the validated default path. Capture `preflight`, `status`, component logs, PVC state, and events before changing the cluster.

The CLI includes `mas-est support-bundle --namespace mas-est` to collect the common status, resource, event, log, configmap, and redacted secret evidence into a timestamped local directory.

### Single Logout / Cross-IDP Session Leakage

When you log out of MAS after authenticating via OIDC or SAML through the bundled Keycloak, the MAS-local session is cleared but the **Keycloak SSO session is not**. The visible symptom: if you then try to log in via a different provider (e.g. log in via OIDC, then log out, then try SAML), Keycloak reuses the existing SSO session and the second provider may fail with `AIUOM0100E` or auto-authenticate as the previous user.

The Keycloak-side configuration is correct (the SAML client has `saml_single_logout_service_url_post/redirect` set; the OIDC client has `post.logout.redirect.uris` set; both have `frontchannelLogout: true`), and the MAS SAML IDPCfg has `spInitiatedLogout: true`. The gap is that **MAS's UI logout button doesn't appear to exercise Liberty's IDP-side logout (SAML SLO / OIDC RP-initiated logout)** — it clears MAS-local cookies only. The MAS Admin API does not expose an `endSessionEndpointUrl` field for OIDC IDPCfgs (it expects Liberty to discover and use the value from `.well-known/openid-configuration` automatically), so this can't be patched from the installer.

Workarounds for beta testing:

- Use a private/incognito browser between provider switches.
- After logging out of MAS, also visit `https://<keycloak-host>/realms/maximo/protocol/openid-connect/logout` (no query parameters) to manually clear the Keycloak SSO session.

We'll revisit this in `v0.1.1+` if it surfaces in real beta usage; in the lab/support context it's a minor inconvenience.

### SMTP / Mailpit

The installer deploys Mailpit as a capture-only SMTP server. Wiring MAS Suite and MAS Manage to send through it works but is **not validated end-to-end in the beta** — neither Suite SMTP user/password flows nor Manage's workflow email outbound were UI-tested before release.

What's known:

- Mailpit pod runs and the web UI route is reachable at `mas-mailpit.apps.<cluster-domain>` (smoke-tested).
- The connection details (`mas-est-smtp-connection` ConfigMap) are correct and match how Mailpit is configured.
- MAS Suite SMTP and MAS Manage `mail.smtp.*` properties are NOT auto-wired by the installer; users must set them manually in the respective Admin UIs.

What's not validated:

- That MAS Suite-generated emails (welcome, password reset, self-reg) arrive in Mailpit.
- That MAS Manage workflow/escalation emails arrive in Mailpit.
- That `mxe.smtp.user`/`mxe.smtp.password=null` correctly disables auth on the Manage side.

Mailpit is also strictly **capture-only** — it does not deliver to real inboxes (gmail etc.) and does not serve POP/IMAP, so inbound listeners (`LSNRCRON` etc.) won't work. If you need real delivery you must point MAS at a proper SMTP server.

See `OBJECT-STORAGE-POC.md` and `INSTALL-ALL-IN-ONE.md` for the documented Mailpit wire-up steps.

### Product Scope

`v0.1.x` is not positioned as:

- a finished v1.0 product (breaking changes are still possible between minor versions)
- a general-purpose identity simulation platform
- a complete multi-profile isolation solution

## What Is Supported

**v0.1.2 release notes:**
- `mas-est preflight` now discovers MAS API routes the same way `install` does: the SCIM base URL prompt is pre-filled from a single detected route, offers a picker when several are found, and prints the detected-route hints before prompting.

**v0.1.1 release notes:**
- Fixed: deselecting options in the interactive multi-select prompts ("Products to install", "MAS auth providers to create") was silently ignored — the resolved config always kept the full default set (e.g. OIDC stayed enabled after being unchecked, which breaks installs on MAS 9.0 where the OIDC Admin API does not exist). Cause: the survey library appends multi-select answers to a pre-seeded response slice, so the result was the union of defaults and picks. Workaround on v0.1.0: pass the selection as flags (`--components ...`, `--mas-auth-providers ...`).
- Known (unchanged in v0.1.1): the OIDC auth provider requires MAS 9.1+ (`PUT /config/oidc/{id}` returns 404 AIUCO1022E on 9.0). Select only `ldap,saml` on MAS 9.0. A preflight guard is planned.

**v0.1.0 release notes:**
- v0.1.0 is the release build of beta.24 — identical code, release version strings. The beta.22–24 notes below describe the fixes that landed since the last widely-tested beta (beta.21): SCIM-user Manage sync (identity key + forced re-sync) and the MinIO RWO Recreate strategy.

**v0.1.0-beta.24 notes:**
- The SCIM-user OIDC linker now flips `applications.manage.sync.state` (and `sync.status`) to `PENDING` when it rewrites a user's identities map. The SCIM bridge provisions users minutes before `mas-auth apply` runs the linker, so the user-sync agent's first Manage pass always lands with the pre-link identities and records a stale `local` MASUSERIDP row; the PENDING flip forces a re-sync with the corrected `default-oidc` identity on the agent's next poll. Without it, OIDC login into Manage cannot map SCIM-provisioned users. Verified end-to-end on a fresh itz-4mwtok install.

**v0.1.0-beta.23 notes:**
- MinIO deployment now uses `strategy: Recreate` instead of the default RollingUpdate. The data PVC is RWO (single-node attach), and the install flow re-applies the deployment with `MINIO_DOMAIN` added after bucket-init — under RollingUpdate the replacement pod deadlocks on a `Multi-Attach error` whenever the scheduler places it on a different node than the old pod. This was a scheduling lottery present since beta.9: installs where both pods landed on the same node worked; multi-worker clusters could hang the install at the MinIO wait. First observed on the itz-4mwtok TechZone cluster.
- The installer migrates pre-existing MinIO deployments to Recreate via a merge patch before the server-side apply (SSA alone cannot clear the server-defaulted `rollingUpdate` block, and the API server rejects `type: Recreate` while it is present).

**v0.1.0-beta.22 notes:**
- Restore SCIM-bridge → Manage workspace sync. Beta.15's IDPCfg rename (`mas-est-{type}` → `default`) silently broke `link-scim-users-oidc.sh` because the helper was still being invoked with the raw `default` idpId, so SCIM-managed users had `identities.default` injected — but MAS Manage's MEA validates against the post-reserved-word shape (`default-oidc`) and rejected the entry with `system#idpnotfound`. The installer now passes `providerKeyWithSuffix(oidcProviderID, "oidc")` to the linker (same translation already applied to selfreg and redirect_uri), and the linker REPLACES the auto-generated `_local`/`default` identities map with a single IDP-shaped entry so the final state matches what direct-OIDC-login users receive.

**v0.1.0-beta.21 notes:**
- Stronger OLM resolver race guard. The install script now applies the CatalogSource (and other operator resources) WITHOUT the Subscription first, then waits for both `CatalogSource.status.connectionState.lastObservedState == READY` AND `packagemanifests/mas-iam-operator.status.channels[*].currentCSV` to be populated, with a 30s settle window after the package first appears. Only then is the Subscription applied. Beta.19's `lastObservedState=READY` guard was insufficient: READY indicates the catalog's gRPC port is open, but OLM's packageserver cache (which is what the Subscription's resolver actually reads) can lag by 30-60s — we saw this when catalog-0.0.14 was pushed but OLM still installed v0.0.13 from cached resolver state.
- ObjectStorageCfg wait bumped from 10m → 20m. The MAS Suite operator can take >10m to reconcile ObjectStorageCfg on heavily-loaded clusters even when the credentials secret + cfg are applied correctly — we saw 12m on the mas91 cluster. 10m was producing false failures.
- Hardening for fresh-cluster installs: the install script now waits for `CatalogSource.status.connectionState.lastObservedState == READY` before allowing the Subscription to resolve. Prevents the OLM resolver race where a Subscription applied seconds after a fresh CatalogSource could create an InstallPlan against partially-loaded catalog state and install the wrong CSV version.
- New `postgresql-anyuid-rolebinding.yaml` chart template that binds the PostgreSQL ServiceAccount to the `anyuid` SCC on OpenShift, matching the same defensive pattern already used by keycloak and openldap. The bitnami postgres chart sets `runAsUser: 1001, runAsNonRoot: true` which historically scheduled "by accident" on most OCP clusters (restricted-v2 rewrites the uid to a project-range one), but failed on clusters with tighter SCC config.
- Bundled operator image rebuilt as `quay.io/lee_forster/mas-iam-operator:0.0.14`. Catalog image at `:catalog-0.0.14`. Includes both the LDIF refresh (`ldap.user1`/`ldap.user2` seeded directly) and the OpenLDAP `runAsUser/SCC anyuid` fixes that were missed in 0.0.12 — the osixia openldap image's startup scripts need to run as root inside the container, so `containerSecurityContext.enabled=false, runAsUser=0, fsGroup=0` is required. The `openldap-anyuid-rolebinding` template now unconditionally binds the openldap SA to the `anyuid` SCC when openldap is enabled on OpenShift, matching the keycloak pattern; the previous `needsAnyuid` heuristic skipped the binding when `runAsNonRoot=true` was set, which on the cluster surfaced as `ReplicaFailure: FailedCreate ... must be in the ranges`.
- LDAP `userIdMap` regression fix in the MAS Admin API path (was `*:uid`, now bare `uid` via the shared `defaultMASAuthLDAPUserIDMap` constant). Without this every LDAP login surfaced as `CWIML4537E` / "invalid username/password" because MAS Liberty's `customUserRegistry` threw a JNDI `InvalidSearchFilterException` at filter-build time.
- IDPCfg `idpId` defaulted to `default` (was `mas-est-{type}`) so the MAS Admin UI's "Configured?" indicator shows green for LDAP / OIDC / SAML. MAS treats `default` as a reserved word and appends `-{type}` for OIDC redirect_uri and selfreg lookups — the installer now handles both sides (Keycloak client gets the extra redirect URI; selfreg ConfigMap keys are written under `default-{type}`).
- The installer now auto-bumps `{instance}-entitymgr-idpcfg` memory to 2Gi via the Suite CR `podTemplates` (durable; the deployment-level patch gets reverted by the Suite + MAS operators). Default 512Mi reliably OOMKills the finalizer playbook when several IDPCfgs reconcile together. Override via `--idpcfg-memory-limit=<size>` or `MAS_EST_IDPCFG_MEMORY_LIMIT`; pass `off` to skip.

Reasonable things to rely on in `v0.1.x`:

- local bootstrap of `mas-est` from the published container image
- `mas-est preflight`
- `mas-est install` (with phased progress output and per-provider connection secrets written on completion)
- `mas-est status`
- `mas-est logs`
- `mas-est details` — print the aggregated connection details secret in redacted form
- `mas-est ldap-info` — print bundled OpenLDAP connection values
- `mas-est support-bundle`
- `mas-est config view`
- `mas-est config set mas-api-token` — rotate the MAS API token name/value; restarts `deployment/scim-bridge` automatically
- `mas-est config set bridge` — toggle bridge log level and payload logging
- `mas-est restart bridge`
- `mas-est uninstall`

Experimental surface (post-beta work, supported on a best-effort basis):

- `mas-est mas-auth apply` — create MAS LDAP, OIDC, and SAML IDPCfg resources backed by the installed OpenLDAP and Keycloak services
- `mas-est object-storage install-minio` and `install-rook-ceph` — stand up S3-compatible object storage
- `mas-est smtp install-mailpit` — SMTP capture service

## What Is Planned Later

These are good next-phase enhancements, but they are not part of the `v0.1.x` promise:

- auto-wiring MAS Manage doclinks to the installed S3 storage (scripting the `mxe.cos*` system properties as an optional install step, planned for `v0.1.1`) — currently a manual follow-up documented in `OBJECT-STORAGE-POC.md`
- SCIM bridge post-provision identity remediation for users created after `mas-auth apply` has run (the install-time linker covers users provisioned before it; later arrivals keep a `_local`-only identity until the linker runs again, planned for `v0.1.1`)
- group-based profile routing (planned for `v0.2.0`)
- precedence rules between `masProfile` and groups
- richer remediation for adopted existing users
- more advanced identity-source emulation
- broader UX polish and richer install summaries
