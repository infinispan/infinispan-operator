IMAGE ?= jboss/infinispan-operator
TAG ?= latest
PROG  := infinispan-operator

.PHONY: dep build image push run clean help

.DEFAULT_GOAL := help

## dep:         Ensure deps as available locally
dep:
	dep ensure

## codegen:     Run k8s code generator for custom resources (https://blog.openshift.com/kubernetes-deep-dive-code-generation-customresources/)
codegen:
	./build/codegen.sh

## build:       Compile and build the operator
build: dep
	./build/build.sh

## image:       Build the Docker image for the operator
image: build
	docker build -t "$(IMAGE):$(TAG)" . -f build/Dockerfile

## push:        Push the image to dockerhub
push: image
	docker push $(IMAGE):$(TAG)

## clean:       Remove all generated files during build
clean:
	rm -rf build/_output

## run:         Run the operator from jboss/infinispan-operator in a running OKD cluster. Specify the config path with the cmd line arg 'KUBECONFIG=/path/to/config'
run:
	build/run-okd.sh ${KUBECONFIG}

## run-local:   Run the operator locally in a running OKD cluster. Specify the config path with the cmd line arg 'KUBECONFIG=/path/to/config'
run-local: build
	build/run-local.sh ${KUBECONFIG}

## test:        Run e2e tests. A KUBECONFIG can be specified with cmd line arg 'KUBECONFIG=/path/to/config'
test: build
	build/run-tests.sh ${KUBECONFIG}

## copy-kubeconfig:   Copy cluster adming kubernetes config file to cmd line arg 'KUBECONFIG=/path/to/config' location
copy-kubeconfig:
	build/copy-kubeconfig.sh ${KUBECONFIG}

help : Makefile
	@sed -n 's/^##//p' $<
