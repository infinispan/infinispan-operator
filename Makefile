# Current Operator version
VERSION ?= $(shell git describe --tags --always --dirty)
# Default bundle image tag
BUNDLE_IMG ?= controller-bundle:$(VERSION)
export KUBECONFIG ?= ${HOME}/.kube/config
export WATCH_NAMESPACE ?= namespace-for-testing

# Options for 'bundle-build'
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# The namespace to deploy the infinispan-operator
DEPLOYMENT_NAMESPACE ?= infinispan-operator-system

# Image URL to use all building/pushing image targets
IMG ?= quay.io/infinispan/operator:latest

# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true,preserveUnknownFields=false"

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

.PHONY: unit-test
## Execute unit tests
unit-test: manager
	go test ./api/... -v
	go test ./controllers/... -v

.PHONY: test
## Execute end to end (e2e) tests on running clusters.
test: manager manifests
	scripts/run-tests.sh main

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

.PHONY: manager
## Build manager binary
manager: generate fmt vet
	go build -o bin/manager main.go

.PHONY: run
## Run the operator against the configured Kubernetes cluster in ~/.kube/config
run: manager manifests
	OSDK_FORCE_RUN_MODE=local go run ./main.go

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
deploy: manifests kustomize
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	cd config/default && $(KUSTOMIZE) edit set namespace $(DEPLOYMENT_NAMESPACE)
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
## Undeploy controller from the configured Kubernetes cluster in ~/.kube/config
undeploy:
	$(KUSTOMIZE) build config/default | kubectl delete -f -

.PHONY: manifests
## Generate manifests locally e.g. CRD, RBAC etc.
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

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
	$(RICE) embed-go -i controllers/dependencies.go -i controllers/grafana.go
	find . -type f -name 'rice-box.go' -exec sed -i "s|time.Unix(.*, 0)|time.Unix(1620137619, 0)|" {} \;

.PHONY: docker-build
## Build the docker image
docker-build: manager
	docker build -t $(IMG) .

.PHONY: docker-push
## Push the docker image
docker-push:
	docker push $(IMG)

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
	$(call go-get-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/cmd/golangci-lint@v1.39.0)

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
	operator-sdk generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | operator-sdk generate bundle -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)
	operator-sdk bundle validate ./bundle

.PHONY: bundle-build
## Build the bundle image.
bundle-build:
	docker build --build-arg VERSION=$(VERSION) -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
## Push the bundle image.
bundle-push:
	docker push $(BUNDLE_IMG)
