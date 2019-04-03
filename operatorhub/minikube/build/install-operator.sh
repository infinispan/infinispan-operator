#!/usr/bin/env bash

set -e -x

NAMESPACE=${1}

kubectl create ns ${NAMESPACE} || true
kubectl apply -f deploy/
