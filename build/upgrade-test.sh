#!/usr/bin/env bash

. $(dirname "$0")/common.sh

validateScripts() {
  sourceBuildFlag="${1^l}"
  buildScriptFile="${2}"
  pushScriptFile="${3}"
  runScriptFile="${4}"
  if [ "${sourceBuildFlag}" == "true" ]; then
    validateFile "${buildScriptFile}" "false" "Default operator deployment will be used"
    validateFile "${pushScriptFile}" "false" "Default operator deployment will be used"
  else
    validateFile "${runScriptFile}" "true"
  fi
}

debugInfinispanCR() {
  crName="${1^l}"
  debugFlag="${2^l}"
  if [ "${debugFlag}" == "true" ]; then
    echo "----------------------------------------------------------------------------------------------"
    oc get infinispan "${crName}" -o yaml
  fi
}

cleanupInfinispan() {
  cleanupFlag="${1^l}"
  if [ "${cleanupFlag}" == "true" ]; then
    # Cleanup Infinispan CRD's and related resources
    for crd in $(oc get crd -o name | grep infinispan); do
      oc delete "${crd}"
    done
    oc wait --for=delete pod -l app=infinispan-pod --timeout="${CLUSTER_WAIT_TIMEOUT}"
  fi
}

installOperator() {
  sourceBuildFlag="${1^l}"
  buildScriptFile="${2}"
  pushScriptFile="${3}"
  runScriptFile="${4}"
  releaseVersion="${5}"
  # If build from source flag is set and build and push scripts are present, try to build and push operator code
  if [ "${sourceBuildFlag}" == "true" ]; then
    export RELEASE_NAME="${releaseVersion}"
    "${buildScriptFile}"
    "${pushScriptFile}"
  else
    "${runScriptFile}" "${KUBECONFIG}"
  fi
}

validateEnvVar "${KUBECONFIG}" "KUBECONFIG" "false"
validateEnvVar "${OC_USER}" "OC_USER" "false"
validateEnvVar "${PROJECT_NAME}" "PROJECT_NAME" "false"
validateEnvVar "${FROM_UPGRADE_VERSION}" "FROM_UPGRADE_VERSION" "true"
validateEnvVar "${TO_UPGRADE_VERSION}" "TO_UPGRADE_VERSION" "false" "Current branch will be used"

validateOC

# Remember current working branch
CURRENT_GIT_BRANCH=$(git branch | sed -n -e 's/^\* \(.*\)/\1/p')
OC_PROJECT_NAME=$(oc project -q)
PROJECT_NAME=${PROJECT_NAME-$OC_PROJECT_NAME}
OC_USER=${OC_USER-$OC_USER_NAME}

# Project defaults
DEFAULT_BUILD_SCRIPT_FILE="./build/build.sh"
DEFAULT_PUSH_SCRIPT_FILE="./build/push-okd4.sh"
DEFAULT_RUN_SCRIPT_FILE="./build/run-okd.sh"
DEFAULT_INFINISPAN_CR_PATH="./deploy/cr/minimal/"
DEFAULT_INFINISPAN_CR_NAME="example-infinispan"
DEFAULT_SOURCE_BUILD_FLAG="false"
DEFAULT_CLUSTER_WAIT_TIMEOUT="60s"
DEFAULT_STATE_WAIT_TIMEOUT="180s"
DEFAULT_INFINISPAN_CR_STATE_FLOW=(
  "upgrade"
  "stopping"
  "wellFormed"
)

# From operator version default configuration
FROM_BUILD_SCRIPT_FILE=${FROM_BUILD_SCRIPT_FILE-$DEFAULT_BUILD_SCRIPT_FILE}
FROM_PUSH_SCRIPT_FILE=${FROM_PUSH_SCRIPT_FILE-$DEFAULT_PUSH_SCRIPT_FILE}
FROM_RUN_SCRIPT_FILE=${FROM_RUN_SCRIPT_FILE-$DEFAULT_RUN_SCRIPT_FILE}
FROM_SOURCE_BUILD_FLAG=${FROM_SOURCE_BUILD_FLAG-$DEFAULT_SOURCE_BUILD_FLAG}

