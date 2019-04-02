#!/usr/bin/env bash

set -e -x

TMP_DIR=${1}
SKIP_ERROR=${2-false}

(
    cd ${TMP_DIR}/operator-marketplace

    # Set --validate=false to workaround:
    # https://github.com/operator-framework/operator-marketplace/issues/142
    kubectl apply -f deploy/upstream --validate=false || ${SKIP_ERROR}
)
