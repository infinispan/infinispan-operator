#!/usr/bin/env bash

OC_USER=${OC_USER:-kubeadmin}
KUBECONFIG=${1-openshift.local.clusterup/openshift-apiserver/admin.kubeconfig}
PROJECT_NAME=${PROJECT_NAME-default}

echo "Using KUBECONFIG '${KUBECONFIG}'"
echo "Using PROJECT_NAME '${PROJECT_NAME}'"

oc login -u "${OC_USER}"
oc project "${PROJECT_NAME}"
oc apply -f deploy/role.yaml
oc apply -f deploy/clusterrole.yaml
oc apply -f deploy/service_account.yaml
oc apply -f deploy/role_binding.yaml
sed -e "s|namespace:.*|namespace: ${PROJECT_NAME}|" deploy/clusterrole_binding.yaml | oc apply -f -
oc apply -f deploy/operator.yaml
./build/install-crds.sh
