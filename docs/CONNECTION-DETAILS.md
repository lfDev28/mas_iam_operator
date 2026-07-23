# Connection Details Reference

After `mas-est install`, every external service the installer stood up exposes its connection details in a dedicated Kubernetes Secret (or ConfigMap, for SMTP) in the MAS-EST namespace. A support engineer can grab everything needed to wire a third-party app, a `kubectl exec` test, or another MAS instance to one of these services in a single `oc get`.

The shorter `mas-est details` command prints the aggregated `mas-est-connection-details` secret in a redacted, human-readable form. This page is for when you need the raw values or want to mount one provider's secret directly into another workload.

## Per-provider secrets and ConfigMaps

| Provider | Resource | Namespace | Keys |
|---|---|---|---|
| LDAP | `secret/mas-est-ldap-connection` | `mas-est` | `url`, `baseDN`, `bindDN`, `bindPassword`, `userIdMap`, `ca.crt` (when LDAPS) |
| OIDC | `secret/mas-est-oidc-connection` | `mas-est` | `issuerUrl`, `discoveryUrl`, `authorizationEndpoint`, `tokenEndpoint`, `jwksEndpoint`, `clientId`, `clientSecret`, `realm`, `redirectUri` |
| SAML | `secret/mas-est-saml-connection` | `mas-est` | `entityId` (URL form), `acsUrl`, `sloUrl`, `idpMetadataUrl`, `idpMetadata` (XML), `nameIdFormat` |
| S3 (MinIO) | `secret/mas-est-s3-connection` | `mas-est` | `provider`, `endpoint`, `manageEndpoint`, `externalEndpoint`, `consoleUrl`, `accessKey`, `secretKey`, `region`, `bucket`, `siblingBuckets` |
| S3 (Rook Ceph RGW) | `secret/mas-est-s3-connection` | `mas-<instance>-core` | `provider`, `endpoint`, `accessKey`, `secretKey`, `region`, `bucket` |
| SMTP (Mailpit, capture-only) | `configmap/mas-est-smtp-connection` | `mas-est` | `host`, `port`, `from`, `webUI`, `tls`, `authentication`, `relayEnabled` |
| SMTP (Mailpit, relay enabled) | `configmap/mas-est-smtp-connection` + `secret/mas-est-mailpit-relay` | `mas-est` | ConfigMap adds `relayHost`/`relayPort`/`relayFrom`/`relayStartTLS`/`relayAuth`/`relayCredentialsSecret`; Secret carries the full Mailpit relay YAML (host, port, auth, username, password, return-path) under key `relay.yaml` |

All resources are labelled with `app.kubernetes.io/managed-by=mas-est-installer` and `mas-est.ibm.com/connection-provider=<provider>`.

The S3 secret lives in the MAS core namespace for the Rook Ceph RGW path because that is where the matching credential secret and `ObjectStorageCfg` already live. The MinIO path puts everything in the `mas-est` namespace alongside the other lab services.

## Quick retrieval commands

Print everything in YAML form:

```bash
oc get secret    mas-est-ldap-connection  -n mas-est -o yaml
oc get secret    mas-est-oidc-connection  -n mas-est -o yaml
oc get secret    mas-est-saml-connection  -n mas-est -o yaml
oc get secret    mas-est-s3-connection    -n mas-est -o yaml
oc get configmap mas-est-smtp-connection  -n mas-est -o yaml
```

Pull one value:

```bash
oc get secret mas-est-ldap-connection -n mas-est -o jsonpath='{.data.bindPassword}' | base64 -d
oc get secret mas-est-oidc-connection -n mas-est -o jsonpath='{.data.clientSecret}' | base64 -d
oc get configmap mas-est-smtp-connection -n mas-est -o jsonpath='{.data.host}'
```

Mount the OIDC client credential into a pod:

```yaml
envFrom:
  - secretRef:
      name: mas-est-oidc-connection
```

## Human-readable view

```bash
mas-est details --namespace mas-est --component all
mas-est details --namespace mas-est --component ldap
mas-est details --namespace mas-est --component oidc
mas-est details --namespace mas-est --component s3
mas-est details --namespace mas-est --component smtp
```

Sensitive values are redacted by default. Add `--show-secrets` only when you really need to see them locally.

## SMTP relay (optional)

By default Mailpit runs in capture-only mode: MAS sends to it on `mas-mailpit.mas-est.svc.cluster.local:1025`, and the messages live in the Mailpit web UI. Outbound delivery to real recipients is disabled.

To turn on real delivery, pass relay flags at install time and Mailpit will both capture **and** forward through an upstream SMTP. Captured copies stay visible in the UI for debugging.

```bash
mas-est install \
  --components ldap,keycloak,scim,smtp \
  --smtp-relay-host smtp.gmail.com \
  --smtp-relay-port 587 \
  --smtp-relay-username 'lab@example.com' \
  --smtp-relay-password "$GMAIL_APP_PASSWORD" \
  --smtp-relay-from 'lab@example.com' \
  --smtp-relay-starttls
```

Or against an already-installed cluster:

```bash
mas-est smtp install-mailpit \
  --smtp-relay-host smtp.gmail.com \
  --smtp-relay-port 587 \
  --smtp-relay-username 'lab@example.com' \
  --smtp-relay-password "$GMAIL_APP_PASSWORD" \
  --smtp-relay-from 'lab@example.com'
```

Same flags exist as env vars (`MAS_EST_SMTP_RELAY_HOST`, `MAS_EST_SMTP_RELAY_PORT`, etc).

Provider notes:
- **Gmail / Google Workspace**: needs an App Password (account must have 2FA enabled). Host `smtp.gmail.com`, port `587`, auth `plain`, STARTTLS on.
- **Outlook / Microsoft 365**: classic basic auth has been deprecated; modern accounts require OAuth which Mailpit doesn't speak natively. Use SendGrid / SES / Postmark instead, or terminate at a local Postfix that does OAuth.
- **SendGrid**: host `smtp.sendgrid.net`, port `587`, username `apikey` (literal), password = the API key.
- **AWS SES**: per-region SMTP endpoint, port `587`, IAM-derived SMTP credentials.

When relay is enabled MAS's own SMTP config doesn't change — it still talks plain SMTP to `mas-mailpit.mas-est.svc.cluster.local:1025`. Mailpit handles the upstream hop.

## Notes

- `entityId` in the SAML secret is the URL form (`https://<auth-host>/ibm/saml20/<sp-name>`), not the bare SP name. Keycloak's `clientId` must match this exactly — see the in-repo memory `saml-login-invalid-request` for the background.
- For the MinIO path the `endpoint` value is the in-cluster service URL (`http://mas-minio.mas-est.svc.cluster.local:9000`), not the OpenShift route. The route URL trips the AWS SDK's virtual-hosted-style addressing without a wildcard cert. See [OBJECT-STORAGE-POC.md](OBJECT-STORAGE-POC.md) for the full background.
- The SMTP resource is a ConfigMap rather than a Secret because Mailpit accepts unauthenticated SMTP and the values aren't sensitive.
- The aggregated `secret/mas-est-connection-details` is still maintained for backwards compatibility and is what `mas-est details` reads; the per-provider resources are additive conveniences.
