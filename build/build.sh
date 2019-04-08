#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

GOOS=${1-linux}
BIN_DIR=${2-$(pwd)/build/_output/bin}
mkdir -p ${BIN_DIR}
PROJECT_NAME="infinispan-operator"
REPO_PATH="github.com/infinispan/infinispan-operator"
BUILD_PATH="${REPO_PATH}/cmd/manager"
VERSION="$(git describe --tags --always --dirty)"
GO_LDFLAGS="-X ${REPO_PATH}/version.Version=${VERSION}"
echo "building ${PROJECT_NAME}..."
GOOS=${GOOS} GOARCH=amd64 CGO_ENABLED=0 go build -o ${BIN_DIR}/${PROJECT_NAME} -ldflags "${GO_LDFLAGS}" $BUILD_PATH
