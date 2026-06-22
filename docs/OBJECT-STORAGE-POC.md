# MAS Object Storage POC

This document captures the experimental S3-compatible object storage path for MAS support labs.

This is post-beta work. It is not part of the current IAM beta launch surface yet.

## Goal

Add a repeatable way to stand up S3-compatible storage on OpenShift so support engineers can reproduce MAS object storage and S3 integration issues without needing external cloud credentials.

The current preferred development target is MinIO. It is simpler to operate for demos than Rook Ceph RGW and includes a browser console for uploading, browsing, and deleting objects.

## Current Cluster Findings

The initial target cluster has:

- `rook-ceph` namespace
- `CephCluster` in `Ready` phase
- block storage class `rook-ceph-block`
- `cert-manager` and MAS cluster issuers
- MAS `ObjectStorageCfg` CRD

It does not currently have:

- Red Hat ODF / NooBaa installed
- a managed object storage service dedicated to MAS testing

The Ceph cluster may report health warnings. Check those before treating PVC or storage behavior as a MAS issue.

## Recommended Experimental Install

From a logged-in OpenShift shell:

```bash
mas-est object-storage install-minio \
  --mas-instance-id lfmas
```

The command derives these defaults:

- MAS core namespace: `mas-<instance-id>-core`
- MinIO namespace: `mas-est`
- MinIO deployment/service name: `mas-minio`
- PVC storage class: `rook-ceph-block`
- PVC size: `20Gi`
- bucket: `mas-s3-demo`
- external S3 API route: `mas-minio-api.<default OpenShift apps domain>`
- external MinIO Console route: `mas-minio-console.<default OpenShift apps domain>`
- MAS credential secret: `mas-minio-objectstorage-credentials`
- MAS ObjectStorageCfg: `ibm-mas-<instance-id>-objectstoragecfg-system`
- Manage endpoint URL: `http://mas-est.svc.cluster.local:9000`

Use explicit flags when testing a different cluster layout:

```bash
mas-est object-storage install-minio \
  --mas-instance-id lfmas \
  --mas-core-namespace mas-lfmas-core \
  --namespace mas-est \
  --storage-class rook-ceph-block \
  --pvc-size 20Gi \
  --bucket mas-s3-demo
```

## MinIO Resources

The command creates or updates:

- namespace for external lab services
- MinIO root credential `Secret`
- MinIO data `PersistentVolumeClaim`
- MinIO `Deployment`
- MinIO `Service`
- OpenShift route for the S3 API
- OpenShift route for the MinIO Console UI
- bucket initialization `Job`
- Manage-compatible bucket service aliases for virtual-hosted-style S3
- MAS-compatible credential `Secret` with `username` and `password`
- MAS `ObjectStorageCfg`

MAS is configured to use the internal cluster URL:

```text
http://mas-minio.mas-est.svc.cluster.local:9000
```

Manage cron tasks and doclinks should use the virtual-host base endpoint:

```text
http://mas-est.svc.cluster.local:9000
```

The installer creates both documented Manage bucket layouts:

```text
mas-s3-demo
mas-s3-demorecovery
mas-s3-demobackup
```

and root-bucket prefixes:

```text
mas-s3-demo/recovery/
mas-s3-demo/backup/
```

It also creates matching Kubernetes service aliases so Manage's AWS SDK can resolve virtual-hosted bucket names such as:

```text
mas-s3-demo.mas-est.svc.cluster.local
mas-s3-demorecovery.mas-est.svc.cluster.local
mas-s3-demobackup.mas-est.svc.cluster.local
```

The external S3 API route is mainly for manual testing from outside the cluster. The Console route is the browser UI for uploading files and managing buckets.
If Manage is pointed at the external HTTPS route, it may fail with PKIX/certificate trust errors unless that route certificate chain is trusted by Manage. The internal HTTP endpoint avoids that trust issue for lab testing.

The fastest way to retrieve every S3 connection value the installer wrote — endpoint URL, access/secret key, region, bucket, external API/console URLs — is the per-provider connection secret:

```bash
oc get secret mas-est-s3-connection -n mas-est -o yaml
```

The keys it ships with are documented in [docs/CONNECTION-DETAILS.md](CONNECTION-DETAILS.md). The legacy source-of-truth secrets are still available if you need them directly:

```bash
oc get secret mas-minio-root -n mas-est -o jsonpath='{.data.MINIO_ROOT_USER}' | base64 -d
echo
oc get secret mas-minio-root -n mas-est -o jsonpath='{.data.MINIO_ROOT_PASSWORD}' | base64 -d
echo
```

For MAS, the credentials are also stored in the MAS core namespace:

```bash
oc get secret mas-minio-objectstorage-credentials -n mas-lfmas-core
```

## Rook Ceph RGW Alternative

The older Rook Ceph RGW path remains available for clusters where a Rook object gateway is the thing being tested directly:

```bash
mas-est object-storage install-rook-ceph \
  --mas-instance-id lfmas
```

The command derives these defaults:

- MAS core namespace: `mas-<instance-id>-core`
- Rook namespace: `rook-ceph`
- object store: `mas-s3`
- route host: `mas-s3.<default OpenShift apps domain>`
- bucket claim: `mas-s3-bucket`
- bucket storage class: `rook-ceph-bucket`
- MAS credential secret: `mas-s3-objectstorage-credentials`
- MAS ObjectStorageCfg: `ibm-mas-<instance-id>-objectstoragecfg-system`
- certificate issuer: `mas-<instance-id>-ca`

