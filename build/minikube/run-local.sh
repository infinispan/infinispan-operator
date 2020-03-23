#!/usr/bin/env bash

set -e -x

NAMESPACE=${1}
KUBECONFIG=${2-${HOME}/.kube/config}

echo "Using KUBECONFIG '$KUBECONFIG'"

kubectl create ns ${NAMESPACE} || true
kubectl apply -f deploy/role.yaml -n ${NAMESPACE}
kubectl apply -f deploy/service_account.yaml -n ${NAMESPACE}
kubectl apply -f deploy/role_binding.yaml -n ${NAMESPACE}
kubectl apply -f deploy/crds/infinispan.org_infinispans_crd.yaml -n ${NAMESPACE}
kubectl apply -f deploy/crds/infinispan.org_caches_crd.yaml -n ${NAMESPACE}
WATCH_NAMESPACE=${NAMESPACE} ./build/_output/bin/infinispan-operator -kubeconfig $KUBECONFIG
