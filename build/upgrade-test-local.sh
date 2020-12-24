#!/usr/bin/env bash

. $(dirname "$0")/common.sh

restoreCurrentBranch() {
  git checkout "${CURRENT_GIT_BRANCH}"
}

validateEnvVar "${KUBECONFIG}" "KUBECONFIG" "false"
validateEnvVar "${FROM_UPGRADE_VERSION}" "FROM_UPGRADE_VERSION" "true"
validateEnvVar "${TO_UPGRADE_VERSION}" "TO_UPGRADE_VERSION" "false" "Current branch will be used"

# Remember current working branch
CURRENT_GIT_BRANCH=$(git branch | sed -n -e 's/^\* \(.*\)/\1/p')

# Project defaults
DEFAULT_TEST_SCRIPT_FILE="./build/run-tests.sh"
DEFAULT_OPERATOR_DEPLOY_FILE="./deploy/operator.yaml"
OPERATOR_TEST_NAME="TestOperatorUpgrade"

# From operator version default configuration
FROM_TEST_SCRIPT_FILE=${FROM_TEST_SCRIPT_FILE-$DEFAULT_TEST_SCRIPT_FILE}
FROM_OPERATOR_DEPLOY_FILE=${FROM_OPERATOR_DEPLOY_FILE-$DEFAULT_OPERATOR_DEPLOY_FILE}
# FROM_OPERATOR_IMAGE can be customized but doesn't have default value

# To operator version default configuration
TO_UPGRADE_VERSION=${TO_UPGRADE_VERSION-$CURRENT_GIT_BRANCH}
TO_TEST_SCRIPT_FILE=${TO_TEST_SCRIPT_FILE-$FROM_TEST_SCRIPT_FILE}
TO_OPERATOR_DEPLOY_FILE=${TO_OPERATOR_DEPLOY_FILE-$FROM_OPERATOR_DEPLOY_FILE}
# TO_OPERATOR_IMAGE can be customized but doesn't have default value

validateBranch "${FROM_UPGRADE_VERSION}" "source"
validateBranch "${TO_UPGRADE_VERSION}" "target"

if git checkout "${FROM_UPGRADE_VERSION}"; then
  validateFile "${FROM_TEST_SCRIPT_FILE}" "true"
  validateFile "${FROM_OPERATOR_DEPLOY_FILE}" "true"
  export TESTING_NAMESPACE="${PROJECT_NAME}"
  export OPERATOR_UPGRADE_STAGE="FROM"
  export TEST_NAME="${OPERATOR_TEST_NAME}"
  unset IMAGE
  if [ -z "${FROM_OPERATOR_IMAGE}" ]; then
    export DEFAULT_IMAGE=$(sed -n '/name: DEFAULT_IMAGE/ {n;p}' "${FROM_OPERATOR_DEPLOY_FILE}" | awk -F '"' '{print $2}')
  else
    export DEFAULT_IMAGE="${FROM_OPERATOR_IMAGE}"
  fi
  if ! "${FROM_TEST_SCRIPT_FILE}" "${KUBECONFIG}"; then
    restoreCurrentBranch
    exit 1
  fi

  git checkout "${TO_UPGRADE_VERSION}"
  validateFile "${TO_TEST_SCRIPT_FILE}" "true"
  export OPERATOR_UPGRADE_STAGE="TO"
  if [ -z "${TO_OPERATOR_IMAGE}" ]; then
    export DEFAULT_IMAGE=$(sed -n '/name: DEFAULT_IMAGE/ {n;p}' "${TO_OPERATOR_DEPLOY_FILE}" | awk -F '"' '{print $2}')
  else
    export DEFAULT_IMAGE="${TO_OPERATOR_IMAGE}"
  fi

  if ! "${TO_TEST_SCRIPT_FILE}" "${KUBECONFIG}"; then
    restoreCurrentBranch
    exit 1
  fi

  # Restore env variables and original branch
  unset DEFAULT_IMAGE
  unset OPERATOR_UPGRADE_STAGE
  restoreCurrentBranch
fi
