---
title: "MAS External Services Toolkit"
subtitle: "User Guide — `mas-est` v0.1.0-beta.17"
author: "MAS Support Engineering"
date: "\\today"
titlepage: true
titlepage-color: "0F62FE"
titlepage-text-color: "FFFFFF"
titlepage-rule-color: "FFFFFF"
toc: true
toc-own-page: true
colorlinks: true
linkcolor: blue
urlcolor: blue
listings-disable-line-numbers: true
header-includes:
  - \usepackage{fancyhdr}
  - \pagestyle{fancy}
  - \fancyfoot[L]{MAS External Services Toolkit User Guide}
  - \fancyfoot[R]{\thepage}
---

# Overview

The MAS External Services Toolkit (**`mas-est`**) is a CLI installer for a self-contained, lab implementation of the external identity and supporting services that MAS depends on in customer environments. It is designed for MAS SUpport Engineers to reproduce IAM, SCIM, S3, and SMTP scenarios on any OpenShift cluster they have access to, without depending on a customer's Entra/Okta/IBM Cloud subscription.

A single `mas-est install` command stands up:

- a Keycloak realm (`maximo`) wired into MAS as both an OIDC and a SAML IdP, with auto-configured selfreg
- an OpenLDAP server federated into Keycloak and exposed as a MAS LDAP IdP
- a SCIM bridge that polls Keycloak and provisions filtered users into a MAS SCIM profile
- MinIO as an in-cluster S3-compatible backend for Manage doclinks/attachments
- Mailpit as a capture-only SMTP server for inspecting MAS-generated email

Everything runs inside the `mas-est` namespace on the same OpenShift cluster as MAS. There is no external dependency once the prerequisites (kubeconfig, MAS API token, RBD storage class) are in place.

# Components

| Component            | Image / source                                                               | Role                                                                                            | Lab-grade?                        |
| -------------------- | ---------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- | --------------------------------- |
| **MAS IAM operator** | `quay.io/lee_forster/mas-iam-operator:0.0.12` (via catalog `catalog-0.0.12`) | OLM operator that reconciles the `MasIamStack` CR — Keycloak, OpenLDAP, PostgreSQL, SCIM bridge | Yes — reused across installs      |
| **Keycloak**         | bitnami chart, realm `maximo`                                                | OIDC + SAML IdP for MAS, with federation to OpenLDAP and demo user seeding                      | Yes                               |
| **OpenLDAP**         | bitnami chart, base DN `dc=demo,dc=local`                                    | LDAP IdP for MAS, seeded with `ldap.user1` and `ldap.user2`                                     | Yes                               |
| **PostgreSQL**       | bitnami chart                                                                | Keycloak persistence                                                                            | Yes                               |
| **SCIM bridge**      | `quay.io/lee_forster/mas-scim-bridge:v0.1.0-beta.17`                         | Polls Keycloak every 5 min, provisions users with prefix `scim.` into MAS                       | Yes                               |
| **MinIO**            | `quay.io/minio/minio:latest`                                                 | S3-compatible object storage; pre-creates `mas-s3-demo` + `*backup`/`*recovery` siblings        | Yes                               |
| **Mailpit**          | `ghcr.io/axllent/mailpit:v1.30.1`                                            | Capture-only SMTP server with web UI                                                            | **Capture-only**, see Limitations |
| **mas-est CLI**      | `quay.io/lee_forster/mas-external-services-tool:v0.1.0-beta.17`              | Local installer, preflight, status, support-bundle, uninstall                                   | Yes                               |

# Install

## Prerequisites

Local tooling: `podman`, `oc`, `bash 3.2+`.

Cluster: working OpenShift kubeconfig (`oc whoami` succeeds), a block/RBD storage class for the PostgreSQL and SCIM bridge PVCs, and the ability to pull from `quay.io/lee_forster/*`.

MAS inputs: SCIM base URL (`https://api.<mas-host>/scim/v2`), an API token name and value with SCIM scope, the MAS instance ID (e.g. `mas91`), and the workspace ID (typically `workspace`).

## Bootstrap the local command

```bash
export MAS_EST_IMAGE='quay.io/lee_forster/mas-external-services-tool:v0.1.0-beta.17'
mkdir -p "$HOME/mas-est"
podman run -ti --rm -v "$HOME/mas-est:/tmp" --pull always "$MAS_EST_IMAGE"
export PATH="$HOME/mas-est:$PATH"
mas-est version    # should report 0.1.0-beta.17
```

If you've used an earlier beta, run with `bootstrap --force` to overwrite the local runtime.

## Run preflight + install

```bash
mas-est preflight    # cluster sanity, storage class probe, MAS URL shape
mas-est install      # interactive
```

