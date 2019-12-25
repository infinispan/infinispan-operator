#!/usr/bin/env bash

KUBECONFIG=${1-openshift.local.clusterup/openshift-apiserver/admin.kubeconfig}
PROJECT_NAME=${PROJECT_NAME-default}

echo "Using KUBECONFIG '$KUBECONFIG'"
echo "Using PROJECT_NAME '$PROJECT_NAME'"

oc login -u system:admin
oc project "$PROJECT_NAME"
oc apply -f deploy/rbac.yaml
oc apply -f deploy/crd.yaml
oc wait --for condition=established crd infinispans.infinispan.org --timeout=60s
WATCH_NAMESPACE="$PROJECT_NAME" ./build/_output/bin/infinispan-operator -kubeconfig "$KUBECONFIG"
