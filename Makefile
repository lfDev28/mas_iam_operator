NAMESPACE            ?= mas-est
RELEASE              ?= mas-est-iam
CHART                ?= charts/mas-iam-stack
CONTAINER_ENGINE     ?= podman
KEYCLOAK_BASE_IMAGE  ?= quay.io/keycloak/keycloak:26.0.5
SCIM_KEYCLOAK_IMG    ?= quay.io/example/mas-iam-keycloak:scim-0.0.1
SCIM_KEYCLOAK_PLATFORM ?= linux/amd64
SCIM_KEYCLOAK_CONTEXT ?= images/keycloak-scim
SCIM_REPO            ?= https://github.com/Metatavu/keycloak-scim-server.git
SCIM_REF             ?= develop
SCIM_BRIDGE_DIR      ?= services/scim-bridge

# OCI image configuration for helper artifacts (override on the CLI/ENV)
TLS_IMG       ?= quay.io/example/openldap-tls-generator:0.1.0
TLS_PLATFORM  ?= linux/amd64
TLS_CONTEXT   ?= images/openldap-tls-generator

VALUES_FLAGS := -f $(CHART)/values.yaml

.PHONY: lint deps deploy status health teardown redeploy tls-image tls-push scim-keycloak-image scim-keycloak-push scim-bridge-build doc-pdf docs-pdf doc-pdf-clean docs-pdf-clean user-guide-pdf user-guide-clean
.PHONY: check check-go check-scripts check-helm check-images check-images-scan release-check bump release-images release-verify release-tag smoke test-openldap-storage login-check

# ---- Checks ------------------------------------------------------------------
# `make check` is the definition of done: the same checks CI runs
# (.github/workflows/go.yml), stopping on the first red.
GO_MODULES    ?= tools/mas-iam-installer services/scim-bridge
GO_TEST_FLAGS ?= -race
HELM_CHART    ?= operators/mas-iam-operator/helm-charts/mas-iam-stack

check: check-go check-scripts check-helm lint check-images-scan
	@echo "make check: all green"

check-go:
	@set -e; for m in $(GO_MODULES); do \
		echo "== go vet/test/build: $$m"; \
		(cd $$m && go vet ./... && go test $(GO_TEST_FLAGS) ./... && go build ./...); \
	done

