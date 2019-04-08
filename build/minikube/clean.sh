#!/usr/bin/env bash

set -e -x

NAMESPACE=${1}

kubectl delete infinispan example-infinispan -n ${NAMESPACE} || true
kubectl delete crd infinispans.infinispan.org -n ${NAMESPACE} || true
kubectl delete rolebinding infinispan-operator -n ${NAMESPACE} || true
kubectl delete serviceaccount infinispan-operator -n ${NAMESPACE} || true
kubectl delete role infinispan-operator -n ${NAMESPACE} || true
kubectl delete deployment infinispan-operator -n ${NAMESPACE} || true