The interactive install prompts cover only the values that can't be derived. Anything that can be derived from the MAS instance ID (`mas-<id>-core` for the core namespace, `auth.<mas-domain>` for the auth host, the workspace ID when only one matches) is auto-filled and shown as `[derived] ...` rather than re-prompted. Override any derivation with the equivalent `--mas-core-namespace`, `--mas-auth-instance-id`, `--mas-auth-host`, or `--workspace-id` flag.

Recommended components for the full lab experience: `ldap,keycloak,scim,s3,smtp`. The installer creates `mas-est-ldap-connection`, `mas-est-oidc-connection`, `mas-est-saml-connection`, `mas-est-s3-connection`, and `mas-est-smtp-connection` secrets/ConfigMaps in the `mas-est` namespace with everything you need to wire MAS up.

If you select `--configure-mas-auth` (or answer **yes** to the auth prompt), the installer also creates the three MAS IDPCfg resources (`mas91-ldap-default-system`, `mas91-oidc-default-system`, `mas91-saml-default-system`), writes the selfreg ConfigMap (`mas91-selfreg`), and bumps the `entitymgr-idpcfg` memory limit to 2 GiB via the Suite CR `podTemplates`.

## Validate

```bash
mas-est status                 # CSV ready, deployments ready, jobs complete, PVCs Bound
mas-est details                # print the aggregated connection-details secret in redacted form
mas-est ldap-info              # bundled LDAP creds + URL
```

In the MAS Admin UI, navigate to **Configurations → IDPs** — all three rows (LDAP, OIDC, SAML) should show **Configured: Yes**.

Then test login as each demo user (all share the password `maxadmin`):

| Provider | Login user                   | Notes                                                                 |
| -------- | ---------------------------- | --------------------------------------------------------------------- |
| LDAP     | `ldap.user1` or `ldap.user2` | bound directly to OpenLDAP                                            |
| OIDC     | `oidc.user1` or `oidc.user2` | Keycloak realm user, federated identity link                          |
| SAML     | `saml.user1` or `saml.user2` | Same Keycloak realm; use a private/incognito window — see Limitations |

# What Works Today

- **IDP autoconfig.** A single `mas-est install --configure-mas-auth` provisions all three IDPCfg resources, writes the per-provider Keycloak/LDAP credential secrets the MAS operators expect, registers the OIDC and SAML clients on Keycloak (with the reserved-word `default-oidc` redirect path), and writes the selfreg ConfigMap so first-login users are auto-created in MAS.
- **Self-registration.** OIDC and SAML users that don't yet exist in MAS are auto-created in the MAS `User` collection on first login with the workspace assignment and `manage` application entitlement defined in the selfreg config.
- **LDAP federated login.** OpenLDAP-resident users (`ldap.user1`/`ldap.user2`) bind via MAS Liberty's customUserRegistry. Without the beta.16/.17 `userIdMap` fix this surfaced as a misleading "invalid username/password" because Liberty was passing the Liberty federated form `*:uid` into a JNDI search filter.
- **SCIM provisioning.** The bridge polls Keycloak every 5 minutes and provisions users whose username starts with the configured prefix (default `scim.`) into the MAS profile (default `demo`) on the configured workspace. Federated LDAP users are excluded by default to avoid double-provisioning.
- **MinIO object storage.** Pre-creates `mas-s3-demo`, `mas-s3-demobackup`, `mas-s3-demorecovery` and writes an `ObjectStorageCfg` CR at the Suite level. Doclinks are a separate Manage-side configuration — see Common Issues.
- **Operational tooling.** `mas-est status`, `mas-est logs --component <ldap|keycloak|bridge|operator>`, `mas-est support-bundle`, `mas-est config view`, `mas-est config set mas-api-token`, `mas-est restart bridge`, `mas-est uninstall`.

# Known Limitations

