#!/bin/bash
set -e

if [[ "$RUNNER_DEBUG" == "1" ]]; then
  set -x
fi

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
source "${SCRIPT_DIR}/operand_common.sh"

requiredEnv IMAGE VERSION

export OPERATOR_VERSION=$(cat ${SCRIPT_DIR}/../../version.txt)

${SCRIPT_DIR}/add_operand_to_csv.sh
${SCRIPT_DIR}/docs_generate_operator_operand_file.sh
${SCRIPT_DIR}/docs_generate_operator_operand_table.sh

# Only update the doc attributes if we have added the latest Operand
LATEST_OPERAND_VERSION=$(operandJson | jq -r '. | last | ."upstream-version"')
if [ "${LATEST_OPERAND_VERSION}" = "${VERSION}" ]; then
  ATTR_FILE=documentation/asciidoc/topics/attributes/community-attributes.adoc
  sed -i "s/^:server_image_version: .*$/:server_image_version: ${VERSION}/" ${ATTR_FILE}
  sed -i "s/^:operand_version: .*$/:operand_version: ${VERSION}/" ${ATTR_FILE}
fi
