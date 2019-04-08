#!/usr/bin/env bash

set -e -x

PROFILE=${1}
VMDRIVER=${2}

minikube profile ${PROFILE}
minikube config set memory 4096
minikube config set cpus 4
minikube config set disk-size 5GB

if [[ -n "${VMDRIVER}" ]]; then
    echo "Using VM driver '$VMDRIVER'"
    # Set virtual machine drivers.
    minikube config set vm-driver ${VMDRIVER}
fi
