#!/usr/bin/env bash

set -e -x

NAMESPACE=${1}
KUBECONFIG=${2-${HOME}/.kube/config}

echo "Using KUBECONFIG '$KUBECONFIG'"

kubectl create ns ${NAMESPACE} || true
kubectl apply -f deploy/crds/infinispan.org_infinispans_crd.yaml
kubectl apply -f deploy/crds/infinispan.org_caches_crd.yaml
kubectl apply -f deploy/crds/infinispan.org_backups_crd.yaml
kubectl apply -f deploy/crds/infinispan.org_restores_crd.yaml
kubectl apply -f deploy/crds/infinispan.org_batches_crd.yaml
WATCH_NAMESPACE=${NAMESPACE} ./build/_output/bin/infinispan-operator -kubeconfig $KUBECONFIG