check-scripts:
	@echo "== bash -n scripts/*.sh"
	@set -e; for f in scripts/*.sh; do bash -n "$$f"; done
	@echo "== python3 syntax scripts/*.py"
	@set -e; for f in scripts/*.py; do python3 -c 'import ast,sys; ast.parse(open(sys.argv[1]).read(), sys.argv[1])' "$$f"; done

check-helm:
	@echo "== helm lint $(HELM_CHART)"
	@helm lint $(HELM_CHART)

# Every image ref in the tree is listed in images.yaml (offline).
check-images-scan:
	@./scripts/check-images.sh --scan-only

# Every images.yaml ref exists on its registry with its archs (network).
check-images:
	@./scripts/check-images.sh

# ---- Release -----------------------------------------------------------------
# Order and approvals: .claude/skills/mas-est-release/SKILL.md. Nothing here
# pushes unless the script is given --push / PUSH=1.
VERSION          ?=
OPERATOR_VERSION ?=
BRIDGE_VERSION   ?=
ARTIFACT         ?= cli
PUSH             ?=

release-check:
	@./scripts/release-check.sh --version "$(VERSION)" \
		$(if $(OPERATOR_VERSION),--operator-version "$(OPERATOR_VERSION)") \
		$(if $(BRIDGE_VERSION),--bridge-version "$(BRIDGE_VERSION)")

bump:
	@./scripts/bump-version.sh $(ARTIFACT) "$(VERSION)"

release-images:
	@./scripts/release-images.sh $(ARTIFACT) "$(VERSION)" $(if $(PUSH),--push)

release-verify:
	@./scripts/release-images.sh $(ARTIFACT) "$(VERSION)" --verify

release-tag:
	@./scripts/release-tag.sh --version "$(VERSION)" \
		$(if $(OPERATOR_VERSION),--operator-version "$(OPERATOR_VERSION)") \
		$(if $(BRIDGE_VERSION),--bridge-version "$(BRIDGE_VERSION)") \
		$(if $(PUSH),--push)

SMOKE_ARGS ?= --help
smoke:
	@./scripts/smoke.sh $(SMOKE_ARGS)

# Container test of the OpenLDAP storage init script (legacy-volume migration).
# Needs podman or docker; CI runs it on every change to the chart.
test-openldap-storage:
	./scripts/test-openldap-storage.sh

# Headless SAML/OIDC logins of the Keycloak demo users through MAS (no browser).
#   make login-check HOME_URL=https://home.<instance>.<apps-domain> [USERS="saml.user1 oidc.user2"]
login-check:
	@test -n "$(HOME_URL)" || { echo "usage: make login-check HOME_URL=https://home.<instance>.<apps-domain> [USERS=...]" >&2; exit 2; }
	@python3 -I ./scripts/mas-login-check.py --home "$(HOME_URL)" $(USERS)

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

scim-bridge-build:
	cd $(SCIM_BRIDGE_DIR) && go build ./...

# ---- Documentation PDFs ------------------------------------------------------
# One-time setup (macOS):
#   brew install pandoc
#   brew install --cask basictex
#   sudo tlmgr update --self && sudo tlmgr install adjustbox titling enumitem soul background
#   mkdir -p ~/.local/share/pandoc/templates
#   curl -fsSL https://github.com/Wandmalfarbe/pandoc-latex-template/releases/latest/download/Eisvogel.tar.gz | tar -xz -C /tmp
#   cp /tmp/Eisvogel-*/eisvogel.latex ~/.local/share/pandoc/templates/
#   test -s ~/.local/share/pandoc/templates/eisvogel.latex   # must be non-empty
# (eisvogel.latex is no longer on the repo's master branch; the old raw URL returns an empty file.)
#
# Render any Markdown doc:  make doc-pdf DOC=docs/ANNOUNCEMENT.md
# Long docs read better as chapters:  make doc-pdf DOC=docs/GUIDE.md DOC_TOP_LEVEL=chapter DOC_PANDOC_FLAGS='-V book -V documentclass=report'
# `make docs-pdf` renders the whole user-facing set (DOCS_PDF_SET). Links between
# the Markdown files are rewritten to point at the sibling PDFs.
DOC              ?= docs/GUIDE.md
DOC_PDF          ?= $(DOC:.md=.pdf)
DOC_TOP_LEVEL    ?= section
DOC_PANDOC_FLAGS ?=
DOC_LUA_FILTER   ?= docs/pandoc/md-links-to-pdf.lua
DOCS_PDF_SET     ?= docs/ANNOUNCEMENT.md docs/GUIDE.md docs/TROUBLESHOOTING.md docs/KNOWN-LIMITATIONS.md docs/RELEASE-NOTES.md

doc-pdf:
	pandoc $(DOC) \
		--from markdown \
		--template eisvogel \
		--lua-filter=$(DOC_LUA_FILTER) \
		--resource-path=$(dir $(DOC)) \
		--top-level-division=$(DOC_TOP_LEVEL) \
		--pdf-engine=xelatex \
		-V geometry:margin=0.75in \
		-V fontsize=10pt \
		$(DOC_PANDOC_FLAGS) \
		-o $(DOC_PDF)
	@echo "Wrote $(DOC_PDF)"

docs-pdf:
	@for d in $(DOCS_PDF_SET); do $(MAKE) --no-print-directory doc-pdf DOC=$$d || exit 1; done

doc-pdf-clean:
	rm -f $(DOC_PDF)

docs-pdf-clean:
	rm -f $(DOCS_PDF_SET:.md=.pdf)

user-guide-pdf:
	$(MAKE) doc-pdf DOC=docs/GUIDE.md DOC_TOP_LEVEL=chapter DOC_PANDOC_FLAGS='-V book -V documentclass=report'

user-guide-clean:
	$(MAKE) doc-pdf-clean DOC=docs/GUIDE.md
