IMAGE ?= quay.io/infinispan/operator
TAG ?= latest
GOOS ?= linux
PROG  := infinispan-operator
OPERATOR_SDK_VERSION ?= v0.16.0
YQ_VERSION ?= 4.6.1
GOLANG_CI_LINT_VERSION ?= 1.39.0
KUBECONFIG ?= ${HOME}/.kube/config

.PHONY: dep build image push run clean help

.DEFAULT_GOAL := help

## dep              Ensure deps is locally available.
##
dep:
	go mod tidy

## lint             Invoke linter to promote Go lang best practices.
##
lint:
	./build/run-lint.sh ${GOLANG_CI_LINT_VERSION}

## vet              Inspects the source code for suspicious constructs.
##
vet:
	go vet ./...

## clean            Remove all generated build files.
##
clean:
	rm -rf build/_output

## updatecsv         Update csv with new roles
##
update-csv:
	./build/update-csv.sh ${YQ_VERSION}

## codegen          Generates CRDs, k8s code for custom resources and install bundle.
##
codegen: update-csv
	./build/crds-gen.sh ${OPERATOR_SDK_VERSION} ${YQ_VERSION}
	./build/install-bundle.sh

## install-bundle   Combine single operator install bundle from the multiples files.
##
install-bundle:
	./build/install-bundle.sh

## build            Compile and build the Infinispan operator.
##
build:
	./build/build.sh ${GOOS}

## image            Build a Docker image for the Infinispan operator.
image: build
ifeq ($(MULTISTAGE),NO)
##                  This branch builds the image in a docker container which provides multistage build
##                  for distro that doesn't provide multistage build directly (i.e. Fedora 30 or early).
##
	-docker run -d --rm --privileged -p 23751:2375 --name dind docker:18-dind --storage-driver overlay2
	-sleep 5
	-docker --host=:23751 build -t "$(IMAGE):$(TAG)" . -f build/Dockerfile
	-docker --host=:23751 save -o ./make.image.out.tar "$(IMAGE):$(TAG)"
	-docker load -i ./make.image.out.tar
	-rm -f make.image.out.tar
	-docker stop dind
else
	docker build -t "$(IMAGE):$(TAG)" . -f build/Dockerfile
endif
## push             Push Docker images to Docker Hub.
##
push: image
	docker push $(IMAGE):$(TAG)

## push-okd4        Push docker image to the OCP|OKD4 internal registry and deploy the operator.
##
push-okd4: build
	build/push-okd4.sh ${YQ_VERSION}

## run              Create the Infinispan operator on OKD with the public image.
##                  - Specify cluster access configuration with KUBECONFIG.
##                  Example: "make run KUBECONFIG=/path/to/admin.kubeconfig"
##
run:
	build/run-okd.sh ${KUBECONFIG}

## run-local        Run Infinispan operator code locally and deploying descriptors to OKD.
##                  - Specify cluster access configuration with KUBECONFIG.
##                  Example: "make run-local KUBECONFIG=/path/to/admin.kubeconfig"
##                  - Override the OKD login user name.
##                  Example: "make run-local OC_USER=myuser"
##                  - Override the project for operator run.
##                  Example: "make run-local PROJECT_NAME=infinispan-operator"
##
run-local: build
	build/run-local.sh ${KUBECONFIG}

## unit-test        Perform unit test
##
unit-test: build
	go test ./pkg/controller/infinispan ./pkg/controller/infinispan/resources/config -v

## test             Perform end to end (e2e) tests on running clusters.
##                  - Specify the target cluster with KUBECONFIG.
##                  Example: "make test KUBECONFIG=/path/to/admin.kubeconfig"
##                  - Specify a single test to run.
##                  Example: "make test TEST_NAME=TestCacheService"
##                  - Specify the namespace/project for running test.
##                  Example: "make test TESTING_NAMESPACE=infinispan-operator"
##                  - Specify whether run operator locally or use the predefined installation.
##                  Example: "make test RUN_LOCAL_OPERATOR=false"
##                  - Specify expose service type. NodePort or LoadBalancer are supported only.
##                  Example: "make test EXPOSE_SERVICE_TYPE=LoadBalancer"
##                  - Specify parallel test running count. Default is one, i.e. no parallel tests enabled.
##                  Example: "make test PARALLEL_COUNT=2"
##
test: build
	build/run-tests.sh ${KUBECONFIG} main

