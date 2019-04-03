#!/usr/bin/env bash

set -e -x

PROFILE=${1}

minikube delete -p ${PROFILE}
