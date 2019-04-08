#!/usr/bin/env bash

set -e -x

PROFILE=${1}
VMDRIVER=${2}

minikube profile ${PROFILE}
minikube config set memory 2048
minikube config set cpus 2
minikube config set disk-size 10GB

if [[ -n "${VMDRIVER}" ]]; then
    echo "Using VM driver '$VMDRIVER'"
    # Set virtual machine drivers.
    minikube config set vm-driver ${VMDRIVER}
fi
