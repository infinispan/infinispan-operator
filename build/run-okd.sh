#!/usr/bin/env bash

OC_USER=${OC_USER:-kubeadmin}
KUBECONFIG=${1-openshift.local.clusterup/openshift-apiserver/admin.kubeconfig}

echo "Using KUBECONFIG '$KUBECONFIG'"

oc login -u ${USER}
oc project default
oc apply -f deploy/role.yaml
oc apply -f deploy/service_account.yaml
oc apply -f deploy/role_binding.yaml
oc apply -f deploy/operator.yaml
oc apply -f deploy/crds/infinispan.org_infinispans_crd.yaml
oc apply -f deploy/crds/infinispan.org_caches_crd.yaml
