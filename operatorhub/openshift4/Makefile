NAMESPACE ?= local-operators
VERSION ?= 0.2.1

PROG  := infinispan-operatorhub-openshift4

.PHONY: clean test help

clean:
	./build/clean.sh ${NAMESPACE} ${VERSION}

install-operatorsource:
	./build/install-operatorsource.sh ${NAMESPACE}

install-operator:
	./build/install-operator.sh ${NAMESPACE}

test:
	./build/test.sh ${NAMESPACE} ${VERSION}

all: install-operatorsource install-operator test
