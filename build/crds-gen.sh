#!/usr/bin/env bash

. $(dirname "$0")/common.sh

OPERATOR_SDK_VERSION=${1}
OPERATOR_SDK_BUNDLE=operator-sdk-${OPERATOR_SDK_VERSION}-x86_64-${OSTYPE}
OPERATOR_SDK_URL=https://github.com/operator-framework/operator-sdk/releases/download/${OPERATOR_SDK_VERSION}/${OPERATOR_SDK_BUNDLE}

unset GOPATH
GO111MODULE=on
# shellcheck disable=SC2155
export GOROOT=$(go env GOROOT)

installOperatorSDK() {
  validateROOT
  printf "Installing Operator SDK version %s ..." "${OPERATOR_SDK_VERSION}"
  curl -LOs "${OPERATOR_SDK_URL}"
  if grep -Fxq "Not Found" "${OPERATOR_SDK_BUNDLE}" &> /dev/null; then
    printf "Incorrect version\n"
    rm -rf "${OPERATOR_SDK_BUNDLE}"
    exit 1
  fi
  chmod +x "${OPERATOR_SDK_BUNDLE}" && sudo mkdir -p /usr/local/bin/ && sudo cp "${OPERATOR_SDK_BUNDLE}" /usr/local/bin/operator-sdk && rm "${OPERATOR_SDK_BUNDLE}"
  printf "Installed\n"
}

printf "Validating Operator SDK installation..."

if ! [ -x "$(command -v operator-sdk)" ]; then
  printf "Not found\n"
  installOperatorSDK
else
  printf "OK\n"
  printf "Validating Operator SDK version..."
  INSTALLED_OPERATOR_SDK_VERSION=$(operator-sdk version | grep -oP 'operator-sdk version: "\K[^"]+')
  if [ "${INSTALLED_OPERATOR_SDK_VERSION}" == "${OPERATOR_SDK_VERSION}" ]; then
    printf "%s\n" "${OPERATOR_SDK_VERSION}"
  else
    printf "Incorrect version %s\n" "${INSTALLED_OPERATOR_SDK_VERSION}"
    installOperatorSDK
  fi
fi

echo "Generating CRDs for API's..."

if operator-sdk generate crds; then
  sed -i -e "/name: infinispans.infinispan.org/a \  labels:\n    name: infinispan-operator" deploy/crds/infinispan.org_infinispans_crd.yaml
  sed -i -e "/name: caches.infinispan.org/a \  labels:\n    name: infinispan-operator" deploy/crds/infinispan.org_caches_crd.yaml

  echo "Generating Kubernetes code for custom resources..."
  operator-sdk generate k8s
fi
