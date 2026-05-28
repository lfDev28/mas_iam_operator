# MAS Object Storage POC

This document captures the experimental S3-compatible object storage path for MAS support labs.

This is post-beta work. It is not part of the current IAM beta launch surface yet.

## Goal

Add a repeatable way to stand up S3-compatible storage on OpenShift so support engineers can reproduce MAS object storage and S3 integration issues without needing external cloud credentials.

The first supported development target is Rook Ceph RGW because the current lab cluster already has a Rook Ceph cluster and the required object bucket CRDs.

## Current Cluster Findings

The initial target cluster has:

- `rook-ceph` namespace
- `CephCluster` in `Ready` phase
- `CephObjectStore` CRD
- `ObjectBucketClaim` CRD
- `cert-manager` and MAS cluster issuers
- MAS `ObjectStorageCfg` CRD

It does not currently have:

- Red Hat ODF / NooBaa installed
- an existing `CephObjectStore`
- an existing S3 route
- an existing bucket storage class
- an existing MAS `ObjectStorageCfg`

The Ceph cluster may report health warnings. Check those before treating any S3 behavior as a MAS issue.

## Experimental Install

From a logged-in OpenShift shell:

```bash
mas-iam object-storage install-rook-ceph \
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
mas-iam object-storage install-rook-ceph \
  --mas-instance-id lfmas \
  --mas-core-namespace mas-lfmas-core \
  --rook-namespace rook-ceph \
  --store-name mas-s3 \
  --route-host mas-s3.apps.example.com \
  --cert-issuer-name mas-lfmas-ca
```

## Created Resources

The command creates or updates:

- `Certificate` for the RGW route and service DNS names
- `CephObjectStore`
- OpenShift `Route`
- bucket `StorageClass`
- `ObjectBucketClaim`
- MAS-compatible credential `Secret` with `username` and `password`
- MAS `ObjectStorageCfg`

The ObjectBucketClaim-generated secret remains in place with the S3-native key names. The MAS credential secret translates those values into the key names expected by the MAS object storage operator.

## MAS Integration Notes

MAS validates Ceph/S3 object storage by calling S3 `list_buckets()` using:

- `ObjectStorageCfg.spec.config.url`
- credential secret key `username`
- credential secret key `password`
- optional custom CA certificates from `ObjectStorageCfg.spec.certificates`

The first POC only creates the MAS object storage configuration. Manage attachment properties should be configured after the endpoint is verified.

Typical follow-on Manage work includes bucket-specific properties such as endpoint, bucket name, region, access key, secret key, and attachment storage mode.

## Future Installer Direction

This feature is one reason the project may eventually be renamed from an IAM-only tool to something broader, such as:

- `MAS External Services Toolkit`
- `MAS Open Services Installer`

Potential future providers:

- existing S3 endpoint
- Rook Ceph RGW
- ODF / NooBaa
- MinIO for lightweight demos

Potential future services:

- IAM: Keycloak, OpenLDAP, SCIM bridge
- object storage: S3-compatible storage
- SMTP test server
- certificate and trust bundle helpers
- support bundle collection across all external services
