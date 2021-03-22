#!/usr/bin/env bash

. $(dirname "$0")/common.sh

OPERATOR_SDK_VERSION=${1}
YQ_VERSION=${2}

ARC=$(echo "${MACHTYPE}" | grep -oE '^[a-z0-9_]+')
OS=$(echo "${OSTYPE}" | sed 's/darwin/apple-darwin/g' | grep -oE '[a-z-]+')
OPERATOR_SDK_BUNDLE=operator-sdk-${OPERATOR_SDK_VERSION}-${ARC}-${OS}
OPERATOR_SDK_URL=https://github.com/operator-framework/operator-sdk/releases/download/${OPERATOR_SDK_VERSION}/${OPERATOR_SDK_BUNDLE}

GO111MODULE=on
# shellcheck disable=SC2155
export GOROOT=$(go env GOROOT)
OPERATOR_SDK_INSTALL_PATH=$(go env GOPATH)/bin

installOperatorSDK() {
  printf "Installing Operator SDK version %s ..." "${OPERATOR_SDK_VERSION}"
  if ! curl -LOsf "${OPERATOR_SDK_URL}"; then
    printf "Incorrect version\n"
    exit 1
  fi

  chmod +x "${OPERATOR_SDK_BUNDLE}" && mv "${OPERATOR_SDK_BUNDLE}" "${OPERATOR_SDK_INSTALL_PATH}"/operator-sdk
  printf "Installed\n"
}

validateOperatorSDK() {
  printf "Validating Operator SDK installation..."

  if ! [ -x "$(command -v operator-sdk)" ]; then
    printf "Not found\n"
    installOperatorSDK
  else
    printf "OK\n"
    printf "Validating Operator SDK version..."
    INSTALLED_OPERATOR_SDK_VERSION=$(operator-sdk version | grep -oE 'operator-sdk version: "[v0-9\.]+"' | grep -oE '[^"][0-9\.]+')
    if [ "${INSTALLED_OPERATOR_SDK_VERSION}" == "${OPERATOR_SDK_VERSION}" ]; then
      printf "%s\n" "${OPERATOR_SDK_VERSION}"
    else
      printf "Incorrect version %s\n" "${INSTALLED_OPERATOR_SDK_VERSION}"
      installOperatorSDK
    fi
  fi
}

if ! validateYQ; then
  printf "Valid yq not found in PATH. Trying adding GOPATH/bin\n"
  PATH=$(go env GOPATH)/bin:$PATH
  if ! validateYQ; then
    installYQ
  fi
fi

if ! validateOperatorSDK; then
  printf "Valid Operator SDK not found in PATH. Trying adding GOPATH/bin\n"
  PATH=$(go env GOPATH)/bin:$PATH
  if ! validateOperatorSDK; then
    installOperatorSDK
  fi
fi

echo "Generating CRDs for API's..."
if operator-sdk generate crds; then
  for CRD_FILE in deploy/crds/infinispan.org_*_crd.yaml; do
    yq ea -ie '.metadata.labels={"name":"infinispan-operator"}' "${CRD_FILE}"
  done

  echo "Generating Kubernetes code for custom resources..."
  operator-sdk generate k8s
fi
