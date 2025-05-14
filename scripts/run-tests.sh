#!/usr/bin/env bash

TEST_BUNDLE=${1-main}
TIMEOUT=${2-120m}

echo "Using KUBECONFIG '${KUBECONFIG}'"
echo "Using test bundle '${TEST_BUNDLE}'"

KUBECONFIG=${KUBECONFIG-${HOME}/.kube/config}
PARALLEL_COUNT=${PARALLEL_COUNT:-1}
VERSION=$(git describe --tags --always --dirty)
GO_LDFLAGS="-X github.com/infinispan/infinispan-operator/launcher.Version=${VERSION}"

go clean -testcache
if [ -z "${TEST_NAME}" ]; then
  go test -v ./test/e2e/"${TEST_BUNDLE}" -timeout ${TIMEOUT} -ldflags "${GO_LDFLAGS}" -parallel "${PARALLEL_COUNT}" 2>&1 | tee /tmp/testOutput
  testExitCode=${PIPESTATUS[0]}
  if [ ! -z ${TEST_REPORT_DIR} ]; then
    mkdir -p ${TEST_REPORT_DIR}
    cat /tmp/testOutput | ${GO_JUNIT_REPORT} > ${TEST_REPORT_DIR}/${TEST_BUNDLE}.xml
  fi
  rm /tmp/testOutput
  exit $testExitCode
else
  echo "Running test '${TEST_NAME}'"
  go test -v ./test/e2e/"${TEST_BUNDLE}" -timeout ${TIMEOUT} -ldflags "${GO_LDFLAGS}" -run "${TEST_NAME}"
fi
