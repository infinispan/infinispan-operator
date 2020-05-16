.PHONY: config start stop delete delete-namespace image deploy-operator undeploy-operator deploy undeploy create-cache put get

NAMESPACE ?= local-operators
SITE_1 ?= SiteA
SITE_2 ?= SiteB

## config                Configure Minikube for cross-site replication set up
config:
	$(call config,${SITE_1})
	$(call config,${SITE_2})

define config
minikube profile ${1}
minikube config set memory 4096
minikube config set cpus 4
minikube config set disk-size 10GB
endef

## start                 Start multiple Minikube instances for cross-site set up
start:
	$(call start,${SITE_1})
	$(call start,${SITE_2})

define start
minikube profile "$1"
minikube start
endef

## stop                  Stop cross-site Minikube instances
stop:
	$(call stop,${SITE_1})
	$(call stop,${SITE_2})

define stop
minikube profile "$1"
minikube stop
endef

## delete                Delete the Minikube VMs
delete:
	$(call delete,${SITE_1})
	$(call delete,${SITE_2})

define delete
minikube profile "$1"
minikube delete
endef

## image                 Builds and creates an operator image
image:
	make clean build -C ../..
	./image.sh ${SITE_1}
	./image.sh ${SITE_2}

## deploy-operator       Deploy operator to both sites
deploy-operator:
	$(call deploy-operator,${SITE_1})
	$(call deploy-operator,${SITE_2})

define deploy-operator
minikube profile "$1"
kubectl create ns "${NAMESPACE}" || true
kubectl config set-context --current --namespace="${NAMESPACE}"
kubectl apply -f ../../deploy/role.yaml
kubectl apply -f ../../deploy/service_account.yaml
kubectl apply -f ../../deploy/role_binding.yaml
kubectl apply -f ../../deploy/crds/infinispan.org_infinispans_crd.yaml
kubectl apply -f ../../deploy/crds/infinispan.org_caches_crd.yaml
cp ../../deploy/operator.yaml .
awk '{sub(/Always/,"IfNotPresent")}1' operator.yaml > temp.txt && mv temp.txt operator.yaml
kubectl apply -f operator.yaml
kubectl wait --for condition=available --timeout=120s deployment/infinispan-operator
endef

## undeploy-operator     Undeploy operator
undeploy-operator:
	$(call undeploy-operator,${SITE_1})
	$(call undeploy-operator,${SITE_2})

define undeploy-operator
minikube profile ${1}
kubectl delete deployment infinispan-operator || true
kubectl delete role infinispan-operator || true
kubectl delete rolebinding infinispan-operator || true
kubectl delete serviceaccount infinispan-operator || true
kubectl delete crd infinispans.infinispan.org || true
endef

## deploy                Deploy Infinispan cross-site
deploy:
	$(call deploy,${SITE_1},a)
	$(call deploy,${SITE_2},b)
	$(call waitWellFormed,${SITE_1})
	$(call waitWellFormed,${SITE_2})

define deploy
minikube profile ${1}
kubectl create secret generic site-secrets \
    --from-file=certificate-authority=/opt/minikube/.minikube/ca.crt \
    --from-file=client-certificate=/opt/minikube/.minikube/client.crt \
    --from-file=client-key=/opt/minikube/.minikube/client.key
kubectl apply -f ../../deploy/cr/xsite-minikube/xsite_${2}.yaml
endef

define waitWellFormed
minikube profile ${1}
kubectl wait --for condition=wellFormed --timeout=120s infinispan/example-infinispan
endef

## undeploy              Undeploy Infinispan cross-site
undeploy:
	$(call undeploy,${SITE_1})
	$(call undeploy,${SITE_2})

define undeploy
minikube profile ${1}
kubectl delete secret site-secrets || true
kubectl delete infinispan example-infinispan || true
endef

## create-cache          Create x-site cache
create-cache:
	$(call create-cache,${SITE_1},B)
	$(call create-cache,${SITE_2},A)

developer-password = $$(kubectl get secret \
    example-infinispan-generated-secret \
    -o jsonpath="{.data.identities\.yaml}" \
    | base64 --decode \
    | yq -r .credentials[0].password)

url = $$(minikube service \
    example-infinispan-external \
    --url --namespace ${NAMESPACE})

define create-cache
minikube profile ${1}

curl -v \
    -d '<infinispan><cache-container><distributed-cache name="example" mode="SYNC"><backups><backup site="Site${2}" strategy="ASYNC" timeout="30000"/></backups></distributed-cache></cache-container></infinispan>' \
    -H "Content-Type: application/xml" \
    -u developer:${call developer-password} \
    -X POST \
    ${call url}/rest/v2/caches/example
endef

## put                   Put key/value pair into SiteA example cache
put:
	minikube profile ${SITE_1}
	curl --fail -v \
        -d 'test-value' \
        -H "Content-Type: text/plain" \
        -u developer:${call developer-password} \
        -X POST \
        ${call url}/rest/v2/caches/example/test-key

## get                   Get value from SiteB example cache
get:
	minikube profile ${SITE_2}
	curl --fail -v \
        -H "Content-Type: text/plain" \
        -u developer:${call developer-password} \
        ${call url}/rest/v2/caches/example/test-key

## delete-namespace      Delete namespace from both sites
delete-namespace:
	$(call delete-namespace,${SITE_1})
	$(call delete-namespace,${SITE_2})

define delete-namespace
minikube profile "$1"
kubectl delete namespace ${NAMESPACE}
endef
