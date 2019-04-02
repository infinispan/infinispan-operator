#!/usr/bin/env bash

set -e -x

NAMESPACE=${1}

kubectl apply \
    -f https://raw.githubusercontent.com/infinispan/infinispan-operator/0.1.0/deploy/cr/cr_minimal.yaml \
    -n ${NAMESPACE}
