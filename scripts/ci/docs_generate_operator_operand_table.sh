#!/bin/bash
set -e

if [[ "$RUNNER_DEBUG" == "1" ]]; then
  set -x
fi

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
source "${SCRIPT_DIR}/operand_common.sh"

VERSIONS=$(ls "${DOCS_OPERAND_DIR}" | grep -Po '([\d+]?)_([\d+]?)_(\d+]?).adoc' | sort -rV)
FIRST=true

# Generate asciidoc table containing all Operator version files and their supported Operand versions
# The most recent operator version is shown expanded; older versions are in a collapsible section.
cat > "${DOCS_OPERAND_TABLE_FILE}" << 'EOF'
////
Auto-generated file, do not update this manually, instead update `scripts/ci/docs_generate_operator_operand_table.sh`
////
EOF

for op in ${VERSIONS}; do
  version=${op%.adoc}
  version=${version//_/.}

  if [[ "${FIRST}" == "true" ]]; then
    cat >> "${DOCS_OPERAND_TABLE_FILE}" << EOF
[%header,cols=2*]
|===
| {ispn_operator} version
| {brandname} Server versions
|
${version}
a|
include::${op}[]
|===
EOF
    FIRST=false
    # Start the collapsible section for older versions
    cat >> "${DOCS_OPERAND_TABLE_FILE}" << 'EOF'

.Older versions
[%collapsible]
====
[%header,cols=2*]
|===
| {ispn_operator} version
| {brandname} Server versions
EOF
  else
    cat >> "${DOCS_OPERAND_TABLE_FILE}" << EOF
|
${version}
a|
include::${op}[]
EOF
  fi
done

# Close the collapsible table and block
cat >> "${DOCS_OPERAND_TABLE_FILE}" << 'EOF'
|===
====
EOF
