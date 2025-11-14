ARG KEYCLOAK_IMAGE=quay.io/keycloak/keycloak:26.0.5
ARG SCIM_REPO=https://github.com/Metatavu/keycloak-scim-server.git
ARG SCIM_REF=develop

FROM eclipse-temurin:21-jdk as scim-build
ARG SCIM_REPO
ARG SCIM_REF
WORKDIR /scim-src
RUN apt-get update \
  && apt-get install -y --no-install-recommends git unzip findutils ca-certificates \
  && rm -rf /var/lib/apt/lists/* \
  && git clone --depth 1 --branch "${SCIM_REF}" "${SCIM_REPO}" .
RUN ./gradlew --no-daemon clean build -x test
RUN mkdir -p /artifacts \
  && find build/libs -type f -name '*.jar' -exec cp {} /artifacts/ \;

FROM ${KEYCLOAK_IMAGE}
COPY --from=scim-build /artifacts/*.jar /opt/keycloak/providers/