## multinamespace-test Perform end to end (e2e) tests in multinamespace mode
##                     Same setting as `ŧest` rule
##

multinamespace-test: build
	build/run-tests.sh ${KUBECONFIG} multinamespace

## backuprestore-test Perform end to end (e2e) tests for Backup/Restore CR's
##                     Same setting as `ŧest` rule
##

backuprestore-test: build
	build/run-tests.sh ${KUBECONFIG} backup-restore

## batch-test Perform end to end (e2e) tests for Batch CR's
##                     Same setting as `ŧest` rule
##

batch-test: build
	build/run-tests.sh ${KUBECONFIG} batch

## upgrade-test         Performs test upgrade from one operator version to another against different git branches
##                      This script deploys operator to the OKD/OCP cluster from the local source code or already
##                      build image located inside the docker registry.
##                      - Specify the testing cluster with KUBECONFIG.
##                      Example: "make upgrade-test KUBECONFIG=/path/to/admin.kubeconfig"
##                      - Override the OKD login user name.
##                      Example: "make upgrade-test OC_USER=myuser"
##                      - Override the project for operator run.
##                      Example: "make upgrade-test PROJECT_NAME=infinispan-operator"
##                      - Specify source migration git branch or tag name. This is required parameter.
##                      Example: "make upgrade-test FROM_UPGRADE_VERSION=2.0.x"
##                      - Override from source code build flag.
##                      Example: "make upgrade-test FROM_SOURCE_BUILD_FLAG=true"
##                      - Specify target migration git branch or tag name. If it's not defined the current branch will be used as target.
##                      Example: "make upgrade-test TO_UPGRADE_VERSION=master"
##                      - Override to source code build flag.
##                      Example: "make upgrade-test TO_SOURCE_BUILD_FLAG=true"
##
upgrade-test:
	build/upgrade-test.sh ${KUBECONFIG}

## upgrade-test-local   Performs test upgrade from one operator version to another
##                      This script runs operator locally connecting to the any located OKD/OCP/K8S cluster
##                      - Specify the testing cluster with KUBECONFIG.
##                      Example: "make upgrade-test-local KUBECONFIG=/path/to/admin.kubeconfig"
##                      Example: "make upgrade-test-local FROM_UPGRADE_VERSION=2.0.x"
##                      - Specify target migration git branch or tag name. If it's not defined the current branch will be used as target.
##                      Example: "make upgrade-test-local TO_UPGRADE_VERSION=master"

##
upgrade-test-local:
	build/upgrade-test-local.sh ${KUBECONFIG}

## release          Release a versioned operator.
##                  - Requires 'RELEASE_NAME=X.Y.Z'. Defaults to dry run.
##                  - Pass 'DRY_RUN=false' to commit the release.
##                  - Example: "make DRY_RUN=false RELEASE_NAME=X.Y.Z release"
##
release:
	build/release.sh

## copy-kubeconfig  Copy admin.kubeconfig for the OKD cluster to the location of KUBECONFIG.
##
copy-kubeconfig:
	build/copy-kubeconfig.sh ${KUBECONFIG}

# Minikube parameters
PROFILE ?= operator-minikube
VMDRIVER ?= virtualbox
NAMESPACE ?= infinispan-operator

## minikube-config        Configure Minikube VM with adequate options.
minikube-config:
	build/minikube/config.sh ${PROFILE} ${VMDRIVER}

## minikube-start         Start Minikube.
minikube-start:
	build/minikube/start.sh ${PROFILE}

## minikube-run-local     Run Infinispan operator image locally deploying descriptors to Minikube.
minikube-run-local: build
	build/minikube/run-local.sh ${NAMESPACE}

## minikube-run-local     Run testsuite on Minikube
minikube-test: build
	build/minikube/run-tests.sh ${NAMESPACE}

## minikube-clean         Remove Infinispan operator descriptors from Minikube.
minikube-clean: clean
	build/minikube/clean.sh ${NAMESPACE}

## minikube-delete        Delete Minikube virtual machine.
minikube-delete:
	build/minikube/delete.sh ${PROFILE}

## minikube-stop          Stop Minikube virtual machine.
minikube-stop:
	build/minikube/stop.sh ${PROFILE}

help : Makefile
	@sed -n 's/^##//p' $<
