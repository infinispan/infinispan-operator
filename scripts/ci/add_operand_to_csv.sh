#!/bin/bash
set -e

if [[ "$RUNNER_DEBUG" == "1" ]]; then
  set -x
fi

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
source "${SCRIPT_DIR}/operand_common.sh"

requiredEnv IMAGE

JSON=$(operandJson)
LATEST_OPERAND_VERSION=$(echo "${JSON}" | jq -r '. | last | ."upstream-version"')

if [[ -z "${VERSION}" ]]; then
  VERSION=$(echo "${LATEST_OPERAND_VERSION}" | awk -F. -v OFS=. '{$NF += 1 ; print}')
fi

VERSION_EXISTS=$(echo "${JSON}" | jq "any(.[]; .\"upstream-version\" == \"${VERSION}\")")
if [[ "${VERSION_EXISTS}" == "true" ]]; then
  NEW_OPERANDS=$(echo "${JSON}" | jq "(.[] | select(.\"upstream-version\" == \"${VERSION}\").image) |= \"${IMAGE}\"")
else
  NEW_OPERANDS=$(echo "${JSON}" | jq -r '.[. | length] |= . + {"upstream-version":"'${VERSION}'", "image":"'${IMAGE}'"} | sort_by(."upstream-version" | split(".") | map(tonumber))')
fi

operands=${NEW_OPERANDS} yq -i '(select(document_index == 1) | .spec.template.spec.containers[0].env[] | select(.name == "INFINISPAN_OPERAND_VERSIONS")).value = strenv(operands)' ${DEPLOYMENT_FILE}