| Area                                    | Limitation                                                                                                                                                                                                                                                                                       | Workaround                                                                                                                                                                     |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Mailpit / SMTP**                      | Capture-only. Doesn't deliver to real inboxes (Gmail etc.) and doesn't serve POP/IMAP, so inbound `LSNRCRON` won't work. **Wire-up not validated end-to-end in beta**; the pod runs and the UI loads, but neither MAS Suite user-password emails nor Manage workflow emails have been UI-tested. | Document the manual wire-up; if you need real delivery, point MAS at a proper SMTP server instead. Real SMTP relay support is planned (see Future Enhancements).               |
| **SCIM bridge**                         | Single profile, prefix-based filter only. No group-to-profile routing, no Entra-style `Switch(...)` expression mapping, no custom-attribute round-tripping beyond the bundled `masProfile`.                                                                                                      | Use `masProfile` for per-user routing. Group routing arrives in v0.2.0.                                                                                                        |
| **SAML SLO / OIDC RP-initiated logout** | MAS logout button clears the MAS-local session but doesn't propagate to Keycloak. Switching IDP in the same browser tab may auto-authenticate as the previous user or surface `AIUOM0100E`. Keycloak SLO endpoints are configured correctly; the gap is on the MAS UI logout flow.               | Use a private/incognito window between provider switches, or visit `https://<keycloak-host>/realms/maximo/protocol/openid-connect/logout` to clear the Keycloak SSO session.   |
| **Existing MAS-side state**             | `mas-est uninstall` removes only the `mas-est` namespace. MAS-side state (MongoDB `User` documents, Maximo `MAXUSER`/`PERSON`) persists across reinstalls. If a stale user record with `_local` identity exists, fresh self-reg via a different IdP fails with `CWIML4537E`.                     | When reinstalling on the same MAS instance, manually clean MongoDB `mas_<instance>_core.User` for the test usernames. A `mas-est uninstall --deep` flag is planned for v0.2.0. |
| **MAS Manage doclinks**                 | The Suite-level `ObjectStorageCfg` CR does **not** auto-wire Manage's doclinks subsystem. You must set the `mxe.cos*` and `mxe.doclink.*` properties manually in Manage's Properties UI (see Common Issues).                                                                                     | One-time manual config; documented in detail in `OBJECT-STORAGE-POC.md`.                                                                                                       |
| **Demo users only**                     | Six demo users in OpenLDAP and Keycloak. Not a multi-tenant simulation. Custom-user injection is not supported through the CLI yet.                                                                                                                                                              | Manual `ldapadd` + `kcadm.sh create users` if you need extra users.                                                                                                            |

# Common Issues You May Run Into

> **Tip:** if you've already logged into MAS via one IdP and are now trying another (e.g. you logged in via OIDC and now want to test SAML), use a private/incognito browser window. Keycloak preserves its SSO cookie across MAS logouts, and reusing the tab can either auto-authenticate as the previous user or surface `AIUOM0100E`. See Limitations for the underlying cause.

## "Invalid username or password" on LDAP login

**Cause:** the MAS IDPCfg has `userIdMap: "*:uid"` (the Liberty federated-LDAP form). MAS Liberty's `customUserRegistry` passes that value straight into a JNDI search filter, which fails to compile, but the UI shows a generic auth error.

**Fix:** `userIdMap` must be the bare attribute `uid`. From beta.16 onwards the installer always writes the bare form; if you see this on an old install, edit the IDPCfg directly:

```bash
oc -n mas-<instance>-core patch idpcfg mas<instance>-ldap-default-system \
  --type=json -p='[{"op":"replace","path":"/spec/ldap/userIdMap","value":"uid"}]'
oc -n mas-<instance>-core rollout restart deploy/mas<instance>-coreidp deploy/mas<instance>-coreidp-login
```

## "AIUOM0013E: not authorized" on OIDC self-reg

**Cause:** the selfreg ConfigMap doesn't have a `default-oidc` key. MAS treats the idpId `default` as a reserved word and appends `-{type}` when looking up the selfreg config — and the Keycloak OIDC client must accept both the verbatim and the `-oidc`-suffixed redirect URIs.

**Fix:** both behaviors are handled automatically from beta.15 onwards. If you see this on a fresh beta.17 install, verify the selfreg ConfigMap has all three keys (`default-ldap`, `default-oidc`, `default-saml`) and the Keycloak `default` OIDC client has both `/oidcclient/redirect/default` and `/oidcclient/redirect/default-oidc` registered.

## `entitymgr-idpcfg` OOMKills during IDPCfg apply or uninstall

**Cause:** the default 512 MiB memory limit on `mas<instance>-entitymgr-idpcfg` is not enough when several IDPCfgs reconcile or finalize concurrently. The MAS Suite and ibm-mas operators reconcile this back to 512 MiB if you patch the deployment directly, so the bump has to come from the Suite CR `spec.podTemplates`.

**Fix:** the installer applies this automatically. If you're cleaning up an old install where you need IDPCfgs to finalize, manually re-apply the bump:

```bash
oc -n mas-<instance>-core patch suite <instance> --type=merge -p='{"spec":{"podTemplates":[{"name":"entitymgr-idpcfg","containers":[{"name":"manager","resources":{"limits":{"memory":"2Gi"},"requests":{"memory":"64Mi"}}}]}]}}'
```

## OIDC/SAML user-sync to Manage fails with `AIUI1101E: 500: Internal Server Error`

**Cause:** orphaned `MAXIMO.PERSONANCESTOR` rows from a previous failed sync attempt. The `usersyncagent` retries the POST to `/meaweb/es/MASSYNC/MASPERUSER`, but the unique constraint on `PERSONANCESTOR(PERSONID, ANCESTOR)` blocks the parent `PERSON` insert before it runs.

