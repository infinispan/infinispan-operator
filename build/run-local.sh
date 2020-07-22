#!/usr/bin/env bash

OC_USER=${OC_USER:-kubeadmin}
KUBECONFIG=${1-openshift.local.clusterup/openshift-apiserver/admin.kubeconfig}
PROJECT_NAME=${PROJECT_NAME-default}

echo "Using KUBECONFIG '$KUBECONFIG'"
echo "Using PROJECT_NAME '$PROJECT_NAME'"

oc login -u ${OC_USER}
oc project "$PROJECT_NAME"
oc apply -f deploy/crds/infinispan.org_infinispans_crd.yaml
oc apply -f deploy/crds/infinispan.org_caches_crd.yaml
oc wait --for condition=established crd infinispans.infinispan.org --timeout=60s
WATCH_NAMESPACE="$PROJECT_NAME" ./build/_output/bin/infinispan-operator -kubeconfig "$KUBECONFIG"
