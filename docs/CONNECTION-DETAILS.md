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
| SMTP (Mailpit) | `configmap/mas-est-smtp-connection` | `mas-est` | `host`, `port`, `from`, `webUI`, `tls`, `authentication` |

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

## Notes

- `entityId` in the SAML secret is the URL form (`https://<auth-host>/ibm/saml20/<sp-name>`), not the bare SP name. Keycloak's `clientId` must match this exactly — see the in-repo memory `saml-login-invalid-request` for the background.
- For the MinIO path the `endpoint` value is the in-cluster service URL (`http://mas-minio.mas-est.svc.cluster.local:9000`), not the OpenShift route. The route URL trips the AWS SDK's virtual-hosted-style addressing without a wildcard cert. See [OBJECT-STORAGE-POC.md](OBJECT-STORAGE-POC.md) for the full background.
- The SMTP resource is a ConfigMap rather than a Secret because Mailpit accepts unauthenticated SMTP and the values aren't sensitive.
- The aggregated `secret/mas-est-connection-details` is still maintained for backwards compatibility and is what `mas-est details` reads; the per-provider resources are additive conveniences.
