#!/usr/bin/env bash

OPERATOR_SDK_VERSION=${1}
OPERATOR_SDK_BUNDLE=operator-sdk-${OPERATOR_SDK_VERSION}-x86_64-${OSTYPE}
OPERATOR_SDK_URL=https://github.com/operator-framework/operator-sdk/releases/download/${OPERATOR_SDK_VERSION}/${OPERATOR_SDK_BUNDLE}

unset GOPATH
GO111MODULE=on
# shellcheck disable=SC2155
export GOROOT=$(go env GOROOT)

validateROOT() {
  if ! [ $(id -u) = 0 ]; then
    echo "It's necessary to run next command with sudo!"
    if ! sudo ls > /dev/null; then
      echo "Invalid sudo credentials"
      exit 1
    fi
  fi
}

installOperatorSDK() {
  validateROOT
  printf "Installing Operator SDK version %s ..." "${OPERATOR_SDK_VERSION}"
  curl -LOs "${OPERATOR_SDK_URL}"
  if grep -Fxq "Not Found" "${OPERATOR_SDK_BUNDLE}" &> /dev/null; then
    printf "Incorrect version\n"
    rm -rf "${OPERATOR_SDK_BUNDLE}"
    exit 1
  fi
  chmod +x operator-sdk-"${OPERATOR_SDK_VERSION}"-x86_64-linux-gnu && sudo mkdir -p /usr/local/bin/ && sudo cp "${OPERATOR_SDK_BUNDLE}" /usr/local/bin/operator-sdk && rm "${OPERATOR_SDK_BUNDLE}"
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
operator-sdk generate crds

echo "Generating Kubernetes code for custom resource..."
operator-sdk generate k8s
