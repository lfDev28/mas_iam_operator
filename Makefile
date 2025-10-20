NAMESPACE ?= iam
RELEASE   ?= mas-iam
CHART     ?= charts/keycloak-stack

VALUES_FLAGS := -f $(CHART)/values.yaml

.PHONY: deps deploy status health teardown redeploy

deps:
	helm dependency update $(CHART)

deploy: deps
	helm upgrade --install $(RELEASE) $(CHART) -n $(NAMESPACE) $(VALUES_FLAGS) --wait --timeout 10m --debug

status:
	helm status $(RELEASE) -n $(NAMESPACE) || true
	kubectl -n $(NAMESPACE) get pods,pvc

health:
	kubectl -n $(NAMESPACE) exec deploy/$(RELEASE)-keycloak -- sh -lc 'curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8080/health/ready'

teardown:
	-helm uninstall $(RELEASE) -n $(NAMESPACE)
	-kubectl -n $(NAMESPACE) delete pvc -l app.kubernetes.io/instance=$(RELEASE),app.kubernetes.io/name=postgresql
	-kubectl -n $(NAMESPACE) wait --for=delete pvc -l app.kubernetes.io/instance=$(RELEASE),app.kubernetes.io/name=postgresql --timeout=120s

redeploy: teardown deploy
