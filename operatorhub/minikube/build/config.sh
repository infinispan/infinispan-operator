#!/usr/bin/env bash

set -e -x

PROFILE=${1}

minikube profile ${PROFILE}
minikube config set memory 4096
minikube config set cpus 4
minikube config set disk-size 5GB
minikube config set vm-driver virtualbox
