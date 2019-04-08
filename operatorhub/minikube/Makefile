TMP_DIR ?= /tmp
PROFILE ?= operatorhub.io
VMDRIVER ?= virtualbox
NAMESPACE ?= local-operators
VERSION ?= 0.2.1

PROG  := infinispan-operatorhub-minikube

.PHONY: config clean start test help

config:
	./build/config.sh ${PROFILE} ${VMDRIVER}

clean:
	./build/clean.s ${NAMESPACE} ${VERSION}

start:
	./build/start.sh ${PROFILE}

checkout-olm:
	./build/checkout.sh \
	${TMP_DIR} \
	operator-lifecycle-manager \
	https://github.com/operator-framework/operator-lifecycle-manager \
	9ba3512

install-olm:
	./build/install-olm.sh ${TMP_DIR}

checkout-marketplace:
	./build/checkout.sh \
	${TMP_DIR} \
	operator-marketplace \
	https://github.com/operator-framework/operator-marketplace \
	2ccd923

install-marketplace:
	./build/install-marketplace.sh ${TMP_DIR}

install-operator:
	./build/install-operator.sh ${NAMESPACE}

test:
	./build/test.sh ${NAMESPACE}

delete:
	./build/delete.sh ${PROFILE}

all: config start checkout-olm install-olm checkout-marketplace install-marketplace install-operator test
