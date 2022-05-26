# Current Operator version
VERSION ?= $(shell git describe --tags --always --dirty)
# Default bundle image tag
BUNDLE_IMG ?= infinispan-operator-bundle:v$(VERSION)
export KUBECONFIG ?= ${HOME}/.kube/config
export WATCH_NAMESPACE ?= namespace-for-testing

# Options for 'bundle-build'
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= --version $(VERSION) $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# The namespace to deploy the infinispan-operator
DEPLOYMENT_NAMESPACE ?= infinispan-operator-system

# Image URL to use all building/pushing image targets
IMG ?= quay.io/infinispan/operator:latest

# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true,preserveUnknownFields=false"

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.19

export CONTAINER_TOOL ?= docker

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

.DEFAULT_GOAL := help

help:
	@awk '/^#/{c=substr($$0,3);next}c&&/^[[:alpha:]][[:alnum:]_-]+:/{print substr($$1,1,index($$1,":")),c}1{c=0}' $(MAKEFILE_LIST) | column -s: -t


.PHONY: lint
## Invoke linter to promote Go lang best practices.
lint: golangci-lint
	$(GOLANGCI_LINT) run --enable errorlint
	$(GOLANGCI_LINT) run --disable-all --enable bodyclose --skip-dirs test

.PHONY: test
## Execute tests
test: manager manifests envtest
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test ./api/... ./controllers/... ./pkg/... -coverprofile cover.out

.PHONY: infinispan-test
## Execute end to end (e2e) tests for Infinispan CRs
infinispan-test: manager manifests
	scripts/run-tests.sh infinispan

.PHONY: cache-test
## Execute end to end (e2e) tests for Cache CRs
cache-test: manager manifests
	scripts/run-tests.sh cache

.PHONY: multinamespace-test
## Execute end to end (e2e) tests in multinamespace mode
multinamespace-test: manager manifests
	scripts/run-tests.sh multinamespace

.PHONY: backuprestore-test
## Execute end to end (e2e) tests for Backup/Restore CR's
backuprestore-test: manager manifests
	scripts/run-tests.sh backup-restore

.PHONY: batch-test
## Execute end to end (e2e) tests for Batch CR's
batch-test: manager manifests
	scripts/run-tests.sh batch

.PHONY: webhook-test
## Execute end to end (e2e) tests for Webhook OLM integration
webhook-test: manager manifests
	scripts/run-tests.sh webhook

.PHONY: upgrade-test
## Execute end to end (e2e) tests for OLM upgrades.
upgrade-test: manager manifests
	scripts/run-tests.sh upgrade

.PHONY: hotrod-rolling-upgrade-test
## Execute end to end (e2e) tests for Hot Rod Rolling upgrades.
hotrod-upgrade-test: manager manifests
	scripts/run-tests.sh hotrod-rolling-upgrade

.PHONY: xsite-test
## Execute end to end (e2e) tests for XSite functionality
xsite-test: manager manifests
	scripts/run-tests.sh xsite 45m

.PHONY: manager
## Build manager binary
manager: generate fmt vet
	go build -ldflags="-X 'github.com/infinispan/infinispan-operator/launcher.Version=$(VERSION)'" -o bin/manager main.go

.PHONY: run
## Run the operator against the configured Kubernetes cluster in ~/.kube/config
run: manager manifests
	OSDK_FORCE_RUN_MODE=local ENABLE_WEBHOOKS=false ./bin/manager operator

.PHONY: install
## Install CRDs into a cluster
install: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
## Uninstall CRDs from a cluster
uninstall: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

.PHONY: deploy
## Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests kustomize deploy-cert-manager
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	cd config/default && $(KUSTOMIZE) edit set namespace $(DEPLOYMENT_NAMESPACE)
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: deploy-cert-manager
## Deploy cert-manager so that webhooks can be utilised by the operator deployment
deploy-cert-manager:
	kubectl apply -f https://github.com/jetstack/cert-manager/releases/download/v1.6.0/cert-manager.yaml
	kubectl rollout status -n cert-manager deploy/cert-manager-webhook -w --timeout=120s

.PHONY: undeploy
## Undeploy controller from the configured Kubernetes cluster in ~/.kube/config
undeploy:
	$(KUSTOMIZE) build config/default | kubectl delete -f -

.PHONY: manifests
## Generate manifests locally e.g. CRD, RBAC etc.
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases
	operator-sdk generate kustomize manifests -q

