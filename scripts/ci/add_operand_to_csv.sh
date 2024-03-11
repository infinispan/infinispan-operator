#!/bin/bash
set -e

if [[ "$RUNNER_DEBUG" == "1" ]]; then
  set -x
fi

IMAGE=$1
DEPLOYMENT_FILE=config/manager/manager.yaml
EXISTING_OPERANDS=$(yq 'select(document_index == 1) | .spec.template.spec.containers[0].env[] | select(.name == "INFINISPAN_OPERAND_VERSIONS").value' ${DEPLOYMENT_FILE})
LATEST_OPERAND_VERSION=$(echo ${EXISTING_OPERANDS} | jq -r '. | last | ."upstream-version"')
NEW_OPERAND_VERSION=$(echo ${LATEST_OPERAND_VERSION} | awk -F. -v OFS=. '{$NF += 1 ; print}')
NEW_OPERANDS=$(echo ${EXISTING_OPERANDS} | jq '.[. | length] |= . + {"upstream-version":"'${NEW_OPERAND_VERSION}'", "image":"'${IMAGE}'"}')

operands=${NEW_OPERANDS} yq -i '(select(document_index == 1) | .spec.template.spec.containers[0].env[] | select(.name == "INFINISPAN_OPERAND_VERSIONS")).value = strenv(operands)' ${DEPLOYMENT_FILE}
