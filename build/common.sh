#!/usr/bin/env bash

installYQ() {
  YQ_MAJOR_VERSION=${YQ_VERSION:0:1}
  printf "Installing yq ..."
  GO111MODULE=off go get -u github.com/myitcv/gobin
  gobin github.com/mikefarah/yq/v"${YQ_MAJOR_VERSION}"@v"${YQ_VERSION}"
  yq --version
}

validateYQ() {
  YQ_MAJOR_VERSION=${YQ_VERSION:0:1}
  printf "Validating yq installation..."
  if ! [ -x "$(command -v yq)" ]; then
    printf "Not found\n"
    return 1
  else
    printf "OK\n"
    printf "Validating yq major version..."
    INSTALLED_YQ_MAJ_VERSION=$(yq --version | grep -oE 'yq version [0-9]+' | grep -oE '[0-9]+')
    if [ "${INSTALLED_YQ_MAJ_VERSION}" == "${YQ_MAJOR_VERSION}" ]; then
      printf "%s\n" "${YQ_MAJOR_VERSION}"
    else
      printf "incorrect major version %s\n" "${INSTALLED_YQ_MAJ_VERSION}"
      return 1
    fi
  fi
}

validateOC() {
  OC_USER_NAME=$(oc whoami 2>/dev/null)
  if [ ! $? -eq 0 ]; then
    echo "You must login with 'oc login' or set KUBECONFIG env variable"
    exit 1
  fi
}

validateROOT() {
  if ! [ $(id -u) = 0 ]; then
    echo "It's necessary to run next command with sudo!"
    if ! sudo ls >/dev/null; then
      echo "Invalid sudo credentials"
      exit 1
    fi
  fi
}

validateBranch() {
  branchName="${1}"
  destination="${2}"
  printf "Validating %s branch or tag version: %s ---> " "${destination}" "${branchName}"
  if git rev-list "${branchName}" &>/dev/null; then
    printf "OK\n"
  else
    printf "Not found\n"
    exit 1
  fi
}

validateEnvVar() {
  variableValue="${1}"
  variableName="${2}"
  exitOnNotFound="${3^l}"
  additionalMessage="${4}"
  if [ -z "${variableValue}" ]; then
    printf "Variable %s not declared\n" "${variableName}"
    if [ -n "${additionalMessage}" ]; then
      echo "${additionalMessage}"
    fi
    if [ "${exitOnNotFound}" == "true" ]; then
      exit 1
    fi
  fi
}

validateFile() {
  fileName="${1}"
  exitOnNotFound="${2^l}"
  additionalMessage="${3}"
  if ! [ -f "${fileName}" ]; then
    printf "File %s not found. " "${fileName}"
    if [ -n "${additionalMessage}" ]; then
      echo "${additionalMessage}"
    fi
    if [ "${exitOnNotFound}" == "true" ]; then
      printf "\n"
      exit 1
    fi
  fi
}
