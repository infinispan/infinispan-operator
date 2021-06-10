#!/usr/bin/env bash

OC_USER=${OC_USER:-kubeadmin}
KUBECONFIG=${1-${HOME}/.kube/config}
PROJECT_NAME=${PROJECT_NAME-default}

echo "Using KUBECONFIG '${KUBECONFIG}'"
echo "Using PROJECT_NAME '${PROJECT_NAME}'"

oc login -u "${OC_USER}"
oc project "${PROJECT_NAME}"
./build/install-crds.sh
oc wait --for condition=established crd infinispans.infinispan.org --timeout=60s
WATCH_NAMESPACE="${PROJECT_NAME}" OSDK_FORCE_RUN_MODE=local OPERATOR_NAME=infinispan-operator ./build/_output/bin/infinispan-operator -kubeconfig "${KUBECONFIG}"
