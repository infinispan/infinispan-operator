IMAGE ?= jboss/infinispan-server-operator
TAG ?= latest
PROG  := infinispan-server-operator

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
	rm -rf tmp

## run:         Run the operator from jboss/infinispan-server-operator in a running OKD cluster
run:
	oc login -u system:admin
	oc create configmap infinispan-app-configuration --from-file=./config || echo "Config map already present"
	oc apply -f deploy/rbac.yaml
	oc apply -f deploy/operator.yaml
	oc apply -f deploy/crd.yaml

## run-local:   Run the operator locally in a running OKD cluster
run-local: build
	oc login -u system:admin
	oc project default
	oc create configmap infinispan-app-configuration --from-file=./config || echo "Config map already present"
	oc apply -f deploy/rbac.yaml
	oc apply -f deploy/crd.yaml
	WATCH_NAMESPACE="default" ./tmp/_output/bin/infinispan-server-operator -kubeconfig openshift.local.clusterup/openshift-apiserver/admin.kubeconfig

## test:        Run e2e tests
test: build
	GOCACHE=off go test -v ./test/e2e

help : Makefile
	@sed -n 's/^##//p' $<
