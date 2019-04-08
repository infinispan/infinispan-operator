#!/usr/bin/env bash

set -e -x

NAMESPACE=${1}
VERSION=${2}

kubectl apply \
    -f https://raw.githubusercontent.com/infinispan/infinispan-operator/${VERSION}/deploy/cr/cr_minimal.yaml \
    -n ${NAMESPACE}
