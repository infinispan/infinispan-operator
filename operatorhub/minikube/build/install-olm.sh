#!/usr/bin/env bash

set -e -x

TMP_DIR=${1}

(
    cd ${TMP_DIR}/operator-lifecycle-manager
    kubectl apply -f deploy/upstream/manifests/latest/
)
