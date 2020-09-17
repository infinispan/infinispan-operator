#!/usr/bin/env bash

OC_USER=${OC_USER:-kubeadmin}
KUBECONFIG=${1-openshift.local.clusterup/openshift-apiserver/admin.kubeconfig}

echo "Using KUBECONFIG '${KUBECONFIG}'"
echo "Using PROJECT_NAME '${PROJECT_NAME}'"

oc login -u "${OC_USER}"
oc project "${PROJECT_NAME}"
oc apply -f deploy/rbac.yaml
oc apply -f deploy/crd.yaml
oc apply -f deploy/operator.yaml
