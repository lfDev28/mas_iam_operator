# mas-est v0.1.0 — 10-Minute Demo Script

Audience: MAS support engineers. Goal: show that one command produces a complete,
working external-IAM lab against a real MAS instance, and that every login path
lands in a Manage workspace.

## Prep (before the demo — 20-30 min, mostly waiting)

Run the install ahead of time; the demo shows the *result* and one live flow.
On a cluster with a running MAS instance:

```bash
export MAS_EST_IMAGE='quay.io/lee_forster/mas-external-services-tool:v0.1.2'
mkdir -p "$HOME/mas-est"
podman run -ti --rm -v "$HOME/mas-est:/tmp" --pull always $MAS_EST_IMAGE
export PATH="$HOME/mas-est:$PATH"
mas-est preflight
mas-est install     # components: ldap,keycloak,scim,s3 · providers: ldap,oidc,saml
```

Keep three browser tabs ready: MAS home (`home.<instance>.apps.<domain>`),
Keycloak admin (route `mas-est-iam`, realm `maximo`), MAS Admin UI → Users.
Have the demo user passwords at hand:

```bash
mas-est details --namespace mas-est --component all
```

## The demo

### 1. The pitch (1 min)

> "Setting up an IAM reproduction environment for a support case — an LDAP,
> an OIDC/SAML IdP, a SCIM feed into MAS — used to take a day of manual
> steps. `mas-est` does it in one command against any MAS instance. Here's
> what that gets you."

### 2. What got installed (2 min)

```bash
mas-est status --namespace mas-est
oc get idpcfg,objectstoragecfg -n mas-<instance>-core
```

Talk track: one namespace with Keycloak, OpenLDAP, PostgreSQL, SCIM bridge,
and MinIO; MAS side has three IDPCfgs (all `Ready`, and — show the MAS Admin
UI → SSO configuration — all three show **Configured: Yes**) plus an
ObjectStorageCfg.

### 3. The four login paths (3 min)

Open MAS home in an incognito window per login (avoids SSO session bleed —
see known limitations):

1. **LDAP**: `ldap.user1` — authenticated by the bundled OpenLDAP via
   MAS's user registry.
2. **OIDC**: `oidc.user1` — Keycloak-issued OIDC, auto-registered on first
   login via the self-registration ConfigMap, straight into the workspace.
3. **SAML**: `saml.user1` — same, over SAML.
4. **SCIM**: `scim.user1` via the OIDC button — this user was *provisioned*
   by the SCIM bridge (not self-registered) and then identity-linked by the
   installer. Open Manage to show it landed with a working workspace user.

Talk track for #4: "This is the flow customers use Entra for — user exists in
the IdP, SCIM pushes it into MAS, MAS syncs it to Manage. We can now reproduce
that end-to-end in a lab."

### 4. Live SCIM provisioning (3 min)

In Keycloak admin (realm `maximo`) create user `scim.user3` with an email and
first/last name, set a password. Then watch:

```bash
mas-est logs --namespace mas-est --component bridge --follow
```

Within one poll cycle (≤5 min — start this early in the demo and come back)
the bridge logs `plan action: create` and the user appears in MAS Admin UI →
Users with the `demo` SCIM profile.

Note honestly if asked: users provisioned *after* install need the identity
linker re-run (`mas-est mas-auth apply` is idempotent) before Manage sync —
automatic remediation is on the v0.1.1 roadmap.

### 5. Supportability (1 min)

```bash
mas-est support-bundle --namespace mas-est
```

Talk track: "When a beta user hits a cluster-specific failure, this collects
status, events, logs, and redacted secrets into one directory — send that,
not screenshots."

### 6. Close (30 s)

- Repo + docs: `docs/BETA-QUICKSTART.md` to get started
- Known limitations + v0.1.1 roadmap: `docs/BETA-KNOWN-LIMITATIONS.md`
  (doclinks auto-wiring, late-user linking, group-based routing in v0.2.0)
- Issues: `mas-est support-bundle` output, please

## Fallbacks

- If live provisioning is too slow for the slot, pre-create `scim.user3`
  10 minutes before and show the bridge log lines + the user in MAS instead.
- If the projector dies: `mas-est details --component all` in a big terminal
  font tells the whole story.