**Fix:** delete the orphan rows in db2, then flip the user's Mongo sync state back to `PENDING`:

```sql
DELETE FROM MAXIMO.PERSONANCESTOR WHERE PERSONID IN ('OIDC.USER1','SAML.USER1');
```

```javascript
db.User.updateMany(
  { _id: { $in: ["oidc.user1", "saml.user1"] } },
  {
    $set: {
      "sync.status": "PENDING",
      "applications.manage.sync.state": "PENDING",
    },
  },
);
```

The user-sync agent polls every 60 seconds; you should see a `200 OK + SyncMASPERUSERResponse` within ~1 minute.

## MinIO doclinks fail with `AmazonS3Exception: SignatureDoesNotMatch`

**Cause:** the AWS Java SDK in Manage requires an explicit region for SigV4 signing. The default `mxe.cosregion` property does not exist in MAS 9.1.4's MAXPROP table, so the SDK falls back to an internal default that MinIO rejects.

**Fix:** in Manage's **System Properties** UI, click **New Row**, name `mxe.cosregion`, value `us-east-1`, save, **Live Refresh**. Also set:

```text
mxe.cosendpointuri  http://mas-est.svc.cluster.local:9000
mxe.cosbucketname   mas-s3-demo
mxe.cosaccesskey    minioadmin
mxe.cossecretkey    <from secret mas-minio-root, key MINIO_ROOT_PASSWORD>
```

Use the in-cluster service URL above, **not** the OpenShift route URL — the AWS SDK uses virtual-hosted-style addressing, which routes through the per-bucket Kubernetes Services the installer pre-creates, and only resolves inside the cluster.

You'll still see a recurring `BMXAA4160E ... SignatureDoesNotMatch` from the `LOADFLATOBJECT.LOADFLATOBJECT` cron task — this is a separate Manage code path that doesn't read `mxe.cosregion`. The doclinks UI attach path is unaffected. To silence the noise, disable that cron task in Manage's **Cron Task Setup**.

# Future Enhancements

The post-beta roadmap focuses on broadening identity coverage and reducing manual config:

**v0.1.1 — beta hardening (next).** Real-delivery SMTP relay path for Mailpit (UI-validated outbound), `mas-est manage configure-doclinks` subcommand that PUTs the `mxe.cos*` properties via the Manage REST API + bounces the workspace pod, and beta-feedback-driven preflight improvements.

**v0.2.0 — routing.** Group-based MAS profile routing in the SCIM bridge: Keycloak group membership maps to MAS profile IDs, with documented precedence between an explicit `masProfile` user attribute and group-derived routing. Adds custom-attribute round-tripping for SCIM resources beyond the bundled set.

**v0.3.0 — reconciliation and diagnostics.** A `mas-est uninstall --deep` flag that cleans MongoDB `User` documents and Maximo `MAXUSER`/`PERSON` rows for the test usernames. A `mas-est doctor` mode that diagnoses stale `PERSONANCESTOR` orphans, mismatched IDPCfg ↔ Keycloak client state, and SLO wire-up gaps.

**v0.4.0 — product polish.** Local runtime upgrade workflow (no full reinstall), richer install completion summary, custom demo users/groups via flags, examples for Fyre / TechZone / bastion-shell deployment modes.

**SLO / cross-IDP logout.** Investigating the MAS UI logout flow to see whether RP-initiated OIDC logout and SAML SLO can be triggered from the MAS side; currently the Keycloak configuration is correct but MAS Liberty's logout doesn't exercise the IDP-side logout endpoints. This may be a configuration issue, more testing is needed.

# Appendix — Connection Details Quick Reference

The installer writes one Kubernetes secret per provider in the `mas-est` namespace, plus an aggregated `mas-est-connection-details` secret. The shape is documented in [`CONNECTION-DETAILS.md`](CONNECTION-DETAILS.md); a few quick retrieval recipes:

```bash
# Print the full aggregated secret in redacted form
mas-est details --component all

# Per-provider extracts
oc -n mas-est get secret mas-est-ldap-connection -o jsonpath='{.data.url}' | base64 -d
oc -n mas-est get secret mas-est-oidc-connection -o jsonpath='{.data.discoveryUrl}' | base64 -d

# Bundled LDAP user passwords (for the seeded demo users)
mas-est ldap-info
```

# Appendix — Getting Help

| Channel                    | Use for                 |
| -------------------------- | ----------------------- |
| Direct ping `@Lee Forster` | Anything, happy to help |

`mas-est support-bundle --namespace mas-est` collects preflight, status, component logs, deployment/job/PVC summaries, recent namespace events, relevant ConfigMaps, and redacted secrets into a timestamped local directory. Attach the bundle when filing an issue.
