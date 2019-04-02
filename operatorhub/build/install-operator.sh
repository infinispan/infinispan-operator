#!/usr/bin/env bash

set -e -x

NAMESPACE=${1}
SKIP_ERROR=${2-false}

kubectl create ns ${NAMESPACE} || ${SKIP_ERROR}
kubectl apply -f deploy/ || ${SKIP_ERROR}
