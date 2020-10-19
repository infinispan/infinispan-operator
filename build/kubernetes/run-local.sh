#!/usr/bin/env bash

NAMESPACE=${1-infinispan}

echo "Using kubeconfig: ${KUBECONFIG}"
echo "Running for cluster: $(kubectl config current-context)"

kubectl apply -f deploy/crds/infinispan.org_infinispans_crd.yaml
kubectl apply -f deploy/crds/infinispan.org_caches_crd.yaml
kubectl wait --for condition=established crd infinispans.infinispan.org --timeout=60s
WATCH_NAMESPACE="${NAMESPACE}" ./build/_output/bin/infinispan-operator -kubeconfig "${KUBECONFIG}"
