IMAGE ?= jboss/infinispan-operator
TAG ?= latest
PROG  := infinispan-operator

.PHONY: dep build image push run clean help

.DEFAULT_GOAL := help

## dep         Ensure deps is locally available.
dep:
	dep ensure

## codegen     Run the k8s code generator for custom resources.
##             See https://blog.openshift.com/kubernetes-deep-dive-code-generation-customresources/
codegen:
	./build/codegen.sh

## build       Compile and build the Infinispan operator.
build: dep
	./build/build.sh

## image       Build a Docker image for the Infinispan operator.
image: build
	docker build -t "$(IMAGE):$(TAG)" . -f build/Dockerfile

## push        Push Docker images to Docker Hub.
push: image
	docker push $(IMAGE):$(TAG)

## clean       Remove all generated build files.
clean:
	rm -rf build/_output

## run         Create the Infinispan operator on OKD with the public image.
##             - Specify cluster access configuration with KUBECONFIG.
##             - Example: "make run KUBECONFIG=/path/to/admin.kubeconfig"
##
run:
	build/run-okd.sh ${KUBECONFIG}

## run-local   Create the Infinispan operator image on OKD from a local image.
##             - Specify cluster access configuration with KUBECONFIG.
##             - Example: "make run-local KUBECONFIG=/path/to/admin.kubeconfig"
##
run-local: build
	build/run-local.sh ${KUBECONFIG}

## test        Perform end to end (e2e) tests on running clusters.
##             - Specify the target cluster with KUBECONFIG.
##             - Example: "make test KUBECONFIG=/path/to/admin.kubeconfig"
##
test: build
	build/run-tests.sh ${KUBECONFIG}

## release     Release a versioned operator.
##             - Requires 'RELEASE_NAME=X.Y.Z'. Defaults to dry run.
##             - Pass 'DRY_RUN=false' to commit the release.
##             - Example: "make DRY_RUN=false RELEASE_NAME=X.Y.Z release"
##
release:
	build/release.sh

## copy-kubeconfig   Copy admin.kubeconfig for the OKD cluster to the location of KUBECONFIG
copy-kubeconfig:
	build/copy-kubeconfig.sh ${KUBECONFIG}

help : Makefile
	@sed -n 's/^##//p' $<
