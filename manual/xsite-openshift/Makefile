.PHONY: delete-namespace image push deploy-operator undeploy-operator deploy undeploy create-cache put get

NAMESPACE ?= infinispan-operator
IMAGE ?= jboss/infinispan-operator
TAG ?= latest

## image                 Builds and creates an operator image
image:
	pushd ../..; \
	make clean build; \
	docker build -t ${IMAGE}:${TAG} . -f build/Dockerfile.single; \
	popd

## push                  Push operator image
push:
	docker push $(IMAGE):$(TAG)

### deploy-operator      Deploy operator to both sites
deploy-operator:
	$(call login-server,A)
	$(call deploy-operator)
	$(call login-server,B)
	$(call deploy-operator)

define login-server
oc login --insecure-skip-tls-verify=true \
    --server=${SERVER_${1}} \
    --token=${TOKEN_${1}}
oc project ${NAMESPACE}
endef

define deploy-operator
oc new-project "${NAMESPACE}" || true
oc apply -f ../../deploy/role.yaml
oc apply -f ../../deploy/service_account.yaml
oc apply -f ../../deploy/role_binding.yaml
oc apply -f ../../deploy/crds/infinispan.org_infinispans_crd.yaml
oc apply -f ../../deploy/crds/infinispan.org_caches_crd.yaml
cp ../../deploy/operator.yaml .
awk '{sub(/jboss\/infinispan-operator/,"${IMAGE}")}1' operator.yaml > temp.txt && mv temp.txt operator.yaml
oc apply -f operator.yaml
oc wait --for condition=available --timeout=120s deployment/infinispan-operator
endef

## undeploy-operator     Undeploy operator
undeploy-operator:
	$(call login-server,A)
	$(call undeploy-operator)
	$(call login-server,B)
	$(call undeploy-operator)

define undeploy-operator
oc delete deployment infinispan-operator || true
oc delete role infinispan-operator || true
oc delete rolebinding infinispan-operator || true
oc delete serviceaccount infinispan-operator || true
oc delete crd infinispans.infinispan.org || true
endef

## deploy                Deploy Infinispan cross-site
deploy:
	$(call login-server,A)
	$(call deploy,a,b,B)
	$(call login-server,B)
	$(call deploy,b,a,A)
	$(call login-server,A)
	$(call waitWellFormed)
	$(call login-server,B)
	$(call waitWellFormed)

define deploy
oc create secret generic site-${2}-secrets --from-literal=token=${TOKEN_${3}}
oc apply -f ../../deploy/cr/xsite-openshift/xsite_${1}.yaml
endef

define waitWellFormed
oc wait --for condition=wellFormed --timeout=240s infinispan/example-infinispan
endef

## undeploy              Undeploy Infinispan cross-site
undeploy:
	$(call login-server,A)
	$(call undeploy,b)
	$(call login-server,B)
	$(call undeploy,a)

define undeploy
oc delete secret site-${1}-secrets || true
oc delete infinispan example-infinispan || true
endef

### create-cache          Create x-site cache
create-cache:
	$(call login-server,A)
	$(call create-cache,B)
	$(call login-server,B)
	$(call create-cache,A)

developer-password = $$(oc get secret \
   example-infinispan-generated-secret \
   -o jsonpath="{.data.identities\.yaml}" \
   | base64 --decode \
   | yq -r .credentials[0].password)

url = $$(oc get service \
   example-infinispan-external \
   -o jsonpath={.status.loadBalancer.ingress\[0\].hostname})

define create-cache
curl --fail -v \
   -d '<infinispan><cache-container><distributed-cache name="example" mode="SYNC"><backups><backup site="Site${1}" strategy="ASYNC" timeout="30000"/></backups></distributed-cache></cache-container></infinispan>' \
   -H "Content-Type: application/xml" \
   -u developer:${call developer-password} \
   -X POST \
   ${call url}:11222/rest/v2/caches/example
endef

## put                   Put key/value pair into SiteA example cache
put:
	$(call login-server,A)
	curl --fail -v \
		-d 'test-value' \
		-H "Content-Type: text/plain" \
		-u developer:${call developer-password} \
		-X POST \
		${call url}:11222/rest/v2/caches/example/test-key

### get                   Get value from SiteB example cache
get:
	$(call login-server,B)
	curl --fail -v \
		-H "Content-Type: text/plain" \
		-u developer:${call developer-password} \
		${call url}:11222/rest/v2/caches/example/test-key

## delete-namespace      Delete namespace from both sites
delete-namespace:
	$(call login-server,A)
	$(call delete-namespace)
	$(call login-server,B)
	$(call delete-namespace)

define delete-namespace
oc delete namespace ${NAMESPACE}
endef
