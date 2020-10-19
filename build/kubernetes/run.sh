#!/usr/bin/env bash

NAMESPACE=${1-infinispan}

echo "Using kubeconfig: ${KUBECONFIG}"
echo "Running for cluster: $(kubectl config current-context)"

kubectl apply -f deploy/role.yaml --namespace $NAMESPACE
kubectl apply -f deploy/clusterrole.yaml --namespace $NAMESPACE
kubectl apply -f deploy/service_account.yaml --namespace $NAMESPACE
kubectl apply -f deploy/role_binding.yaml --namespace $NAMESPACE
sed -e "s|namespace:.*|namespace: ${NAMESPACE}|" deploy/clusterrole_binding.yaml | kubectl apply -f -
kubectl apply -f deploy/crds/infinispan.org_infinispans_crd.yaml
kubectl apply -f deploy/crds/infinispan.org_caches_crd.yaml
kubectl wait --for condition=established crd infinispans.infinispan.org --timeout=60s
kubectl apply -f deploy/operator.yaml --namespace $NAMESPACE
