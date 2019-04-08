#!/usr/bin/env bash

set -e -x

NAMESPACE=${1}
VERSION=${2}

# Delete operator group instances
# Names are random and there are not match selectors, so list them and delete each
kubectl get og -o go-template --template '{{range .items}}{{.metadata.name}}{{"\n"}}{{end}}' -n ${NAMESPACE} | xargs oc delete og || true

kubectl delete infinispan example-infinispan -n ${NAMESPACE} || true
kubectl delete csv infinispan-operator.v${VERSION} -n ${NAMESPACE} || true
kubectl delete subscription infinispan -n ${NAMESPACE} || true
kubectl delete opsrc gzamarre-operators -n openshift-marketplace || true
