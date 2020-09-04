#!/usr/bin/env bash

set -e -x

NAMESPACE=${1}

kubectl delete infinispan example-infinispan -n ${NAMESPACE} || true
kubectl delete all,crd,sa,role,clusterrole,rolebinding,clusterrolebinding -l name=infinispan-operator -n ${NAMESPACE} || true
