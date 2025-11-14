# Keycloak + SCIM image

This Dockerfile builds a Keycloak image that bundles the Metatavu
[`keycloak-scim-server`](https://github.com/Metatavu/keycloak-scim-server)
extension. It clones the upstream repo, builds the SCIM server JAR with Gradle,
and copies it into the Keycloak `providers/` directory so SCIM endpoints are
available out of the box.

## Build & push

```bash
SCIM_KEYCLOAK_IMG=quay.io/<org>/mas-iam-keycloak:scim-0.0.1 \
SCIM_REF=develop \
make scim-keycloak-push
```

By default the build pulls from `quay.io/keycloak/keycloak:26.0.5` and tracks
the upstream `main` branch. Override `KEYCLOAK_BASE_IMAGE`, `SCIM_REPO`, or
`SCIM_REF` if you need different sources.
