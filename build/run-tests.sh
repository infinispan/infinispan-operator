#!/usr/bin/env bash

KUBECONFIG=${1-openshift.local.clusterup/openshift-apiserver/admin.kubeconfig}
TEST_BUNDLE=${2-main}

echo "Using KUBECONFIG '${KUBECONFIG}'"
echo "Using test bundle '${TEST_BUNDLE}'"

PARALLEL_COUNT=${PARALLEL_COUNT:-1}
VERSION=$(git describe --tags --always --dirty)
GO_LDFLAGS="-X github.com/infinispan/infinispan-operator/version.Version=${VERSION}"

go clean -testcache ./test/e2e
if [ -z "${TEST_NAME}" ]; then
  go test -v ./test/e2e/"${TEST_BUNDLE}" -timeout 45m -ldflags "${GO_LDFLAGS}" -parallel "${PARALLEL_COUNT}"
else
  echo "Running test '${TEST_NAME}'"
  go test -v ./test/e2e/"${TEST_BUNDLE}" -timeout 45m -ldflags "${GO_LDFLAGS}" -run "${TEST_NAME}"
fi
