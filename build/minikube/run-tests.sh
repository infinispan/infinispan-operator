#!/usr/bin/env bash

NAMESPACE=${1}

echo "Using KUBECONFIG '$KUBECONFIG'"

go clean -testcache
KUBECONFIG=${2-${HOME}/.kube/config} go test -v ./test/e2e -timeout 30m
