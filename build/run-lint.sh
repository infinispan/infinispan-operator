#!/usr/bin/env bash

. $(dirname "$0")/common.sh

GOLANG_CI_LINT_VERSION=${1}

installGolangCILint() {
  printf "Installing golangci-lint ..."
  installGoBin
  gobin github.com/golangci/golangci-lint/cmd/golangci-lint@v"${GOLANG_CI_LINT_VERSION}"
  golangci-lint version
}

validateGolangCILint() {
  printf "Validating golangci-lint installation..."
  if ! [ -x "$(command -v golangci-lint)" ]; then
    printf "Not found\n"
    return 1
  else
    printf "OK\n"
    printf "Validating golangci-lint version..."
    INSTALLED_GOLANG_CI_LINT_VERSION=$(golangci-lint --version | grep -oE 'golangci-lint has version [v0-9\.]+' | grep -oE '[^v][0-9\.]+')
    if [ "${INSTALLED_GOLANG_CI_LINT_VERSION}" == "${GOLANG_CI_LINT_VERSION}" ]; then
      printf "%s\n" "${GOLANG_CI_LINT_VERSION}"
    else
      printf "incorrect version %s\n" "${INSTALLED_GOLANG_CI_LINT_VERSION}"
      return 1
    fi
  fi
}

#Ensure that GOPATH/bin is in the PATH
PATH=$(go env GOPATH)/bin:$PATH
if ! validateGolangCILint; then
  installGolangCILint
fi

golangci-lint run