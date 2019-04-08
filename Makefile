IMAGE ?= jboss/infinispan-operator
TAG ?= latest
GOOS ?= linux
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
	./build/build.sh ${GOOS}

## image       Build a Docker image for the Infinispan operator.
image: build
ifeq ($(MULTISTAGE),NO)
## This branch builds the image in a docker container which provides multistage build
## for distro that doesn't provide multistage build directly (i.e. Fedora 29)
	-docker run -d --rm --privileged -p 23751:2375 --name dind docker:stable-dind --storage-driver overlay2
	-docker --host=:23751 build -t "$(IMAGE):$(TAG)" . -f build/Dockerfile
	-docker --host=:23751 save -o ./make.image.out.tar "$(IMAGE):$(TAG)"
	-docker load -i ./make.image.out.tar
	-rm -f make.image.out.tar
	-docker stop dind
else
	docker build -t "$(IMAGE):$(TAG)" . -f build/Dockerfile
endif
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


# Minikube parameters
PROFILE ?= operator-minikube
VMDRIVER ?= virtualbox
NAMESPACE ?= local-operators

minikube-config:
	build/minikube/config.sh ${PROFILE} ${VMDRIVER}

minikube-start:
	build/minikube/start.sh ${PROFILE}

minikube-run-local: build
	build/minikube/run-local.sh ${NAMESPACE}

minikube-clean: clean
	build/minikube/clean.sh ${NAMESPACE}

minikube-delete:
	build/minikube/delete.sh ${PROFILE}

help : Makefile
	@sed -n 's/^##//p' $<
