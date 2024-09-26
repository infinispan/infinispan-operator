#!/bin/bash
set -e

if [[ "$RUNNER_DEBUG" == "1" ]]; then
  set -x
fi

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
source "${SCRIPT_DIR}/operand_common.sh"

requiredEnv OPERATOR_VERSION

# Create operator version file containing all upstream-versions currently defined in INFINISPAN_OPERAND_VERSIONS
OPERANDS=$(operandJson | jq -r '.[]."upstream-version"')
cat > "${DOCS_OPERAND_DIR}/${OPERATOR_VERSION//./_}.adoc" << EOF
////
Auto-generated file, do not update this manually.
To add additional Operands to this file, update the \`INFINISPAN_OPERAND_VERSIONS\` array in \`config/manager/manager.yaml\`.
////
${OPERANDS}
EOF
