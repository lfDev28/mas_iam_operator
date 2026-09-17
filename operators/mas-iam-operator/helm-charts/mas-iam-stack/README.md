# mas-iam-stack chart

Deploys Keycloak and its PostgreSQL database (Bitnami subchart), plus an optional OpenLDAP server. A post-install hook Job registers OpenLDAP as a Keycloak user federation provider. The [mas-iam-operator](../../README.md) reconciles each `MasIamStack` CR as a release of this chart, and the CR `spec` is used as the values. `mas-est install` creates the CR from `manifests/install-olm-sample.yaml` with release name `mas-est-iam`. For installation, see [docs/GUIDE.md](../../../../docs/GUIDE.md).

The chart is single-sourced here. `charts/mas-iam-stack` at the repo root is a symlink to this directory.

## Layout

| Path | Contents |
| --- | --- |
| `values.yaml` | Defaults |
| `templates/deployment.yaml` | Keycloak Deployment (LDAP truststore init, bootstrap admin, realm import) |
| `templates/keycloak-ldap-config-job.yaml` | `<release>-ldap-config` hook Job that configures LDAP federation with `kcadm.sh` |
| `templates/openldap-*.yaml` | OpenLDAP Deployment, Service, PVC, and seed ConfigMap |
| `templates/*-anyuid-rolebinding.yaml` | On OpenShift, grant `anyuid` to the Keycloak, OpenLDAP, and PostgreSQL service accounts |
| `ldap-seed/dev-base.ldif` | Seed users `ldap.user1` and `ldap.user2`. `@@PASSWORD{<uid>}@@` placeholders are filled from the user-passwords secret |
| `realm-config/maximo-realm.json` | Realm imported at Keycloak startup |
| `charts/` | Vendored PostgreSQL subchart (`helm dependency update` refreshes it) |

## Secrets the chart does not create

Rendering fails unless these secrets are referenced. The chart never creates them, and the `createSecret: true` options fail on purpose.

| Value | Keys | Sample name |
| --- | --- | --- |
| `keycloak.bootstrapAdmin.secretName` | `username`, `password` | `mas-est-iam-bootstrap-admin` |
| `postgresql.auth.existingSecret` (default `<release>-postgresql`) | `password`, `postgres-password` | `mas-est-iam-postgresql` |
| `openldap.admin.secretName` | `password`, `configPassword` | `mas-est-iam-openldap-admin` |
| `openldap.userPasswords.secretName` (required when `seedLDIFs` is set) | one key per seeded uid | `mas-est-iam-openldap-user-passwords` |
| `openldap.tls.secretName` / `keycloak.ldap.tls.caSecret` (default `<release>-keycloak-openldap-tls`) | `tls.crt`, `tls.key`, `ca.crt`, `ldap-truststore.p12`, `truststorePassword` | created by the TLS generator Job in the sample manifest, or locally by `scripts/dev-generate-openldap-tls.sh` |

## Key values

| Value | Default | Notes |
| --- | --- | --- |
| `keycloak.image.*` | `quay.io/keycloak/keycloak:26.0.5` | |
| `keycloak.route.host` / `keycloak.route.autoHost.{enabled,appsDomain}` | `""` / `false` | autoHost builds `<release>-<ns>.<appsDomain>`; `KC_HOSTNAME` is set when a host is known |
| `keycloak.route.tls.{certificate,key,caCertificate,destinationCACertificate}` | `""` | Custom route certificate; edge termination |
| `keycloak.realmImport.{enabled,overrideExisting,files}` | `true`, `false`, `maximo-realm.json` | |
| `keycloak.ldap.autoConfigure` | `true` | Runs the `<release>-ldap-config` post-install hook |
| `keycloak.ldap.autoConfigureOnUpgrade` | `true` | Also runs the hook on post-upgrade. The operator upgrades on every reconcile, so the sample sets `false` |
| `keycloak.ldap.{connectionUrl,bindDn,usersDn,groupsDn,bindCredentialSecret}` | derived | Derived from the embedded OpenLDAP when empty |
| `keycloak.scim.*` | `enabled: false` | Env vars for the Metatavu SCIM extension; needs an image that includes it ([images/keycloak-scim](../../../../images/keycloak-scim/README.md)) |
| `postgresql.image.*` | `quay.io/lee_forster/mas-iam-operator:postgresql-17.6.0-debian-12-r4-multi` | Quay mirror, to avoid Docker Hub rate limits |
| `postgresql.primary.persistence.{storageClass,size}` | `rook-ceph-block`, `8Gi` | The installer overrides the storage class |
| `openldap.enabled` | `true` | |
| `openldap.image.*` | `quay.io/lee_forster/mas-iam-operator:openldap-1.5.0-multi` | Mirror of `osixia/openldap:1.5.0` |
| `openldap.config.{domain,baseDN}` | `demo.local`, derived | |
| `openldap.persistence.{enabled,size,storageClass}` | `true`, `2Gi`, `""` | |
| `openldap.seedLDIFs` | `ldap-seed/dev-base.ldif` | |
| `openldap.containerSecurityContext` | runs as uid 0 | The osixia image's startup scripts need root |

## Develop and test

```bash
make lint                          # repo root: verifies the charts/ symlink
helm lint operators/mas-iam-operator/helm-charts/mas-iam-stack
helm template t operators/mas-iam-operator/helm-charts/mas-iam-stack \
  --set keycloak.bootstrapAdmin.secretName=a \
  --set openldap.admin.secretName=b \
  --set openldap.userPasswords.secretName=c
```

To refresh `realm-config/` from a running stack, use `scripts/export-keycloak-realm.sh <ns> <release> <realm> [output-path]`.

## Release

The chart ships inside the operator image, so a chart change reaches clusters only through a new operator release. See the `/mas-est-release` skill, §9 ([.claude/skills/mas-est-release/SKILL.md](../../../../.claude/skills/mas-est-release/SKILL.md)).
