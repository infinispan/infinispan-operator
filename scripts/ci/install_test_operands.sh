#!/bin/bash

set -e
if [[ "$RUNNER_DEBUG" == "1" ]]; then
  set -x
fi

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
source "${SCRIPT_DIR}/operand_common.sh"

current_versions=$(operandJson)

# Test the latest release from each previous stable stream in order to reduce resources and time required by CI
test_operands=$(echo "${current_versions}" | jq '
 # 1. Group by Major.Minor
 group_by(.["upstream-version"] | sub("^(?<a>\\d+\\.\\d+).*"; .a)) as $groups

 # 2. Identify the "Oldest of the Latest Stream"
 # We take the last group, then the first element of that group
 | ($groups | last | first | .["upstream-version"]) as $oldestLatest

 # 3. Process the groups
 | $groups
 | map(
     . as $group
     | .[]
     # Test both the oldest and the latest patch release in the latest major.minor stream
     | select(. == ($group | last) or .["upstream-version"] == $oldestLatest)
   )
 | unique_by(.["upstream-version"])
')

# Explicitly include 14.0.24 so that we can continue to test upgrades from 2.3.x CSV bundles
test_operands=$(echo "${test_operands}" | jq '[{"upstream-version": "14.0.24", "image": "quay.io/infinispan/server:14.0.24.Final"}] + .')

# Append Dev version only if there's no release in the stream yet
if [ $(echo ${test_operands} | jq 'map(select(.["upstream-version"] | startswith("16."))) | length') -eq 0 ]; then
  test_operands=$(echo "${test_operands}" | jq '.[length] |= . + {"upstream-version": "16.0.0", "image": "quay.io/infinispan/server:16.0"}')
fi

# Append latest snapshot
test_operands=$(echo "${test_operands}" | jq '.[length] |= . + {"upstream-version": "16.0.99", "image": "quay.io/infinispan-test/server:main"}')

operands=${test_operands} yq -i '(select(document_index == 1) | .spec.template.spec.containers[0].env[] | select(.name == "INFINISPAN_OPERAND_VERSIONS")).value = strenv(operands)' ${DEPLOYMENT_FILE}
echo "Testing Operands:"
operandJson