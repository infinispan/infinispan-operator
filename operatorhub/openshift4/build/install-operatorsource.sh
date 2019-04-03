#!/usr/bin/env bash

set -e -x

NAMESPACE=${1}

oc create ns ${NAMESPACE} || true
oc apply -f deploy/00-operatorsource.cr.yaml

set +x
echo
echo "--> Check the deployment of the operator source with:"
echo "oc get pods -n openshift-marketplace -w"
echo
