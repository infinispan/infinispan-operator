#!/usr/bin/env bash

KUBECONFIG=${1-openshift.local.clusterup/openshift-apiserver/admin.kubeconfig}

echo "Using KUBECONFIG '$KUBECONFIG'"

VERSION=$(git describe --tags --always --dirty)
GO_LDFLAGS="-X github.com/infinispan/infinispan-operator/version.Version=${VERSION}"

go clean -testcache ./test/e2e
go test -v ./test/e2e -timeout 45m -ldflags "${GO_LDFLAGS}"
