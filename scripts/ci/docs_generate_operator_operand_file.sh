#!/bin/bash
set -e

if [[ "$RUNNER_DEBUG" == "1" ]]; then
  set -x
fi

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
source "${SCRIPT_DIR}/operand_common.sh"

requiredEnv OPERATOR_VERSION

# Create operator version file containing all upstream-versions currently defined in INFINISPAN_OPERAND_VERSIONS
# Versions sharing the same major.minor are grouped on a single line (e.g. "* 14.0: 1, 6, 9, 13")
OPERANDS=$(operandJson | jq -r '.[]."upstream-version"' | sort -rV)

LINES=""
prev_major=""
prev_minor=""
patches=""

flush_group() {
  if [[ -n "${prev_major}" ]]; then
    LINES+="* ${prev_major}.${prev_minor}: ${patches}"$'\n'
  fi
}

while IFS= read -r version; do
  [[ -z "${version}" ]] && continue
  major=$(echo "${version}" | cut -d. -f1)
  minor=$(echo "${version}" | cut -d. -f2)
  patch=$(echo "${version}" | cut -d. -f3)

  if [[ "${major}" == "${prev_major}" && "${minor}" == "${prev_minor}" ]]; then
    patches="${patch}, ${patches}"
  else
    flush_group
    patches="${patch}"
    prev_major="${major}"
    prev_minor="${minor}"
  fi
done <<< "${OPERANDS}"
flush_group

cat > "${DOCS_OPERAND_DIR}/${OPERATOR_VERSION//./_}.adoc" << EOF
////
Auto-generated file, do not update this manually.
To add additional Operands to this file, update the \`INFINISPAN_OPERAND_VERSIONS\` array in \`config/manager/manager.yaml\`.
////
${LINES}
EOF
