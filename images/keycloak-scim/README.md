# keycloak-scim image

Builds Keycloak with the Metatavu [keycloak-scim-server](https://github.com/Metatavu/keycloak-scim-server) extension. The Dockerfile clones the extension, builds the JAR with Gradle, and copies it into `/opt/keycloak/providers/`. Enable the extension with the chart's `keycloak.scim.*` values.

`mas-est install` does not use this image. The default stack runs stock `quay.io/keycloak/keycloak`, and the SCIM bridge reads users through the Keycloak Admin REST API.

## Build and push

From the repo root:

```bash
SCIM_KEYCLOAK_IMG=<registry>/mas-iam-keycloak:<tag> make scim-keycloak-push
```

The defaults are `KEYCLOAK_BASE_IMAGE=quay.io/keycloak/keycloak:26.0.5`, `SCIM_REPO=https://github.com/Metatavu/keycloak-scim-server.git`, `SCIM_REF=develop`, and `SCIM_KEYCLOAK_PLATFORM=linux/amd64`.
