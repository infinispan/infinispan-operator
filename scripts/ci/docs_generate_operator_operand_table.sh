#!/bin/bash
set -e

if [[ "$RUNNER_DEBUG" == "1" ]]; then
  set -x
fi

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
source "${SCRIPT_DIR}/operand_common.sh"

# Generate asciidoc table containing all Operator version files and their supported Operand versions
cat > "${DOCS_OPERAND_TABLE_FILE}" << EOF
////
Auto-generated file, do not update this manually, instead update \`scripts/ci/docs_generate_operator_operand_table.sh\`
////
[%header,cols=2*]
|===
| {ispn_operator} version
| {brandname} Server versions
EOF

for op in $(ls "${DOCS_OPERAND_DIR}" | grep -Po '([\d+]?)_([\d+]?)_(\d+]?).adoc'); do
  version=${op%.adoc}
  version=${version//_/.}
  cat >> "${DOCS_OPERAND_TABLE_FILE}" << EOF
|
${version}
|
include::${op}[]
EOF
done

echo "|===" >> "${DOCS_OPERAND_TABLE_FILE}"
