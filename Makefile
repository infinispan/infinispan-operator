# Current Operator version
VERSION ?= $(shell git describe --tags --always --dirty)
# Default bundle image tag
IMAGE_TAG_BASE ?= infinispan-operator
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:v$(VERSION)
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

# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
    BUNDLE_GEN_FLAGS += --use-image-digests
endif

# The namespace to deploy the infinispan-operator
DEPLOYMENT_NAMESPACE ?= infinispan-operator-system

# Image URL to use all building/pushing image targets
IMG ?= operator:latest

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.24

export CONTAINER_TOOL ?= docker

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
export GO_JUNIT_REPORT ?= $(LOCALBIN)/go-junit-report
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint
KUSTOMIZE ?= $(LOCALBIN)/kustomize
MOCKGEN ?= $(LOCALBIN)/mockgen
RICE ?= $(LOCALBIN)/rice

## Tool Versions
CONTROLLER_TOOLS_VERSION ?= v0.9.2
GO_JUNIT_REPORT_VERSION ?= latest
GOLANGCI_LINT_VERSION ?= v1.53.3
KUSTOMIZE_VERSION ?= v3.8.7
RICE_VERSION ?= v1.0.2
JQ_VERSION ?= 1.7
YQ_VERSION ?= v4.31.1

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
test: manager manifests generate-mocks envtest
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
	cd config/manager && $(KUSTOMIZE) edit set image operator=$(IMG)
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
manifests: controller-gen operator-sdk
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
	$(OPERATOR_SDK) generate kustomize manifests -q

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

.PHONY: generate-mocks
## Generate testing mocks
generate-mocks: mockgen
	$(MOCKGEN) -source=./pkg/reconcile/pipeline/infinispan/api.go -destination=./pkg/reconcile/pipeline/infinispan/api_mocks.go -package=infinispan

.PHONY: operator-build
## Build the operator image
operator-build: manager
	$(CONTAINER_TOOL) build --build-arg OPERATOR_VERSION=$(VERSION) -t $(IMG) .

.PHONY: operator-push
## Push the operator image
operator-push:
	$(CONTAINER_TOOL) push $(IMG)

.PHONY: rice
rice: $(RICE) ## Download Rice locally if necessary.
$(RICE): $(LOCALBIN)
	test -s $(RICE) || GOBIN=$(LOCALBIN) go install github.com/GeertJohan/go.rice/rice@$(RICE_VERSION)

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	test -s $(KUSTOMIZE) || { curl -s $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN); }

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(CONTROLLER_GEN) || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	test -s $(GOLANGCI_LINT) || GOBIN=$(LOCALBIN) go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.PHONY: go-junit-report
go-junit-report: $(GO_JUNIT_REPORT) ## Download go-junit-report locally if necessary.
$(GO_JUNIT_REPORT): $(LOCALBIN)
	test -s $(GO_JUNIT_REPORT) || GOBIN=$(LOCALBIN) go install github.com/jstemmer/go-junit-report@$(GO_JUNIT_REPORT_VERSION)

.PHONY: mockgen
mockgen: $(MOCKGEN) ## Download mockgen locally if necessary
$(MOCKGEN): $(LOCALBIN)
	test -s $(MOCKGEN) || GOBIN=$(LOCALBIN) go install github.com/golang/mock/mockgen@v1.6.0

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(ENVTEST) || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: bundle
## Generate bundle manifests and metadata, then validate generated files.
bundle: manifests kustomize yq
# Remove old bundle as old files aren't always cleaned up by operator-sdk
	rm -rf bundle
	cd config/manager && $(KUSTOMIZE) edit set image operator=$(IMG)
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS)
# TODO is there a better way todo this with operator-sdk and/or kustomize. `commonAnnotations` adds annotations to all resources, not just CSV.
	sed -i -e "s,<IMAGE>,$(IMG)," bundle/manifests/infinispan-operator.clusterserviceversion.yaml
# Hack to set the metadata package name to "infinispan". `operator-sdk --package infinispan` can't be used as it
# changes the csv name from  infinispan-operator.v0.0.0 -> infinispan.v0.0.0
	sed -i -e 's/infinispan-operator/infinispan/' bundle/metadata/annotations.yaml bundle.Dockerfile
	rm bundle/manifests/infinispan-operator-webhook-service_v1_service.yaml
# Minimum Openshift version must correspond to `minKubeVersion` set in CSV
	$(YQ) -i '.annotations += {"com.redhat.openshift.versions": "v4.11"}' bundle/metadata/annotations.yaml
	$(OPERATOR_SDK) bundle validate ./bundle

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
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.23.0/$${OS}-$${ARCH}-opm ;\
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
	curl -sSLo $(JQ) https://github.com/stedolan/jq/releases/download/jq-$(JQ_VERSION)/jq-linux64 ;\
	chmod +x $(JQ) ;\
	}
else
JQ = $(shell which jq)
endif
endif

.PHONY: yq
export YQ = ./bin/yq
yq: ## Download yq locally if necessary.
ifeq (,$(wildcard $(YQ)))
ifeq (,$(shell which yq 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(YQ)) ;\
	curl -sSLo $(YQ) https://github.com/mikefarah/yq/releases/download/$(YQ_VERSION)/yq_linux_amd64 ;\
	chmod +x $(YQ) ;\
	}
else
YQ = $(shell which yq)
endif
endif

.PHONY: oc
export OC = ./bin/oc
oc: ## Download oc locally if necessary.
ifeq (,$(wildcard $(OC)))
ifeq (,$(shell which oc 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OC)) ;\
	curl -sSLo oc.tar.gz https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/4.11.6/openshift-client-linux.tar.gz ;\
	tar -xf oc.tar.gz -C $(dir $(OC)) oc ;\
	}
else
OC = $(shell which oc)
endif
endif

.PHONY: operator-sdk
## Download operator-sdk locally if necessary.
export OPERATOR_SDK = ./bin/operator-sdk
operator-sdk:
ifeq (,$(wildcard $(OPERATOR_SDK)))
ifeq (,$(shell which operator-sdk 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPERATOR_SDK)) ;\
	curl -sSLo $(OPERATOR_SDK) https://github.com/operator-framework/operator-sdk/releases/download/v1.24.1/operator-sdk_linux_amd64 ;\
	chmod +x $(OPERATOR_SDK) ;\
	}
else
OPERATOR_SDK = $(shell which operator-sdk)
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
