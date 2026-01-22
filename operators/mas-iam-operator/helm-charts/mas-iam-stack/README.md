# MAS IAM Stack Helm Chart

This chart packages Keycloak together with a PostgreSQL dependency for MAS IAM.
The notes below capture the steps required to prepare the chart for distribution
and to keep the bundled configuration (realms, credentials, persistence) in
sync with the running environment.

## Bootstrap admin credentials

Bootstrap credentials live in a Kubernetes Secret. The chart now requires an
existing secret referenced by `keycloak.bootstrapAdmin.secretName`.

```yaml
keycloak:
  bootstrapAdmin:
    createSecret: false
    secretName: <secret-name>
    usernameKey: username
    passwordKey: password
```

Create the secret before installing:

```bash
kubectl create secret generic mas-iam-keycloak-admin \
  --from-literal=username=<admin-user> \
  --from-literal=password=<strong-password> \
  -n <namespace>
```

The deployment runs `kc.sh bootstrap-admin user` during startup; it does not
overwrite existing users, so rotate the admin password with `kcadm.sh` if
needed.

## Keycloak service account

Keycloak runs under a dedicated service account so that SCC/RBAC assignments can
be scoped to the identity instead of the namespace default. Configure it via:

```yaml
keycloak:
  serviceAccount:
    create: true
    name: ""   # defaults to <release>-keycloak
```

When `create=true` (the default) the chart now checks for an existing service
account with the resolved name. If one is found—for example, left behind after a
failed install—the chart skips creating a new object and reuses the existing
account. This prevents Helm from bailing out with “resource already exists”
errors while still allowing you to pre-create or customise the account when
needed.

## Preparing LDAP TLS material

OpenLDAP is shipped with TLS enabled by default. Before installing the chart
you must create the TLS secret referenced by both OpenLDAP and Keycloak. Use
your preferred PKI, or for quick starts run the helper script:

```bash
./scripts/dev-generate-openldap-tls.sh -n iam -r <release>
```

When you install through the consolidated operator manifest this secret is
generated automatically by an in-cluster job (with a fresh random truststore
password per install). Fetch the generated password by inspecting the secret:

```bash
kubectl get secret <release>-keycloak-openldap-tls \
  -n <namespace> -o jsonpath='{.data.truststorePassword}' | base64 -d && echo
```

Use the helper script for manual chart deployments or to rotate the development
secret on demand. The Keycloak deployment reads the password from
`keycloak.ldap.tls.truststorePasswordSecret` (defaulting to the TLS secret) and
`keycloak.ldap.tls.truststorePasswordKey`, so you do not need to hard-code the
value in your chart configuration.

The script generates a throw-away CA, server certificate/key, PKCS#12
truststore, recreates the `<release>-keycloak-openldap-tls` secret, and prints
the truststore password. Rerun the script whenever you need to rotate the dev
credentials. In production, replace this with CA-issued material and ensure the
same password is reflected in `keycloak.ldap.tls.truststorePassword`.

## OpenLDAP support

### Deploying OpenLDAP

The chart can spin up an OpenLDAP instance when `openldap.enabled=true`. When
`openldap.admin.createSecret` is no longer supported. Create the admin secret
yourself and reference it via `openldap.admin.secretName`. If you enable seed
LDIFs, also provide `openldap.userPasswords.secretName` so the init container
can substitute `@@PASSWORD{...}@@` placeholders.

Key options:

```yaml
openldap:
  enabled: true
  admin:
    createSecret: false
    secretName: <admin-secret>
    passwordKey: password
    configPasswordKey: configPassword
  userPasswords:
    createSecret: false
    secretName: <seed-passwords-secret>
  config:
    organisation: "Example Inc."
    domain: "example.org"
    baseDN: "dc=example,dc=org"
  persistence:
    enabled: true
    storageClass: <class>
    size: 2Gi
  seedLDIFs:
    - file: ldap-seed/prod-base.ldif
  serviceAccount:
    create: true
    name: mas-openldap

By default OpenLDAP runs with persistence enabled, a non-root security context
(runAsUser 1001, runAsNonRoot=true, fsGroup 1001), and resource requests/limits.
To opt out, explicitly set `openldap.persistence.enabled=false`,
`openldap.podSecurityContext.enabled=false`,
`openldap.containerSecurityContext.enabled=false` (or `runAsNonRoot=false`), and
override `openldap.resources` with `{}`.
```

Seed LDIFs are optional; provide one or more LDIF files under the chart directory
and list them in `seedLDIFs` to have them mounted into
`/container/service/slapd/assets/config/bootstrap/ldif/custom`.

### TLS materials

The chart does **not** ship TLS key material. When `openldap.tls.enabled=true`
and `openldap.tls.createSecret=false` (recommended), create the referenced secret
before installation:

```bash
kubectl create secret generic mas-iam-keycloak-openldap-tls \
  --from-file=tls.crt=server.crt \
  --from-file=tls.key=server.key \
  --from-file=ca.crt=ca.crt \
  --from-file=ldap-truststore.p12=ldap-truststore.p12 \
  --from-literal=truststorePassword=<truststore-password> \
  -n <namespace>
```

The default key names match `values.yaml`; adjust the command if you override
them. Ensure the same CA bundle is mounted into Keycloak by setting
`keycloak.ldap.tls.caSecret` to the same secret name.
If you are using self-signed material, `scripts/dev-generate-openldap-tls.sh`
can produce the required files and secret for you.

> **OpenShift note:** When `openldap.containerSecurityContext.runAsNonRoot=false`
> (opt-out), the chart creates a RoleBinding to grant the OpenLDAP service
> account `system:openshift:scc:anyuid`. With the secure defaults, that binding
> is omitted and OpenLDAP should run under the default restricted SCC.