# To operator version default configuration
TO_UPGRADE_VERSION=${TO_UPGRADE_VERSION-$CURRENT_GIT_BRANCH}
TO_BUILD_SCRIPT_FILE=${TO_BUILD_SCRIPT_FILE-$FROM_BUILD_SCRIPT_FILE}
TO_PUSH_SCRIPT_FILE=${TO_PUSH_SCRIPT_FILE-$FROM_PUSH_SCRIPT_FILE}
TO_RUN_SCRIPT_FILE=${TO_RUN_SCRIPT_FILE-$FROM_RUN_SCRIPT_FILE}
TO_SOURCE_BUILD_FLAG=${TO_SOURCE_BUILD_FLAG-$FROM_SOURCE_BUILD_FLAG}

# Cluster CR trace output setup
UPGRADE_TEST_CR_TRACE=${UPGRADE_TEST_CR_TRACE-false}
CLEANUP_INFINISPAN_ON_FINISH=${CLEANUP_INFINISPAN_ON_FINISH-true}

# Infinispan example CR for deploy
INFINISPAN_CR_PATH=${INFINISPAN_CR_PATH-$DEFAULT_INFINISPAN_CR_PATH}
INFINISPAN_CR_NAME=${INFINISPAN_CR_NAME-$DEFAULT_INFINISPAN_CR_NAME}
INFINISPAN_CR_STATE_FLOW=(${INFINISPAN_CR_STATE_FLOW[@]:-${DEFAULT_INFINISPAN_CR_STATE_FLOW[@]}})
CLUSTER_WAIT_TIMEOUT=${CLUSTER_WAIT_TIMEOUT-$DEFAULT_CLUSTER_WAIT_TIMEOUT}
STATE_WAIT_TIMEOUT=${STATE_WAIT_TIMEOUT-$DEFAULT_STATE_WAIT_TIMEOUT}

validateBranch "${FROM_UPGRADE_VERSION}" "source"
validateBranch "${TO_UPGRADE_VERSION}" "target"

oc new-project "${PROJECT_NAME}"

if git checkout "${FROM_UPGRADE_VERSION}"; then
  validateScripts "${FROM_SOURCE_BUILD_FLAG}" "${FROM_BUILD_SCRIPT_FILE}" "${FROM_PUSH_SCRIPT_FILE}" "${FROM_RUN_SCRIPT_FILE}"
  cleanupInfinispan "true"

  # Install operator from bundle or source code
  installOperator "${FROM_SOURCE_BUILD_FLAG}" "${FROM_BUILD_SCRIPT_FILE}" "${FROM_PUSH_SCRIPT_FILE}" "${FROM_RUN_SCRIPT_FILE}" "${FROM_UPGRADE_VERSION}"

  oc wait --for condition=ready pod -l name=infinispan-operator --timeout="${CLUSTER_WAIT_TIMEOUT}"
  echo "Operator installed"

  oc apply -f "${INFINISPAN_CR_PATH}"
  oc wait --for condition=wellFormed infinispan "${INFINISPAN_CR_NAME}" --timeout="${STATE_WAIT_TIMEOUT}"
  debugInfinispanCR "${INFINISPAN_CR_NAME}" "${UPGRADE_TEST_CR_TRACE}"
  echo "Infinispan cluster created"

  git checkout "${TO_UPGRADE_VERSION}"
  validateScripts "${TO_SOURCE_BUILD_FLAG}" "${TO_BUILD_SCRIPT_FILE}" "${TO_PUSH_SCRIPT_FILE}" "${TO_RUN_SCRIPT_FILE}"
  installOperator "${TO_SOURCE_BUILD_FLAG}" "${TO_BUILD_SCRIPT_FILE}" "${TO_PUSH_SCRIPT_FILE}" "${TO_RUN_SCRIPT_FILE}" "${TO_UPGRADE_VERSION}"

  for CR_STATE in "${INFINISPAN_CR_STATE_FLOW[@]}"; do
    debugInfinispanCR "${INFINISPAN_CR_NAME}" "${UPGRADE_TEST_CR_TRACE}"
    oc wait --for condition="${CR_STATE}" infinispan "${INFINISPAN_CR_NAME}" --timeout="${STATE_WAIT_TIMEOUT}"
  done
  echo "Operator upgraded"
  cleanupInfinispan "${CLEANUP_INFINISPAN_ON_FINISH}"

  # Restore original branch
  git checkout "${CURRENT_GIT_BRANCH}"
fi
