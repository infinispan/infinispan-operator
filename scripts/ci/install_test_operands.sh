#!/bin/bash

set -e
if [[ "$RUNNER_DEBUG" == "1" ]]; then
  set -x
fi

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
source "${SCRIPT_DIR}/operand_common.sh"

current_versions=$(operandJson)

# Test the latest release from each previous stable streams in order to reduce resources and time required by CI
# We explicitly include 14.0.24 so that we can continue to test upgrades from 2.3.x CSV bundles
test_operands=$(echo "${current_versions}" | jq '
[
  {
    "upstream-version": "14.0.24",
    "image": "quay.io/infinispan/server:14.0.24.Final"
  },
  (map(select(.["upstream-version"] | startswith("14.0."))) | sort_by(.["upstream-version"] | split(".") | map(tonumber)) | .[-1]),
  (map(select(.["upstream-version"] | startswith("15.0."))) | sort_by(.["upstream-version"] | split(".") | map(tonumber)) | .[-1]),
  (map(select(.["upstream-version"] | startswith("15.1."))) | sort_by(.["upstream-version"] | split(".") | map(tonumber)) | .[-1]),
  (map(select(.["upstream-version"] | startswith("15.2."))) | sort_by(.["upstream-version"] | split(".") | map(tonumber)) | .[-1]),
  (map(select(.["upstream-version"] | startswith("16.")))   | sort_by(.["upstream-version"] | split(".") | map(tonumber)) | .[])
] | map(select(. != null))
')
operands=${test_operands} yq -i '(select(document_index == 1) | .spec.template.spec.containers[0].env[] | select(.name == "INFINISPAN_OPERAND_VERSIONS")).value = strenv(operands)' ${DEPLOYMENT_FILE}
echo "Testing Operands:"
operandJson