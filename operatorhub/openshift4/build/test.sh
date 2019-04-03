#!/usr/bin/env bash

set -e -x

NAMESPACE=${1}
VERSION=${2}

oc apply \
    -f https://raw.githubusercontent.com/infinispan/infinispan-operator/${VERSION}/deploy/cr/cr_minimal.yaml \
    -n ${NAMESPACE}

set +x
echo
echo "--> Check that all pods are running with:"
echo "oc get pods -w"
echo
echo "--> If no pods are running yet, check the event log to make sure good progress is being made:"
echo "oc get events -w"
