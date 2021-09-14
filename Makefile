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

all: manager

lint: golangci-lint
	$(GOLANGCI_LINT) run --enable errorlint
	$(GOLANGCI_LINT) run --disable-all --enable bodyclose --skip-dirs test

unit-test: manager
	go test ./api/... -v
	go test ./controllers/... -v

test: manager manifests
	scripts/run-tests.sh main

multinamespace-test: manager manifests
	scripts/run-tests.sh multinamespace

backuprestore-test: manager manifests
	scripts/run-tests.sh backup-restore

batch-test: manager manifests
	scripts/run-tests.sh batch
	
# Build manager binary
manager: generate fmt vet
	go build -o bin/manager main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
run: manager manifests
	OSDK_FORCE_RUN_MODE=local go run ./main.go

# Install CRDs into a cluster
install: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

# Uninstall CRDs from a cluster
uninstall: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests kustomize
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	cd config/default && $(KUSTOMIZE) edit set namespace $(DEPLOYMENT_NAMESPACE)
	$(KUSTOMIZE) build config/default | kubectl apply -f -

# UnDeploy controller from the configured Kubernetes cluster in ~/.kube/config
undeploy:
	$(KUSTOMIZE) build config/default | kubectl delete -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

# Generate code
generate: controller-gen rice
	$(CONTROLLER_GEN) object paths="./..."
# Generate rice-box files and fix timestamp value
	$(RICE) embed-go -i controllers/dependencies.go -i controllers/grafana.go
	find . -type f -name 'rice-box.go' -exec sed -i "s|time.Unix(.*, 0)|time.Unix(1620137619, 0)|" {} \;


# Build the docker image
docker-build: manager
	docker build -t $(IMG) .

# Push the docker image
docker-push:
	docker push $(IMG)

# Download Rice locally if necessary
RICE = $(shell pwd)/bin/rice
rice:
	$(call go-get-tool,$(RICE),github.com/GeertJohan/go.rice/rice@v1.0.2)

# Download controller-gen locally if necessary
CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen:
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.4.1)

# Download kustomize locally if necessary
KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize:
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v3@v3.8.7)

GOLANGCI_LINT = $(shell pwd)/bin/golangci-lint
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

# Generate bundle manifests and metadata, then validate generated files.
.PHONY: bundle
bundle: manifests kustomize
	operator-sdk generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | operator-sdk generate bundle -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)
	operator-sdk bundle validate ./bundle

# Build the bundle image.
.PHONY: bundle-build
bundle-build:
	docker build --build-arg VERSION=$(VERSION) -f bundle.Dockerfile -t $(BUNDLE_IMG) .

# Push the bundle image.
.PHONY: bundle-push
bundle-push:
	docker push $(BUNDLE_IMG)
