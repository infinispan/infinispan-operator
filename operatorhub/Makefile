TMP_DIR ?= /tmp
PROFILE ?= operatorhub.io
NAMESPACE ?= local-operators

PROG  := infinispan-operatorhub

.PHONY: config clean start test help

config:
	./build/config.sh ${PROFILE}

clean:
	./build/clean.sh

start:
	./build/start.sh ${PROFILE}

checkout-olm:
	./build/checkout.sh \
	${TMP_DIR} \
	operator-lifecycle-manager \
	https://github.com/operator-framework/operator-lifecycle-manager \
	9ba3512

install-olm:
	./build/install-olm.sh ${TMP_DIR} true

install-olm-ff:
	./build/install-olm.sh ${TMP_DIR}

checkout-marketplace:
	./build/checkout.sh \
	${TMP_DIR} \
	operator-marketplace \
	https://github.com/operator-framework/operator-marketplace \
	2ccd923

install-marketplace:
	./build/install-marketplace.sh ${TMP_DIR} true

install-marketplace-ff:
	./build/install-marketplace.sh ${TMP_DIR}

install-operator:
	./build/install-operator.sh ${NAMESPACE} true

install-operator-ff:
	./build/install-operator.sh ${NAMESPACE}

test:
	./build/test.sh ${NAMESPACE}

delete:
	./build/delete.sh ${PROFILE}

all: config start checkout-olm install-olm-ff checkout-marketplace install-marketplace-ff install-operator-ff test
