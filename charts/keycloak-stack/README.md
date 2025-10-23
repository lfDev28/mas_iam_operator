# Keycloak Stack Helm Chart

This chart packages Keycloak together with a PostgreSQL dependency for MAS IAM.
The notes below capture the steps required to prepare the chart for distribution
and to keep the bundled configuration (realms, credentials, persistence) in
sync with the running environment.

## Bootstrap admin credentials

Bootstrap credentials live in a Kubernetes Secret. When
`keycloak.bootstrapAdmin.createSecret=true` the chart will materialise the secret
(default name `<release>-bootstrap-admin`), reusing the existing data on later
upgrades. A 24‑character random password is generated automatically unless you
explicitly provide `keycloak.bootstrapAdmin.password`, and the username defaults
to `admin`.

```yaml
keycloak:
  bootstrapAdmin:
    createSecret: true
    secretName: ""         # optional override; defaults to <release>-bootstrap-admin
    usernameKey: username
    passwordKey: password
    username: admin        # override if you do not want the default
    # password: <leave blank to auto-generate on first install>
```

Retrieve the generated password at any time:

```bash
kubectl get secret <release>-bootstrap-admin \
  -n <namespace> \
  -o jsonpath='{.data.password}' | base64 -d && echo
```

The deployment executes `kc.sh bootstrap-admin user` as an init container, so
the password from this secret is immediately valid (no forced change on the
first login) for both the admin console and CLI automation.

Production options:

1. **Let the chart manage the secret** (recommended for most clusters):
   keep `createSecret=true`, rotate the password by deleting the secret, and
   capture it post-install with the command above.
2. **Bring your own secret**: set `createSecret=false` and populate
   `secretName`, `usernameKey`, and `passwordKey`. Create the secret beforehand:
   ```bash
   kubectl create secret generic mas-iam-keycloak-admin \
     --from-literal=username=<admin-user> \
     --from-literal=password=<strong-password> \
     -n <namespace>
   ```

## Preparing LDAP TLS material

OpenLDAP is shipped with TLS enabled by default. Before installing the chart
you must create the TLS secret referenced by both OpenLDAP and Keycloak. Use
your preferred PKI, or for quick starts run the helper script:

```bash
./scripts/dev-generate-openldap-tls.sh -n iam -r <release>
```

When you install via the MAS IAM operator’s consolidated manifest the same logic
runs in-cluster (with a fresh random truststore password each time); use the
script when driving the chart directly or when you need to rotate the secret
manually. The Keycloak deployment reads the password from
`keycloak.ldap.tls.truststorePasswordSecret` (defaulting to the TLS secret) and
`keycloak.ldap.tls.truststorePasswordKey`, so no hard-coded value needs to live
in your values file.

The script generates a throw-away CA, server certificate/key, PKCS#12
truststore, recreates the `<release>-keycloak-openldap-tls` secret, and prints
the truststore password. Rerun the script whenever you need to rotate the dev
credentials. In production, replace this with CA-issued material and ensure the
same password is reflected in `keycloak.ldap.tls.truststorePassword`.

## OpenLDAP support

### Deploying OpenLDAP

The chart can spin up an OpenLDAP instance when `openldap.enabled=true`. When
`openldap.admin.createSecret=true` a secret named
`<release>-keycloak-openldap-admin` (or the value of
`openldap.admin.secretName`) is created and reused on upgrades. The admin
password is randomly generated unless you set `openldap.admin.password`. If you
enable the configuration admin interface (`openldap.config.enableConfigAdmin`)
and do not provide `openldap.admin.configPassword`, the chart will generate a
random value for that credential as well.

Retrieve the generated password(s):

```bash
kubectl get secret <release>-keycloak-openldap-admin \
  -n <namespace> \
  -o jsonpath='{.data.password}' | base64 -d && echo

# optional config password (only present when enableConfigAdmin=true)
kubectl get secret <release>-keycloak-openldap-admin \
  -n <namespace> \
  -o jsonpath='{.data.configPassword}' | base64 -d && echo
```

Set `openldap.admin.createSecret=false` and `openldap.admin.secretName` if you
prefer to manage the secret yourself.

Key options:

```yaml
openldap:
  enabled: true
  admin:
    createSecret: true         # set false if you supply your own secret
    secretName: ""             # optional override; defaults to <release>-keycloak-openldap-admin
    passwordKey: password
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

> **OpenShift note:** The default `osixia/openldap` image expects to start as
> `root` in order to bootstrap the LDAP database before dropping privileges.
> Grant the generated service account permission to use the `anyuid` SCC:
> ```bash
> oc adm policy add-scc-to-user anyuid system:serviceaccount:iam:mas-iam-keycloak-openldap
> ```
> (replace namespace and service account name if you override them). Without
> this step the pod will remain in `CrashLoopBackOff` on OpenShift.

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
kubectl logs job/mas-iam-keycloak-ldap-config -n <namespace>
```

### Seed data

The chart seeding logic mounts the LDIF files listed under `openldap.seedLDIFs`
into the container so the directory is populated on first start. The default
configuration includes `ldap-seed/dev-base.ldif`; replace or extend this list
to match your directory layout.

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
  `charts/keycloak-stack/realm-config/` (or the optional output path).

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
  apply as expected, and the `*-keycloak-ldap-config` job reports success.
