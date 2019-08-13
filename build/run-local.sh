#!/usr/bin/env bash

KUBECONFIG=${1-openshift.local.clusterup/openshift-apiserver/admin.kubeconfig}

echo "Using KUBECONFIG '$KUBECONFIG'"

oc login -u system:admin
oc project default
oc apply -f deploy/rbac.yaml
oc apply -f deploy/crd.yaml
WATCH_NAMESPACE="default" ./build/_output/bin/infinispan-operator -kubeconfig $KUBECONFIG
