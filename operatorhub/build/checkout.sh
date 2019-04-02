#!/usr/bin/env bash

set -e -x

TMP_DIR=${1}
NAME=${2}
URL=${3}
REVISION=${4}

(
    cd ${TMP_DIR}
    rm -drf ${NAME}
    git clone ${URL}
    cd ${NAME}
    git checkout ${REVISION}
)
