.PHONY: image push-docker make-target-dir new-project delete-project checkout-operatorhub copy-operatorhub install-operator-courier build-upgrade clean-upgrade verify-upgrade auto-push-quay manual-push-quay add-operatorsource add-catalogsourceconfig install-operator create-cache put get clean-olm clean upgrade-operator create-cluster upgrade-cluster delete-cluster all

GIT_DIR ?= $(TARGET_DIR)/git
IMAGE_REPO ?= jboss
IMAGE_NAME ?= infinispan-operator
MANIFESTS_DIR ?= $(TARGET_DIR)/infinispan
NAMESPACE ?= infinispan-testing
PACKAGE_NAME ?= infinispan
PACKAGE_YAML ?= infinispan.package.yaml
PROFILE ?= operator-minikube
QUAY_VERSION_PREFIX ?= "0.0"
TAG ?= latest
TARGET_DIR ?= target
UPGRADE_CSV_YAML ?= infinispan-operator.v$(UPGRADE_VERSION).clusterserviceversion.yaml
UPGRADE_VERSION ?= 1.0.99

image:
	pushd ..; \
	make clean build; \
	docker build -t ${IMAGE_REPO}/${IMAGE_NAME}:${TAG} . -f build/Dockerfile.single; \
	popd

push-docker:
	docker push $(IMAGE_REPO)/${IMAGE_NAME}:$(TAG)

make-target-dir:
	mkdir -p ${GIT_DIR} || true

new-project:
	oc new-project ${NAMESPACE} || true

delete-project:
	oc delete project ${NAMESPACE} || true

checkout-operatorhub: make-target-dir
	cd ${GIT_DIR}; \
	git clone https://github.com/operator-framework/community-operators

copy-operatorhub:
	cp -r \
	${GIT_DIR}/community-operators/upstream-community-operators/infinispan \
	${MANIFESTS_DIR}

latest-operator = `ls -d */ | sort -V -r | head -n 1 | cut -f1 -d'/'`

swap-image:
	cd ${MANIFESTS_DIR}; \
	latest=${call latest-operator}; \
	csv=infinispan-operator.v$${latest}.clusterserviceversion.yaml; \
	cd $${latest}; \
	sed -i'.backup' "s/jboss\/infinispan-operator/${IMAGE_REPO}\/${IMAGE_NAME}/g" $${csv}; \
	rm -f *.backup

latest-server = `curl -s https://registry.hub.docker.com/v1/repositories/infinispan/server/tags \
    | jq '.[].name' \
    | sort -r \
    | sed "2q;d" \
    | sed 's/"//g'`

install-operator-courier:
	pip3 install operator-courier

