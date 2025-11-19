#!/bin/bash
set -e

if [[ "$RUNNER_DEBUG" == "1" ]]; then
  set -x
fi

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
source "${SCRIPT_DIR}/operand_common.sh"

# Remove defined operands from the local registry to free up space on the CI
for img in ${SERVER_IMAGES}; do
  docker rmi "${img}" || true
done