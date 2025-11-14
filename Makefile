NAMESPACE            ?= iam
RELEASE              ?= mas-iam
CHART                ?= charts/mas-iam-stack
CONTAINER_ENGINE     ?= podman
KEYCLOAK_BASE_IMAGE  ?= quay.io/keycloak/keycloak:26.0.5
SCIM_KEYCLOAK_IMG    ?= quay.io/example/mas-iam-keycloak:scim-0.0.1
SCIM_KEYCLOAK_PLATFORM ?= linux/amd64
SCIM_KEYCLOAK_CONTEXT ?= images/keycloak-scim
SCIM_REPO            ?= https://github.com/Metatavu/keycloak-scim-server.git
SCIM_REF             ?= develop

# OCI image configuration for helper artifacts (override on the CLI/ENV)
TLS_IMG       ?= quay.io/example/openldap-tls-generator:0.1.0
TLS_PLATFORM  ?= linux/amd64
TLS_CONTEXT   ?= images/openldap-tls-generator

VALUES_FLAGS := -f $(CHART)/values.yaml

.PHONY: lint deps deploy status health teardown redeploy tls-image tls-push scim-keycloak-image scim-keycloak-push

lint:
	./scripts/verify-helm-chart.sh

deps:
	helm dependency update $(CHART)

deploy: deps
	helm upgrade --install $(RELEASE) $(CHART) -n $(NAMESPACE) $(VALUES_FLAGS) --wait --timeout 10m --debug

status:
	helm status $(RELEASE) -n $(NAMESPACE) || true
	kubectl -n $(NAMESPACE) get pods,pvc

health:
	kubectl -n $(NAMESPACE) exec deploy/$(RELEASE) -- sh -lc 'curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8080/health/ready'

teardown:
	-helm uninstall $(RELEASE) -n $(NAMESPACE)
	-kubectl -n $(NAMESPACE) delete pvc -l app.kubernetes.io/instance=$(RELEASE),app.kubernetes.io/name=postgresql
	-kubectl -n $(NAMESPACE) wait --for=delete pvc -l app.kubernetes.io/instance=$(RELEASE),app.kubernetes.io/name=postgresql --timeout=120s

redeploy: teardown deploy

tls-image:
	$(CONTAINER_ENGINE) build \
		--platform $(TLS_PLATFORM) \
		-t $(TLS_IMG) \
		$(TLS_CONTEXT)

tls-push: tls-image
	$(CONTAINER_ENGINE) push $(TLS_IMG)

scim-keycloak-image:
	$(CONTAINER_ENGINE) build \
		--platform $(SCIM_KEYCLOAK_PLATFORM) \
		--build-arg KEYCLOAK_IMAGE=$(KEYCLOAK_BASE_IMAGE) \
		--build-arg SCIM_REPO=$(SCIM_REPO) \
		--build-arg SCIM_REF=$(SCIM_REF) \
		-t $(SCIM_KEYCLOAK_IMG) \
		$(SCIM_KEYCLOAK_CONTEXT)

scim-keycloak-push: scim-keycloak-image
	$(CONTAINER_ENGINE) push $(SCIM_KEYCLOAK_IMG)