build-upgrade:
	pushd ${GIT_DIR}/community-operators/upstream-community-operators/infinispan > /dev/null; \
	replaced=${call latest-operator}; \
	popd > /dev/null; \
	cd ${MANIFESTS_DIR}; \
	mkdir ${UPGRADE_VERSION}; \
	cp ../../../deploy/olm-catalog/infinispan-operator.clusterserviceversion.yaml \
	${UPGRADE_VERSION}/${UPGRADE_CSV_YAML}; \
	cp ../../../deploy/olm-catalog/infinispan.org_infinispans_crd.yaml ${UPGRADE_VERSION}; \
	cp ../../../deploy/olm-catalog/infinispan.org_caches_crd.yaml ${UPGRADE_VERSION}; \
	sed -i'.backup' "s/9.9.9/${UPGRADE_VERSION}/g" ${UPGRADE_VERSION}/${UPGRADE_CSV_YAML}; \
	sed -i'.backup' "s/9.9.8/$${replaced}/g" ${UPGRADE_VERSION}/${UPGRADE_CSV_YAML}; \
	sed -i'.backup' "s/infinispan\/server:latest/infinispan\/server:${call latest-server}/g" ${UPGRADE_VERSION}/${UPGRADE_CSV_YAML}; \
	sed -i'.backup' "s/jboss\/infinispan-operator/${IMAGE_REPO}\/${IMAGE_NAME}/g" ${UPGRADE_VERSION}/${UPGRADE_CSV_YAML}; \
	rm -f ${UPGRADE_VERSION}/*.backup; \
	sed -i'.backup' "s/$${replaced}/${UPGRADE_VERSION}/g" ${PACKAGE_YAML}; \
	rm -f *.backup

clean-upgrade:
	cd ${MANIFESTS_DIR}; \
	rm -drf ${UPGRADE_VERSION} || true

verify-upgrade: build-upgrade
	cd ${TARGET_DIR}; \
	operator-courier verify ${PACKAGE_NAME}

latest-quay-version = `curl -s \
    https://quay.io/api/v1/repository/${QUAY_USERNAME}/infinispan \
    | jq '.releases|sort_by(.name)|map(.name)|reverse|.[0]' \
    | sed 's/"//g' \
    | cut -d. -f3`

auto-push-quay:
	version=${call latest-quay-version}; \
	let "version+=1"; \
	${call push-quay,${QUAY_VERSION_PREFIX}.$${version}}

check-env:
ifndef QUAY_VERSION
	$(error QUAY_VERSION is undefined)
endif

manual-push-quay: check-env checkout-operatorhub copy-operatorhub
	${call push-quay,${QUAY_VERSION}}

define push-quay
cd ${TARGET_DIR}; \
operator-courier push \
	${PACKAGE_NAME} \
	${QUAY_USERNAME} \
	${PACKAGE_NAME} \
	${1} \
	"${QUAY_TOKEN}"
endef

add-operatorsource:
	./olm-operatorsource.sh
	until oc wait --for condition=available --timeout=60s deployment/${QUAY_USERNAME}-operators -n openshift-marketplace; \
	do echo "Waiting for operator deployment"; sleep 2; \
	done

add-catalogsourceconfig:
	./olm-catalogsourceconfig.sh ${NAMESPACE} ${PACKAGE_NAME}

add-operatorgroup:
	./olm-operatorgroup.sh ${NAMESPACE}

# remove loop once https://github.com/kubernetes/kubernetes/issues/83242is implemented
add-subscription:
	./olm-subscription.sh ${NAMESPACE} ${PACKAGE_NAME}
	until oc wait --for condition=available --timeout=120s deployment/infinispan-operator; \
	do echo "Waiting for operator"; sleep 2; \
	done

install-operator: add-catalogsourceconfig add-operatorgroup add-subscription

# remove loop once https://github.com/kubernetes/kubernetes/issues/83242is implemented
install-example:
	./infinispan-example.sh ${NAMESPACE}
	until oc wait --for condition=wellFormed --timeout=240s infinispan/example-infinispan; \
	do echo "Waiting for Infinispan"; sleep 2; \
	done

developer-password = $$(oc get secret \
   example-infinispan-generated-secret \
   -o jsonpath="{.data.identities\.yaml}" \
   | base64 -d \
   | yq -r .credentials[0].password)

url = $$(oc get service example-infinispan -o jsonpath={.spec.clusterIP})

create-cache:
	oc exec -it example-infinispan-0 -- \
	curl --fail -v \
	-d '<infinispan><cache-container><distributed-cache name="example"><persistence><file-store/></persistence></distributed-cache></cache-container></infinispan>' \
	-H "Content-Type: application/xml" \
	-u developer:${call developer-password} \
	-X POST \
	${call url}:11222/rest/v2/caches/example

delete-cache:
	oc exec -it example-infinispan-0 -- \
	curl --fail -v \
	-u developer:${call developer-password} \
	-X DELETE \
	${call url}:11222/rest/v2/caches/example || true

put:
	oc exec -it example-infinispan-0 -- \
	curl --fail -v \
	-d 'test-value' \
	-H "Content-Type: text/plain" \
	-u developer:${call developer-password} \
	-X POST \
	${call url}:11222/rest/v2/caches/example/test-key

get:
	oc exec -it example-infinispan-0 -- \
	curl --fail -v \
	-H "Content-Type: text/plain" \
	-u developer:${call developer-password} \
	${call url}:11222/rest/v2/caches/example/test-key

clean:
	rm -drf ${TARGET_DIR} || true

# oc scale method works in OpenShift 4.1
# oc patch method works in OpenShift 4.2
disable-default-operatorsources:
	oc scale --replicas 0 -n openshift-cluster-version deployments/cluster-version-operator
	oc patch operatorhub cluster \
	-n openshift-marketplace \
	-p '{"spec":{"disableAllDefaultSources": true}}' \
	--type=merge

clean-olm: disable-default-operatorsources
	operatorsources=`oc get operatorsources -n openshift-marketplace -o name`; \
	for opsrc in $${operatorsources}; \
	do oc delete $${opsrc} -n openshift-marketplace; \
	done
	oc delete catalogsourceconfig ${QUAY_USERNAME}-operators -n openshift-marketplace || true
	oc delete catalogsourceconfig infinispan -n openshift-marketplace || true
	oc delete operatorgroup infinispan -n ${NAMESPACE} || true
	oc delete subscription infinispan -n ${NAMESPACE} || true
	oc delete infinispan example-infinispan -n ${NAMESPACE} || true
	oc delete deployment infinispan-operator -n ${NAMESPACE} || true
	oc delete csv infinispan-operator.v${UPGRADE_VERSION} || true

upgrade-operator:
	oc patch operatorsource ${QUAY_USERNAME}-operators \
	-n openshift-marketplace \
	-p '[{"op":"replace","path":"/status","value":{}}]' \
	--type=json

create-cluster: new-project checkout-operatorhub copy-operatorhub swap-image auto-push-quay add-operatorsource install-operator install-example create-cache put get

upgrade-cluster: verify-upgrade auto-push-quay upgrade-operator

delete-cluster: delete-cache clean-olm clean
