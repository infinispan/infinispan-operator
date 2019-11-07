#!/usr/bin/env bash

NAMESPACE=${1}

echo "Using KUBECONFIG '$KUBECONFIG'"

KUBECONFIG=${2-${HOME}/.kube/config} GOCACHE=off go test -v ./test/e2e -timeout 15m
