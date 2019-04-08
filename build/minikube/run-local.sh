#!/usr/bin/env bash

set -e -x

NAMESPACE=${1}
KUBECONFIG=${2-${HOME}/.kube/config}

echo "Using KUBECONFIG '$KUBECONFIG'"

kubectl create ns ${NAMESPACE} || true
kubectl create configmap infinispan-app-configuration --from-file=./config -n ${NAMESPACE} || echo "Config map already present"
kubectl apply -f deploy/rbac.yaml -n ${NAMESPACE}
kubectl apply -f deploy/crd.yaml -n ${NAMESPACE}
WATCH_NAMESPACE=${NAMESPACE} ./build/_output/bin/infinispan-operator -kubeconfig $KUBECONFIG