### Auto-configuring the Keycloak federation

When `keycloak.ldap.autoConfigure=true` the chart registers an LDAP user storage
provider during install/upgrade via a Helm hook job (runs `kcadm.sh`). Required
inputs:

```yaml
keycloak:
  ldap:
    autoConfigure: true
    realm: maximo
    baseDn: dc=demo,dc=local
    usersDn: ou=users,dc=demo,dc=local
    groupsDn: ou=groups,dc=demo,dc=local
    connectionUrl: ldap://ldap.example.org:389   # optional when openldap.enabled
    bindDn: cn=admin,dc=demo,dc=local
    bindCredentialSecret: mas-openldap-admin     # optional when openldap enabled
    bindCredentialKey: password
```

If an embedded OpenLDAP is enabled and uses the default admin secret, the job
will reuse that secret automatically. The hook deletes any existing component of
the same name before creating it, so re-running upgrades keeps the provider in
sync with the latest settings. Inspect the job logs to confirm success:

```bash
kubectl logs job/mas-iam-ldap-config -n <namespace>
```

### Seed data

The chart seeding logic mounts the LDIF files listed under `openldap.seedLDIFs`
into the container so the directory is populated on first start. The default
configuration includes `ldap-seed/dev-base.ldif`; replace or extend this list
to match your directory layout.

### SCIM extension (Keycloak)

Set `keycloak.scim.enabled=true` to expose the
[Metatavu Keycloak SCIM server](https://github.com/Metatavu/keycloak-scim-server)
endpoints. The chart injects the required environment variables:

| Value                                   | Description                                                                 |
|-----------------------------------------|-----------------------------------------------------------------------------|
| `keycloak.scim.authenticationMode`      | `KEYCLOAK` (default) or `EXTERNAL` for JWTs issued outside Keycloak.        |
| `keycloak.scim.externalIssuer`          | External token issuer (required when `authenticationMode=EXTERNAL`).        |
| `keycloak.scim.externalAudience`        | Expected audience claim for external tokens.                                |
| `keycloak.scim.externalJwksUri`         | JWKS endpoint used to validate external tokens.                             |
| `keycloak.scim.linkIdentityProvider`    | Sets `SCIM_LINK_IDP` to `true`/`false`.                                     |
| `keycloak.scim.emailAsUsername`         | Forces SCIM to treat email as username (`SCIM_EMAIL_AS_USERNAME`).          |

> **Important:** The Keycloak image must include the SCIM provider JAR. Build
> and push an image via `images/keycloak-scim/` (for example,
> `SCIM_KEYCLOAK_IMG=quay.io/<org>/mas-iam-keycloak:scim-0.0.1 make scim-keycloak-push`)
> and set `keycloak.image.repository`/`keycloak.image.tag` accordingly before
> enabling this option.
> Use `scripts/configure-scim-client.sh` to seed the `scim-access` /
> `scim-managed` roles and a confidential client once Keycloak is running.

## Realm export workflow

Use the helper script to pull an updated realm JSON from a running deployment.

```bash
scripts/export-keycloak-realm.sh <namespace> <release> <realm> [output-path]
```

The script:
- Finds the Keycloak pod for the release.
- Runs `kc.sh export` with `KC_HTTP_PORT=0 KC_HTTP_MANAGEMENT_PORT=0` to avoid
  port conflicts with the running instance.
- Streams the resulting `<realm>-realm.json` to
  `charts/mas-iam-stack/realm-config/` (or the optional output path).

After exporting, enable/refresh the import list in `values.yaml`:

```yaml
keycloak:
  realmImport:
    enabled: true
    overrideExisting: false  # set true to replace existing data on next start
    files:
      - file: realm-config/maximo-realm.json
        target: maximo-realm.json
```

For development you can temporarily set `overrideExisting: true` to force an
import; production deployments should leave it `false` once the database is the
source of truth.

## Hostname and proxy configuration

The chart now relies on the hostname v2 settings. Provide the external route via
`keycloak.route` (or enable `keycloak.route.autoHost`) to ensure the following
environment variables are populated:

- `KC_HOSTNAME` and `KC_HOSTNAME_URL` (set automatically when a host is known)
- `KC_HTTP_ENABLED=true`
- `KC_PROXY_HEADERS=xforwarded`
- `KC_HOSTNAME_STRICT=false`

Remove the legacy `KC_PROXY` configuration from any overrides to avoid runtime
warnings. When TLS is terminated at the ingress/router layer, the upstream
address must forward `X-Forwarded-*` headers, which OpenShift routes already do.

## PostgreSQL persistence

The Bitnami PostgreSQL subchart ships with persistence enabled by default. Adjust
storage requirements through `postgresql.primary.persistence`:

```yaml
postgresql:
  primary:
    persistence:
      enabled: true
      storageClass: rook-ceph-block   # override to match the target cluster
      size: 8Gi
```

Operator packaging should document the expected storage class and expose `values`
for bring-your-own database scenarios if target clusters cannot provision the
included StatefulSet.

## Verification checklist before release

- `helm template` (or `make redeploy`) renders a `mas-iam-keycloak-realm-import`
  ConfigMap with the expected JSON payload.
- `kubectl exec` into the pod confirms `/opt/keycloak/data/import/<realm>.json`
  exists when realm import is enabled.
- The admin console shows the imported realm after a clean deployment.
- The admin credentials secret exists in the target namespace and is referenced
  correctly by the deployment environment variables.
- PostgreSQL PVCs bind successfully (storage class + size) in staging environments.
- When OpenLDAP is enabled, the deployment and service are healthy, seed LDIFs
  apply as expected, and the `*-ldap-config` job reports success.
