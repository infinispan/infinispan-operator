#!/usr/bin/env bash

OC_USER=${OC_USER:-kubeadmin}
KUBECONFIG=${1-openshift.local.clusterup/openshift-apiserver/admin.kubeconfig}

echo "Using KUBECONFIG '$KUBECONFIG'"

oc login -u ${USER}
oc project default
oc apply -f deploy/rbac.yaml
oc apply -f deploy/operator.yaml
oc apply -f deploy/crd.yaml
