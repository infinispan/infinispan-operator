#!/usr/bin/env bash

set -e -x

NAMESPACE=${1}
VERSION=${2}

# Delete operator group instances
# Names are random and there are not match selectors, so list them and delete each
oc get og -o go-template --template '{{range .items}}{{.metadata.name}}{{"\n"}}{{end}}' | xargs oc delete og || true

oc delete infinispan example-infinispan -n ${NAMESPACE} || true
oc delete csv infinispan-operator.v${VERSION} -n ${NAMESPACE} || true
oc delete subscription infinispan -n ${NAMESPACE} || true
oc delete opsrc gzamarre-operators -n openshift-marketplace || true
