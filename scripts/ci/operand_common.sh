#!/bin/bash
set -e

if [[ "$RUNNER_DEBUG" == "1" ]]; then
  set -x
fi

export DEPLOYMENT_FILE=config/manager/manager.yaml
export DOCS_OPERAND_DIR=documentation/asciidoc/topics/supported_operands
export DOCS_OPERAND_TABLE_FILE=${DOCS_OPERAND_DIR}/operand_table.adoc

function operandJson() {
    yq 'select(document_index == 1) | .spec.template.spec.containers[0].env[] | select(.name == "INFINISPAN_OPERAND_VERSIONS").value' ${DEPLOYMENT_FILE}
}

function requiredEnv() {
  for ENV in $@; do
      if [ -z "${!ENV}" ]; then
        echo "${ENV} variable must be set"
        exit 1
      fi
  done
}

if [[ -z "${SERVER_IMAGES}" ]]; then
  SERVER_IMAGES=$(operandJson | jq -r '.[].image')
fi