.PHONY: fmt
## Run go fmt against code
fmt:
	go fmt ./...

.PHONY: vet
## Inspects the source code for suspicious constructs.
vet:
	go vet ./...

.PHONY: generate
## Generate code
generate: controller-gen rice
	$(CONTROLLER_GEN) object paths="./..."
# Generate rice-box files and fix timestamp value
	$(RICE) embed-go -i controllers/grafana.go -i pkg/templates/templates.go
	find . -type f -name 'rice-box.go' -exec sed -i "s|time.Unix(.*, 0)|time.Unix(1620137619, 0)|" {} \;

.PHONY: operator-build
## Build the operator image
operator-build: manager
	$(CONTAINER_TOOL) build --build-arg OPERATOR_VERSION=$(VERSION) -t $(IMG) .

.PHONY: operator-push
## Push the operator image
operator-push:
	$(CONTAINER_TOOL) push $(IMG)

RICE = $(shell pwd)/bin/rice
.PHONY: rice
## Download Rice locally if necessary
rice:
	$(call go-get-tool,$(RICE),github.com/GeertJohan/go.rice/rice@v1.0.2)

CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
.PHONY: controller-gen
## Download controller-gen locally if necessary
controller-gen:
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.4.1)

KUSTOMIZE = $(shell pwd)/bin/kustomize
.PHONY: kustomize
## Download kustomize locally if necessary
kustomize:
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v3@v3.8.7)

GOLANGCI_LINT = $(shell pwd)/bin/golangci-lint
.PHONY: golanci-lint
## Download golanci-lint locally if necessary
golangci-lint:
	$(call go-get-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/cmd/golangci-lint@v1.46.2)

export GO_JUNIT_REPORT = $(shell pwd)/bin/go-junit-report
.PHONY: GO_JUNIT_REPORT
## Download go-junit-report locally if necessary
go-junit-report:
	$(call go-get-tool,$(GO_JUNIT_REPORT),github.com/jstemmer/go-junit-report@latest)

ENVTEST = $(shell pwd)/bin/setup-envtest
.PHONY: envtest
## Download envtest-setup locally if necessary.
envtest:
	$(call go-get-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest@latest)

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go get $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef

.PHONY: bundle
## Generate bundle manifests and metadata, then validate generated files.
bundle: manifests kustomize
# Remove old bundle as old files aren't always cleaned up by operator-sdk
	rm -rf bundle
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | operator-sdk generate bundle -q --overwrite $(BUNDLE_METADATA_OPTS)
# TODO is there a better way todo this with operator-sdk and/or kustomize. `commonAnnotations` adds annotations to all resources, not just CSV.
	sed -i -e "s,<IMAGE>,$(IMG)," bundle/manifests/infinispan-operator.clusterserviceversion.yaml
# Hack to set the metadata package name to "infinispan". `operator-sdk --package infinispan` can't be used as it
# changes the csv name from  infinispan-operator.v0.0.0 -> infinispan.v0.0.0
	sed -i -e 's/infinispan-operator/infinispan/' bundle/metadata/annotations.yaml bundle.Dockerfile
	rm bundle/manifests/infinispan-operator-controller-manager_v1_serviceaccount.yaml
	rm bundle/manifests/infinispan-operator-webhook-service_v1_service.yaml
	operator-sdk bundle validate ./bundle

.PHONY: bundle-build
## Build the bundle image.
bundle-build:
	$(CONTAINER_TOOL) build --build-arg VERSION=$(VERSION) -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
## Push the bundle image.
bundle-push:
	$(CONTAINER_TOOL) push $(BUNDLE_IMG)

.PHONY: opm
export OPM = ./bin/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.21.0/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

.PHONY: jq
export JQ = ./bin/jq
jq: ## Download opm locally if necessary.
ifeq (,$(wildcard $(JQ)))
ifeq (,$(shell which jq 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(JQ)) ;\
	curl -sSLo $(JQ) https://github.com/stedolan/jq/releases/download/jq-1.6/jq-linux64 ;\
	chmod +x $(JQ) ;\
	}
else
JQ = $(shell which jq)
endif
endif

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
export BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
export CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

## This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
## https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
## Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
catalog-build: opm jq ## Build a catalog image.
	./scripts/create-olm-catalog.sh

.PHONY: catalog-push
## Push the catalog image.
catalog-push: ## Push a catalog image.
	$(CONTAINER_TOOL) push $(CATALOG_IMG)
