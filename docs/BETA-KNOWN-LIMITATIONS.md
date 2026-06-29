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

**v0.1.0-beta.17 notes:**
- Bundled operator image rebuilt as `quay.io/lee_forster/mas-iam-operator:0.0.12` with the updated `helm-charts/mas-iam-stack/ldap-seed/dev-base.ldif` from commit `cf89d5d`. OpenLDAP now seeds `ldap.user1` / `ldap.user2` directly instead of the legacy `sysadmin/jane.doe/joe.bloggs/alex.manager` set. Catalog image bumped to `:catalog-0.0.12`. Older installs (using `catalog-0.0.11` and seeding the legacy users) still work with the `jane.doe / maxadmin` documented test path.
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

- group-based profile routing (planned for `v0.2.0`)
- precedence rules between `masProfile` and groups
- richer remediation for adopted existing users
- more advanced identity-source emulation
- broader UX polish and richer install summaries
