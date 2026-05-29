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
- MAS-compatible credential `Secret` with `username` and `password`
- MAS `ObjectStorageCfg`

MAS is configured to use the internal cluster URL:

```text
http://mas-minio.mas-est.svc.cluster.local:9000
```

The external S3 API route is mainly for manual testing from outside the cluster. The Console route is the browser UI for uploading files and managing buckets.

Retrieve the MinIO Console credentials from the root secret:

```bash
oc get secret mas-minio-root -n mas-est -o jsonpath='{.data.MINIO_ROOT_USER}' | base64 -d
echo
oc get secret mas-minio-root -n mas-est -o jsonpath='{.data.MINIO_ROOT_PASSWORD}' | base64 -d
echo
```

For MAS, the credentials are stored in the MAS core namespace:

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
