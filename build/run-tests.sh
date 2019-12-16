#!/usr/bin/env bash

KUBECONFIG=${1-openshift.local.clusterup/openshift-apiserver/admin.kubeconfig}

echo "Using KUBECONFIG '$KUBECONFIG'"

go clean -testcache ./test/e2e
go test -v ./test/e2e -timeout 30m