Use explicit flags when testing a different cluster layout:

```bash
mas-est object-storage install-rook-ceph \
  --mas-instance-id lfmas \
  --mas-core-namespace mas-lfmas-core \
  --rook-namespace rook-ceph \
  --store-name mas-s3 \
  --route-host mas-s3.apps.example.com \
  --cert-issuer-name mas-lfmas-ca
```

### Rook Ceph Resources

The Rook command creates or updates:

- `Certificate` for the RGW route and service DNS names
- `CephObjectStore`
- OpenShift `Route`
- bucket `StorageClass`
- `ObjectBucketClaim`
- MAS-compatible credential `Secret` with `username` and `password`
- MAS `ObjectStorageCfg`

The ObjectBucketClaim-generated secret remains in place with the S3-native key names. The MAS credential secret translates those values into the key names expected by the MAS object storage operator.

## MAS Integration Notes

MAS validates S3 object storage by calling S3 `list_buckets()` using:

- `ObjectStorageCfg.spec.config.url`
- credential secret key `username`
- credential secret key `password`
- optional custom CA certificates from `ObjectStorageCfg.spec.certificates`

The current POC only creates the MAS object storage configuration. Manage attachment properties should be configured after the endpoint is verified.

Typical follow-on Manage work includes bucket-specific properties such as endpoint, bucket name, region, access key, secret key, and attachment storage mode.

### Required Manage System Property values (validated 2026-06-18, MAS 9.1.18)

Setting these in Manage's **System Properties** application is what actually wires up doclinks/attachments to MinIO. The `ObjectStorageCfg` CR the installer creates only wires MinIO into the MAS Suite layer (backups etc.), NOT into Manage attachment storage — that's a separate manual step today.

```text
mxe.cosaccesskey            value from secret mas-minio-root key MINIO_ROOT_USER (minioadmin)
mxe.cossecretkey            value from secret mas-minio-root key MINIO_ROOT_PASSWORD
mxe.cosendpointuri          http://mas-est.svc.cluster.local:9000      ← see CRITICAL note below
mxe.cosbucketname           mas-s3-demo
mxe.cosregion               us-east-1                                   ← required to force V4 signing
mxe.attachmentstorage       com.ibm.tivoli.maximo.oslc.provider.COSAttachmentStorage
mxe.doclink.doctypes.defpath        cos:doclinks
mxe.doclink.doctypes.topLevelPaths  cos:doclinks
mxe.doclink.path01          cos:doclinks=<manage-ui-base-url>
mxe.doclink.securedAttachment       True
```

Then in Manage's **Document Types** application, edit each doctype (e.g. `Attachments`) and change its `DEFAULTPATH` from the legacy filesystem path (e.g. `\DOCLINKS\ATTACHMENTS`) to `cos:doclinks/attachment` (must start with the `cos:doclinks/` prefix to match topLevelPaths). Then restart the Manage `all` deployment so the new config is picked up.

### CRITICAL — use the in-cluster URL, NOT the OpenShift route URL

**Do NOT** follow the IBM Cloud Object Storage examples that tell you to use `https://<public-s3-endpoint>` as `mxe.cosendpointuri`. The MinIO installed by mas-est is only reachable for Manage doclinks via the in-cluster Kubernetes service URL `http://mas-est.svc.cluster.local:9000`. Reasons:

1. **The AWS SDK in Maximo defaults to virtual-hosted-style addressing** (`<bucket>.<endpoint-host>`). For the OpenShift route, this would require a wildcard route admission + a two-level wildcard TLS cert covering `*.mas-minio-api.apps.<cluster-domain>`, neither of which mas-est sets up by default. Result: HTTP 503 from the OpenShift router.
2. **The installer pre-creates per-bucket Kubernetes Services** (`mas-s3-demo`, `mas-s3-demobackup`, `mas-s3-demorecovery`) in the `mas-est` namespace. Combined with MinIO's `MINIO_DOMAIN=mas-est.svc.cluster.local` setting, this means `<bucket>.mas-est.svc.cluster.local` DNS-resolves to the right MinIO pod via the per-bucket service. That's only true *inside* the cluster.
3. **MinIO `:latest` requires AWS Signature V4**; setting `mxe.cosregion=us-east-1` nudges the AWS SDK out of legacy V2 mode and avoids the `AmazonS3Exception: The authorization mechanism you have provided is not supported. Please use AWS4-HMAC-SHA256` error.

If you follow the IBM public-cloud docs verbatim against mas-est MinIO, you will see a cascade of misleading errors: `BMXAA4195E - A value is required for the URL/File Name field`, a `NullPointerException` in `psdi.app.doclink.AppDoctype.init` (which resolves itself once COS is actually reachable), and `AmazonS3Exception: 503 Service Unavailable` with `Request ID: null`. The fix for all of them is using the in-cluster URL above.

## Future Installer Direction

This feature now sits under the broader MAS External Services Toolkit direction.

Potential future providers:

- existing S3 endpoint
- MinIO for lightweight demos
- Rook Ceph RGW
- ODF / NooBaa

Potential future services:

- IAM: Keycloak, OpenLDAP, SCIM bridge
- object storage: S3-compatible storage
- SMTP test server
- certificate and trust bundle helpers
- support bundle collection across all external services
