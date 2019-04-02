#!/usr/bin/env bash

set -e -x

TMP_DIR=${1}
SKIP_ERROR=${2-false}

(
    cd ${TMP_DIR}/operator-lifecycle-manager
    kubectl create -f deploy/upstream/manifests/latest/ || ${SKIP_ERROR}
)
